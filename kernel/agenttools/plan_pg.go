package agenttools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
)

var _ PlanStore = PGPlans{}

var _ StepIDs = (*Planner)(nil)

// PGPlans is a PlanStore over agent.plan. A row holds one plan: its id, its run and a body of
// tool steps only (index, title, status, call ids, step id). It never stores prompt or completion text.
type PGPlans struct {
	DB db.DB
}

// planStep is the stored shape of one step. Its fields are the whole body: nothing else
// reaches the table.
type planStep struct {
	Index   int        `json:"index"`
	Title   string     `json:"title"`
	Status  StepStatus `json:"status"`
	CallIDs []string   `json:"call_ids,omitempty"`
	ID      string     `json:"id,omitempty"`
}

const (
	sqlPlanByRun = `SELECT id, run_id, body FROM agent.plan WHERE run_id = $1`
	sqlPlanByID  = `SELECT id, run_id, body FROM agent.plan WHERE id = $1`
	// sqlPlanSave stores the run's plan. A run has one plan, so a new plan for the run
	// replaces the old row.
	sqlPlanSave = `INSERT INTO agent.plan (id, run_id, body) VALUES ($1, $2, $3)
ON CONFLICT (run_id) DO UPDATE SET id = EXCLUDED.id, body = EXCLUDED.body, updated_at = now()`
)

// ByRun implements PlanStore.
func (s PGPlans) ByRun(ctx context.Context, runID string) (Plan, error) {
	p, err := s.load(ctx, sqlPlanByRun, runID)
	if err != nil {
		return Plan{}, fmt.Errorf("plan of run %s: %w", runID, err)
	}
	return p, nil
}

// ByID implements PlanStore.
func (s PGPlans) ByID(ctx context.Context, planID string) (Plan, error) {
	p, err := s.load(ctx, sqlPlanByID, planID)
	if err != nil {
		return Plan{}, fmt.Errorf("plan %s: %w", planID, err)
	}
	return p, nil
}

func (s PGPlans) load(ctx context.Context, sql, key string) (Plan, error) {
	var (
		p    Plan
		body []byte
	)
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, sql, key).Scan(&p.ID, &p.RunID, &body)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Plan{}, ErrNotFound
	}
	if err != nil {
		return Plan{}, fmt.Errorf("query: %w", err)
	}
	var steps []planStep
	if err := json.Unmarshal(body, &steps); err != nil {
		return Plan{}, fmt.Errorf("decode body: %w", err)
	}
	if len(steps) > 0 {
		p.Steps = make([]Step, len(steps))
		for i, st := range steps {
			p.Steps[i] = Step{Index: st.Index, Title: st.Title, Status: st.Status, CallIDs: st.CallIDs, ID: st.ID}
		}
	}
	return p, nil
}

// Save implements PlanStore.
func (s PGPlans) Save(ctx context.Context, p Plan) error {
	steps := make([]planStep, len(p.Steps))
	for i, st := range p.Steps {
		steps[i] = planStep{Index: st.Index, Title: st.Title, Status: st.Status, CallIDs: st.CallIDs, ID: st.ID}
	}
	body, err := json.Marshal(steps)
	if err != nil {
		return fmt.Errorf("save plan %s: encode: %w", p.ID, err)
	}
	err = s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, sqlPlanSave, p.ID, p.RunID, body)
		return err
	})
	if err != nil {
		return fmt.Errorf("save plan %s: %w", p.ID, err)
	}
	return nil
}
