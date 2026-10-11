package matter

import (
	"context"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/eventroute/eventroutetest"
	"github.com/dagi377/counsel/kernel/registry"
)

type eventWritesStub struct{ stages, notes int }

func (s *eventWritesStub) ChangeStage(context.Context, StageChange, string) error {
	s.stages++
	return nil
}

func (s *eventWritesStub) AddNote(context.Context, NewNote) (string, error) {
	s.notes++
	return "n1", nil
}

// rule: ARC-I-042
// rule: ARC-I-051
// rule: ARC-I-017
func TestMatterEventRoutesAccess(t *testing.T) {
	agent := registry.Principal{Kind: registry.PrincipalAgent, RunID: "r1", ActingFor: eventroutetest.Lawyer}
	tests := []struct {
		name   string
		p      registry.Principal
		path   string
		body   string
		status int
		writes int
	}{
		{"stage: walled matter is not found", eventroutetest.User(eventroutetest.Walled), "/matters/m1/stage", `{"to_stage_id":"s2"}`, 404, 0},
		{"stage: no capability on the matter", eventroutetest.User(eventroutetest.NoCap), "/matters/m1/stage", `{"to_stage_id":"s2"}`, 403, 0},
		{"stage: no capability firm-wide", eventroutetest.User(eventroutetest.NoFirmCap), "/matters/m1/stage", `{"to_stage_id":"s2"}`, 403, 0},
		{"stage: allowed user", eventroutetest.User(eventroutetest.Lawyer), "/matters/m1/stage", `{"from_stage_id":"s1","to_stage_id":"s2"}`, 200, 1},
		{"stage: agent may not move a stage", agent, "/matters/m1/stage", `{"to_stage_id":"s2"}`, 403, 0},
		{"stage: no target stage", eventroutetest.User(eventroutetest.Lawyer), "/matters/m1/stage", `{}`, 400, 0},
		{"note: walled matter is not found", eventroutetest.User(eventroutetest.Walled), "/matters/m1/notes", `{"body_ref":"obj/n1"}`, 404, 0},
		{"note: no capability on the matter", eventroutetest.User(eventroutetest.NoCap), "/matters/m1/notes", `{"body_ref":"obj/n1"}`, 403, 0},
		{"note: allowed user", eventroutetest.User(eventroutetest.Lawyer), "/matters/m1/notes", `{"body_ref":"obj/n1"}`, 201, 1},
		{"note: text is refused", eventroutetest.User(eventroutetest.Lawyer), "/matters/m1/notes", `{"body_ref":"obj/n1","text":"x"}`, 400, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &eventWritesStub{}
			h := &EventHTTP{Route: eventroutetest.Route(tt.p), Writes: w, Now: func() time.Time { return time.Unix(0, 0) }}
			rec := eventroutetest.Post(h.Routes, tt.path, tt.body)
			if rec.Code != tt.status || w.stages+w.notes != tt.writes {
				t.Fatalf("status %d writes %d, want %d and %d: %s", rec.Code, w.stages+w.notes, tt.status, tt.writes, rec.Body)
			}
		})
	}
}

func TestMatterEventRoutesNotWired(t *testing.T) {
	h := &EventHTTP{}
	if rec := eventroutetest.Post(h.Routes, "/matters/m1/notes", `{"body_ref":"obj/n1"}`); rec.Code != 503 {
		t.Fatalf("status %d, want 503", rec.Code)
	}
}
