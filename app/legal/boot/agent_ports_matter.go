package boot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/kernel/agenttools"
	agentrun "github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/deadline"
)

// Matter-side agent ports over Postgres (Q-BOOT-PORTS-B). Each adapter reads or writes the
// module's own tables only; a drafted body goes to the encrypted object store (DM-020) and
// the row holds its reference, never the text.

// ErrSubjectKind means submit_for_approval named a subject kind no adapter resolves. It wraps
// agenttools.ErrNotFound, so the caller sees 404 and nothing is opened.
var ErrSubjectKind = fmt.Errorf("boot: unknown subject kind: %w", agenttools.ErrNotFound)

// ErrCheckClosed means parties were proposed for a conflict check already cleared or declined.
var ErrCheckClosed = errors.New("boot: conflict check is closed")

// SubjectDocumentDraft is the subject kind of a docs.draft row.
const SubjectDocumentDraft = "document_draft"

// Work configuration names of an agent-proposed task: the task workflow and its proposed
// status (work.workflow.name, work.status.name).
const (
	TaskWorkflowName   = agent.WorkTypeTask
	ProposedStatusName = "proposed"
)

// Draft row markers. An agent draft is unconfirmed until its gate is approved.
const (
	draftDocStatus  = "ready"
	draftFilingRule = "agent_draft"
	draftDocKind    = "draft"
)

// MatterGetter reads one matter through the matter module (matter.Query, ARC-I-041): no
// other module reads the matter base tables.
type MatterGetter interface {
	Get(ctx context.Context, id matter.ID) (matter.Matter, error)
}

// WireMatterPorts fills the matter-side ports of p that are still nil from Postgres stores
// over d, with matters reading matters and bodies holding drafted text. It also sets Plans to a Planner over agent.plan
// and makes one Provenance that every tool, write dep and runner shares.
func WireMatterPorts(p *AgentPorts, d db.DB, matters MatterGetter, bodies matter.Bodies) error {
	if err := requireDeps("agent matter ports", []dep{
		{"AgentPorts", p == nil},
		{"db.DB", d == nil},
		{"matter Query", matters == nil},
		{"matter Bodies", bodies == nil},
	}); err != nil {
		return err
	}
	if p.PublishedPacks == nil {
		p.PublishedPacks = PGPublishedPacks{DB: d}
	}
	if p.SubjectMatters == nil {
		p.SubjectMatters = PGSubjectMatters{DB: d, Matters: matters}
	}
	if p.ProposedTasks == nil {
		p.ProposedTasks = PGProposedTasks{DB: d}
	}
	if p.ConflictParties == nil {
		p.ConflictParties = PGConflictParties{DB: d}
	}
	if p.MatterContacts == nil {
		p.MatterContacts = PGMatterContacts{DB: d, Matters: matters}
	}
	if p.DocumentDrafts == nil {
		p.DocumentDrafts = PGDocumentDrafts{DB: d, Bodies: bodies}
	}
	if p.EmailDrafts == nil {
		p.EmailDrafts = PGEmailDrafts{DB: d, Bodies: bodies}
	}
	if p.NoteDrafts == nil {
		p.NoteDrafts = PGNoteDrafts{DB: d, Bodies: bodies}
	}
	if p.Plans == nil {
		// The Planner stores step ids, so report_step finds a step after a restart.
		p.Plans = agenttools.NewPlanner(agenttools.PGPlans{DB: d})
	}
	if p.Provenance == nil {
		p.Provenance = agenttools.NewProvenance()
	}
	return nil
}

// AgentWriteDeps returns base with the ports' shared Provenance, so the write tools check
// sourced figures against the same run values the read tools recorded.
func AgentWriteDeps(p AgentPorts, base agent.WriteDeps) agent.WriteDeps {
	base.Provenance = p.Provenance
	return base
}

// ForgetOnEnd wraps r so that every run it drives drops its provenance values when it ends.
// A runner already wrapped for pv is returned as is; a nil pv or r returns r.
func ForgetOnEnd(pv *agenttools.Provenance, r agentrun.Runner) agentrun.Runner {
	if pv == nil || r == nil {
		return r
	}
	if f, ok := r.(ForgetRunner); ok && f.Provenance == pv {
		return r
	}
	return ForgetRunner{Runner: r, Provenance: pv}
}

func inTx(ctx context.Context, d db.DB, fn func(ctx context.Context, tx pgx.Tx) error) error {
	if d == nil {
		return fmt.Errorf("agent port: %w", agent.ErrMissingDep)
	}
	return d.InTx(ctx, fn)
}

