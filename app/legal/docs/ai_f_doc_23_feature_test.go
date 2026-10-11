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

// docgContentFlag is an F-DOC-23 output of one flag row for the seeded matter citing c.
func docgContentFlag(flag string, c docgCite) func(string, string) map[string]any {
	return func(matter, v string) map[string]any {
		return map[string]any{"items": []map[string]any{{"flag": flag, "matter_id": matter, "citation": docgCitation(c, v)}}}
	}
}

// rule: AI-R-003
// rule: ARC-I-038
func TestFDOC23_DefMatchesSpec(t *testing.T) {
	docCheckDefMatchesSpec(t, docs.DetectMatterContentDef(), false)
}

// rule: AI-R-005
func TestFDOC23_Prompt(t *testing.T) {
	docCheckPrompt(t, docs.DetectMatterContentDef())
}

// rule: AI-R-003
func TestFDOC23_TriggerBinding(t *testing.T) {
	docgCheckTrigger(t, docs.DetectMatterContentDef(), "firm-library", "Detect matter content")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
// The result is cited flag rows naming the matter to move the file to: no write tool in the
// Def, no gate row.
func TestFDOC23_FromFeature(t *testing.T) {
	def := docs.DetectMatterContentDef()
	docgCheckReadOnlyDef(t, def)
	cases := []struct {
		name        string
		grant       []string
		lines       []string
		out         func(matter, version string) map[string]any
		wantRefused error
		wantErr     error // docgCheckItems
		injected    bool
	}{
		{name: "ok", lines: docgLibraryLines, out: docgContentFlag(docs.FlagNamesParty, docgCite{from: 2, to: 2})},
		{name: "injection quoted only", lines: docgLibraryInjected, out: docgContentFlag(docs.FlagNamesParty, docgCite{from: 2, to: 2}), injected: true},
		{name: "line never read", lines: docgLibraryLines, out: docgContentFlag(docs.FlagNamesParty, docgCite{from: 2, to: 47}), wantErr: agenttools.ErrUnsourcedValue},
		{name: "version never read", lines: docgLibraryLines, out: docgContentFlag(docs.FlagNamesParty, docgCite{version: "00000000-0000-4000-8000-0000000000f9", from: 2, to: 2}), wantErr: run.ErrUncitedAnswer},
		{name: "403 without documents.view", grant: docgNoGrant(def.Grant, agent.CapDocumentsView), lines: docgLibraryLines,
			out: docgContentFlag(docs.FlagNamesParty, docgCite{from: 2, to: 2}), wantRefused: agenttools.ErrMissingCapability, wantErr: agenttools.ErrUnsourcedValue},
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
			flags, err := docs.MatterContentFlags(r.run.JSON)
			if err != nil {
				t.Fatalf("MatterContentFlags: %v", err)
			}
			if len(flags) != 1 || flags[0].MatterID != r.matter || flags[0].Citation.VersionID != r.version {
				t.Fatalf("flags = %+v, want one row for matter %s citing %s", flags, r.matter, r.version)
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
func TestFDOC23_AgentNeverApproves(t *testing.T) {
	docgCheckNoApproval(t, docs.DetectMatterContentDef())
}

func TestFDOC23_FlagsRefuseBadOutput(t *testing.T) {
	cite := `"citation":{"version_id":"v","page":1,"from_line":1,"to_line":1}`
	tests := []struct {
		name string
		out  string
	}{
		{"not json", `items`},
		{"unknown flag", `{"items":[{"flag":"looks_private","matter_id":"m-1",` + cite + `}]}`},
		{"no matter", `{"items":[{"flag":"names_party","matter_id":"",` + cite + `}]}`},
		{"name echoed", `{"items":[{"flag":"names_party","matter_id":"m-1","name":"Synth",` + cite + `}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := docs.MatterContentFlags([]byte(tt.out)); !errors.Is(err, docs.ErrBadItem) {
				t.Fatalf("err = %v, want ErrBadItem", err)
			}
		})
	}
}
