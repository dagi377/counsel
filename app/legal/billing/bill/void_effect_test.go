package bill

import (
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"gopkg.in/yaml.v3"

	"github.com/dagi377/counsel/kernel/ledger"
	"github.com/dagi377/counsel/kernel/registry"
)

type voidMemStore struct {
	bills map[ID]Bill
	saved []Bill
}

func (m *voidMemStore) Lock(_ context.Context, _ pgx.Tx, id ID) (Bill, error) {
	b, ok := m.bills[id]
	if !ok {
		return Bill{}, ErrNotFound
	}
	return b, nil
}

func (m *voidMemStore) SaveVoid(_ context.Context, _ pgx.Tx, b Bill) error {
	m.bills[b.ID] = b
	m.saved = append(m.saved, b)
	return nil
}

type memApprovalEntries map[ID]ledger.EntryID

func (m memApprovalEntries) ApprovalEntry(_ context.Context, _ pgx.Tx, id ID) (ledger.EntryID, error) {
	e, ok := m[id]
	if !ok {
		return "", ledger.ErrNotFound
	}
	return e, nil
}

type memReverser struct{ reversed []ledger.EntryID }

func (m *memReverser) Reverse(_ context.Context, _ pgx.Tx, id ledger.EntryID, reason string) (ledger.EntryID, error) {
	if reason == "" {
		return "", ledger.ErrInvalidEntry
	}
	m.reversed = append(m.reversed, id)
	return "rev-" + id, nil
}

type memVoidedSink struct{ plans []VoidPlan }

func (m *memVoidedSink) Voided(_ context.Context, _ pgx.Tx, plan VoidPlan, _ string) error {
	m.plans = append(m.plans, plan)
	return nil
}

