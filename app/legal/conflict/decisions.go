package conflict

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Consent errors.
var (
	// ErrConsentMissing means G2 cannot clear: a hit disposed as needs_consent or needs_wall
	// has no signed written consent on record (RPC 3.4-2, r.1.1 "consent").
	ErrConsentMissing = errors.New("conflict: hit needs a signed written consent before G2 clears")
	// ErrBadConsent means a consent record is missing a value or has an unknown type or status.
	ErrBadConsent = errors.New("conflict: invalid consent record")
)

// ConsentType is the kind of consent. Values mirror the conflict.consent type constraint.
type ConsentType string

// Consent types.
const (
	ConsentJointRetainer       ConsentType = "joint_retainer"
	ConsentActingAgainstFormer ConsentType = "acting_against_former"
	ConsentOther               ConsentType = "other"
)

// ConsentStatus is where a consent stands: requested, signed, refused or withdrawn.
type ConsentStatus string

// Consent statuses.
const (
	ConsentRequested ConsentStatus = "requested"
	ConsentSigned    ConsentStatus = "signed"
	ConsentRefused   ConsentStatus = "refused"
	ConsentWithdrawn ConsentStatus = "withdrawn"
)

// Consent is one record of a client's consent for one hit of a run. Records are appended,
// never edited or deleted, and kept for the conflict retention class (RC-CONFLICT); the
// latest record per hit counts. A signed consent points at the signed document; it never
// holds the document's text.
type Consent struct {
	RunID             string
	Hit               int // index in the run's Hits
	Type              ConsentType
	Status            ConsentStatus
	DisclosureSummary string    // what was disclosed to the client before consenting
	ContactID         string    // the consenting client, when known
	DocumentID        string    // the signed consent document; required when Status is signed
	SignedAt          time.Time // required when Status is signed
	RecordedBy        string
	RecordedAt        time.Time
}

// validConsent refuses a consent record missing a value its table needs.
func validConsent(c Consent) error {
	switch c.Type {
	case ConsentJointRetainer, ConsentActingAgainstFormer, ConsentOther:
	default:
		return ErrBadConsent
	}
	switch c.Status {
	case ConsentRequested, ConsentSigned, ConsentRefused, ConsentWithdrawn:
	default:
		return ErrBadConsent
	}
	if strings.TrimSpace(c.DisclosureSummary) == "" || c.RecordedBy == "" || c.RecordedAt.IsZero() || c.Hit < 0 {
		return ErrBadConsent
	}
	// LSO RPC 3.4-2 and r.1.1 "consent": written consent (verify).
	// Consent must be in writing: a signed consent names its signed document and date.
	if c.Status == ConsentSigned && (c.DocumentID == "" || c.SignedAt.IsZero()) {
		return ErrBadConsent
	}
	return nil
}

// DispositionRecord is one lawyer's disposition of one hit. Records are appended; the latest
// per hit counts.
type DispositionRecord struct {
	RunID       string
	Hit         int
	Disposition Disposition
	Note        string
	By          string
	At          time.Time
}

// DecisionRecord is the one G2 decision on a run: clear or reject.
type DecisionRecord struct {
	RunID   string
	By      string
	Approve bool
	Reason  string
	At      time.Time
}

// ClearCheck is called by DecisionStore.Decide inside its transaction with the run's latest
// dispositions and consents per hit, so the clearing rules are checked on the records the
// decision commits with.
type ClearCheck func(dispositions map[int]DispositionRecord, consents map[int]Consent) error

