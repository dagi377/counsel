package billing

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/agent/schemas"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/registry"
)

// bil07Answer is a digest answer citing bilcBill's line line.
func bil07Answer(t *testing.T, text string, line int) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"answer": text, "citations": []map[string]any{
		{"version_id": agent.BillLineCitation(bilcBill, 1).VersionID, "page": 1, "from_line": line, "to_line": line},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// bil07Reads are the digest's read calls: the matter's unbilled time, the bill and the trust
// balance.
var bil07Reads = []bilcCall{
	{name: agent.ToolListUnbilled, input: `{"matter_id":"` + agenttest.TrustMatter + `"}`},
	bilcReads[0], bilcReads[1],
}

// bil07Digest states only figures the reads return: the bill total, the trust balance and the
// unbilled time (90 minutes at 300.00 an hour).
const bil07Digest = "Billing digest for 2026-09: bill SYN-2026-0003 has $1,130.00 unpaid; trust holds $565.00; unbilled time $450.00."

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFBIL07_FromFeature(t *testing.T) {
	cases := []struct {
		name       string
		narrative  string
		answer     string
		line       int
		wantAnswer error // run.CheckAnswer
		wantAmount error // every amount sourced
	}{
		{name: "ok", narrative: "Settlement review", answer: bil07Digest, line: 1},
		{name: "citation not returned", narrative: "Settlement review", answer: bil07Digest, line: 2, wantAnswer: run.ErrUncitedAnswer},
		// rule: ARC-U-027
		{name: "model-invented amount", narrative: "Settlement review", answer: "Bill SYN-2026-0003 has $1,200.00 unpaid.", line: 1, wantAmount: agenttools.ErrUnsourcedValue},
		{name: "injection", narrative: "Settlement review. " + bilcInjected, answer: bil07Digest + " One bill line held instructions; treated as quoted data.", line: 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			def := MonthlyDigestDef()
			fd := bilcFeature(t, def, c.narrative)
			runID := "run-fbil07-" + strings.ReplaceAll(c.name, " ", "-")
			r := fd.Fire(context.Background(), bilcScript(t, def.ID, bil07Reads, bil07Answer(t, c.answer, c.line)), runID, "Run the monthly billing digest for 2026-09.")
			if r.Err != nil || len(r.Refused) != 0 {
				t.Fatalf("run: %v refused %v", r.Err, r.Refused)
			}
			// rule: ARC-I-038
			if !slices.Equal(r.Tools, bilcNames(bil07Reads)) {
				t.Fatalf("tools = %v, want %v (no extra call)", r.Tools, bilcNames(bil07Reads))
			}
			// rule: AI-R-003
			// An internal digest: no write tool ran and no gate row exists.
			if r.FirstWrite != -1 || len(r.GateIDs) != 0 {
				t.Fatalf("digest wrote at %d, gates %v; want none", r.FirstWrite, r.GateIDs)
			}
			spec, err := def.Spec()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := run.CheckAnswer(spec, bilcOutcome(r)); !errors.Is(err, c.wantAnswer) {
				t.Fatalf("CheckAnswer = %v, want %v", err, c.wantAnswer)
			}
			var ans struct {
				Answer string `json:"answer"`
			}
			if err := json.Unmarshal([]byte(r.Text), &ans); err != nil {
				t.Fatalf("answer: %v", err)
			}
			if err := bilcSourcedAmounts(fd, runID, ans.Answer); !errors.Is(err, c.wantAmount) {
				t.Fatalf("amounts = %v, want %v", err, c.wantAmount)
			}
			if c.name == "injection" {
				if err := agenttest.QuotedOnly(r.Run, bilcInjected); err != nil {
					t.Fatal(err)
				}
				if bilcToolInputsCarry(r.Run, bilcInjected) {
					t.Fatal("injected text reached a tool input")
				}
			}
			// The agent never approves: whatever gate a lawyer later opens from the digest,
			// an agent principal deciding it is 403.
			bilcDeciders(t, gates.Kind{Code: GateCreditNote, SpecGate: "G-B5", Approver: "billing.credit"}, agenttest.FeatureActingFor, "g-bil07-any")
		})
	}
}

// rule: AI-R-002
// rule: ARC-I-035
func TestFBIL07_Def(t *testing.T) {
	def := MonthlyDigestDef()
	bilcPromptLint(t, def, "Monthly billing digest", "report for lawyers (internal, no gate)")
	for _, name := range def.Tools {
		d, err := schemas.ByName(name)
		if err != nil {
			t.Fatal(err)
		}
		if d.Kind == registry.ToolWrite {
			t.Fatalf("digest Def has write tool %s", name)
		}
	}
	b, err := MonthlyDigestBinding("user-synthetic-rule-admin")
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	tests := []struct {
		field     string
		got, want string
	}{
		{"feature", b.FeatureID, "F-BIL-07"},
		{"events", strings.Join(b.Events, ","), ""},
		{"cadence", string(b.Cadence), string(trigger.CadenceMonthly)},
		{"acting for", b.ActingFor, "user-synthetic-rule-admin"},
		{"gate kinds", strings.Join(def.GateKinds, ","), ""},
		{"kind", string(def.Kind), "chat"},
		{"source", string(def.Source), "job"},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %q, want %q", tt.field, tt.got, tt.want)
			}
		})
	}
}

// rule: ARC-U-027
func TestFBIL07_PrepareOnly(t *testing.T) {
	if err := bilcPrepareOnly("ai_f_bil_07.go"); err != nil {
		t.Fatal(err)
	}
}
