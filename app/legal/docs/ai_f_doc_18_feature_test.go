package docs_test

import (
	"errors"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/docs"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/run"
)

// docgEmailLines and docgEmailInjected are the selected email as page 1 of a seeded document.
var (
	docgEmailLines    = []string{"Re: Synth v Example, court file CV-26-0412", "Attached is the issued claim for the file."}
	docgEmailInjected = []string{"Re: Synth v Example, court file CV-26-0412", docgInjected + "."}
)

// docgEmailFiling is an F-DOC-18 output of one suggestion for matter (the seeded one when
// empty) citing c.
func docgEmailFiling(matter string, c docgCite, ppm int) func(string, string) map[string]any {
	return func(seededMatter, v string) map[string]any {
		if matter == "" {
			matter = seededMatter
		}
		return map[string]any{"items": []map[string]any{{
			"matter_id": matter, "folder": "Correspondence", "confidence_ppm": ppm, "citation": docgCitation(c, v),
		}}}
	}
}

// rule: AI-R-003
// rule: ARC-I-038
func TestFDOC18_DefMatchesSpec(t *testing.T) {
	docCheckDefMatchesSpec(t, docs.EmailAutoFileDef(), false)
}

// rule: AI-R-005
func TestFDOC18_Prompt(t *testing.T) {
	docCheckPrompt(t, docs.EmailAutoFileDef())
}

// rule: AI-R-003
func TestFDOC18_TriggerBinding(t *testing.T) {
	docgCheckTrigger(t, docs.EmailAutoFileDef(), "email-filing", "Auto-file")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
// The result is a cited filing suggestion above the confidence threshold: no write tool in
// the Def, no gate row.
func TestFDOC18_FromFeature(t *testing.T) {
	def := docs.EmailAutoFileDef()
	docgCheckReadOnlyDef(t, def)
	cases := []struct {
		name        string
		grant       []string
		lines       []string
		out         func(matter, version string) map[string]any
		wantRefused error
		wantErr     error // docgCheckItems
		wantShown   int
		injected    bool
	}{
		{name: "ok", lines: docgEmailLines, out: docgEmailFiling("", docgCite{from: 1, to: 2}, 950_000), wantShown: 1},
		{name: "below threshold not shown", lines: docgEmailLines, out: docgEmailFiling("", docgCite{from: 1, to: 1}, 700_000)},
		{name: "injection quoted only", lines: docgEmailInjected, out: docgEmailFiling("", docgCite{from: 1, to: 1}, 920_000), wantShown: 1, injected: true},
		{name: "line never read", lines: docgEmailLines, out: docgEmailFiling("", docgCite{from: 1, to: 47}, 950_000), wantErr: agenttools.ErrUnsourcedValue},
		{name: "version never read", lines: docgEmailLines, out: docgEmailFiling("", docgCite{version: "00000000-0000-4000-8000-0000000000f9", from: 1, to: 2}, 950_000), wantErr: run.ErrUncitedAnswer},
		{name: "403 without documents.view", grant: docgNoGrant(def.Grant, agent.CapDocumentsView), lines: docgEmailLines,
			out: docgEmailFiling("", docgCite{from: 1, to: 2}, 950_000), wantRefused: agenttools.ErrMissingCapability, wantErr: agenttools.ErrUnsourcedValue},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := docgFire(t, def, tc.grant, tc.lines, tc.out)
			docgCheckRun(t, r, tc.wantRefused)
			if err := docgCheckItems(t, r); !errors.Is(err, tc.wantErr) {
				t.Fatalf("items = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}
			shown, err := docs.EmailFilings(r.run.JSON)
			if err != nil {
				t.Fatalf("EmailFilings: %v", err)
			}
			if len(shown) != tc.wantShown {
				t.Fatalf("shown %d suggestions, want %d", len(shown), tc.wantShown)
			}
			for _, s := range shown {
				if s.MatterID != r.matter || s.Citation.VersionID != r.version || s.ConfidencePPM < docs.EmailFileAbovePPM {
					t.Fatalf("suggestion %+v, want matter %s citing %s", s, r.matter, r.version)
				}
			}
			if tc.injected {
				// rule: ARC-I-038
				if err := agenttest.QuotedOnly(r.run.Run, docgInjected); err != nil {
					t.Fatal(err)
				}
				if err := docgAbsent(r, docgInjected); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// rule: AI-R-003
func TestFDOC18_AgentNeverApproves(t *testing.T) {
	docgCheckNoApproval(t, docs.EmailAutoFileDef())
}

func TestFDOC18_EmailFilingsRefusesBadOutput(t *testing.T) {
	tests := []struct {
		name string
		out  string
	}{
		{"not json", `items`},
		{"unknown field", `{"items":[],"note":"x"}`},
		{"no matter", `{"items":[{"matter_id":"","confidence_ppm":990000,"citation":{"version_id":"v","page":1,"from_line":1,"to_line":1}}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := docs.EmailFilings([]byte(tt.out)); !errors.Is(err, docs.ErrBadItem) {
				t.Fatalf("err = %v, want ErrBadItem", err)
			}
		})
	}
}
