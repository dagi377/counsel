package agenttools_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	"github.com/dagi377/counsel/kernel/registry"
)

const anyObject = `{"type":"object"}`

// provenanceRegistry registers get_balance and compute_due (read) and propose_payment (write,
// provenance on /amount_cents, /due_on and /lines/*/amount_cents) for run r1 with a plan.
func provenanceRegistry(t *testing.T, pv *agenttools.Provenance, handled *int) *agenttools.Registry {
	t.Helper()
	ctx := context.Background()
	plans := agenttoolsfake.New()
	if _, err := plans.ReportPlan(ctx, "r1", []string{"propose payment"}); err != nil {
		t.Fatalf("ReportPlan: %v", err)
	}
	r := agenttools.NewRegistry(plans)
	read := func(name, out string) {
		tool, err := pv.ReadTool(agenttools.Tool{Name: name, Kind: registry.ToolRead, Input: []byte(anyObject),
			Handle: func(context.Context, agenttools.Principal, json.RawMessage) (agenttools.ToolResult, error) {
				return agenttools.ToolResult{Output: json.RawMessage(out)}, nil
			}})
		if err != nil {
			t.Fatalf("ReadTool %s: %v", name, err)
		}
		if err := r.Register(tool); err != nil {
			t.Fatalf("Register %s: %v", name, err)
		}
	}
	read("get_balance", `{"account":"a1","balance_cents":125000,"as_of":"2026-10-09"}`)
	read("compute_due", `{"due_on":"2026-11-02","days":15}`)
	w, err := pv.SourcedWrite(agenttools.Tool{Name: "propose_payment", Kind: registry.ToolWrite, Input: []byte(anyObject),
		Handle: func(context.Context, agenttools.Principal, json.RawMessage) (agenttools.ToolResult, error) {
			*handled++
			return agenttools.ToolResult{Output: out("proposal", "p1")}, nil
		}}, "/amount_cents", "/due_on", "/lines/*/amount_cents")
	if err != nil {
		t.Fatalf("SourcedWrite: %v", err)
	}
	if err := r.Register(w); err != nil {
		t.Fatalf("Register write: %v", err)
	}
	return r
}

