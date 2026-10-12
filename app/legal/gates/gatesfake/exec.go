package gatesfake

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/registry"
)

// Exec is a gates.Execer that records each statement. A write answers StepRows rows and a
// SELECT (an effect's proof) answers ProofRows rows.
type Exec struct {
	StepRows  int64
	ProofRows int64
	SQL       []string
	Args      [][]any
}

// Exec implements gates.Execer.
func (e *Exec) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	e.SQL, e.Args = append(e.SQL, sql), append(e.Args, args)
	if strings.HasPrefix(strings.TrimSpace(sql), "SELECT") {
		return pgconn.NewCommandTag("SELECT " + strconv.FormatInt(e.ProofRows, 10)), nil
	}
	return pgconn.NewCommandTag("UPDATE " + strconv.FormatInt(e.StepRows, 10)), nil
}

// GateID and DecisionID are the gate and its approving gate.gate_decision row in the runs
// RunEffectCases makes; DecisionCtx is an effect context carrying DecisionID.
const (
	GateID     = "00000000-0000-0000-0000-0000000000ff"
	DecisionID = "00000000-0000-0000-0000-0000000000dd"
)

// DecisionCtx returns an effect context for GateID approved by DecisionID.
func DecisionCtx() context.Context {
	return gate.WithDecision(gate.WithEffect(context.Background(), GateID), DecisionID)
}

// Principals used by EffectCases.
var (
	Lawyer   = registry.Principal{Kind: registry.PrincipalUser, UserID: "00000000-0000-0000-0000-00000000000a"}
	Preparer = registry.Principal{Kind: registry.PrincipalUser, UserID: "00000000-0000-0000-0000-00000000000b"}
	Partner  = registry.Principal{Kind: registry.PrincipalUser, UserID: "00000000-0000-0000-0000-00000000000c"}
	AgentRun = registry.Principal{Kind: registry.PrincipalAgent, RunID: "r-1", ActingFor: Preparer.UserID}
)

// EffectCase is one approval effect under test: the kind, its effect, an approved payload,
// the gate subject and a substring the effect's first write must contain.
type EffectCase struct {
	Kind    string
	Effect  gates.ExecEffect
	Payload any
	Subject registry.Subject
	Writes  string
	// SubjectOnly marks an effect that reads the gate subject, not the payload.
	SubjectOnly bool
	// TwoApprovers marks a kind that needs two distinct approvers: approval runs carry
	// Lawyer and Partner, and a gate with Lawyer alone fails closed.
	TwoApprovers bool
}

// effectRun is one run of an effect: the gate, the rows the fake answers, and the outcome.
type effectRun struct {
	name       string
	g          registry.Approved
	step, prof int64
	wantErr    error
	wantSQL    bool
}

// RunEffectCases checks each case: approval applies (writes run, the proof holds), a repeat
// whose writes touch nothing is a no-op, a subject the approval cannot move fails closed, and
// a payload without the change, an agent approver or the preparer (also as the user an agent
// acted for) is refused before any statement runs.
func RunEffectCases(t *testing.T, cases []EffectCase) {
	t.Helper()
	for _, c := range cases {
		raw, err := json.Marshal(c.Payload)
		if err != nil {
			t.Fatal(err)
		}
		g := func(prep registry.Principal, approvers ...registry.Principal) registry.Approved {
			return registry.Approved{GateID: GateID, Kind: c.Kind, Subject: c.Subject,
				Payload: raw, Preparer: prep, Approvers: approvers}
		}
		ok := []registry.Principal{Lawyer}
		if c.TwoApprovers {
			ok = append(ok, Partner)
		}
		runs := []effectRun{
			{"approval applies", g(AgentRun, ok...), 1, 1, nil, true},
			{"repeat is a no-op", g(AgentRun, ok...), 0, 1, nil, true},
			{"subject in another state fails closed", g(Preparer, ok...), 0, 0, gates.ErrNotApplied, true},
			// rule: ARC-I-019
			{"agent never approves", g(Preparer, AgentRun), 1, 1, gates.ErrAgentApproves, false},
			// rule: ARC-I-016
			{"preparer never approves", g(Preparer, Preparer), 1, 1, gates.ErrPreparerApproves, false},
			{"run's acting user never approves", g(AgentRun, Preparer), 1, 1, gates.ErrPreparerApproves, false},
			{"no approver", g(Preparer), 1, 1, gates.ErrNotApplied, false},
		}
		if c.TwoApprovers {
			runs = append(runs, effectRun{"one approver is not enough", g(AgentRun, Lawyer), 1, 1, gates.ErrNotApplied, false},
				effectRun{"the same approver twice is not enough", g(AgentRun, Lawyer, Lawyer), 1, 1, gates.ErrNotApplied, false})
		}
		if !c.SubjectOnly {
			empty := g(AgentRun, ok...)
			empty.Payload = json.RawMessage(`{}`)
			runs = append(runs, effectRun{"payload without the change fails closed", empty, 1, 1, gates.ErrNotApplied, false})
		}
		for _, r := range runs {
			t.Run(c.Kind+"/"+r.name, func(t *testing.T) {
				x := &Exec{StepRows: r.step, ProofRows: r.prof}
				err := c.Effect(DecisionCtx(), x, r.g)
				if !errors.Is(err, r.wantErr) || (r.wantErr == nil) != (err == nil) {
					t.Fatalf("err = %v, want %v", err, r.wantErr)
				}
				if !r.wantSQL {
					if len(x.SQL) != 0 {
						t.Fatalf("ran %d statements, want none", len(x.SQL))
					}
					return
				}
				if len(x.SQL) < 2 || !strings.Contains(x.SQL[0], c.Writes) {
					t.Fatalf("statements = %q, want a write containing %q then a proof", x.SQL, c.Writes)
				}
				if !strings.HasPrefix(strings.TrimSpace(x.SQL[len(x.SQL)-1]), "SELECT") {
					t.Fatalf("last statement %q is not the proof", x.SQL[len(x.SQL)-1])
				}
			})
		}
	}
}