// notFound maps no rows to agenttools.ErrNotFound.
func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return agenttools.ErrNotFound
	}
	return err
}

// PGPublishedPacks lists the active rule packs of cal.rule_pack, the newest active version of
// each code, with their holidays. Draft and verified-but-not-active packs are not listed.
type PGPublishedPacks struct{ DB db.DB }

const sqlPublishedPacks = `SELECT p.code, p.version, p.rules,
  COALESCE(array_agg(h.day ORDER BY h.day) FILTER (WHERE h.day IS NOT NULL), '{}')
FROM (SELECT DISTINCT ON (code) id, code, version, rules FROM cal.rule_pack
      WHERE status = 'active' ORDER BY code, effective_from DESC, version DESC) p
LEFT JOIN cal.holiday h ON h.rule_pack_id = p.id
GROUP BY p.code, p.version, p.rules
ORDER BY p.code`

// PublishedPacks implements agent.PublishedPacks.
func (s PGPublishedPacks) PublishedPacks(ctx context.Context) ([]deadline.Pack, error) {
	var out []deadline.Pack
	err := inTx(ctx, s.DB, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, sqlPublishedPacks)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				code, version string
				rules         []byte
				days          []time.Time
			)
			if err := rows.Scan(&code, &version, &rules, &days); err != nil {
				return err
			}
			p, err := packOf(code, version, rules, days)
			if err != nil {
				return err
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("published packs: %w", err)
	}
	return out, nil
}

// packRule is the stored shape of one cal.rule_pack rule (the pack YAML's rule keys).
type packRule struct {
	ID          string `json:"id"`
	Trigger     string `json:"trigger"`
	Mode        string `json:"mode"`
	Days        int    `json:"days"`
	MonthsAfter int    `json:"months_after"`
	Roll        string `json:"roll"`
}

// packOf builds a deadline.Pack from a stored row. The pack id is its code, the key the
// deadline engine computes by.
func packOf(code, version string, rules []byte, days []time.Time) (deadline.Pack, error) {
	var rs []packRule
	if err := json.Unmarshal(rules, &rs); err != nil {
		return deadline.Pack{}, fmt.Errorf("pack %s %s rules: %w", code, version, deadline.ErrInvalidPack)
	}
	p := deadline.Pack{ID: code, Version: version}
	for _, r := range rs {
		roll := deadline.Roll(r.Roll)
		if roll == "" {
			roll = deadline.RollNone
		}
		p.Rules = append(p.Rules, deadline.Rule{
			ID: r.ID, Trigger: r.Trigger, Mode: deadline.DayCount(r.Mode),
			Days: r.Days, MonthsAfter: r.MonthsAfter, Roll: roll,
		})
	}
	for _, d := range days {
		p.Holidays = append(p.Holidays, clock.DateOf(d, time.UTC))
	}
	return p, nil
}

// PGSubjectMatters resolves the matter of a submitted subject from the subject's own table.
// A kind it does not know fails closed with ErrSubjectKind.
type PGSubjectMatters struct {
	DB      db.DB
	Matters MatterGetter
}

// subjectMatterSQL maps a subject kind to the query of its matter id. An empty result means
// the subject is on no matter (a lead's deadline or conflict check).
var subjectMatterSQL = map[string]string{
	SubjectDocumentDraft:    `SELECT matter_id::text FROM docs.draft WHERE id = $1`,
	agent.SubjectAgentNote:  `SELECT matter_id::text FROM matter.note WHERE id = $1`,
	agent.SubjectEmailDraft: `SELECT matter_id::text FROM comms.comm_filing WHERE comm_item_id = $1 ORDER BY matter_id LIMIT 1`,
	agent.SubjectDeadline:   `SELECT COALESCE(matter_id::text, '') FROM cal.deadline WHERE id = $1`,
	agent.SubjectBill:       `SELECT matter_id::text FROM billing.bill WHERE id = $1`,
	agent.SubjectTimeEntry:  `SELECT matter_id::text FROM timekeeping.time_entry WHERE id = $1`,
	subjectConflictCheck:    `SELECT COALESCE(matter_id::text, '') FROM conflict.conflict_check WHERE id = $1`,
}

const subjectConflictCheck = "conflict_check"

