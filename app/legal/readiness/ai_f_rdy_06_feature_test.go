package readiness_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/readiness"
	tk "github.com/dagi377/counsel/app/legal/timekeeping"
	"github.com/dagi377/counsel/app/legal/timekeeping/timekeepingfake"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/gate"
)

// rdy06Note is the synthetic voice-note transcript: the date and the minutes it states are
// the only ones the run reads.
var rdy06Note = []string{
	"Voice note transcript, recorded 2026-10-05.",
	"Voice note: 45 minutes on the call with opposing counsel about the motion schedule.",
}

// rdy06World is one F-RDY-06 run over the real handlers and the time entry store
// propose_time_entries saves into.
type rdy06World struct {
	rdycWorld
	entries *timekeepingfake.Fake
}

// rdy06Calls is the F-RDY-06 script: find the transcript, plan, propose one entry.
func rdy06Calls(w rdy06World, workDate string, minutes int) []rdycCall {
	entry, _ := json.Marshal(map[string]any{"entries": []map[string]any{{
		"matter_id": w.matterID, "work_date": workDate, "minutes": minutes,
		"narrative": "Call with opposing counsel about the motion schedule.",
	}}})
	return []rdycCall{
		rdycSearch(w.rdycWorld, "voice note"),
		rdycPlan,
		{agent.ToolProposeTimeEntries, string(entry)},
	}
}

func rdy06Run(t *testing.T, workDate string, minutes int, lines ...string) (rdy06World, agenttest.FeatureRun, []rdycCall) {
	t.Helper()
	entries := timekeepingfake.New(nil, nil)
	fd := agenttest.FromFeatureWith(t, readiness.VoiceNoteToTimeEntryDef(), rdycHandlers(t, entries))
	w := rdy06World{rdycWorld: rdycFeature(fd, lines...), entries: entries}
	calls := rdy06Calls(w, workDate, minutes)
	model := rdycScript("F-RDY-06", calls, "Proposed one suggested entry from the voice note.")
	return w, w.fd.Fire(context.Background(), model, "run-frdy06", "screen: mobile-app\nrecords: matter:"+w.matterID), calls
}

// rule: AI-R-003
// rule: ARC-I-035
func TestFRDY06_DefMatchesSpec(t *testing.T) {
	rdycCheckDefMatchesSpec(t, readiness.VoiceNoteToTimeEntryDef())
}

// rule: AI-R-005
func TestFRDY06_Prompt(t *testing.T) {
	rdycCheckPrompt(t, readiness.VoiceNoteToTimeEntryDef(),
		"Your task: Voice note to time entry", "voice note to time entry on mobile-app (Becomes a suggested entry)",
		agent.GateTimeAccept, "report_plan", "propose_time_entries", "search_documents")
}

// F-RDY-06 runs on request from the mobile app's Voice note to time entry action.
func TestFRDY06_TriggerBinding(t *testing.T) {
	rdycCheckAction(t, readiness.VoiceNoteToTimeEntryDef(), readiness.ActionVoiceNoteToTimeEntry)
}

// rule: AI-R-003
// rule: ARC-I-035
// rule: ARC-I-039
// Every tool runs its real handler. The plan comes before propose_time_entries, which saves
// the entry as suggested for the acting user and opens timekeeping.accept prepared for that
// user. A work date or a minute count the transcript never stated is refused before anything
// is saved.
func TestFRDY06_FromFeature(t *testing.T) {
	tests := []struct {
		name     string
		workDate string
		minutes  int
		wantErr  error // the refusal of propose_time_entries; nil means the gate opens
	}{
		{"sourced date and minutes", "2026-10-05", 45, nil},
		// rule: ARC-U-027
		{"unsourced date", "2026-10-09", 45, agenttools.ErrUnsourcedValue},
		// rule: ARC-U-027
		{"unsourced minutes", "2026-10-05", 60, agenttools.ErrUnsourcedValue},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, run, calls := rdy06Run(t, tt.workDate, tt.minutes, rdy06Note...)
			if tt.wantErr != nil {
				rdycCheckRefused(t, run, agent.ToolProposeTimeEntries, tt.wantErr)
				saved, err := w.entries.List(context.Background(), tk.EntryFilter{})
				if err != nil || len(saved) != 0 {
					t.Fatalf("entries = %v (%v), want nothing saved", saved, err)
				}
				return
			}
			rdycCheckRun(t, w.rdycWorld, run, calls, agent.ToolProposeTimeEntries, agent.GateTimeAccept)
			// The entry stays suggested for the acting user: the agent never accepts it.
			g, err := w.fd.Seed.Gates.Get(context.Background(), gate.GateID(run.GateIDs[0]))
			if err != nil {
				t.Fatal(err)
			}
			var payload struct {
				Entries []struct {
					EntryID string `json:"entry_id"`
				} `json:"entries"`
			}
			if err := json.Unmarshal(g.Payload, &payload); err != nil || len(payload.Entries) != 1 {
				t.Fatalf("payload = %s (%v), want one entry", g.Payload, err)
			}
			e, err := w.entries.Get(context.Background(), tk.EntryID(payload.Entries[0].EntryID))
			if err != nil || e.State != tk.StateSuggested || e.UserID != agenttest.FeatureActingFor || e.Minutes != 45 || e.MatterID != w.matterID {
				t.Fatalf("entry = %+v (%v), want 45 suggested minutes for the acting user on %s", e, err, w.matterID)
			}
		})
	}
}

// rule: ARC-I-038
func TestFRDY06_Injection(t *testing.T) {
	lines := append(slices.Clone(rdy06Note), "Voice note: "+rdycInjected+".")
	w, run, calls := rdy06Run(t, "2026-10-05", 45, lines...)
	rdycCheckInjection(t, w.rdycWorld, run, calls, rdycInjected)
}
