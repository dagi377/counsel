package matter_test

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/kernel/agenttools/run"
)

// rule: AI-R-003
func TestFMAT10_DefMatchesSpec(t *testing.T) {
	matfCheckDefMatchesSpec(t, matter.RelatedMattersDef())
}

// rule: AI-R-005
func TestFMAT10_Prompt(t *testing.T) {
	matfCheckPrompt(t, matter.RelatedMattersDef())
}

// F-MAT-10 starts when a matter is opened or gains a party (matter.party_added), acting for the
// event's user; it has no schedule and no screen action.
func TestFMAT10_TriggerBinding(t *testing.T) {
	b, err := matter.RelatedMattersBinding()
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	tests := []struct {
		field     string
		got, want any
	}{
		{"feature", b.FeatureID, matter.FeatureRelatedMatters},
		{"events", strings.Join(b.Events, ","), "matter.party_added,matter.opened"},
		{"cadence", b.Cadence, trigger.CadenceNone},
		{"designated lawyer", b.Designated, false},
		{"acting for", b.ActingFor, ""},
		{"actions", len(matter.RelatedMattersDef().Actions), 0},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %v, want %v", tt.field, tt.got, tt.want)
			}
		})
	}
}

// mat10Calls is the F-MAT-10 script: read the new matter and its client, read the other
// matter, then find and read the document that links them.
func mat10Calls(w matfSeeded) []matfCall {
	return []matfCall{
		{agent.ToolGetMatter, `{"matter_id": "` + w.MatterID + `"}`},
		{agent.ToolSearchContacts, `{"query": "Synth Client", "limit": 10}`},
		{agent.ToolGetMatter, `{"matter_id": "` + w.OtherMatterID + `"}`},
		{agent.ToolSearchDocuments, `{"matter_id": "` + w.MatterID + `", "query": "appeal"}`},
		{agent.ToolReadDocument, `{"doc_id": "` + w.DocID + `", "version_id": "` + w.VersionID + `"}`},
	}
}

// mat10Ranked is a ranked list: one "N. ..." line per proposed link, numbered from 1.
var mat10Ranked = regexp.MustCompile(`^(\d+)\. \S`)

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFMAT10_FromFeature(t *testing.T) {
	d := matter.RelatedMattersDef()
	matfCheckReadOnly(t, d)
	lines := []string{"Notice of appeal filed in matter 2026-0303.", "The appeal concerns the same supply contract."}
	const ranked = "1. 2026-0303 Synth Appeal: the appeal of the same supply contract.\n2. 2026-0302 Synth: same client, no shared document."
	cases := []struct {
		name       string
		from, to   int
		version    string // version cited; "" cites the read version
		wantAnswer error
	}{
		{name: "ok", from: 1, to: 2},
		{name: "citation not returned", from: 1, to: 1, version: "00000000-0000-4000-8000-0000000000ff", wantAnswer: run.ErrUncitedAnswer},
		{name: "lines outside the read", from: 2, to: 4, wantAnswer: run.ErrUncitedAnswer},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := matfFireRead(t, d, lines, mat10Calls, func(w matfSeeded) string {
				v := w.VersionID
				if tc.version != "" {
					v = tc.version
				}
				return matfAnswer(t, ranked, v, tc.from, tc.to)
			})
			// Every tool of the Def runs its real handler, and only the script's calls ran.
			if len(r.fd.StandIns) != 0 || len(r.run.Refused) != 0 {
				t.Fatalf("stand-ins = %v, refused = %v; want none", r.fd.StandIns, r.run.Refused)
			}
			if want := matfToolNames(mat10Calls(r.matfSeeded)); !slices.Equal(r.run.Tools, want) {
				t.Fatalf("tools = %v, want %v", r.run.Tools, want)
			}
			// rule: AI-R-005
			// No write, no gate row: a proposed link is an answer the user may ignore.
			if r.run.FirstWrite >= 0 || len(r.run.GateIDs) != 0 {
				t.Fatalf("write at %d, gates %v; want none", r.run.FirstWrite, r.run.GateIDs)
			}
			spec, err := d.Spec()
			if err != nil {
				t.Fatalf("Spec: %v", err)
			}
			// rule: ARC-I-038
			ans, err := run.CheckAnswer(spec, matfOutcome(r.run))
			if !errors.Is(err, tc.wantAnswer) {
				t.Fatalf("CheckAnswer = %v, want %v", err, tc.wantAnswer)
			}
			if err != nil {
				return
			}
			if len(ans.Citations) != 1 || ans.Citations[0].VersionID != r.VersionID {
				t.Fatalf("citations = %v, want the read version", ans.Citations)
			}
			for i, l := range strings.Split(ans.Text, "\n") {
				m := mat10Ranked.FindStringSubmatch(l)
				if m == nil || m[1] != strconv.Itoa(i+1) {
					t.Fatalf("answer line %d = %q, want rank %d", i, l, i+1)
				}
			}
		})
	}
}

// rule: ARC-I-038
func TestFMAT10_Injection(t *testing.T) {
	d := matter.RelatedMattersDef()
	lines := []string{"Notice of appeal filed in matter 2026-0303.", matfInjected}
	r := matfFireRead(t, d, lines, mat10Calls, func(w matfSeeded) string {
		return matfAnswer(t, "1. 2026-0303 Synth Appeal: notice of appeal. A document line asks for an action; not acted on.", w.VersionID, 1, 1)
	})
	if len(r.run.Refused) != 0 {
		t.Fatalf("refused = %v", r.run.Refused)
	}
	if want := matfToolNames(mat10Calls(r.matfSeeded)); !slices.Equal(r.run.Tools, want) {
		t.Fatalf("tools = %v, want %v (no extra call)", r.run.Tools, want)
	}
	if err := agenttest.QuotedOnly(r.run.Run, matfInjected); err != nil {
		t.Fatal(err)
	}
	if err := matfAbsent(context.Background(), r.fd, r.run, matfInjected); err != nil {
		t.Fatal(err)
	}
}
