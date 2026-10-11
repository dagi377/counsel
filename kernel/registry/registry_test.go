package registry

import (
	"context"
	"io/fs"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/go-chi/chi/v5"
)

// TestRegistryMatchesK1 pins the Registry method set and signatures to spec/eng/k1-kernel.html.
func TestRegistryMatchesK1(t *testing.T) {
	want := map[string]string{
		"Capability":      "func(registry.Capability)",
		"Role":            "func(registry.Role)",
		"SubjectKind":     "func(registry.SubjectKind, registry.SubjectResolver)",
		"GateKind":        "func(registry.GateKind)",
		"EventType":       "func(string, []uint8)",
		"InboundProvider": "func(registry.Provider)",
		"RulePack":        "func(fs.FS)",
		"DocumentKind":    "func(registry.DocumentKind)",
		"RetentionClass":  "func(registry.RetentionClass)",
		"Report":          "func(registry.ReportDef)",
		"PortalPage":      "func(registry.PortalPage)",
		"Tool":            "func(registry.Tool)",
		"ChartOfAccounts": "func(registry.Chart)",
		"NumberSeries":    "func(string, string)",
		"SmokeCheck":      "func(string, registry.CheckFunc)",
		"RestoreCheck":    "func(string, registry.CheckFunc)",
		"Routes":          "func(func(chi.Router))",
	}
	typ := reflect.TypeOf((*Registry)(nil)).Elem()
	if typ.NumMethod() != len(want) {
		t.Errorf("Registry has %d methods, k1 lists %d", typ.NumMethod(), len(want))
	}
	for i := 0; i < typ.NumMethod(); i++ {
		m := typ.Method(i)
		sig, ok := want[m.Name]
		if !ok {
			t.Errorf("method %s is not in k1", m.Name)
			continue
		}
		if got := m.Type.String(); got != sig {
			t.Errorf("%s: signature %s, want %s", m.Name, got, sig)
		}
	}
}

// TestContractHeaders checks both contract files carry a CONTRACT header.
func TestContractHeaders(t *testing.T) {
	for _, name := range []string{"registry.go", "types.go"} {
		t.Run(name, func(t *testing.T) {
			b, err := os.ReadFile(name)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if !strings.HasPrefix(string(b), "// CONTRACT:") {
				t.Errorf("%s: first line is not a CONTRACT header", name)
			}
		})
	}
}

type allowAll struct{}

func (allowAll) Visible(context.Context, Principal, Subject) (bool, error) { return true, nil }

func TestRecorderCapturesEveryCall(t *testing.T) {
	check := CheckFunc(func(context.Context) error { return nil })
	pack := fstest.MapFS{"pack.yaml": {Data: []byte("id: x")}}
	tests := []struct {
		method string
		call   func(Registry)
		args   int
	}{
		{MethodCapability, func(r Registry) { r.Capability(Capability{ID: "desk.read"}) }, 1},
		{MethodRole, func(r Registry) { r.Role(Role{ID: "desk.agent"}) }, 1},
		{MethodSubjectKind, func(r Registry) { r.SubjectKind(SubjectKind{ID: "desk.ticket"}, allowAll{}) }, 2},
		{MethodGateKind, func(r Registry) { r.GateKind(GateKind{Slug: "desk.refund", Risk: RiskHigh, MinApprovers: 1}) }, 1},
		{MethodEventType, func(r Registry) { r.EventType("desk.ticket_opened", []byte(`{}`)) }, 2},
		{MethodInboundProvider, func(r Registry) { r.InboundProvider(Provider{ID: "mail"}) }, 1},
		{MethodRulePack, func(r Registry) { r.RulePack(pack) }, 1},
		{MethodDocumentKind, func(r Registry) { r.DocumentKind(DocumentKind{ID: "desk.attachment"}) }, 1},
		{MethodRetentionClass, func(r Registry) { r.RetentionClass(RetentionClass{ID: "desk.short", MinYears: 1}) }, 1},
		{MethodReport, func(r Registry) { r.Report(ReportDef{ID: "desk.backlog"}) }, 1},
		{MethodPortalPage, func(r Registry) { r.PortalPage(PortalPage{ID: "desk.status", Route: "/status"}) }, 1},
		{MethodTool, func(r Registry) { r.Tool(Tool{Name: "desk_search", Kind: ToolRead}) }, 1},
		{MethodChartOfAccounts, func(r Registry) {
			r.ChartOfAccounts(Chart{Book: "desk", Accounts: []Account{{Code: "1000", Name: "Cash", Type: AccountAsset}}})
		}, 1},
		{MethodNumberSeries, func(r Registry) { r.NumberSeries("desk.ticket", "T-{seq:6}") }, 2},
		{MethodSmokeCheck, func(r Registry) { r.SmokeCheck("desk.ping", check) }, 2},
		{MethodRestoreCheck, func(r Registry) { r.RestoreCheck("desk.rows", check) }, 2},
		{MethodRoutes, func(r Registry) { r.Routes(func(chi.Router) {}) }, 1},
	}

	rec := &Recorder{}
	for _, tc := range tests {
		t.Run(tc.method, func(t *testing.T) {
			tc.call(rec)
			got := rec.Method(tc.method)
			if len(got) != 1 {
				t.Fatalf("recorded %d calls, want 1", len(got))
			}
			if len(got[0].Args) != tc.args {
				t.Errorf("recorded %d args, want %d", len(got[0].Args), tc.args)
			}
		})
	}

	// Every interface method is covered, and calls are kept in order.
	calls := rec.Calls()
	if len(calls) != len(tests) {
		t.Fatalf("Calls() has %d entries, want %d", len(calls), len(tests))
	}
	var covered []string
	for i, c := range calls {
		if c.Method != tests[i].method {
			t.Errorf("call %d is %s, want %s", i, c.Method, tests[i].method)
		}
		covered = append(covered, c.Method)
	}
	var all []string
	typ := reflect.TypeOf((*Registry)(nil)).Elem()
	for i := 0; i < typ.NumMethod(); i++ {
		all = append(all, typ.Method(i).Name)
	}
	sort.Strings(covered)
	sort.Strings(all)
	if !reflect.DeepEqual(covered, all) {
		t.Errorf("recorder test covers %v, interface has %v", covered, all)
	}

	// Recorded values round-trip.
	if c := rec.Method(MethodCapability)[0].Args[0].(Capability); c.ID != "desk.read" {
		t.Errorf("Capability arg = %+v", c)
	}
	if f, ok := rec.Method(MethodRulePack)[0].Args[0].(fs.FS); !ok || f == nil {
		t.Errorf("RulePack arg not an fs.FS")
	}
	// Calls returns a copy.
	calls[0].Method = "changed"
	if rec.Calls()[0].Method != MethodCapability {
		t.Errorf("Calls() exposed internal state")
	}
}
