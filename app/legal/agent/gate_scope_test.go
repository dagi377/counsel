package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/conflict"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/registry"
)

// gatesOpened counts the gates g holds. gatefake numbers gates gate-1, gate-2, ... in order.
func gatesOpened(g *gatefake.Fake) int {
	n := 0
	for {
		if _, err := g.Get(context.Background(), gate.GateID(fmt.Sprintf("gate-%d", n+1))); err != nil {
			return n
		}
		n++
	}
}

// gateCall is one call of a gate-opening tool: what it returned, the gates it opened and the
// rows (drafts, proposals) it stored.
type gateCall struct {
	err   error
	gates int
	rows  int
}

// rule: ARC-I-035
// Every agent tool that opens a gate is refused with ErrGateKindNotAllowed when its run's
// feature does not declare that gate kind, and the refused call opens and stores nothing. The
// declared case proves each input is otherwise valid, so the refusal comes from the scope.
func TestARC_I_035_GateOpeningToolsScoped(t *testing.T) {
	agentP := func(run, capability string) registry.Principal {
		return registry.Principal{Kind: registry.PrincipalAgent, RunID: run, ActingFor: clerk, Grant: []string{capability}}
	}
	scope := func(run string, kinds []string, drafts ...string) context.Context {
		s := agenttools.NewScope(run, kinds)
		for _, d := range drafts {
			s.AddDraft(d)
		}
		return agenttools.WithScope(context.Background(), s)
	}
	cases := []struct {
		tool string
		kind string
		call func(t *testing.T, kinds []string) gateCall
	}{
		{agent.ToolDraftNote, agent.GateAgentNote, func(t *testing.T, kinds []string) gateCall {
			w := newNoteWorld(t, map[string]bool{"matters.edit": true})
			_, err := w.reg.Call(scope("run-note", kinds), agentP("run-note", "matters.edit"), agent.ToolDraftNote, json.RawMessage(noteInput(noteMatter, "")))
			return gateCall{err, gatesOpened(w.gates), len(w.notes.notes)}
		}},
		{agent.ToolDraftEmail, agent.GateOutgoingEmail, func(t *testing.T, kinds []string) gateCall {
			w := newEmailWorld(t, map[string]bool{"documents.edit": true})
			in := emailInput(emailMatter, []string{emailClient, emailOpposing}, `,"submit":true`)
			_, err := w.reg.Call(scope("run-email", kinds), agentP("run-email", "documents.edit"), agent.ToolDraftEmail, json.RawMessage(in))
			return gateCall{err, gatesOpened(w.gates), len(w.drafts.drafts)}
		}},
		{agent.ToolDraftBill, agent.GatePrebillReview, func(t *testing.T, kinds []string) gateCall {
			w := newDraftBillWorld(t, map[string]bool{billCapName: true})
			_, err := w.reg.Call(scope(draftBillRun, kinds), agentP(draftBillRun, billCapName), agent.ToolDraftBill, json.RawMessage(billInput(draftBillMatter)))
			return gateCall{err, *w.opened, 0}
		}},
		{agent.ToolProposeTimeEntries, agent.GateTimeAccept, func(t *testing.T, kinds []string) gateCall {
			w := newTimeWorld(t, map[string]bool{timeCapName: true})
			in := timeInput(timeEntry(timeMatter, "2026-10-09", 30))
			_, err := w.reg.Call(scope(timeRun, kinds), agentP(timeRun, timeCapName), agent.ToolProposeTimeEntries, json.RawMessage(in))
			return gateCall{err, gatesOpened(w.gates), 0}
		}},
		{agent.ToolProposeDeadline, agent.GateCalConfirm, func(t *testing.T, kinds []string) gateCall {
			w := newDeadlineWorld(t, map[string]bool{dlCap: true})
			in := deadlineInput(dlMatter, dlDue, map[string]any{"rule_id": "rcp-30.03", "trigger_date": dlTrigger})
			_, err := w.reg.Call(scope(dlRun, kinds), agentP(dlRun, dlCap), agent.ToolProposeDeadline, json.RawMessage(in))
			return gateCall{err, gatesOpened(w.gates), len(w.deadlines.saved)}
		}},
		{agent.ToolProposePaymentAllocation, agent.GatePaymentAllocation, func(t *testing.T, kinds []string) gateCall {
			w := newPayWorld(t, map[string]bool{payCapName: true})
			_, err := w.reg.Call(scope(payRun, kinds), agentP(payRun, payCapName), agent.ToolProposePaymentAllocation, json.RawMessage(payInput(payOK, toBill(6000))))
			return gateCall{err, *w.opened, 0}
		}},
		{agent.ToolSubmitForApproval, sfKind, func(t *testing.T, kinds []string) gateCall {
			w := newSubmitWorld(t, map[string]bool{agenttools.CapAgentAsk: true})
			_, err := w.reg.Call(scope(sfRun, kinds, sfDraft), agentP(sfRun, agenttools.CapAgentAsk), agent.ToolSubmitForApproval, json.RawMessage(sfInput(sfDraft, sfKind)))
			return gateCall{err, gatesOpened(w.gates), 0}
		}},
		{agent.ToolPrepareTrustWithdrawal, agent.GateTrustWithdrawal, func(t *testing.T, kinds []string) gateCall {
			reg, wd := newTrustWithdrawalRegistry(t)
			res, err := reg.Call(scope("run-1", kinds), agentP("run-1", agent.CapTrustRequest), agent.ToolPrepareTrustWithdrawal, json.RawMessage(withdrawalCall("run-1", matterA, 5_000, "").Input))
			gates := 0
			if res.GateID != "" {
				gates = 1
			}
			return gateCall{err, gates, wd.requests}
		}},
		{agent.ToolProposeConflictParties, conflict.GateClearance, func(t *testing.T, kinds []string) gateCall {
			w := newConflictWorld(t, map[string]bool{cpCap: true})
			ctx := scope(cpRun, kinds)
			_, err := w.reg.Call(ctx, agentP(cpRun, cpCap), agent.ToolProposeConflictParties, json.RawMessage(cpInput(cpCheck, cpParty("Synth Holdings", ""))))
			runs, rerr := w.checks.Runs(ctx, cpMatter)
			if rerr != nil {
				t.Fatalf("runs: %v", rerr)
			}
			return gateCall{err, len(runs), len(w.parties.parties)}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.tool+"/declared", func(t *testing.T) {
			got := tc.call(t, []string{tc.kind})
			if got.err != nil || got.gates == 0 {
				t.Fatalf("declared %s: err = %v, gates = %d; want the gate opened", tc.kind, got.err, got.gates)
			}
		})
		for name, kinds := range map[string][]string{"other kind": {"unrelated.kind"}, "no kinds": nil} {
			t.Run(tc.tool+"/"+name, func(t *testing.T) {
				// rule: ARC-I-035
				got := tc.call(t, kinds)
				if !errors.Is(got.err, agenttools.ErrGateKindNotAllowed) {
					t.Fatalf("err = %v, want ErrGateKindNotAllowed", got.err)
				}
				if got.gates != 0 || got.rows != 0 {
					t.Fatalf("refused call opened %d gates and stored %d rows, want none", got.gates, got.rows)
				}
			})
		}
	}
}

// newTrustWithdrawalRegistry registers prepare_trust_withdrawal, built through its handler over
// newWriteTools, in a registry where run-1 reported a plan.
func newTrustWithdrawalRegistry(t *testing.T) (*agenttools.Registry, *countingWithdrawals) {
	t.Helper()
	wt, wd := newWriteTools(t)
	tool, err := agent.PrepareTrustWithdrawalHandler().Build(agent.NewDeps(wt))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	plans := agenttoolsfake.New()
	if _, err := plans.ReportPlan(context.Background(), "run-1", []string{"prepare the fee transfer"}); err != nil {
		t.Fatalf("plan: %v", err)
	}
	reg := agenttools.NewRegistry(plans)
	if err := reg.Register(tool); err != nil {
		t.Fatalf("register: %v", err)
	}
	return reg, wd
}

// rule: ARC-I-035
// Guard: in this package, every GateOpener an agent tool reads from Deps is used only as the
// argument of agenttools.ScopedOpener. A tool that keeps or calls the unscoped opener fails.
func TestARC_I_035_NoUnscopedGateOpener(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	sites := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			for _, obj := range openerVars(fn.Body) {
				sites++
				for _, use := range unscopedUses(fn.Body, obj) {
					t.Errorf("%s: %s uses the unscoped GateOpener %q; wrap it in agenttools.ScopedOpener", fset.Position(use), fn.Name.Name, obj.Name)
				}
			}
		}
	}
	// draft_note, draft_email, draft_bill, propose_time_entries, propose_deadline,
	// propose_payment_allocation and submit_for_approval read a GateOpener.
	if sites < 7 {
		t.Fatalf("found %d GateOpener reads, want at least 7: the guard no longer sees the tools", sites)
	}
}