func TestBIL_C_084_VoidEffect(t *testing.T) {
	// 2026-10-08 01:30 UTC is 2026-10-07 in Toronto.
	now := time.Date(2026, 10, 8, 1, 30, 0, 0, time.UTC)
	user := func(id string) registry.Principal {
		return registry.Principal{Kind: registry.PrincipalUser, UserID: id}
	}
	agent := registry.Principal{Kind: registry.PrincipalAgent, ActingFor: "u-clerk"}
	payload := func(id, reason string) json.RawMessage {
		raw, err := json.Marshal(VoidPayload{BillID: id, Reason: reason})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	tests := []struct {
		name      string
		bill      Bill
		gate      registry.Approved
		err       error
		reversed  bool
		wantState Status
	}{
		// rule: BIL-C-084
		{name: "approved undelivered is voided with an exact reversal",
			bill: voidTestBill(StatusApproved, false),
			gate: registry.Approved{GateID: "g-void", Kind: GateBillVoid, Payload: payload("b-150", "duplicate"),
				Preparer: user("u-clerk"), Approvers: []registry.Principal{user("u-lawyer")}},
			reversed: true, wantState: StatusVoid},
		{name: "delivered bill uses a credit note",
			bill: voidTestBill(StatusDelivered, true),
			gate: registry.Approved{GateID: "g-void", Kind: GateBillVoid, Payload: payload("b-150", "duplicate"),
				Preparer: user("u-clerk"), Approvers: []registry.Principal{user("u-lawyer")}},
			err: ErrUseCreditNote, wantState: StatusDelivered},
		{name: "bill preparer cannot decide the void",
			bill: voidTestBill(StatusApproved, false),
			gate: registry.Approved{GateID: "g-void", Kind: GateBillVoid, Payload: payload("b-150", "duplicate"),
				Preparer: user("u-clerk"), Approvers: []registry.Principal{user("u-prep")}},
			err: ErrPreparerApproves, wantState: StatusApproved},
		{name: "requester cannot decide the void",
			bill: voidTestBill(StatusApproved, false),
			gate: registry.Approved{GateID: "g-void", Kind: GateBillVoid, Payload: payload("b-150", "duplicate"),
				Preparer: user("u-lawyer"), Approvers: []registry.Principal{user("u-lawyer")}},
			err: ErrPreparerApproves, wantState: StatusApproved},
		{name: "agent's acting-for user cannot decide the void",
			bill: voidTestBill(StatusApproved, false),
			gate: registry.Approved{GateID: "g-void", Kind: GateBillVoid, Payload: payload("b-150", "duplicate"),
				Preparer: agent, Approvers: []registry.Principal{user("u-clerk")}},
			err: ErrPreparerApproves, wantState: StatusApproved},
		{name: "agent never decides",
			bill: voidTestBill(StatusApproved, false),
			gate: registry.Approved{GateID: "g-void", Kind: GateBillVoid, Payload: payload("b-150", "duplicate"),
				Preparer: user("u-clerk"), Approvers: []registry.Principal{{Kind: registry.PrincipalAgent, ActingFor: "u-lawyer"}}},
			err: ErrPreparerApproves, wantState: StatusApproved},
		{name: "pending bill is not voided through the gate",
			bill: voidTestBill(StatusPendingApproval, false),
			gate: registry.Approved{GateID: "g-void", Kind: GateBillVoid, Payload: payload("b-150", "duplicate"),
				Preparer: user("u-clerk"), Approvers: []registry.Principal{user("u-lawyer")}},
			err: ErrNotApproved, wantState: StatusPendingApproval},
		{name: "wrong gate kind",
			bill: voidTestBill(StatusApproved, false),
			gate: registry.Approved{GateID: "g-void", Kind: GateBillApproval, Payload: payload("b-150", "duplicate"),
				Preparer: user("u-clerk"), Approvers: []registry.Principal{user("u-lawyer")}},
			err: ErrInvalid, wantState: StatusApproved},
		{name: "payload without reason",
			bill: voidTestBill(StatusApproved, false),
			gate: registry.Approved{GateID: "g-void", Kind: GateBillVoid, Payload: payload("b-150", ""),
				Preparer: user("u-clerk"), Approvers: []registry.Principal{user("u-lawyer")}},
			err: ErrInvalid, wantState: StatusApproved},
		{name: "no approver",
			bill: voidTestBill(StatusApproved, false),
			gate: registry.Approved{GateID: "g-void", Kind: GateBillVoid, Payload: payload("b-150", "duplicate"),
				Preparer: user("u-clerk")},
			err: ErrInvalid, wantState: StatusApproved},
		{name: "unknown bill",
			bill: voidTestBill(StatusApproved, false),
			gate: registry.Approved{GateID: "g-void", Kind: GateBillVoid, Payload: payload("b-404", "duplicate"),
				Preparer: user("u-clerk"), Approvers: []registry.Principal{user("u-lawyer")}},
			err: ErrNotFound, wantState: StatusApproved},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.bill.IssueDate = time.Date(2026, 10, 6, 0, 0, 0, 0, business)
			store := &voidMemStore{bills: map[ID]Bill{tt.bill.ID: tt.bill}}
			rev := &memReverser{}
			sink := &memVoidedSink{}
			e := &VoidEffect{Store: store, Entries: memApprovalEntries{"b-150": "je-approval"}, Ledger: rev, Sink: sink,
				Now: func() time.Time { return now }}
			err := VoidEffects(e)[GateBillVoid](context.Background(), nil, tt.gate)
			if !errors.Is(err, tt.err) {
				t.Fatalf("Apply err = %v, want %v", err, tt.err)
			}
			got := store.bills[tt.bill.ID]
			if got.Status != tt.wantState {
				t.Fatalf("status = %s, want %s", got.Status, tt.wantState)
			}
			if got.Number != tt.bill.Number {
				t.Fatalf("number = %q, want kept %q", got.Number, tt.bill.Number)
			}
			if !tt.reversed {
				if len(rev.reversed) != 0 || len(store.saved) != 0 || len(sink.plans) != 0 {
					t.Fatalf("failed void wrote: reversed %v saved %d sink %d", rev.reversed, len(store.saved), len(sink.plans))
				}
				return
			}
			if len(rev.reversed) != 1 || rev.reversed[0] != "je-approval" {
				t.Fatalf("reversed = %v, want the approval entry once", rev.reversed)
			}
			if len(sink.plans) != 1 {
				t.Fatalf("sink got %d plans, want 1", len(sink.plans))
			}
			p := sink.plans[0]
			if want := time.Date(2026, 10, 7, 0, 0, 0, 0, business); !p.ReverseOn.Equal(want) {
				t.Fatalf("reverse on %v, want the Toronto void date %v", p.ReverseOn, want)
			}
			if strings.Join(p.TimeEntryIDs, ",") != "te-1,te-2" || strings.Join(p.ExpenseIDs, ",") != "ex-1" {
				t.Fatalf("freed entries = %v %v", p.TimeEntryIDs, p.ExpenseIDs)
			}
		})
	}
}

