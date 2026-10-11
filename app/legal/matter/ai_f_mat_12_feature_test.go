package matter_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/matter"
)

// F-MAT-12 starts from a document filed on a matter, and on request from the chronology
// screen; it has no schedule.
func TestFMAT12_TriggerBinding(t *testing.T) {
	b, err := matter.ChronologyBinding()
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	def := matter.ChronologyDef()
	tests := []struct {
		field     string
		got, want string
	}{
		{"feature", b.FeatureID, "F-MAT-12"},
		{"events", strings.Join(b.Events, ","), "document.uploaded"},
		{"cadence", string(b.Cadence), string(trigger.CadenceNone)},
		{"designated", map[bool]string{true: "yes", false: "no"}[b.Designated], "no"},
		{"acting for", b.ActingFor, ""},
		{"trigger", def.Trigger, "on request; event: new document or email on a matter with a chronology"},
		{"on request", strings.Join(def.Actions, ";"), matter.ActionBuildChronology + ";" + matter.ActionRefreshChronology},
		{"gate kinds", strings.Join(def.GateKinds, ","), matter.GateKeyFact},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %q, want %q", tt.field, tt.got, tt.want)
			}
		})
	}
}

// rule: ARC-I-035
// rule: ARC-I-039
// rule: AI-R-003
// rule: AI-R-005
// The tool-gap kind matter.key_fact opens only through draft_document then
// submit_for_approval, with the acting user as preparer; no one in the run can decide it.
func TestFMAT12_FromFeature(t *testing.T) {
	matdFromFeature(t, matter.ChronologyDef(), matter.GateKeyFact,
		"Key-fact check: chronology", "2026-09-28: the closing checklist was rewritten [c1]. Check the key fact on the intake date.",
		matdCases())
}

// rule: AI-R-003
func TestFMAT12_KeyFactSubmitInput(t *testing.T) {
	tests := []struct {
		name    string
		draftID string
		wantErr error
	}{
		{"draft", "draft-document-2", nil},
		{"no draft", "", matter.ErrBadItem},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := matter.KeyFactSubmitInput(tt.draftID)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			var in map[string]string
			if err := json.Unmarshal(b, &in); err != nil || in["gate_kind"] != matter.GateKeyFact || in["subject_id"] != tt.draftID || in["subject_type"] != matter.SubjectAgentDraft {
				t.Fatalf("input = %s (%v)", b, err)
			}
		})
	}
}
