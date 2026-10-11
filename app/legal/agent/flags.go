package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/notify"
)

// Flag sentinel errors.
var (
	// ErrBadFlag: a flag is missing an id, has a code or subject kind that is not a machine
	// code, an unknown severity, or a citation outside the kernel shape.
	ErrBadFlag = errors.New("agent: invalid flag")
	// ErrFlagNotFound: no open flag with that id.
	ErrFlagNotFound = errors.New("agent: flag not found")
)

// Severity is how much a flag matters.
type Severity string

// Severities.
const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// FlagDigestTemplate is the notify template that tells the acting user a job feature raised
// flags. Its data is ids and counts only; the text is the template's, rendered at delivery.
var FlagDigestTemplate = notify.Template{
	ID:       "agent.flag_digest",
	Channels: []notify.Channel{notify.InApp},
	Subject:  "{{feature_id}}: {{count}} new flags",
	Body:     "{{feature_id}} raised {{count}} new flags (highest severity {{severity}}).",
}

// Flag is one structured finding of a read-only job feature (agent.agent_flag). It has no
// free-text field (AI-R-005): what the user reads is the code, rendered by the screen.
type Flag struct {
	ID          string
	FeatureID   string
	RunID       string
	SubjectKind string
	SubjectID   string
	Code        string // machine code, "limitation.near"
	Severity    Severity
	Citations   []agenttools.Citation
	BlocksGate  string // gate kind the flag blocks; set only from the sink's BlocksGate
	ActingFor   string
}

var machineCode = regexp.MustCompile(`^[a-z][a-z0-9_.]{0,63}$`)

// validate checks f has ids, machine codes and kernel-shaped citations.
func (f Flag) validate() error {
	switch {
	case f.FeatureID == "" || f.RunID == "" || f.SubjectID == "" || f.ActingFor == "":
		return fmt.Errorf("flag %s: feature, run, subject and acting user required: %w", f.FeatureID, ErrBadFlag)
	case len(f.RunID) > 64 || len(f.SubjectID) > 64:
		return fmt.Errorf("flag %s: id too long: %w", f.FeatureID, ErrBadFlag)
	// rule: AI-R-005
	// Codes and kinds are machine codes, so no sentence can ride in them.
	case !machineCode.MatchString(f.Code) || !machineCode.MatchString(f.SubjectKind):
		return fmt.Errorf("flag %s: code and subject kind must be machine codes: %w", f.FeatureID, ErrBadFlag)
	}
	switch f.Severity {
	case SeverityInfo, SeverityWarning, SeverityCritical:
	default:
		return fmt.Errorf("flag %s: severity %q: %w", f.FeatureID, f.Severity, ErrBadFlag)
	}
	for i, c := range f.Citations {
		if err := agenttools.ValidCitation(c); err != nil {
			return fmt.Errorf("flag %s: citation %d: %w: %w", f.FeatureID, i, ErrBadFlag, err)
		}
	}
	return nil
}

// FlagStore keeps flags. Every method runs in the caller's transaction.
type FlagStore interface {
	// Insert stores f. created is false when an open flag with the same feature, subject
	// and code already exists; that flag is kept and f is dropped.
	Insert(ctx context.Context, tx pgx.Tx, f Flag) (id string, created bool, err error)
	// Dismiss closes the open flag id as by. An unknown or closed flag is ErrFlagNotFound.
	Dismiss(ctx context.Context, tx pgx.Tx, id, by string) error
	// OpenBlocking lists the open flags that block gateKind on the subject.
	OpenBlocking(ctx context.Context, tx pgx.Tx, gateKind, subjectKind, subjectID string) ([]Flag, error)
}

// Sender queues a notification; *notify.Service satisfies it.
type Sender interface {
	Send(ctx context.Context, tx pgx.Tx, to string, template string, data map[string]string) error
}

// FlagSink records what read-only job features find. A flag is a proposal: the user may
// dismiss it, and it blocks no gate unless BlocksGate names that gate kind for its feature.
type FlagSink struct {
	Store  FlagStore
	Notify Sender
	// BlocksGate maps a feature id to the gate kinds its flags block (for example the GA1
	// duty scan). A feature not listed blocks nothing. Boot fills it from the feature
	// definitions.
	BlocksGate map[string][]string
}

// Record stores flags raised by one run and sends the acting user one digest per feature.
// It returns how many were new. Every flag is checked before any is stored.
func (s *FlagSink) Record(ctx context.Context, tx pgx.Tx, flags []Flag) (int, error) {
	for _, f := range flags {
		if err := f.validate(); err != nil {
			return 0, err
		}
		if f.BlocksGate != "" && !slices.Contains(s.BlocksGate[f.FeatureID], f.BlocksGate) {
			return 0, fmt.Errorf("flag %s: feature does not block %q: %w", f.FeatureID, f.BlocksGate, ErrBadFlag)
		}
	}
	type digest struct {
		to    string
		count int
		top   Severity
	}
	byFeature := map[string]*digest{}
	var order []string
	created := 0
	for _, f := range flags {
		_, isNew, err := s.Store.Insert(ctx, tx, f)
		if err != nil {
			return 0, fmt.Errorf("flag %s: store: %w", f.FeatureID, err)
		}
		if !isNew {
			continue
		}
		created++
		d := byFeature[f.FeatureID]
		if d == nil {
			d = &digest{to: f.ActingFor}
			byFeature[f.FeatureID] = d
			order = append(order, f.FeatureID)
		}
		d.count++
		if rank(f.Severity) > rank(d.top) {
			d.top = f.Severity
		}
	}
	for _, id := range order {
		d := byFeature[id]
		// rule: AI-R-005
		// The digest goes to the user through notify with ids and counts; Counsel keeps no text.
		data := map[string]string{"feature_id": id, "count": strconv.Itoa(d.count), "severity": string(d.top)}
		if err := s.Notify.Send(ctx, tx, d.to, FlagDigestTemplate.ID, data); err != nil {
			return 0, fmt.Errorf("flag %s: digest: %w", id, err)
		}
	}
	return created, nil
}