// rule: ARC-I-035
// Guard: the trust module opens trust.withdrawal, not a GateOpener, so the opener guard cannot
// see prepare_trust_withdrawal. In this package, every call of a *WriteTools read from Deps
// must come after an agenttools.CheckGateKind call in the same function literal.
func TestARC_I_035_WriteToolsCallScoped(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	calls := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			vars := depVars(fn.Body, func(e ast.Expr) bool {
				star, ok := e.(*ast.StarExpr)
				if !ok {
					return false
				}
				id, ok := star.X.(*ast.Ident)
				return ok && id.Name == "WriteTools"
			})
			for _, obj := range vars {
				for _, c := range methodCalls(fn.Body, obj, "Call") {
					calls++
					if !checkedBefore(fn.Body, c) {
						t.Errorf("%s: %s calls %s.Call without agenttools.CheckGateKind first", fset.Position(c.Pos()), fn.Name.Name, obj.Name)
					}
				}
			}
		}
	}
	// prepare_trust_withdrawal's handler calls WriteTools.Call.
	if calls < 1 {
		t.Fatalf("found %d WriteTools calls, want at least 1: the guard no longer sees the tool", calls)
	}
}

// depVars returns the variables assigned from DepOf[T](...) in body where index(T) holds.
func depVars(body *ast.BlockStmt, index func(ast.Expr) bool) []*ast.Object {
	var out []*ast.Object
	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Rhs) != 1 || len(as.Lhs) == 0 {
			return true
		}
		call, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		idx, ok := call.Fun.(*ast.IndexExpr)
		if !ok || !index(idx.Index) {
			return true
		}
		if id, ok := as.Lhs[0].(*ast.Ident); ok && id.Obj != nil {
			out = append(out, id.Obj)
		}
		return true
	})
	return out
}

