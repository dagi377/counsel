package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/app/legal/matter/matterkind"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/ids"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// CapAsk lets a member start an agent run and read the runs they started (spec/legal/access.json).
const CapAsk = "agent.ask"

// Problem codes the agent run routes write.
const (
	CodeUnauthenticated   = "unauthenticated"
	CodeMissingCapability = "missing_capability"
	CodeNotFound          = "not_found"
	CodeUnavailable       = "unavailable"
	// CodeUnknownSkill: the skill id names no published skill (proposed).
	CodeUnknownSkill = "unknown_skill"
)

// Sentinel errors of the run port.
var (
	// ErrUnknownSkill: no published skill has this id.
	ErrUnknownSkill = errors.New("agent: unknown skill")
	// ErrRunNotFound: no run has this id.
	ErrRunNotFound = errors.New("agent: run not found")
)

// Run states as the API names them.
const (
	RunQueued    = "queued"
	RunRunning   = "running"
	RunWaiting   = "waiting"
	RunSucceeded = "succeeded"
	RunFailed    = "failed"
	RunCancelled = "cancelled"
)

// AgentRun is one agent run as the routes show it. Grant is the run principal's narrowed
// capability set.
type AgentRun struct {
	ID        string
	SkillID   string
	ActingFor string
	MatterID  string
	State     string
	Grant     []string
	CreatedAt time.Time
}

// StartRun is what the route hands the run port: a new run id, the skill, its structured
// inputs and the run principal (Access.AgentPrincipal).
type StartRun struct {
	ID        string
	SkillID   string
	Inputs    json.RawMessage
	MatterID  string
	Principal registry.Principal
}

// RunService is the narrow run port the routes use (agent.Service has no start or query):
// Paploo starts the run; the store records it.
type RunService interface {
	// SkillGrant returns the capabilities the skill asks for, or ErrUnknownSkill.
	SkillGrant(ctx context.Context, skillID string) ([]string, error)
	// Start records and starts the run as p.Principal.
	Start(ctx context.Context, s StartRun) (AgentRun, error)
	// Run returns the run, or ErrRunNotFound.
	Run(ctx context.Context, id string) (AgentRun, error)
}

// RunAccess is the part of identity.Access the routes use.
type RunAccess interface {
	Can(ctx context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error)
	AgentPrincipal(ctx context.Context, run identity.RunID, actingFor identity.UserID, grant []string) (registry.Principal, error)
}

// RunHTTP serves POST /agent/runs and GET /agent/runs/{run_id}. Every dependency is
// required; a route answers 503 and calls nothing while one is missing.
type RunHTTP struct {
	Runs     RunService
	Access   RunAccess
	Idem     idem.Store
	Problems server.ProblemWriter
	// Principal returns who the request acts as; false means unauthenticated.
	Principal func(r *http.Request) (registry.Principal, bool)
	// NewID returns a new run id; nil means ids.New.
	NewID func() string
}

var (
	runIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	skillPattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)
)

// maxInputBytes bounds a run's structured inputs.
const maxInputBytes = 64 << 10

// Routes mounts the agent run routes.
func (h *RunHTTP) Routes(r chi.Router) {
	// rule: ARC-I-012
	// Starting a run writes no gate-owned state; the run's own writes go through gates.
	r.Method(http.MethodPost, "/agent/runs", server.Declare(h.start, "agent_run.created"))
	r.Get("/agent/runs/{run_id}", h.get)
}

func (h *RunHTTP) ready() bool {
	return h.Runs != nil && h.Access != nil && h.Idem != nil && h.Problems != nil && h.Principal != nil
}

func (h *RunHTTP) problems() server.ProblemWriter {
	if h.Problems == nil {
		return &server.Problems{}
	}
	return h.Problems
}

func (h *RunHTTP) fail(w http.ResponseWriter, r *http.Request, status int, code, title string) {
	h.problems().WriteProblem(w, r, server.NewProblem(status, code, title))
}

// writeErr maps a port error to a problem. Error text never reaches the body.
func (h *RunHTTP) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrRunNotFound), errors.Is(err, identity.ErrNotVisible):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such run")
	case errors.Is(err, ErrUnknownSkill):
		h.fail(w, r, http.StatusUnprocessableEntity, CodeUnknownSkill, "No such skill")
	default:
		h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
	}
}

// authorize answers 503, 401, 403 or 404 itself; it runs before any port call.
func (h *RunHTTP) authorize(w http.ResponseWriter, r *http.Request) (registry.Principal, bool) {
	if !h.ready() {
		h.fail(w, r, http.StatusServiceUnavailable, CodeUnavailable, "Service not configured")
		return registry.Principal{}, false
	}
	p, ok := h.Principal(r)
	if !ok {
		h.fail(w, r, http.StatusUnauthorized, CodeUnauthenticated, "Authentication required")
		return registry.Principal{}, false
	}
	if err := h.can(r.Context(), p, registry.Subject{}); err != nil {
		h.denied(w, r, err)
		return registry.Principal{}, false
	}
	return p, true
}

// errMissingCapability marks a fail-closed refusal (403).
var errMissingCapability = errors.New("agent: missing capability")