// Dismiss closes a flag as by. A dismissed flag no longer blocks anything.
func (s *FlagSink) Dismiss(ctx context.Context, tx pgx.Tx, id, by string) error {
	if id == "" || by == "" {
		return fmt.Errorf("dismiss flag: id and user required: %w", ErrBadFlag)
	}
	return s.Store.Dismiss(ctx, tx, id, by)
}

// Blocking lists the open flags that block gateKind on the subject. Only a feature whose
// BlocksGate lists gateKind can block it; every other flag is advisory.
func (s *FlagSink) Blocking(ctx context.Context, tx pgx.Tx, gateKind, subjectKind, subjectID string) ([]Flag, error) {
	open, err := s.Store.OpenBlocking(ctx, tx, gateKind, subjectKind, subjectID)
	if err != nil {
		return nil, fmt.Errorf("blocking flags %s: %w", gateKind, err)
	}
	var out []Flag
	for _, f := range open {
		// rule: AI-R-003
		// A flag is a proposal; it blocks a gate only when its feature declares that gate.
		if f.BlocksGate == gateKind && slices.Contains(s.BlocksGate[f.FeatureID], gateKind) {
			out = append(out, f)
		}
	}
	return out, nil
}

func rank(s Severity) int {
	switch s {
	case SeverityCritical:
		return 3
	case SeverityWarning:
		return 2
	case SeverityInfo:
		return 1
	}
	return 0
}

// PGFlagStore keeps flags in agent.agent_flag.
type PGFlagStore struct{}

var _ FlagStore = PGFlagStore{}

type citationRow struct {
	VersionID string `json:"version_id"`
	Page      int    `json:"page"`
	FromLine  int    `json:"from_line"`
	ToLine    int    `json:"to_line"`
}

// Insert implements FlagStore.
func (PGFlagStore) Insert(ctx context.Context, tx pgx.Tx, f Flag) (string, bool, error) {
	rows := make([]citationRow, 0, len(f.Citations))
	for _, c := range f.Citations {
		rows = append(rows, citationRow{VersionID: c.VersionID, Page: c.Page, FromLine: c.FromLine, ToLine: c.ToLine})
	}
	cites, err := json.Marshal(rows)
	if err != nil {
		return "", false, fmt.Errorf("insert flag: citations: %w", err)
	}
	var blocks *string
	if f.BlocksGate != "" {
		blocks = &f.BlocksGate
	}
	var id string
	err = tx.QueryRow(ctx, `
INSERT INTO agent.agent_flag (feature_id, run_id, subject_kind, subject_id, code, severity, citations, blocks_gate, acting_for)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (feature_id, subject_kind, subject_id, code) WHERE dismissed_at IS NULL DO NOTHING
RETURNING id::text`,
		f.FeatureID, f.RunID, f.SubjectKind, f.SubjectID, f.Code, string(f.Severity), cites, blocks, f.ActingFor).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("insert flag: %w", err)
	}
	return id, true, nil
}

// Dismiss implements FlagStore.
func (PGFlagStore) Dismiss(ctx context.Context, tx pgx.Tx, id, by string) error {
	tag, err := tx.Exec(ctx, `
UPDATE agent.agent_flag SET dismissed_at = now(), dismissed_by = $2
WHERE id = $1 AND dismissed_at IS NULL`, id, by)
	if err != nil {
		return fmt.Errorf("dismiss flag: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("dismiss flag %s: %w", id, ErrFlagNotFound)
	}
	return nil
}

// OpenBlocking implements FlagStore.
func (PGFlagStore) OpenBlocking(ctx context.Context, tx pgx.Tx, gateKind, subjectKind, subjectID string) ([]Flag, error) {
	rows, err := tx.Query(ctx, `
SELECT id::text, feature_id, run_id, subject_kind, subject_id, code, severity, citations, blocks_gate, acting_for::text
FROM agent.agent_flag
WHERE dismissed_at IS NULL AND blocks_gate = $1 AND subject_kind = $2 AND subject_id = $3
ORDER BY created_at, id`, gateKind, subjectKind, subjectID)
	if err != nil {
		return nil, fmt.Errorf("blocking flags: %w", err)
	}
	defer rows.Close()
	var out []Flag
	for rows.Next() {
		var f Flag
		var sev string
		var cites []byte
		if err := rows.Scan(&f.ID, &f.FeatureID, &f.RunID, &f.SubjectKind, &f.SubjectID, &f.Code, &sev, &cites, &f.BlocksGate, &f.ActingFor); err != nil {
			return nil, fmt.Errorf("blocking flags: scan: %w", err)
		}
		f.Severity = Severity(sev)
		var cr []citationRow
		if err := json.Unmarshal(cites, &cr); err != nil {
			return nil, fmt.Errorf("blocking flags: citations: %w", err)
		}
		for _, c := range cr {
			f.Citations = append(f.Citations, agenttools.Citation{VersionID: c.VersionID, Page: c.Page, FromLine: c.FromLine, ToLine: c.ToLine})
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("blocking flags: %w", err)
	}
	return out, nil
}