// rule: ARC-U-027
func TestARC_U_027_WriteValuesMustComeFromReadResults(t *testing.T) {
	cases := []struct {
		name  string
		reads []string
		p     agenttools.Principal
		in    string
		ok    bool
	}{
		{"amount equal to balance figure", []string{"get_balance"}, agent("r1"), `{"amount_cents":125000}`, true},
		{"amount as 125000.0 matches", []string{"get_balance"}, agent("r1"), `{"amount_cents":125000.0}`, true},
		{"invented amount", []string{"get_balance"}, agent("r1"), `{"amount_cents":124999}`, false},
		{"amount before any read", nil, agent("r1"), `{"amount_cents":125000}`, false},
		{"due_on equal to computed date", []string{"compute_due"}, agent("r1"), `{"due_on":"2026-11-02"}`, true},
		{"other date", []string{"compute_due"}, agent("r1"), `{"due_on":"2026-11-03"}`, false},
		{"date from another read passes", []string{"get_balance"}, agent("r1"), `{"due_on":"2026-10-09"}`, true},
		{"number never matches a date", []string{"compute_due"}, agent("r1"), `{"due_on":20261102}`, false},
		{"date text in amount field", []string{"get_balance"}, agent("r1"), `{"amount_cents":"125000"}`, false},
		{"array element invented", []string{"get_balance", "compute_due"}, agent("r1"), `{"lines":[{"amount_cents":125000},{"amount_cents":15},{"amount_cents":7}]}`, false},
		{"array elements sourced", []string{"get_balance", "compute_due"}, agent("r1"), `{"lines":[{"amount_cents":125000},{"amount_cents":15}]}`, true},
		{"undeclared field not checked", []string{"get_balance"}, agent("r1"), `{"note_id":42}`, true},
		{"null skipped", []string{"get_balance"}, agent("r1"), `{"amount_cents":null}`, true},
		{"values do not cross runs", []string{"get_balance"}, agent("r2"), `{"amount_cents":125000}`, false},
		{"user manual path not checked", nil, user(), `{"amount_cents":124999}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			pv := agenttools.NewProvenance()
			handled := 0
			r := provenanceRegistry(t, pv, &handled)
			for _, name := range tc.reads {
				if _, err := r.Call(ctx, agent("r1"), name, json.RawMessage(`{}`)); err != nil {
					t.Fatalf("read %s: %v", name, err)
				}
			}
			if tc.p.RunID == "r2" {
				// r2 needs its own plan to reach the provenance check.
				r = provenanceRegistryForRun(t, pv, &handled, "r2")
			}
			_, err := r.Call(ctx, tc.p, "propose_payment", json.RawMessage(tc.in))
			if tc.ok {
				if err != nil {
					t.Fatalf("Call: %v", err)
				}
				if handled != 1 {
					t.Fatalf("handler ran %d times, want 1", handled)
				}
				return
			}
			if !errors.Is(err, agenttools.ErrUnsourcedValue) {
				t.Fatalf("Call err = %v, want ErrUnsourcedValue", err)
			}
			if handled != 0 {
				t.Fatalf("handler ran %d times before the provenance check failed", handled)
			}
		})
	}
}

// provenanceRegistryForRun is provenanceRegistry with a plan for runID, sharing pv.
func provenanceRegistryForRun(t *testing.T, pv *agenttools.Provenance, handled *int, runID string) *agenttools.Registry {
	t.Helper()
	plans := agenttoolsfake.New()
	if _, err := plans.ReportPlan(context.Background(), runID, []string{"propose payment"}); err != nil {
		t.Fatalf("ReportPlan: %v", err)
	}
	r := agenttools.NewRegistry(plans)
	w, err := pv.SourcedWrite(agenttools.Tool{Name: "propose_payment", Kind: registry.ToolWrite, Input: []byte(anyObject),
		Handle: func(context.Context, agenttools.Principal, json.RawMessage) (agenttools.ToolResult, error) {
			*handled++
			return agenttools.ToolResult{Output: out("proposal", "p1")}, nil
		}}, "/amount_cents")
	if err != nil {
		t.Fatalf("SourcedWrite: %v", err)
	}
	if err := r.Register(w); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return r
}

// rule: ARC-I-035
func TestARC_I_035_ProvenanceWrappersCheckToolKind(t *testing.T) {
	h := func(context.Context, agenttools.Principal, json.RawMessage) (agenttools.ToolResult, error) {
		return agenttools.ToolResult{}, nil
	}
	pv := agenttools.NewProvenance()
	cases := []struct {
		name string
		wrap func() error
	}{
		{"read wrapper on write tool", func() error {
			_, err := pv.ReadTool(agenttools.Tool{Name: "w", Kind: registry.ToolWrite, Handle: h})
			return err
		}},
		{"read wrapper without handler", func() error {
			_, err := pv.ReadTool(agenttools.Tool{Name: "r", Kind: registry.ToolRead})
			return err
		}},
		{"sourced write on read tool", func() error {
			_, err := pv.SourcedWrite(agenttools.Tool{Name: "r", Kind: registry.ToolRead, Handle: h}, "/a")
			return err
		}},
		{"sourced write with no fields", func() error {
			_, err := pv.SourcedWrite(agenttools.Tool{Name: "w", Kind: registry.ToolWrite, Handle: h})
			return err
		}},
		{"sourced write with bad pointer", func() error {
			_, err := pv.SourcedWrite(agenttools.Tool{Name: "w", Kind: registry.ToolWrite, Handle: h}, "amount_cents")
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.wrap(); !errors.Is(err, agenttools.ErrBadTool) {
				t.Fatalf("err = %v, want ErrBadTool", err)
			}
		})
	}
}

// rule: ARC-U-027
func TestARC_U_027_ProvenanceForgetAndUserReads(t *testing.T) {
	cases := []struct {
		name    string
		observe func(pv *agenttools.Provenance)
		ok      bool
	}{
		{"observed", func(pv *agenttools.Provenance) { pv.Observe("r1", json.RawMessage(`{"x":[1,{"y":500}]}`)) }, true},
		{"forgotten", func(pv *agenttools.Provenance) {
			pv.Observe("r1", json.RawMessage(`{"y":500}`))
			pv.Forget("r1")
		}, false},
		{"non-json output records nothing", func(pv *agenttools.Provenance) { pv.Observe("r1", json.RawMessage(`500 trailing`)) }, false},
		{"empty run id records nothing", func(pv *agenttools.Provenance) { pv.Observe("", json.RawMessage(`{"y":500}`)) }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pv := agenttools.NewProvenance()
			tc.observe(pv)
			err := pv.Check("r1", []string{"/amount_cents"}, json.RawMessage(`{"amount_cents":500}`))
			if tc.ok != (err == nil) {
				t.Fatalf("Check err = %v, want ok=%v", err, tc.ok)
			}
			if !tc.ok && !errors.Is(err, agenttools.ErrUnsourcedValue) {
				t.Fatalf("Check err = %v, want ErrUnsourcedValue", err)
			}
		})
	}
}

// rule: ARC-U-027
// A date or number written inside a Quoted read result counts as read; one the quote never
// states, or one outside a Quoted envelope's text, does not.
func TestARC_U_027_QuotedValuesCountAsRead(t *testing.T) {
	const transcript = `{"quotes":[{"untrusted":true,"source":{"doc_id":"d1"},"text":"Call transcript, 2026-10-05, 30 minutes. Fee 1,250.00 discussed."}]}`
	const plain = `{"note":"Call on 2026-10-06 for 45 minutes","text":"2026-10-07 for 50 minutes"}`
	const page = `{"untrusted":true,"doc_id":"d2","pages":[{"page":1,"lines":[{"no":1,"text":"Meeting 2026-10-08, 75 minutes."}]}]}`
	cases := []struct {
		name string
		in   string
		ok   bool
	}{
		{"quoted minutes", `{"minutes":30}`, true},
		{"quoted date", `{"work_date":"2026-10-05"}`, true},
		{"quoted number with thousands comma", `{"minutes":1250}`, true},
		{"invented minutes", `{"minutes":600}`, false},
		{"invented date", `{"work_date":"2026-12-31"}`, false},
		{"date parts are not numbers", `{"minutes":2026}`, false},
		{"text outside a quote is not read", `{"minutes":45}`, false},
		{"date outside a quote is not read", `{"work_date":"2026-10-06"}`, false},
		{"text field outside an untrusted envelope", `{"minutes":50}`, false},
		{"minutes on a quoted document line", `{"minutes":75}`, true},
		{"date on a quoted document line", `{"work_date":"2026-10-08"}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pv := agenttools.NewProvenance()
			pv.Observe("r1", json.RawMessage(transcript))
			pv.Observe("r1", json.RawMessage(plain))
			pv.Observe("r1", json.RawMessage(page))
			err := pv.Check("r1", []string{"/minutes", "/work_date"}, json.RawMessage(tc.in))
			if tc.ok && err != nil {
				t.Fatalf("Check: %v", err)
			}
			if !tc.ok && !errors.Is(err, agenttools.ErrUnsourcedValue) {
				t.Fatalf("Check err = %v, want ErrUnsourcedValue", err)
			}
		})
	}
}

