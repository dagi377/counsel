package gate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"
)

// PG is the Postgres gate engine. Its methods are spread over the T-007 subtask files.
type PG struct {
	kinds  *Kinds
	mu     sync.RWMutex
	policy SelfApprovalPolicy
	// decide holds the app's Decide inputs (SetDecideHooks), guarded by mu. Each engine
	// owns its hooks; there is no package-level state.
	decide DecideHooks
}

// NewPG returns an engine whose kinds are checked against modules (see NewKinds).
func NewPG(modules ...string) *PG {
	return &PG{kinds: NewKinds(modules...)}
}

// Kinds returns the engine's kind set.
func (e *PG) Kinds() *Kinds { return e.kinds }

// Register adds a gate kind; the registering module is the slug's prefix.
func (e *PG) Register(k GateKind) error {
	return e.kinds.Register(Module(k.Slug), k)
}

// SetSelfApprovalPolicy installs the one optional self-approval policy.
func (e *PG) SetSelfApprovalPolicy(p SelfApprovalPolicy) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.policy = p
}

// opened is a validated Open request, ready to insert.
type opened struct {
	kind      GateKind
	label     string
	hash      []byte
	requester string
	runID     string
}

// prepareOpen checks an Open request without touching the database.
func (e *PG) prepareOpen(kind string, subject Subject, payload json.RawMessage, preparer Principal) (opened, error) {
	// rule: ARC-I-013
	k, schema, label, err := e.kinds.Lookup(kind)
	if err != nil {
		return opened{}, fmt.Errorf("open: %w", err)
	}
	if label == "" {
		return opened{}, fmt.Errorf("open %q: no spec label: %w", kind, ErrInvalidKind)
	}
	if subject.Kind == "" || subject.ID == "" {
		return opened{}, fmt.Errorf("open %q: empty subject: %w", kind, ErrInvalidPayload)
	}
	doc, err := decodeJSON(payload)
	if err != nil {
		return opened{}, fmt.Errorf("open %q: %w", kind, err)
	}
	if _, ok := doc.(map[string]any); !ok {
		return opened{}, fmt.Errorf("open %q: payload is not an object: %w", kind, ErrInvalidPayload)
	}
	if err := schema.Validate(doc); err != nil {
		return opened{}, fmt.Errorf("open %q: %w", kind, ErrInvalidPayload)
	}
	sum, err := payloadSum(payload)
	if err != nil {
		return opened{}, fmt.Errorf("open %q: %w", kind, err)
	}
	o := opened{kind: k, label: label, hash: sum}
	switch preparer.Kind {
	case PrincipalUser:
		o.requester = preparer.UserID
	case PrincipalAgent:
		// An agent only proposes; the user it acts for is the preparer.
		o.requester, o.runID = preparer.ActingFor, preparer.RunID
	default:
		return opened{}, fmt.Errorf("open %q: principal kind %q may not prepare: %w", kind, preparer.Kind, ErrNotAuthorized)
	}
	if o.requester == "" {
		return opened{}, fmt.Errorf("open %q: no preparing user: %w", kind, ErrNotAuthorized)
	}
	return o, nil
}

// Open creates version 1 of a gate for kind on subject, prepared by preparer, in tx.
// An unregistered kind returns ErrUnknownKind (HTTP 422 through Problem).
func (e *PG) Open(ctx context.Context, tx pgx.Tx, kind string, subject Subject, payload json.RawMessage, preparer Principal) (GateID, error) {
	o, err := e.prepareOpen(kind, subject, payload, preparer)
	if err != nil {
		return "", err
	}
	var runID any
	if o.runID != "" {
		runID = o.runID
	}
	var id string
	err = tx.QueryRow(ctx, `
INSERT INTO gate.approval_request
  (gate_code, spec_gate, version, payload_type, payload_id, payload_hash, requested_by,
   agent_run_id, eligible_role, required_approvals, risk, expires_at)
VALUES ($1, $2, 1, $3, $4, $5, $6, $7, $8, $9, $10, now() + make_interval(hours => $11))
RETURNING id::text`,
		o.kind.Slug, o.label, subject.Kind, subject.ID, o.hash, o.requester,
		runID, o.kind.Approver, o.kind.MinApprovers, string(o.kind.Risk), int(o.kind.TTL.Hours()),
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("open %q: insert: %w", kind, err)
	}
	return GateID(id), nil
}

// rule: ARC-I-014
// PayloadHash returns "sha256:" + hex(SHA-256(JCS(payload))); see JCS for the RFC 8785
// canonical form and the integer-only rule for payload numbers.
func PayloadHash(payload json.RawMessage) (string, error) {
	sum, err := payloadSum(payload)
	if err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(sum), nil
}

// payloadSum is SHA-256(JCS(payload)), the hash stored on a gate version.
func payloadSum(payload json.RawMessage) ([]byte, error) {
	b, err := JCS(payload)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(b)
	return sum[:], nil
}