// SubjectMatter implements agent.SubjectMatters.
func (s PGSubjectMatters) SubjectMatter(ctx context.Context, subjectType, subjectID string) (string, error) {
	q := subjectMatterSQL[subjectType]
	if q == "" && subjectType != agent.WorkSubjectMatter {
		return "", fmt.Errorf("subject %s: %w", subjectType, ErrSubjectKind)
	}
	if !isUUID(subjectID) {
		return "", fmt.Errorf("subject %s: %w", subjectType, agenttools.ErrNotFound)
	}
	if subjectType == agent.WorkSubjectMatter {
		m, err := getMatter(ctx, s.Matters, subjectID)
		if err != nil {
			return "", fmt.Errorf("subject %s: %w", subjectType, err)
		}
		return string(m.ID), nil
	}
	var matterID string
	err := inTx(ctx, s.DB, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, q, subjectID).Scan(&matterID)
	})
	if err != nil {
		return "", fmt.Errorf("subject %s: %w", subjectType, notFound(err))
	}
	return matterID, nil
}

// PGProposedTasks reads the proposed status of the newest task workflow version.
type PGProposedTasks struct{ DB db.DB }

const sqlProposedTask = `SELECT w.id::text, s.id::text FROM work.workflow w
JOIN work.status s ON s.workflow_id = w.id
WHERE w.name = $1 AND s.name = $2
ORDER BY w.version DESC LIMIT 1`

// ProposedTaskStatus implements agent.ProposedTasks.
func (s PGProposedTasks) ProposedTaskStatus(ctx context.Context) (string, string, error) {
	var wf, st string
	err := inTx(ctx, s.DB, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, sqlProposedTask, TaskWorkflowName, ProposedStatusName).Scan(&wf, &st)
	})
	if err != nil {
		return "", "", fmt.Errorf("proposed task status: %w", notFound(err))
	}
	return wf, st, nil
}

// PGConflictParties keeps proposed parties on conflict.check_party.
type PGConflictParties struct{ DB db.DB }

const (
	sqlCheckMatter = `SELECT COALESCE(matter_id::text, '') FROM conflict.conflict_check WHERE id = $1`
	// sqlCheckTouch records who prepared the proposal and refuses a closed check.
	sqlCheckTouch = `UPDATE conflict.conflict_check
SET updated_by = $2, updated_at = now(), row_version = row_version + 1
WHERE id = $1 RETURNING status`
	sqlPartyInsert = `INSERT INTO conflict.check_party (check_id, name, kind, role) VALUES ($1, $2, $3, $4)`
	sqlPartyTerms  = `SELECT t FROM (
  SELECT name AS t FROM conflict.check_party WHERE check_id = $1
  UNION SELECT unnest(aliases) FROM conflict.check_party WHERE check_id = $1) x ORDER BY t`
)

// partyKind maps an agent party kind onto conflict.check_party.kind.
func partyKind(k string) (string, error) {
	switch k {
	case "", "person":
		return "person", nil
	case "org", "organization":
		return "organization", nil
	}
	return "", fmt.Errorf("party kind %q: %w", k, agent.ErrInvalidInput)
}

// CheckMatter implements agent.ConflictCheckParties.
func (s PGConflictParties) CheckMatter(ctx context.Context, checkID string) (string, error) {
	if !isUUID(checkID) {
		return "", fmt.Errorf("conflict check: %w", agenttools.ErrNotFound)
	}
	var matterID string
	err := inTx(ctx, s.DB, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, sqlCheckMatter, checkID).Scan(&matterID)
	})
	if err != nil {
		return "", fmt.Errorf("conflict check: %w", notFound(err))
	}
	return matterID, nil
}

// ProposeParties implements agent.ConflictCheckParties. The parties and the preparer are
// written in one transaction; the check's clearance stays a lawyer's gate.
func (s PGConflictParties) ProposeParties(ctx context.Context, checkID string, parties []agent.ProposedParty, preparedBy string) ([]string, error) {
	if !isUUID(checkID) {
		return nil, fmt.Errorf("propose parties: %w", agenttools.ErrNotFound)
	}
	if !isUUID(preparedBy) {
		return nil, fmt.Errorf("propose parties: preparer: %w", agent.ErrInvalidInput)
	}
	kinds := make([]string, len(parties))
	for i, p := range parties {
		k, err := partyKind(p.Kind)
		if err != nil {
			return nil, fmt.Errorf("propose parties: %w", err)
		}
		kinds[i] = k
	}
	var terms []string
	err := inTx(ctx, s.DB, func(ctx context.Context, tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, sqlCheckTouch, checkID, preparedBy).Scan(&status); err != nil {
			return notFound(err)
		}
		if status == "cleared" || status == "declined" {
			return ErrCheckClosed
		}
		for i, p := range parties {
			if _, err := tx.Exec(ctx, sqlPartyInsert, checkID, p.Name, kinds[i], p.Role); err != nil {
				return err
			}
		}
		rows, err := tx.Query(ctx, sqlPartyTerms, checkID)
		if err != nil {
			return err
		}
		terms, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("propose parties: %w", err)
	}
	return terms, nil
}

