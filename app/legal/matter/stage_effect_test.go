package matter

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
	"github.com/dagi377/counsel/kernel/registry"
)

const (
	stMatter = "0190c000-0000-4000-8000-0000000000aa"
	stArea   = "pa1"
	// stStages is the practice area's active stage list as stageStateSQL returns it.
	stStages = `[{"id":"s1","name":"Intake","ordinal":0},{"id":"s2","name":"Discovery","ordinal":1},{"id":"s3","name":"Trial","ordinal":2}]`
)

// seqTx answers each QueryRow with the next row and records every call's arguments.
type seqTx struct {
	pgx.Tx
	rows  []stubRow
	calls [][]any
}

func (t *seqTx) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	t.calls = append(t.calls, args)
	if i := len(t.calls) - 1; i < len(t.rows) {
		return t.rows[i]
	}
	return stubRow{err: errors.New("seqTx: unexpected query")}
}

// stateRow is the stage state of stMatter currently in stage from.
func stateRow(from string) stubRow { return stubRow{vals: []any{from, stArea, stStages}} }

// rule: ARC-I-016
// rule: ARC-I-017
// rule: ARC-I-030
// The matter.stage_move effect moves the stage along the list and publishes
// matter.stage_changed once, naming the gate; the agent and the preparer never approve, and a
// move the list refuses writes nothing.
func TestStageMoveEffect(t *testing.T) {
	at := time.Date(2026, 10, 11, 14, 0, 0, 0, time.UTC)
	lawyer := registry.Principal{Kind: registry.PrincipalUser, UserID: "u3"}
	preparer := registry.Principal{Kind: registry.PrincipalUser, UserID: "u1"}
	agentFor := func(user string) registry.Principal {
		return registry.Principal{Kind: registry.PrincipalAgent, RunID: "r1", ActingFor: user}
	}
	tests := []struct {
		name      string
		payload   string
		preparer  registry.Principal
		approvers []registry.Principal
		rows      []stubRow
		wantErr   error
		wantFrom  string
		wantTo    string
	}{
		{name: "forward one stage", payload: `{"matter_id":"` + stMatter + `","to_stage":"s2"}`, preparer: preparer,
			approvers: []registry.Principal{lawyer}, rows: []stubRow{stateRow("s1"), {vals: []any{"sc1"}}}, wantFrom: "s1", wantTo: "s2"},
		{name: "stage by name", payload: `{"matter_id":"` + stMatter + `","to_stage":"Trial"}`, preparer: preparer,
			approvers: []registry.Principal{lawyer}, rows: []stubRow{stateRow("s2"), {vals: []any{"sc1"}}}, wantFrom: "s2", wantTo: "s3"},
		{name: "first stage", payload: `{"matter_id":"` + stMatter + `","to_stage":"s1"}`, preparer: preparer,
			approvers: []registry.Principal{lawyer}, rows: []stubRow{stateRow(""), {vals: []any{"sc1"}}}, wantTo: "s1"},
		{name: "back to an earlier stage", payload: `{"matter_id":"` + stMatter + `","to_stage":"s1"}`, preparer: preparer,
			approvers: []registry.Principal{lawyer}, rows: []stubRow{stateRow("s3"), {vals: []any{"sc1"}}}, wantFrom: "s3", wantTo: "s1"},
		{name: "F-MAT-03 agent-prepared, another lawyer approves", payload: `{"matter_id":"` + stMatter + `","to_stage":"s2"}`,
			preparer: agentFor("u1"), approvers: []registry.Principal{lawyer}, rows: []stubRow{stateRow("s1"), {vals: []any{"sc1"}}},
			wantFrom: "s1", wantTo: "s2"},
		{name: "skip refused", payload: `{"matter_id":"` + stMatter + `","to_stage":"s3"}`, preparer: preparer,
			approvers: []registry.Principal{lawyer}, rows: []stubRow{stateRow("s1")}, wantErr: ErrStageSkip},
		{name: "same stage refused", payload: `{"matter_id":"` + stMatter + `","to_stage":"s2"}`, preparer: preparer,
			approvers: []registry.Principal{lawyer}, rows: []stubRow{stateRow("s2")}, wantErr: ErrSameStage},
		{name: "stage of another area refused", payload: `{"matter_id":"` + stMatter + `","to_stage":"s9"}`, preparer: preparer,
			approvers: []registry.Principal{lawyer}, rows: []stubRow{stateRow("s1")}, wantErr: ErrUnknownStage},
		{name: "missing matter", payload: `{"matter_id":"` + stMatter + `","to_stage":"s2"}`, preparer: preparer,
			approvers: []registry.Principal{lawyer}, rows: []stubRow{{err: pgx.ErrNoRows}}, wantErr: ErrNotFound},
		{name: "agent approver refused", payload: `{"matter_id":"` + stMatter + `","to_stage":"s2"}`, preparer: preparer,
			approvers: []registry.Principal{agentFor("u3")}, wantErr: ErrStageMoveApprover},
		{name: "preparer approving refused", payload: `{"matter_id":"` + stMatter + `","to_stage":"s2"}`, preparer: preparer,
			approvers: []registry.Principal{preparer}, wantErr: ErrStageMoveApprover},
		{name: "user the agent acted for approving refused", payload: `{"matter_id":"` + stMatter + `","to_stage":"s2"}`,
			preparer: agentFor("u3"), approvers: []registry.Principal{lawyer}, wantErr: ErrStageMoveApprover},
		{name: "no approver refused", payload: `{"matter_id":"` + stMatter + `","to_stage":"s2"}`, preparer: preparer,
			wantErr: ErrStageMoveApprover},
		{name: "no stage refused", payload: `{"matter_id":"` + stMatter + `"}`, preparer: preparer,
			approvers: []registry.Principal{lawyer}, wantErr: ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewStore(&stubDB{tx: &stubTx{}}, nil)
			em := outboxfake.New()
			s.Events = em
			tx := &seqTx{rows: tt.rows}
			err := s.StageMoveEffect(clockfake.New(at, time.UTC))(context.Background(), tx, registry.Approved{
				GateID: "g1", Kind: GateStageMove, Payload: []byte(tt.payload), Preparer: tt.preparer, Approvers: tt.approvers})
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				if n := len(em.Messages()); n != 0 {
					t.Fatalf("events = %d on refusal, want 0", n)
				}
				if len(tx.calls) > 1 {
					t.Fatalf("stage change written on refusal: %v", tx.calls)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			// changeStageSQL: matter, from, to, approved_by, at.
			w := tx.calls[1]
			if w[1] != tt.wantFrom || w[2] != tt.wantTo || w[3] != "u3" || w[4] != at {
				t.Fatalf("stage change args = %v", w)
			}
			msgs := em.Messages()
			if len(msgs) != 1 || msgs[0].Type != EventStageChanged || msgs[0].Subject.ID != stMatter {
				t.Fatalf("events = %+v, want one %s", msgs, EventStageChanged)
			}
			var body StageChanged
			if err := json.Unmarshal(msgs[0].Payload, &body); err != nil {
				t.Fatal(err)
			}
			if body.GateID != "g1" || body.FromStageID != tt.wantFrom || body.ToStageID != tt.wantTo {
				t.Fatalf("body = %+v", body)
			}
		})
	}
}