func TestBIL_C_084_VoidEffectMissingApprovalEntry(t *testing.T) {
	b := voidTestBill(StatusApproved, false)
	store := &voidMemStore{bills: map[ID]Bill{b.ID: b}}
	rev := &memReverser{}
	e := &VoidEffect{Store: store, Entries: memApprovalEntries{}, Ledger: rev,
		Now: func() time.Time { return time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC) }}
	raw, err := json.Marshal(VoidPayload{BillID: string(b.ID), Reason: "duplicate"})
	if err != nil {
		t.Fatal(err)
	}
	err = e.Apply(context.Background(), nil, registry.Approved{GateID: "g", Kind: GateBillVoid, Payload: raw,
		Preparer:  registry.Principal{Kind: registry.PrincipalUser, UserID: "u-clerk"},
		Approvers: []registry.Principal{{Kind: registry.PrincipalUser, UserID: "u-lawyer"}}})
	if !errors.Is(err, ledger.ErrNotFound) {
		t.Fatalf("Apply err = %v, want ledger.ErrNotFound", err)
	}
	if len(store.saved) != 0 || len(rev.reversed) != 0 {
		t.Fatalf("void without an approval entry wrote: saved %d reversed %v", len(store.saved), rev.reversed)
	}
}

// rule: BIL-C-084
// Void runs only through the billing.bill_void gate: the route only opens the gate, and
// no code but planVoid builds a void status.
func TestBIL_C_084_OnlyGateVoids(t *testing.T) {
	t.Run("void route only opens the gate", func(t *testing.T) {
		raw, err := os.ReadFile(openAPIPath)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Paths      map[string]map[string]any `yaml:"paths"`
			Components struct {
				Schemas map[string]struct {
					Properties map[string]any `yaml:"properties"`
				} `yaml:"schemas"`
			} `yaml:"components"`
		}
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		op, ok := doc.Paths["/bills/{bill_id}/void"]["post"].(map[string]any)
		if !ok {
			t.Fatal("POST /bills/{bill_id}/void not found; the contract moved")
		}
		resp, _ := op["responses"].(map[string]any)
		for code := range resp {
			if strings.HasPrefix(code, "2") && code != "202" {
				t.Errorf("void route answers %s; it may only open the gate (202)", code)
			}
		}
		if _, ok := resp["202"]; !ok {
			t.Error("void route does not answer 202 Gate")
		}
		if _, ok := doc.Components.Schemas["BillPatch"].Properties["status"]; ok {
			t.Error("BillPatch lets a client write status")
		}
	})

	t.Run("only planVoid builds a void bill", func(t *testing.T) {
		files, err := filepath.Glob("*.go")
		if err != nil {
			t.Fatal(err)
		}
		fset := token.NewFileSet()
		for _, name := range files {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, name, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range f.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok {
					continue
				}
				ast.Inspect(fn, func(n ast.Node) bool {
					switch x := n.(type) {
					case *ast.AssignStmt:
						for _, rhs := range x.Rhs {
							if isIdent(rhs, "StatusVoid") {
								t.Errorf("%s: %s assigns StatusVoid", fset.Position(x.Pos()), fn.Name.Name)
							}
						}
					case *ast.KeyValueExpr:
						if isIdent(x.Key, "Status") && isIdent(x.Value, "StatusVoid") && fn.Name.Name != "planVoid" {
							t.Errorf("%s: %s builds a void bill", fset.Position(x.Pos()), fn.Name.Name)
						}
					case *ast.CallExpr:
						if isIdent(x.Fun, "planVoid") && name != "void_effect.go" {
							t.Errorf("%s: %s plans a void outside the gate effect", fset.Position(x.Pos()), fn.Name.Name)
						}
					}
					return true
				})
			}
		}
	})
}