// can checks agent.ask on s, failing closed.
func (h *RunHTTP) can(ctx context.Context, p registry.Principal, s registry.Subject) error {
	// rule: ARC-I-051
	allowed, err := h.Access.Can(ctx, p, CapAsk, s)
	switch {
	case err != nil:
		return err
	case !allowed:
		return errMissingCapability
	}
	return nil
}

func (h *RunHTTP) denied(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	// rule: ARC-I-042
	case errors.Is(err, identity.ErrNotVisible):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "Not found")
	case errors.Is(err, errMissingCapability):
		h.fail(w, r, http.StatusForbidden, CodeMissingCapability, "Missing capability")
	default:
		h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
	}
}

type runView struct {
	ID        string   `json:"id"`
	SkillID   string   `json:"skill_id"`
	ActingFor string   `json:"acting_for"`
	MatterID  string   `json:"matter_id,omitempty"`
	State     string   `json:"state"`
	Grant     []string `json:"grant"`
	CreatedAt string   `json:"created_at"`
}

func runViewOf(a AgentRun) runView {
	g := a.Grant
	if g == nil {
		g = []string{}
	}
	return runView{ID: a.ID, SkillID: a.SkillID, ActingFor: a.ActingFor, MatterID: a.MatterID, State: a.State,
		Grant: g, CreatedAt: a.CreatedAt.UTC().Format(time.RFC3339)}
}

// callerOf is the member a request acts for: the user, or the user an agent acts for.
func callerOf(p registry.Principal) string {
	if p.Kind == registry.PrincipalAgent {
		return p.ActingFor
	}
	return p.UserID
}

type startBody struct {
	SkillID  string          `json:"skill_id"`
	Inputs   json.RawMessage `json:"inputs,omitempty"`
	MatterID string          `json:"matter_id,omitempty"`
}

func (b startBody) valid() bool {
	if !skillPattern.MatchString(b.SkillID) || (b.MatterID != "" && !runIDPattern.MatchString(b.MatterID)) ||
		len(b.Inputs) > maxInputBytes {
		return false
	}
	if len(b.Inputs) == 0 {
		return true
	}
	var obj map[string]any
	return json.Unmarshal(b.Inputs, &obj) == nil && obj != nil
}

// start serves POST /agent/runs: it starts a run as the calling member, holding only the
// capabilities both the member and the skill's grant hold.
func (h *RunHTTP) start(w http.ResponseWriter, r *http.Request) {
	p, ok := h.authorize(w, r)
	if !ok {
		return
	}
	// rule: ARC-I-007
	// The key is required before any port call.
	if strings.TrimSpace(r.Header.Get(idem.HeaderName)) == "" {
		h.fail(w, r, http.StatusBadRequest, idem.CodeKeyRequired, "Idempotency-Key header is required")
		return
	}
	// rule: ARC-I-008
	idem.New(h.Idem, func(*http.Request) string { return callerOf(p) }, idem.DefaultTTL)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.startRun(w, r, p) }),
	).ServeHTTP(w, r)
}

func (h *RunHTTP) startRun(w http.ResponseWriter, r *http.Request, p registry.Principal) {
	var b startBody
	if err := decodeStrict(r, &b); err != nil || !b.valid() {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
		return
	}
	ctx := r.Context()
	if b.MatterID != "" {
		if err := h.can(ctx, p, registry.Subject{Kind: matterkind.Subject, ID: strings.ToLower(b.MatterID)}); err != nil {
			h.denied(w, r, err)
			return
		}
	}
	grant, err := h.Runs.SkillGrant(ctx, b.SkillID)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	id := h.newID()
	// rule: ARC-I-036
	// The run principal holds only the intersection of the caller's capabilities and the grant.
	rp, err := h.Access.AgentPrincipal(ctx, identity.RunID(id), identity.UserID(callerOf(p)), grant)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	run, err := h.Runs.Start(ctx, StartRun{ID: id, SkillID: b.SkillID, Inputs: b.Inputs,
		MatterID: strings.ToLower(b.MatterID), Principal: rp})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	writeRunJSON(w, http.StatusAccepted, runViewOf(run))
}

// get serves GET /agent/runs/{run_id}. A run another member started, or one on a matter hidden
// by a wall, answers 404.
func (h *RunHTTP) get(w http.ResponseWriter, r *http.Request) {
	p, ok := h.authorize(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "run_id")
	if !runIDPattern.MatchString(id) {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "run_id is not valid")
		return
	}
	ctx := r.Context()
	run, err := h.Runs.Run(ctx, strings.ToLower(id))
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	// rule: ARC-I-042
	if run.ActingFor != callerOf(p) {
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such run")
		return
	}
	if run.MatterID != "" {
		if err := h.can(ctx, p, registry.Subject{Kind: matterkind.Subject, ID: run.MatterID}); err != nil {
			h.denied(w, r, err)
			return
		}
	}
	writeRunJSON(w, http.StatusOK, runViewOf(run))
}

func (h *RunHTTP) newID() string {
	if h.NewID != nil {
		return h.NewID()
	}
	return string(ids.New())
}

func writeRunJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func decodeStrict(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing data")
	}
	return nil
}