// getMatter reads a matter through the matter module, mapping its not-found onto the agent
// tools' not-found.
func getMatter(ctx context.Context, matters MatterGetter, id string) (matter.Matter, error) {
	if matters == nil {
		return matter.Matter{}, fmt.Errorf("matter query: %w", agent.ErrMissingDep)
	}
	m, err := matters.Get(ctx, matter.ID(id))
	if errors.Is(err, matter.ErrNotFound) {
		return matter.Matter{}, fmt.Errorf("matter: %w", agenttools.ErrNotFound)
	}
	if err != nil {
		return matter.Matter{}, fmt.Errorf("matter: %w", err)
	}
	return m, nil
}

// PGMatterContacts lists a matter's client (read through the matter module) and its current
// party contacts.
type PGMatterContacts struct {
	DB      db.DB
	Matters MatterGetter
}

const sqlMatterContacts = `SELECT DISTINCT contact_id::text FROM matter.party_role
WHERE matter_id = $1 AND archived_at IS NULL AND (ends_on IS NULL OR ends_on >= current_date)
ORDER BY 1`

// MatterContactIDs implements agent.MatterContacts.
func (s PGMatterContacts) MatterContactIDs(ctx context.Context, matterID string) ([]string, error) {
	if !isUUID(matterID) {
		return nil, fmt.Errorf("matter contacts: %w", agenttools.ErrNotFound)
	}
	m, err := getMatter(ctx, s.Matters, matterID)
	if err != nil {
		return nil, fmt.Errorf("matter contacts: %w", err)
	}
	var parties []string
	err = inTx(ctx, s.DB, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, sqlMatterContacts, matterID)
		if err != nil {
			return err
		}
		parties, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("matter contacts: %w", err)
	}
	ids := []string{string(m.ClientID)}
	for _, id := range parties {
		if id != ids[0] {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// draftRow checks the ids every draft row carries.
func draftRow(kind, matterID, preparedBy, runID string) error {
	if !isUUID(matterID) {
		return fmt.Errorf("%s: matter: %w", kind, agenttools.ErrNotFound)
	}
	// The agent never prepares in its own name: the preparer is the acting user.
	if !isUUID(preparedBy) {
		return fmt.Errorf("%s: preparer: %w", kind, agent.ErrInvalidInput)
	}
	if runID != "" && !isUUID(runID) {
		return fmt.Errorf("%s: run id: %w", kind, agent.ErrInvalidInput)
	}
	return nil
}

// PGDocumentDrafts stores draft documents on docs.draft. The body goes to the object store;
// source_scope holds its reference and the citations' anchors, never body text.
type PGDocumentDrafts struct {
	DB     db.DB
	Bodies matter.Bodies
}

const sqlDocDraft = `INSERT INTO docs.draft
  (matter_id, instruction, doc_kind, source_scope, status, agent_run_id, created_by)
VALUES ($1, $2, $3, $4, $5, NULLIF($6, '')::uuid, $7)
RETURNING id::text`

type draftScope struct {
	BodyRef    string          `json:"body_ref"`
	TemplateID string          `json:"template_id,omitempty"`
	Citations  []draftCiteJSON `json:"citations,omitempty"`
}

type draftCiteJSON struct {
	Anchor    string `json:"anchor"`
	DocID     string `json:"doc_id,omitempty"`
	QuoteHash string `json:"quote_hash,omitempty"`
	VersionID string `json:"version_id,omitempty"`
	Page      int    `json:"page,omitempty"`
	FromLine  int    `json:"from_line,omitempty"`
	ToLine    int    `json:"to_line,omitempty"`
}

// CreateDraft implements agent.DocumentDrafts.
func (s PGDocumentDrafts) CreateDraft(ctx context.Context, d agent.NewDraftDocument) (string, error) {
	if err := draftRow("document draft", d.MatterID, d.PreparedBy, d.RunID); err != nil {
		return "", err
	}
	ref, err := s.Bodies.Put(ctx, d.BodyMarkdown)
	if err != nil {
		return "", fmt.Errorf("document draft: body: %w", err)
	}
	scope := draftScope{BodyRef: ref, TemplateID: d.TemplateID}
	for _, c := range d.Citations {
		scope.Citations = append(scope.Citations, draftCiteJSON{
			Anchor: c.Anchor, DocID: c.DocID, QuoteHash: c.QuoteHash, VersionID: c.Source.VersionID,
			Page: c.Source.Page, FromLine: c.Source.FromLine, ToLine: c.Source.ToLine,
		})
	}
	raw, err := json.Marshal(scope)
	if err != nil {
		return "", fmt.Errorf("document draft: scope: %w", err)
	}
	kind := draftDocKind
	if d.TemplateID != "" {
		kind = d.TemplateID
	}
	var id string
	err = inTx(ctx, s.DB, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, sqlDocDraft, d.MatterID, d.Title, kind, raw, draftDocStatus, d.RunID, d.PreparedBy).Scan(&id)
	})
	if err != nil {
		return "", fmt.Errorf("document draft: %w", err)
	}
	return id, nil
}

