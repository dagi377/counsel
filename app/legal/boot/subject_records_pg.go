package boot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/conflict"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/db"
)

// PGSubjectRecords reads the stored record of a submitted subject for the gate payload
// builders (agent.SubjectRecords): the subject's own row as field name to value, its matter
// as matter_id and its id under "<kind>_id". A matter subject is read through matter.Query
// (ARC-I-041). A kind it does not know fails closed with ErrSubjectKind, so no gate opens.
type PGSubjectRecords struct {
	DB      db.DB
	Matters MatterGetter
	// Subjects resolves the subject's matter, as submit_for_approval does.
	Subjects agent.SubjectMatters
}

var _ agent.SubjectRecords = PGSubjectRecords{}

// subjectRecordSQL maps a subject kind to the query of its row as JSON. The rows hold ids,
// dates, amounts in cents and states; drafted text lives in the body store, not here.
var subjectRecordSQL = map[string]string{
	SubjectDocumentDraft:    `SELECT to_jsonb(t) FROM docs.draft t WHERE id = $1`,
	agent.SubjectAgentNote:  `SELECT to_jsonb(t) FROM matter.note t WHERE id = $1`,
	agent.SubjectEmailDraft: `SELECT to_jsonb(t) FROM comms.comm_filing t WHERE comm_item_id = $1 ORDER BY matter_id LIMIT 1`,
	agent.SubjectDeadline:   `SELECT to_jsonb(t) FROM cal.deadline t WHERE id = $1`,
	agent.SubjectBill:       `SELECT to_jsonb(t) FROM billing.bill t WHERE id = $1`,
	agent.SubjectTimeEntry: `SELECT to_jsonb(t) FROM timekeeping.time_entry t WHERE id = $1
UNION ALL SELECT to_jsonb(s) FROM timekeeping.time_suggestion s WHERE id = $1 LIMIT 1`,
	subjectConflictCheck: `SELECT to_jsonb(t) FROM conflict.conflict_check t WHERE id = $1`,
	conflict.SubjectRun:  `SELECT to_jsonb(t) FROM conflict.check_run t WHERE id = $1`,
	subjectWorkItem:      `SELECT to_jsonb(t) FROM work.work_item t WHERE id = $1`,
}

// SubjectRecord implements agent.SubjectRecords.
func (s PGSubjectRecords) SubjectRecord(ctx context.Context, subjectType, subjectID string) (map[string]any, error) {
	if !isUUID(subjectID) {
		return nil, fmt.Errorf("subject %s: %w", subjectType, agenttools.ErrNotFound)
	}
	if subjectType == agent.WorkSubjectMatter {
		m, err := getMatter(ctx, s.Matters, subjectID)
		if err != nil {
			return nil, fmt.Errorf("subject %s: %w", subjectType, err)
		}
		rec := map[string]any{"matter_id": string(m.ID), "client_id": string(m.ClientID),
			"responsible_lawyer_id": m.ResponsibleLawyerID, "status": string(m.Status)}
		if !m.OpenDate.IsZero() {
			rec["open_date"] = m.OpenDate.Format("2006-01-02")
		}
		return rec, nil
	}
	q := subjectRecordSQL[subjectType]
	if q == "" {
		return nil, fmt.Errorf("subject %s: %w", subjectType, ErrSubjectKind)
	}
	var raw []byte
	err := inTx(ctx, s.DB, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, q, subjectID).Scan(&raw)
	})
	if err != nil {
		return nil, fmt.Errorf("subject %s: %w", subjectType, notFound(err))
	}
	return s.record(ctx, subjectType, subjectID, raw)
}

// record decodes raw (numbers kept exact) and adds the subject's id and matter.
func (s PGSubjectRecords) record(ctx context.Context, subjectType, subjectID string, raw []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var rec map[string]any
	if err := dec.Decode(&rec); err != nil || rec == nil {
		return nil, fmt.Errorf("subject %s: record: %w", subjectType, agenttools.ErrNotFound)
	}
	rec[subjectIDField(subjectType)] = subjectID
	if v, _ := rec["matter_id"].(string); v == "" && s.Subjects != nil {
		m, err := s.Subjects.SubjectMatter(ctx, subjectType, subjectID)
		if err != nil {
			return nil, fmt.Errorf("subject %s: matter: %w", subjectType, err)
		}
		if m != "" {
			rec["matter_id"] = m
		}
	}
	return rec, nil
}

// subjectIDField names a subject's id in a payload: "billing.bill" is bill_id.
func subjectIDField(subjectType string) string {
	name := subjectType[strings.LastIndex(subjectType, ".")+1:]
	return name + "_id"
}
