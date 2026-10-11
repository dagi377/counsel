package trigger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// ActionPath is the screen-action route (Q-FEAT-CORE-12 in docs/QUESTIONS.md).
const ActionPath = "/api/agent/actions/{feature_id}"

// maxRecords bounds the record ids one action may name.
const maxRecords = 100

// Screen is what a screen's selected records are and the capability the acting user needs
// to read them.
type Screen struct {
	SubjectKind string // registry subject kind of the screen's records, "matter"
	Capability  string // capability the acting user must hold on every record
}

// Access is the part of identity.Access the route uses.
type Access interface {
	Can(ctx context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error)
	AgentPrincipal(ctx context.Context, run identity.RunID, actingFor identity.UserID, grant []string) (registry.Principal, error)
}

// ActionQueue enqueues a write feature's run for later and returns its run id. The job it
// inserts holds ids only (AI-R-005).
type ActionQueue interface {
	EnqueueAction(ctx context.Context, featureID string, subjects []SubjectRef, actingFor string) (runID string, err error)
}

// Actions serves POST /api/agent/actions/{feature_id}. A chat feature runs inline and its
// checked answer goes back in the response only; any other feature is queued and answers
// 202 with its run id.
type Actions struct {
	Features  *feature.Set
	Screens   map[string]Screen // by screen id, "matter-detail"
	Access    Access
	Runner    run.Runner
	Queue     ActionQueue
	NewRunID  func() string
	Principal func(r *http.Request) (registry.Principal, bool)
	Problems  server.ProblemWriter
}

// Routes mounts the route.
func (a *Actions) Routes(r chi.Router) {
	r.Post(ActionPath, a.handle)
}

type actionRequest struct {
	Screen    string   `json:"screen"`
	RecordIDs []string `json:"record_ids"`
}

type citationJSON struct {
	VersionID string `json:"version_id"`
	Page      int    `json:"page"`
	FromLine  int    `json:"from_line"`
	ToLine    int    `json:"to_line"`
}

func (a *Actions) handle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, ok := a.Principal(r)
	if !ok {
		a.Problems.WriteProblem(w, r, server.NewProblem(http.StatusUnauthorized, "unauthenticated", "Authentication required"))
		return
	}
	// rule: ARC-I-036
	// Only a person starts a screen action; an agent run never starts another.
	if user.Kind != registry.PrincipalUser || user.UserID == "" {
		a.Problems.WriteProblem(w, r, server.NewProblem(http.StatusForbidden, "forbidden", "Only a user can start a screen action"))
		return
	}
	d, ok := a.Features.ByID(chi.URLParam(r, "feature_id"))
	if !ok {
		a.Problems.WriteProblem(w, r, server.NewProblem(http.StatusNotFound, "not_found", "Feature not found"))
		return
	}
	var req actionRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		a.Problems.WriteProblem(w, r, server.NewProblem(http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid"))
		return
	}
	scr, ok := a.Screens[req.Screen]
	if !ok || !onScreen(d, req.Screen) || len(req.RecordIDs) == 0 || len(req.RecordIDs) > maxRecords || slices.Contains(req.RecordIDs, "") {
		a.Problems.WriteProblem(w, r, server.NewProblem(http.StatusUnprocessableEntity, server.CodeValidationFailed, "Screen or record ids not valid for this feature"))
		return
	}
	subjects := make([]SubjectRef, 0, len(req.RecordIDs))
	for _, id := range req.RecordIDs {
		subjects = append(subjects, SubjectRef{Kind: scr.SubjectKind, ID: id})
	}
	if p := a.authorize(ctx, user, d, scr, subjects); p != nil {
		a.Problems.WriteProblem(w, r, *p)
		return
	}
	if d.Kind == ai.KindChat {
		a.chat(w, r, user, d, req.Screen, subjects)
		return
	}
	runID, err := a.Queue.EnqueueAction(ctx, d.ID, subjects, user.UserID)
	if err != nil {
		slog.ErrorContext(ctx, "screen action enqueue failed", "feature", d.ID, "error_type", fmt.Sprintf("%T", err))
		a.Problems.WriteProblem(w, r, server.NewProblem(http.StatusInternalServerError, server.CodeInternal, "Internal error"))
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"run_id": runID})
}

