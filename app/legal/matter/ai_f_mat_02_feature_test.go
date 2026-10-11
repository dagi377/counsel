package matter_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/kernel/agenttools/run"
)

// rule: AI-R-003
func TestFMAT02_DefMatchesSpec(t *testing.T) {
	matfCheckDefMatchesSpec(t, matter.MatterDigestDef())
}

// rule: AI-R-005
func TestFMAT02_Prompt(t *testing.T) {
	matfCheckPrompt(t, matter.MatterDigestDef())
}

// F-MAT-02 runs nightly for the designated lawyer, on every matter event for the event's
// user, and on request from firm home.
func TestFMAT02_TriggerBinding(t *testing.T) {
	b, err := matter.MatterDigestBinding()
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	tests := []struct {
		field     string
		got, want any
	}{
		{"feature", b.FeatureID, matter.FeatureMatterDigest},
		{"events", strings.Join(b.Events, ","), "matter.opened,matter.updated,matter.stage_changed,matter.team_changed,matter.closed"},
		{"cadence", b.Cadence, trigger.CadenceDaily},
		{"designated lawyer", b.Designated, true},
		{"acting for", b.ActingFor, ""},
		{"on request", strings.Join(matter.MatterDigestDef().Actions, ";"), "firm-home > Compose morning digest"},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %v, want %v", tt.field, tt.got, tt.want)
			}
		})
	}
}

// mat02Calls is the F-MAT-02 script: read the matter, find and read its latest order, look
// up the client.
func mat02Calls(w matfSeeded) []matfCall {
	return []matfCall{
		{agent.ToolGetMatter, `{"matter_id": "` + w.MatterID + `"}`},
		{agent.ToolSearchDocuments, `{"matter_id": "` + w.MatterID + `", "query": "order"}`},
		{agent.ToolReadDocument, `{"doc_id": "` + w.DocID + `", "version_id": "` + w.VersionID + `"}`},
		{agent.ToolSearchContacts, `{"query": "Synth Client", "limit": 5}`},
	}
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFMAT02_FromFeature(t *testing.T) {
	d := matter.MatterDigestDef()
	matfCheckReadOnly(t, d)
	lines := []string{"Order dated 2026-10-01: the motion is adjourned.", "Costs reserved to the trial judge."}
	cases := []struct {
		name       string
		from, to   int
		version    string // version cited; "" cites the read version
		wantAnswer error
	}{
		{name: "ok", from: 1, to: 2},
		{name: "citation not returned", from: 1, to: 1, version: "00000000-0000-4000-8000-0000000000ff", wantAnswer: run.ErrUncitedAnswer},
		{name: "lines outside the read", from: 2, to: 3, wantAnswer: run.ErrUncitedAnswer},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := matfFireRead(t, d, lines, mat02Calls, func(w matfSeeded) string {
				v := w.VersionID
				if tc.version != "" {
					v = tc.version
				}
				return matfAnswer(t, "Motion adjourned; costs reserved. Risk 2 of 5.", v, tc.from, tc.to)
			})
			// Every tool of the Def runs its real handler, and only the script's calls ran.
			if len(r.fd.StandIns) != 0 || len(r.run.Refused) != 0 {
				t.Fatalf("stand-ins = %v, refused = %v; want none", r.fd.StandIns, r.run.Refused)
			}
			if want := matfToolNames(mat02Calls(r.matfSeeded)); !slices.Equal(r.run.Tools, want) {
				t.Fatalf("tools = %v, want %v", r.run.Tools, want)
			}
			// rule: AI-R-005
			// No write, no gate row: the digest is the answer only.
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
			if err == nil && (len(ans.Citations) != 1 || ans.Citations[0].VersionID != r.VersionID) {
				t.Fatalf("citations = %v, want the read version", ans.Citations)
			}
		})
	}
}

// rule: ARC-I-038
func TestFMAT02_Injection(t *testing.T) {
	d := matter.MatterDigestDef()
	lines := []string{"Order dated 2026-10-01: the motion is adjourned.", matfInjected}
	r := matfFireRead(t, d, lines, mat02Calls, func(w matfSeeded) string {
		return matfAnswer(t, "Motion adjourned. A document line asks for an action; not acted on. Risk 2 of 5.", w.VersionID, 1, 1)
	})
	if len(r.run.Refused) != 0 {
		t.Fatalf("refused = %v", r.run.Refused)
	}
	if want := matfToolNames(mat02Calls(r.matfSeeded)); !slices.Equal(r.run.Tools, want) {
		t.Fatalf("tools = %v, want %v (no extra call)", r.run.Tools, want)
	}
	if err := agenttest.QuotedOnly(r.run.Run, matfInjected); err != nil {
		t.Fatal(err)
	}
	if err := matfAbsent(context.Background(), r.fd, r.run, matfInjected); err != nil {
		t.Fatal(err)
	}
}