// rule: ARC-I-016
// rule: ARC-I-017
// F-MAT-03: the stage advance watcher drafts a move and submits it with the acting user as
// preparer; the gate stays pending with nothing moved. The agent deciding and the acting user
// approving are refused; only another person with matters.edit applies it.
func TestStageMoveGateFMAT03(t *testing.T) {
	at := time.Date(2026, 10, 11, 14, 0, 0, 0, time.UTC)
	agentRun := registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-fmat03", ActingFor: "u1"}
	acting := registry.Principal{Kind: registry.PrincipalUser, UserID: "u1"}
	lawyer := registry.Principal{Kind: registry.PrincipalUser, UserID: "u3"}
	steps := []struct {
		name       string
		by         registry.Principal
		wantErr    error
		wantStatus gate.Status
		wantEvents int
	}{
		{name: "agent submits, gate pending", wantStatus: gate.StatusPending},
		{name: "agent deciding refused", by: agentRun, wantErr: gate.ErrAgentApprover, wantStatus: gate.StatusPending},
		{name: "acting user approving refused", by: acting, wantErr: gate.ErrSelfApproval, wantStatus: gate.StatusPending},
		{name: "another lawyer approves", by: lawyer, wantStatus: gate.StatusApproved, wantEvents: 1},
	}

	s := NewStore(&stubDB{tx: &stubTx{}}, nil)
	em := outboxfake.New()
	s.Events = em
	g := gatefake.New(func() time.Time { return at }, gatefake.Caps{"u1": {CapEdit}, "u3": {CapEdit}})
	if err := g.Register(gate.GateKind{Slug: GateStageMove, Approver: CapEdit, Risk: gate.RiskHigh, MinApprovers: 1,
		TTL: 168 * time.Hour, Effect: s.StageMoveEffect(clockfake.New(at, time.UTC))}); err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"matter_id":"` + stMatter + `","to_stage":"s2"}`)
	hash, err := gatefake.PayloadHash(payload)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	var id gate.GateID
	for _, st := range steps {
		t.Run(st.name, func(t *testing.T) {
			if st.by.Kind == "" {
				if id, err = g.Open(ctx, nil, GateStageMove, registry.Subject{Kind: "matter", ID: stMatter}, payload, agentRun); err != nil {
					t.Fatal(err)
				}
			} else {
				tx := &seqTx{rows: []stubRow{stateRow("s1"), {vals: []any{"sc1"}}}}
				err := g.Decide(ctx, tx, id, 1, gate.Decision{Verdict: gate.VerdictApprove, PayloadHash: hash, MFAVerifiedAt: at}, st.by)
				if !errors.Is(err, st.wantErr) {
					t.Fatalf("decide err = %v, want %v", err, st.wantErr)
				}
			}
			got, err := g.Get(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != st.wantStatus || got.Preparer.Kind != registry.PrincipalAgent || got.Preparer.ActingFor != "u1" {
				t.Fatalf("gate status = %s preparer = %+v, want %s", got.Status, got.Preparer, st.wantStatus)
			}
			if n := len(em.Messages()); n != st.wantEvents {
				t.Fatalf("events = %d, want %d", n, st.wantEvents)
			}
		})
	}
}
