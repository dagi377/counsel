package rates

import (
	"context"
	"errors"
	"testing"
)

// rule: BIL-C-005
func TestMove(t *testing.T) {
	manual := int64(50000)
	rows := []RateRow{
		{Source: SourceMatterUser, MatterID: "m1", UserID: "u1", RateCents: 30000, EffectiveFrom: from},
		{Source: SourceMatterUser, MatterID: "m2", UserID: "u1", RateCents: 32000, EffectiveFrom: from},
		{Source: SourceClientUser, ClientID: "c3", UserID: "u1", RateCents: 34000, EffectiveFrom: from},
	}
	manualEntry := Entry{ID: "e1", Input: input, Resolved: Resolved{RateCents: manual, Source: SourceManual}}
	manualEntry.Input.ManualRateCents, manualEntry.Input.CanSetRates = &manual, true
	tiered := Entry{ID: "e2", Input: input, Resolved: Resolved{RateCents: 30000, Source: SourceMatterUser}}
	tests := []struct {
		name        string
		entry       Entry
		toMatter    string
		toClient    string
		want        Resolved
		wantRecheck bool
		wantErr     error
	}{
		{"tier-1 manual rate kept, recheck set", manualEntry, "m2", "c1", Resolved{RateCents: manual, Source: SourceManual}, true, nil},
		{"matter rate re-resolved on new matter", tiered, "m2", "c1", Resolved{RateCents: 32000, Source: SourceMatterUser}, false, nil},
		{"falls to client tier of new client", tiered, "m3", "c3", Resolved{RateCents: 34000, Source: SourceClientUser}, false, nil},
		{"no rate on new matter", tiered, "m9", "c9", Resolved{}, false, ErrNoRate},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := store(t, rows)
			got, err := s.Move(context.Background(), tc.entry, tc.toMatter, tc.toClient)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("move: %v", err)
			}
			if got.Resolved != tc.want || got.RateRecheck != tc.wantRecheck {
				t.Fatalf("got %+v recheck=%v, want %+v recheck=%v", got.Resolved, got.RateRecheck, tc.want, tc.wantRecheck)
			}
			if got.Input.MatterID != tc.toMatter || got.Input.ClientID != tc.toClient {
				t.Fatalf("entry on %s/%s, want %s/%s", got.Input.MatterID, got.Input.ClientID, tc.toMatter, tc.toClient)
			}
		})
	}
}