// authorize checks the acting user on every record: the screen's capability and each
// capability the feature grants its run. A walled record is 404 so its existence does not
// leak; a missing capability is 403.
func (a *Actions) authorize(ctx context.Context, user registry.Principal, d feature.Def, scr Screen, subjects []SubjectRef) *api.Problem {
	caps := append([]string{scr.Capability}, d.Grant...)
	for _, s := range subjects {
		for _, c := range caps {
			if c == "" {
				continue
			}
			// rule: ARC-I-036
			// Walls of the acting user apply to the run, so they are checked per record here.
			ok, err := a.Access.Can(ctx, user, c, registry.Subject{Kind: s.Kind, ID: s.ID})
			switch {
			case errors.Is(err, identity.ErrNotVisible):
				p := server.NewProblem(http.StatusNotFound, "not_found", "Record not found")
				return &p
			case err != nil:
				slog.ErrorContext(ctx, "screen action access check failed", "feature", d.ID, "error_type", fmt.Sprintf("%T", err))
				p := server.NewProblem(http.StatusInternalServerError, server.CodeInternal, "Internal error")
				return &p
			case !ok:
				p := server.NewProblem(http.StatusForbidden, "missing_capability", "Missing capability")
				p.Rule = "ARC-I-036"
				return &p
			}
		}
	}
	return nil
}

// chat runs d inline as an agent acting for user and returns the checked answer. Nothing is
// stored: the answer and its citations go back in the response only.
func (a *Actions) chat(w http.ResponseWriter, r *http.Request, user registry.Principal, d feature.Def, screen string, subjects []SubjectRef) {
	ctx := r.Context()
	fail := func(msg string, err error) {
		slog.ErrorContext(ctx, msg, "feature", d.ID, "error_type", fmt.Sprintf("%T", err))
		a.Problems.WriteProblem(w, r, server.NewProblem(http.StatusInternalServerError, server.CodeInternal, "Internal error"))
	}
	spec, err := d.Spec()
	if err != nil {
		fail("screen action spec failed", err)
		return
	}
	if len(spec.OutputSchema) == 0 {
		spec.OutputSchema = run.AnswerSchema
	}
	agent, err := a.Access.AgentPrincipal(ctx, identity.RunID(a.NewRunID()), identity.UserID(user.UserID), spec.Grant)
	if err != nil {
		fail("screen action agent principal failed", err)
		return
	}
	out, err := a.Runner.Run(ctx, spec, agent, prompt(screen, subjects))
	if err != nil {
		fail("screen action run failed", err)
		return
	}
	if out.RetryQueued {
		w.Header().Set("Retry-After", "60")
		a.Problems.WriteProblem(w, r, server.NewProblem(http.StatusServiceUnavailable, "model_unavailable", "The model is busy; try again shortly"))
		return
	}
	// rule: AI-R-005
	// The answer is checked against this run's citations and returned only, never stored.
	ans, err := run.CheckAnswer(spec, out)
	if err != nil {
		fail("screen action answer refused", err)
		return
	}
	cites := make([]citationJSON, 0, len(ans.Citations))
	for _, c := range ans.Citations {
		cites = append(cites, citationJSON{VersionID: c.VersionID, Page: c.Page, FromLine: c.FromLine, ToLine: c.ToLine})
	}
	writeJSON(w, http.StatusOK, map[string]any{"answer": ans.Text, "citations": cites})
}

// prompt is the run's prompt: the screen and record ids only, no client text.
func prompt(screen string, subjects []SubjectRef) string {
	ids := make([]string, 0, len(subjects))
	for _, s := range subjects {
		ids = append(ids, s.Kind+":"+s.ID)
	}
	return "screen: " + screen + "\nrecords: " + strings.Join(ids, ", ")
}

// onScreen reports whether d has an action on screen ("screen > Button").
func onScreen(d feature.Def, screen string) bool {
	for _, act := range d.Actions {
		s, _, _ := strings.Cut(act, ">")
		if strings.TrimSpace(s) == screen {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