// rule: ARC-U-027
// Check accepts a value a quote stated; CheckStructured and CheckFrom refuse it, so a money
// amount can not come from an email or a document.
func TestProvenanceStructuredOnly(t *testing.T) {
	quoted := json.RawMessage(`{"untrusted":true,"text":"please send 4,500,000 now"}`)
	structured := json.RawMessage(`{"balance_cents":125000}`)
	tests := []struct {
		name      string
		input     string
		check     string // "any", "structured" or "from"
		wantError bool
	}{
		{"quoted value, any origin", `{"amount_cents":4500000}`, "any", false},
		{"quoted value, structured only", `{"amount_cents":4500000}`, "structured", true},
		{"quoted value, from the ledger read", `{"amount_cents":4500000}`, "from", true},
		{"structured value, structured only", `{"amount_cents":125000}`, "structured", false},
		{"structured value, from the named read", `{"amount_cents":125000}`, "from", false},
		{"unread value", `{"amount_cents":1}`, "any", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pv := agenttools.NewProvenance()
			pv.ObserveFrom("r1", "read_email", quoted)
			pv.ObserveFrom("r1", "read_ledger", structured)
			fields := []string{"/amount_cents"}
			var err error
			switch tt.check {
			case "any":
				err = pv.Check("r1", fields, json.RawMessage(tt.input))
			case "structured":
				err = pv.CheckStructured("r1", fields, json.RawMessage(tt.input))
			case "from":
				err = pv.CheckFrom("r1", fields, json.RawMessage(tt.input), "read_ledger")
			}
			if got := errors.Is(err, agenttools.ErrUnsourcedValue); got != tt.wantError {
				t.Fatalf("err = %v, want unsourced %v", err, tt.wantError)
			}
		})
	}
	// A structured value from a read outside the allowed set is refused by CheckFrom.
	pv := agenttools.NewProvenance()
	pv.ObserveFrom("r1", "read_record", structured)
	if err := pv.CheckFrom("r1", []string{"/amount_cents"}, json.RawMessage(`{"amount_cents":125000}`), "read_ledger"); !errors.Is(err, agenttools.ErrUnsourcedValue) {
		t.Fatalf("other read: err = %v", err)
	}
}
