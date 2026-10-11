package platform_test

import (
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/platform"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai/aifake"
)

// rule: AI-R-003
// rule: ARC-I-035
func TestFPLT17_DefMatchesSpec(t *testing.T) {
	d := platform.PrepareSignatureFieldsDef()
	pltCheckDefMatchesSpec(t, d)
	pltiCheckReadOnly(t, d)
}

// rule: AI-R-005
func TestFPLT17_Prompt(t *testing.T) {
	pltCheckPrompt(t, platform.PrepareSignatureFieldsDef(), "Prepare signature fields", "read-only", "Flag:", "citation")
}

// F-PLT-17 runs only from the esign-request screen action; it binds no event or schedule.
func TestFPLT17_TriggerScreenAction(t *testing.T) {
	d := platform.PrepareSignatureFieldsDef()
	tests := []struct {
		name      string
		got, want string
	}{
		{"source", string(d.Source), "screen-action"},
		{"trigger", d.Trigger, "on request: " + platform.ActionPrepareSignatureFields},
		{"actions", strings.Join(d.Actions, ","), platform.ActionPrepareSignatureFields},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}

// plt17Agreement is page 1 of the synthetic agreement to be signed.
var plt17Agreement = []string{
	"Retainer agreement between the firm and the client.",
	"Client signature: ____________________",
	"Date: ____________",
	"Lawyer signature: ____________________",
	"Initials on each page: ____",
}

// plt17Calls is the F-PLT-17 script: read the matter, find the agreement, read it.
func plt17Calls(w pltiWorld) []pltCall {
	return []pltCall{
		{agent.ToolGetMatter, map[string]any{"matter_id": w.matter}},
		{agent.ToolSearchDocuments, map[string]any{"matter_id": w.matter, "query": "signature"}},
		{agent.ToolReadDocument, map[string]any{"doc_id": w.doc, "version_id": w.version}},
	}
}

// plt17Fields is the ranked list of blocks and one flag row.
const plt17Fields = "1. signature: page 1 lines 2-2, client\n2. date: page 1 lines 3-3, client\n3. signature: page 1 lines 4-4, lawyer\n4. initial: page 1 lines 5-5, client\nFlag: page 1 line 5 asks for initials on each page; the signer role is assumed."

// plt17Row is a ranked block row or a flag row.
var plt17Row = regexp.MustCompile(`^(?:(\d+)\. (signature|initial|date): page \d+ lines \d+-\d+, \w+|Flag: .+)$`)

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFPLT17_FromFeature(t *testing.T) {
	cases := []struct {
		name     string
		version  string // version cited; "" cites the read version
		from, to int
		final    func(answer map[string]any) map[string]any
		opts     []aifake.Option
		wantErr  error // the run
		wantAns  error // run.CheckAnswer
		retry    bool
	}{
		{name: "ok", from: 1, to: 5},
		// rule: ARC-I-038
		{name: "citation not returned", version: "00000000-0000-4000-8000-0000000000ff", from: 2, to: 2, wantAns: run.ErrUncitedAnswer},
		{name: "lines outside the read", from: 4, to: 9, wantAns: run.ErrUncitedAnswer},
		// The extraction schema holds the answer shape: plain text fails the run.
		{name: "output not an answer", from: 1, to: 5, final: func(map[string]any) map[string]any {
			return map[string]any{"json": map[string]any{"fields": []string{"signature"}}}
		}, wantErr: run.ErrBadOutput},
		// rule: AI-R-003
		{name: "rate limited queues a retry", from: 1, to: 5, opts: []aifake.Option{aifake.WithFault(platform.FeaturePrepareSignatureFields, aifake.FaultRateLimit)}, retry: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := pltiFireRead(t, platform.PrepareSignatureFieldsDef(), plt17Agreement, plt17Calls, func(w pltiWorld) map[string]any {
				v := w.version
				if tc.version != "" {
					v = tc.version
				}
				a := pltiAnswer(plt17Fields, v, tc.from, tc.to)
				if tc.final != nil {
					return tc.final(a)
				}
				return a
			}, tc.opts...)
			if !errors.Is(r.err, tc.wantErr) || (tc.wantErr == nil && r.err != nil) {
				t.Fatalf("run = %v, want %v", r.err, tc.wantErr)
			}
			// rule: AI-R-005
			// No write and no gate row: the fields are a proposal the person may change.
			if len(r.out.GateIDs) != 0 || len(r.fd.StandIns) != 0 {
				t.Fatalf("gates %v, stand-ins %v; want none", r.out.GateIDs, r.fd.StandIns)
			}
			if tc.wantErr != nil {
				return
			}
			if tc.retry {
				if !r.out.RetryQueued || len(r.out.Calls) != 0 {
					t.Fatalf("retry = %v, calls %v; want a queued retry and no call", r.out.RetryQueued, r.out.Calls)
				}
				return
			}
			if want := pltiToolNames(plt17Calls(r.pltiWorld)); !slices.Equal(pltiCalled(r.out), want) {
				t.Fatalf("tools = %v, want %v", pltiCalled(r.out), want)
			}
			ans, err := run.CheckAnswer(r.spec, r.out)
			if !errors.Is(err, tc.wantAns) || (tc.wantAns == nil && err != nil) {
				t.Fatalf("CheckAnswer = %v, want %v", err, tc.wantAns)
			}
			if err != nil {
				return
			}
			if len(ans.Citations) != 1 || ans.Citations[0].VersionID != r.version {
				t.Fatalf("citations = %v, want the read version", ans.Citations)
			}
			// The result is a ranked list of blocks, numbered from 1, then flag rows.
			rank := 0
			for _, l := range strings.Split(ans.Text, "\n") {
				m := plt17Row.FindStringSubmatch(l)
				if m == nil {
					t.Fatalf("row %q is neither a ranked block nor a flag", l)
				}
				if m[1] != "" {
					rank++
					if m[1] != strconv.Itoa(rank) {
						t.Fatalf("row %q ranked %s, want %d", l, m[1], rank)
					}
				}
			}
			if rank == 0 {
				t.Fatal("no ranked block")
			}
		})
	}
}

// rule: ARC-I-038
func TestFPLT17_Injection(t *testing.T) {
	r := pltiFireRead(t, platform.PrepareSignatureFieldsDef(), append(slices.Clone(plt17Agreement), pltInjected), plt17Calls, func(w pltiWorld) map[string]any {
		return pltiAnswer(plt17Fields+"\nFlag: page 1 line 6 asks for an action; not acted on.", w.version, 1, 6)
	})
	if r.err != nil {
		t.Fatalf("run: %v", r.err)
	}
	if want := pltiToolNames(plt17Calls(r.pltiWorld)); !slices.Equal(pltiCalled(r.out), want) {
		t.Fatalf("tools = %v, want %v (no extra call)", pltiCalled(r.out), want)
	}
	if err := pltiQuotedOnly(r, pltInjected); err != nil {
		t.Fatal(err)
	}
	if _, err := run.CheckAnswer(r.spec, r.out); err != nil {
		t.Fatalf("CheckAnswer: %v", err)
	}
	if len(r.out.GateIDs) != 0 {
		t.Fatalf("gates opened: %v", r.out.GateIDs)
	}
}