// methodCalls returns the calls obj.method(...) in body.
func methodCalls(body *ast.BlockStmt, obj *ast.Object, method string) []*ast.CallExpr {
	var out []*ast.CallExpr
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != method {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Obj == obj {
			out = append(out, call)
		}
		return true
	})
	return out
}

// checkedBefore reports whether the innermost function literal of body that holds c calls
// agenttools.CheckGateKind before c.
func checkedBefore(body *ast.BlockStmt, c *ast.CallExpr) bool {
	var scope ast.Node = body
	ast.Inspect(body, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok && lit.Pos() <= c.Pos() && c.End() <= lit.End() {
			scope = lit
		}
		return true
	})
	found := false
	ast.Inspect(scope, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && isSelector(call.Fun, "agenttools", "CheckGateKind") && call.Pos() < c.Pos() {
			found = true
		}
		return true
	})
	return found
}

// openerVars returns the variables assigned from DepOf[agenttools.GateOpener](...) in body.
func openerVars(body *ast.BlockStmt) []*ast.Object {
	return depVars(body, func(e ast.Expr) bool { return isSelector(e, "agenttools", "GateOpener") })
}

// unscopedUses returns the positions where obj is used other than as the sole argument of
// agenttools.ScopedOpener or in a nil check. Composite-literal keys that share its name are
// field names, not uses.
func unscopedUses(body *ast.BlockStmt, obj *ast.Object) []token.Pos {
	scoped := map[*ast.Ident]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			if isSelector(n.Fun, "agenttools", "ScopedOpener") && len(n.Args) == 1 {
				if id, ok := n.Args[0].(*ast.Ident); ok {
					scoped[id] = true
				}
			}
		case *ast.KeyValueExpr:
			if id, ok := n.Key.(*ast.Ident); ok {
				scoped[id] = true
			}
		case *ast.BinaryExpr:
			if y, ok := n.Y.(*ast.Ident); ok && y.Name == "nil" && (n.Op == token.EQL || n.Op == token.NEQ) {
				if id, ok := n.X.(*ast.Ident); ok {
					scoped[id] = true
				}
			}
		}
		return true
	})
	var out []token.Pos
	ast.Inspect(body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || id.Obj != obj || id.Obj.Decl == nil {
			return true
		}
		if as, ok := obj.Decl.(*ast.AssignStmt); ok && len(as.Lhs) > 0 && as.Lhs[0] == id {
			return true // the definition
		}
		if !scoped[id] {
			out = append(out, id.Pos())
		}
		return true
	})
	return out
}

func isSelector(e ast.Expr, pkg, name string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	return ok && x.Name == pkg
}
