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

	"github.com/dagi377/counsel/kernel/registry"
)

const openAPIPath = "../../../../api/openapi.yaml"

// rule: BIL-C-120
// No HTTP route sets a bill to approved: approval goes through the gate decision route, and
// in this package only applyApproval writes StatusApproved, called only by the G-B2 effect.
func TestBIL_C_120_OnlyGateApproves(t *testing.T) {
	t.Run("no bill route approves", func(t *testing.T) {
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
		bills := 0
		for path, ops := range doc.Paths {
			if !strings.HasPrefix(path, "/bills") {
				continue
			}
			bills++
			if strings.Contains(path, "approv") {
				t.Errorf("route %s approves a bill outside the gate", path)
			}
			for method, op := range ops {
				m, _ := op.(map[string]any)
				if id, _ := m["operationId"].(string); strings.Contains(strings.ToLower(id), "approve") {
					t.Errorf("%s %s operation %s approves a bill outside the gate", method, path, id)
				}
			}
		}
		if bills == 0 {
			t.Fatal("no /bills routes found; the contract moved")
		}
		patch, ok := doc.Components.Schemas["BillPatch"]
		if !ok {
			t.Fatal("BillPatch schema not found")
		}
		for _, f := range []string{"status", "number", "approved_by", "approved_at", "approval_id", "issue_date"} {
			if _, ok := patch.Properties[f]; ok {
				t.Errorf("BillPatch lets a client write %s", f)
			}
		}
	})

	t.Run("only the effect applies approval", func(t *testing.T) {
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
							if isIdent(rhs, "StatusApproved") && fn.Name.Name != "applyApproval" {
								t.Errorf("%s: %s assigns StatusApproved", fset.Position(x.Pos()), fn.Name.Name)
							}
						}
					case *ast.KeyValueExpr:
						if isIdent(x.Key, "Status") && isIdent(x.Value, "StatusApproved") {
							t.Errorf("%s: %s builds an approved bill", fset.Position(x.Pos()), fn.Name.Name)
						}
					case *ast.CallExpr:
						if isIdent(x.Fun, "applyApproval") && name != "approve_effect.go" {
							t.Errorf("%s: %s calls applyApproval outside the gate effect", fset.Position(x.Pos()), fn.Name.Name)
						}
					}
					return true
				})
			}
		}
	})
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

type memStore struct {
	bills map[ID]Bill
	saved int
}

func (m *memStore) Lock(_ context.Context, _ pgx.Tx, id ID) (Bill, error) {
	b, ok := m.bills[id]
	if !ok {
		return Bill{}, ErrNotFound
	}
	return b, nil
}

func (m *memStore) SaveApproval(_ context.Context, _ pgx.Tx, b Bill) error {
	m.bills[b.ID] = b
	m.saved++
	return nil
}

type memFeesBook []FeesBookEntry

func (m *memFeesBook) Append(_ context.Context, _ pgx.Tx, e FeesBookEntry) error {
	*m = append(*m, e)
	return nil
}

// rule: BIL-C-120
func TestApproveEffect(t *testing.T) {
	// 2026-09-30 23:30 Toronto: the bill date and number year are 2026, September.
	now := time.Date(2026, 10, 1, 3, 30, 0, 0, time.UTC)
	user := func(id string) registry.Principal {
		return registry.Principal{Kind: registry.PrincipalUser, UserID: id}
	}
	payload := func(total int64) json.RawMessage {
		raw, _ := json.Marshal(ApprovalPayload{BillID: "b1", MatterID: "m1", TotalCents: total, DeliveryMethod: "email"})
		return raw
	}
	gate := func(f func(*registry.Approved)) registry.Approved {
		g := registry.Approved{GateID: "g1", Kind: GateBillApproval, Payload: payload(113000),
			Preparer: user("u1"), Approvers: []registry.Principal{user("u2")}}
		if f != nil {
			f(&g)
		}
		return g
	}
	tests := []struct {
		name   string
		g      registry.Approved
		status Status // bill status before
		err    error
		number string // "" = bill not approved by this call
	}{
		{"approves with next number", gate(nil), StatusPendingApproval, nil, "INV-2026-0147"},
		{"agent approver refused", gate(func(g *registry.Approved) {
			g.Approvers = []registry.Principal{{Kind: registry.PrincipalAgent, RunID: "r1", ActingFor: "u2"}}
		}), StatusPendingApproval, ErrAgentApproves, ""},
		{"preparer approver refused", gate(func(g *registry.Approved) { g.Approvers = []registry.Principal{user("u1")} }),
			StatusPendingApproval, ErrPreparerApproves, ""},
		{"agent's acting-for user is the preparer", gate(func(g *registry.Approved) {
			g.Preparer = registry.Principal{Kind: registry.PrincipalAgent, RunID: "r1", ActingFor: "u2"}
		}), StatusPendingApproval, ErrPreparerApproves, ""},
		{"payload total differs", gate(func(g *registry.Approved) { g.Payload = payload(1) }), StatusPendingApproval, ErrInvalid, ""},
		{"other gate kind", gate(func(g *registry.Approved) { g.Kind = "billing.bill_void" }), StatusPendingApproval, ErrInvalid, ""},
		{"draft not pending", gate(nil), StatusDraft, ErrNotPending, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := pendingBill()
			b.Status = tc.status
			store := &memStore{bills: map[ID]Bill{"b1": b}}
			var book memFeesBook
			issued := 0
			e := &ApproveEffect{Store: store, FeesBook: &book, Prefix: "INV", Now: func() time.Time { return now },
				Next: func(_ context.Context, _ pgx.Tx, prefix string, year int) (string, error) {
					issued++
					return FormatNumber(prefix, year, 146+issued)
				}}
			err := e.Apply(context.Background(), nil, tc.g)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if tc.err != nil {
				if store.saved != 0 || len(book) != 0 {
					t.Fatalf("failed effect wrote: saved %d, fees book %d", store.saved, len(book))
				}
				return
			}
			got := store.bills["b1"]
			if got.Status != StatusApproved || got.Number != tc.number || got.ApprovedBy != "u2" || got.ApprovalID != "g1" {
				t.Fatalf("bill = %+v", got)
			}
			if got.IssueDate.In(business).Format(time.DateOnly) != "2026-09-30" {
				t.Fatalf("issue date = %v", got.IssueDate)
			}
			if len(book) != 1 || book[0].Number != tc.number || book[0].Status != FeesBookIssued {
				t.Fatalf("fees book = %+v", book)
			}
			// Replaying the same gate is a no-op: no new number, no second row.
			if err := e.Apply(context.Background(), nil, tc.g); err != nil {
				t.Fatalf("replay: %v", err)
			}
			if issued != 1 || store.saved != 1 || len(book) != 1 {
				t.Fatalf("replay wrote again: issued %d saved %d rows %d", issued, store.saved, len(book))
			}
		})
	}
}
