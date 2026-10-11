package report

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/audit"
)

// ActionRun is the audit action recorded for every report run.
const ActionRun = "report.run"

// ParamsHash is "sha256:" + hex of params re-encoded with sorted keys and no whitespace.
// Params must be a JSON object.
func ParamsHash(params json.RawMessage) (string, error) {
	var v map[string]any
	if err := json.Unmarshal(params, &v); err != nil || v == nil {
		return "", fmt.Errorf("params: %w", ErrInvalidParams)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("params: %w", ErrInvalidParams)
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// Run runs reportID for user by inside tx and records the run. The audit event carries the
// report id, params hash and row count only; params values and rows never reach it.
func (e *Reports) Run(ctx context.Context, tx pgx.Tx, reportID string, params json.RawMessage, by string) (Run, Rows, error) {
	en, ok := e.lookup(reportID)
	if !ok {
		return Run{}, Rows{}, fmt.Errorf("run %q: %w", reportID, ErrNotFound)
	}
	return e.execute(ctx, tx, en, params, by, by)
}

// execute authorizes actor, validates params, runs the definition, appends the audit event
// and records the run. by is stored on the run (empty for a scheduled run).
func (e *Reports) execute(ctx context.Context, tx pgx.Tx, en entry, params json.RawMessage, actor, by string) (Run, Rows, error) {
	d := en.def
	ok, err := e.auth.Can(ctx, actor, d.Capability)
	if err != nil {
		return Run{}, Rows{}, fmt.Errorf("run %s: authorize: %w", d.ID, err)
	}
	if !ok {
		return Run{}, Rows{}, fmt.Errorf("run %s: %w", d.ID, ErrForbidden)
	}
	hash, err := ParamsHash(params)
	if err != nil {
		return Run{}, Rows{}, fmt.Errorf("run %s: %w", d.ID, err)
	}
	if en.schema != nil {
		var doc any
		if err := json.Unmarshal(params, &doc); err != nil {
			return Run{}, Rows{}, fmt.Errorf("run %s: params: %w", d.ID, ErrInvalidParams)
		}
		if err := en.schema.Validate(doc); err != nil {
			// The validation message can echo param values; keep only the sentinel.
			return Run{}, Rows{}, fmt.Errorf("run %s: params do not match schema: %w", d.ID, ErrInvalidParams)
		}
	}
	started := e.now()
	rows, err := d.Run(ctx, tx, params)
	if err != nil {
		return Run{}, Rows{}, fmt.Errorf("run %s: %w", d.ID, err)
	}
	e.mu.Lock()
	e.seq++
	r := Run{
		ID: RunID("run-" + strconv.Itoa(e.seq)), ReportID: d.ID, ParamsHash: hash,
		RowCount: len(rows.Rows), By: by, StartedAt: started, FinishedAt: e.now(),
	}
	e.mu.Unlock()
	ev := audit.Event{
		ActorKind: "user", ActorID: actor, Action: ActionRun,
		Subject: audit.Subject{Kind: "report_run", ID: string(r.ID)},
		Detail: map[string]string{
			"report":      d.ID,
			"params_hash": hash,
			"row_count":   strconv.Itoa(r.RowCount),
		},
	}
	if err := e.audit.Append(ctx, tx, ev); err != nil {
		return Run{}, Rows{}, fmt.Errorf("run %s: audit: %w", d.ID, err)
	}
	e.mu.Lock()
	e.runs = append(e.runs, r)
	e.mu.Unlock()
	return r, rows, nil
}

// Runs lists the runs of a report in start order.
func (e *Reports) Runs(_ context.Context, reportID string) ([]Run, error) {
	if _, ok := e.lookup(reportID); !ok {
		return nil, fmt.Errorf("runs %q: %w", reportID, ErrNotFound)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []Run
	for _, r := range e.runs {
		if r.ReportID == reportID {
			out = append(out, r)
		}
	}
	return out, nil
}