// PGEmailDrafts stores draft emails as unsent outbound comms.comm_item rows filed to the
// matter as proposed. It has no send path: delivery_status stays empty until the
// outgoing-email gate's effect sends it.
type PGEmailDrafts struct {
	DB     db.DB
	Bodies matter.Bodies
}

const (
	sqlEmailDraft = `INSERT INTO comms.comm_item
  (channel, direction, participants, display_subject, body_ref, occurred_at)
VALUES ('email', 'outbound', $1, $2, $3, now())
RETURNING id::text`
	sqlEmailFiling = `INSERT INTO comms.comm_filing (comm_item_id, matter_id, rule, status, filed_by)
VALUES ($1, $2, $3, 'proposed', $4)`
)

type emailParticipants struct {
	ToContactIDs         []string `json:"to_contact_ids"`
	AttachmentVersionIDs []string `json:"attachment_version_ids,omitempty"`
	PreparedBy           string   `json:"prepared_by"`
	RunID                string   `json:"run_id,omitempty"`
}

// CreateEmailDraft implements agent.EmailDrafts.
func (s PGEmailDrafts) CreateEmailDraft(ctx context.Context, d agent.NewDraftEmail) (string, error) {
	if err := draftRow("email draft", d.MatterID, d.PreparedBy, d.RunID); err != nil {
		return "", err
	}
	ref, err := s.Bodies.Put(ctx, d.BodyMarkdown)
	if err != nil {
		return "", fmt.Errorf("email draft: body: %w", err)
	}
	// drafted_by_agent_run_id needs an approval id (comm_item CHECK), so the run id waits in
	// participants until the gate's effect stamps the row.
	raw, err := json.Marshal(emailParticipants{
		ToContactIDs: d.ToContactIDs, AttachmentVersionIDs: d.AttachmentVersionIDs,
		PreparedBy: d.PreparedBy, RunID: d.RunID,
	})
	if err != nil {
		return "", fmt.Errorf("email draft: participants: %w", err)
	}
	var id string
	err = inTx(ctx, s.DB, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, sqlEmailDraft, raw, d.Subject, ref).Scan(&id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, sqlEmailFiling, id, d.MatterID, draftFilingRule, d.PreparedBy)
		return err
	})
	if err != nil {
		return "", fmt.Errorf("email draft: %w", err)
	}
	return id, nil
}

// PGNoteDrafts stores draft file notes on matter.note, unconfirmed (confirmed_by empty) until
// the matter.agent_note gate is approved.
type PGNoteDrafts struct {
	DB     db.DB
	Bodies matter.Bodies
}

const sqlNoteDraft = `INSERT INTO matter.note (matter_id, author_id, agent_run_id, body_ref, updated_by)
VALUES ($1, $2, NULLIF($3, '')::uuid, $4, $2)
RETURNING id::text`

// CreateNoteDraft implements agent.NoteDrafts.
func (s PGNoteDrafts) CreateNoteDraft(ctx context.Context, n agent.NewDraftNote) (string, error) {
	if err := draftRow("note draft", n.MatterID, n.PreparedBy, n.RunID); err != nil {
		return "", err
	}
	ref, err := s.Bodies.Put(ctx, n.BodyMarkdown)
	if err != nil {
		return "", fmt.Errorf("note draft: body: %w", err)
	}
	var id string
	err = inTx(ctx, s.DB, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, sqlNoteDraft, n.MatterID, n.PreparedBy, n.RunID, ref).Scan(&id)
	})
	if err != nil {
		return "", fmt.Errorf("note draft: %w", err)
	}
	return id, nil
}

// isUUID reports whether s has the canonical 8-4-4-4-12 hex form.
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return false
			}
		}
	}
	return true
}