// DecisionStore keeps the conflict record of a run: hit dispositions, consents and the G2
// decision (RPC 3.4). Every record is appended; nothing is edited or deleted. PGDecisions is
// the production store; NewChecks and NewChecksWith keep them in memory.
type DecisionStore interface {
	// Dispose appends d and returns how many hits of the run were disposed before and after
	// it. A decided run is ErrDecided.
	Dispose(ctx context.Context, d DispositionRecord) (before, after int, err error)
	// Dispositions returns the latest disposition per hit of runID.
	Dispositions(ctx context.Context, runID string) (map[int]DispositionRecord, error)
	// RecordConsent appends c. A decided run is ErrDecided.
	RecordConsent(ctx context.Context, c Consent) error
	// Consents returns the latest consent per hit of runID.
	Consents(ctx context.Context, runID string) (map[int]Consent, error)
	// Decide runs check on the run's records and, when it passes, appends d in the same
	// transaction. A second decision on a run is ErrDecided.
	Decide(ctx context.Context, d DecisionRecord, check ClearCheck) error
	// Decision returns the decision on runID; ok is false while the run is open.
	Decision(ctx context.Context, runID string) (d DecisionRecord, ok bool, err error)
}

// memDecisions is the in-memory DecisionStore NewChecks and NewChecksWith use. It is lost on
// restart and is not shared between instances; production uses PGDecisions.
type memDecisions struct {
	mu           sync.Mutex
	dispositions []DispositionRecord
	consents     []Consent
	decisions    map[string]DecisionRecord
}

var _ DecisionStore = (*memDecisions)(nil)

func newMemDecisions() *memDecisions { return &memDecisions{decisions: map[string]DecisionRecord{}} }

func (m *memDecisions) Dispose(ctx context.Context, d DispositionRecord) (int, int, error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.decisions[d.RunID]; ok {
		return 0, 0, ErrDecided
	}
	before := len(m.latestDispositions(d.RunID))
	m.dispositions = append(m.dispositions, d)
	return before, len(m.latestDispositions(d.RunID)), nil
}

func (m *memDecisions) Dispositions(ctx context.Context, runID string) (map[int]DispositionRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.latestDispositions(runID), nil
}

func (m *memDecisions) latestDispositions(runID string) map[int]DispositionRecord {
	out := map[int]DispositionRecord{}
	for _, d := range m.dispositions {
		if d.RunID == runID {
			out[d.Hit] = d
		}
	}
	return out
}

func (m *memDecisions) RecordConsent(ctx context.Context, c Consent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.decisions[c.RunID]; ok {
		return ErrDecided
	}
	m.consents = append(m.consents, c)
	return nil
}

func (m *memDecisions) Consents(ctx context.Context, runID string) (map[int]Consent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.latestConsents(runID), nil
}

func (m *memDecisions) latestConsents(runID string) map[int]Consent {
	out := map[int]Consent{}
	for _, c := range m.consents {
		if c.RunID == runID {
			out[c.Hit] = c
		}
	}
	return out
}

func (m *memDecisions) Decide(ctx context.Context, d DecisionRecord, check ClearCheck) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.decisions[d.RunID]; ok {
		return ErrDecided
	}
	if check != nil {
		if err := check(m.latestDispositions(d.RunID), m.latestConsents(d.RunID)); err != nil {
			return err
		}
	}
	m.decisions[d.RunID] = d
	return nil
}

func (m *memDecisions) Decision(ctx context.Context, runID string) (DecisionRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return DecisionRecord{}, false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.decisions[runID]
	return d, ok, nil
}

// clearCheck is the G2 clearing rule for a run with hits: every hit is disposed as not a
// conflict, waived by consent or walled off with consent, and every consent or wall hit has
// a signed written consent on record.
func clearCheck(runID string, hits int) ClearCheck {
	return func(dispositions map[int]DispositionRecord, consents map[int]Consent) error {
		for h := 0; h < hits; h++ {
			switch dispositions[h].Disposition {
			case DispositionNotAConflict:
			case DispositionNeedsConsent, DispositionNeedsWall:
				// LSO RPC 3.4-2 and r.1.1 "consent": written consent (verify).
				c, ok := consents[h]
				if !ok || c.Status != ConsentSigned || c.DocumentID == "" {
					return fmt.Errorf("conflict decide %s hit %d: %w", runID, h, ErrConsentMissing)
				}
			default:
				return fmt.Errorf("conflict decide %s hit %d: %w", runID, h, ErrUndisposedHits)
			}
		}
		return nil
	}
}
