package run

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
)

// Run record states (agent.run_record.state).
const (
	StateQueued    = "queued"
	StateRunning   = "running"
	StateSucceeded = "succeeded"
	StateFailed    = "failed"
)

// ErrRecordNotFound: no run record has this id.
var ErrRecordNotFound = errors.New("run: record not found")

// Record is what Counsel keeps about one run: ids, state, the run principal, timestamps and
// model cost. It has no prompt, input or answer field (AI-R-005); ErrorCode is a machine code.
type Record struct {
	ID         string
	FeatureID  string
	SkillID    string
	ActingFor  string
	Grant      []string
	SubjectID  string // the app subject the run is about; empty: none
	State      string
	ErrorCode  string
	CreatedAt  time.Time
	StartedAt  *time.Time
	EndedAt    *time.Time
	InTokens   int64
	OutTokens  int64
	CostMicroC int64 // model cost in micro-cents; not client money
}

// Finish is how a run ended: its state, a machine error code and the model usage it spent.
type Finish struct {
	State      string
	ErrorCode  string
	InTokens   int64
	OutTokens  int64
	CostMicroC int64
}

// PGRecords stores run records in agent.run_record.
type PGRecords struct {
	DB db.DB
}

// Statements PGRecords issues. None selects or writes conversation text.
const (
	sqlInsertRecord = `INSERT INTO agent.run_record
  (id, feature_id, skill_id, acting_for, grant_caps, subject_id, state, created_at)
VALUES ($1::uuid, $2, $3, $4::uuid, $5, NULLIF($6, '')::uuid, 'queued', $7)`
	sqlStartRecord = `UPDATE agent.run_record SET state = 'running', started_at = $2
WHERE id = $1::uuid AND state = 'queued'`
	sqlFinishRecord = `UPDATE agent.run_record SET state = $2, error_code = NULLIF($3, ''), ended_at = $4,
  input_tokens = input_tokens + $5, output_tokens = output_tokens + $6, cost_micro_cents = cost_micro_cents + $7
WHERE id = $1::uuid AND state IN ('queued', 'running')`
	sqlGetRecord = `SELECT id::text, feature_id, skill_id, acting_for::text, grant_caps, COALESCE(subject_id::text, ''),
  state, COALESCE(error_code, ''), created_at, started_at, ended_at, input_tokens, output_tokens, cost_micro_cents
FROM agent.run_record WHERE id::text = $1`
)

// Create inserts r as a queued run.
func (s PGRecords) Create(ctx context.Context, r Record) error {
	grant := r.Grant
	if grant == nil {
		grant = []string{}
	}
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, sqlInsertRecord, r.ID, r.FeatureID, r.SkillID, r.ActingFor, grant, r.SubjectID, r.CreatedAt)
		return err
	})
	if err != nil {
		return fmt.Errorf("create run record %s: %w", r.ID, err)
	}
	return nil
}

// Start marks a queued run running at at.
func (s PGRecords) Start(ctx context.Context, id string, at time.Time) error {
	return s.update(ctx, "start", id, sqlStartRecord, id, at)
}

// Finish ends a queued or running run at at and adds its usage. An ended run is
// ErrRecordNotFound, so a run never ends twice.
func (s PGRecords) Finish(ctx context.Context, id string, at time.Time, f Finish) error {
	if !slices.Contains([]string{StateSucceeded, StateFailed}, f.State) {
		return fmt.Errorf("finish run record %s: state %q", id, f.State)
	}
	return s.update(ctx, "finish", id, sqlFinishRecord, id, f.State, f.ErrorCode, at, f.InTokens, f.OutTokens, f.CostMicroC)
}

func (s PGRecords) update(ctx context.Context, op, id, sql string, args ...any) error {
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, sql, args...)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrRecordNotFound
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("%s run record %s: %w", op, id, err)
	}
	return nil
}

// Get returns the run record id, or ErrRecordNotFound.
func (s PGRecords) Get(ctx context.Context, id string) (Record, error) {
	var r Record
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, sqlGetRecord, id).Scan(&r.ID, &r.FeatureID, &r.SkillID, &r.ActingFor, &r.Grant,
			&r.SubjectID, &r.State, &r.ErrorCode, &r.CreatedAt, &r.StartedAt, &r.EndedAt, &r.InTokens, &r.OutTokens,
			&r.CostMicroC)
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Record{}, fmt.Errorf("get run record %s: %w", id, ErrRecordNotFound)
	case err != nil:
		return Record{}, fmt.Errorf("get run record %s: %w", id, err)
	}
	return r, nil
}
