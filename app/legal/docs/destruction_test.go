package docs

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type stubDocs struct{ docs []Document }

func (s stubDocs) MatterDocuments(_ context.Context, _ pgx.Tx, matterID string) ([]Document, error) {
	var out []Document
	for _, d := range s.docs {
		if d.MatterID == matterID {
			out = append(out, d)
		}
	}
	return out, nil
}

type stubGates struct {
	opened   int
	ids      []string
	preparer string
	err      error
}

func (g *stubGates) OpenDestruction(_ context.Context, _ pgx.Tx, _ string, ids []string, preparer string) (string, error) {
	if g.err != nil {
		return "", g.err
	}
	g.opened++
	g.ids, g.preparer = slices.Clone(ids), preparer
	return "gate-1", nil
}

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func TestProposeDestructionOpensG14ForClosedMatter(t *testing.T) {
	matterDocs := []Document{
		{ID: "d2", MatterID: "m1", Kind: KindCourtForm},
		{ID: "d1", MatterID: "m1", Kind: KindEngagementLetter},
		{ID: "x1", MatterID: "m1", Kind: "unregistered"},
		{ID: "o1", MatterID: "m2", Kind: KindCourtForm},
	}
	closed := day(2010, time.March, 1)
	gateErr := errors.New("gate down")
	cases := []struct {
		name     string
		closedOn time.Time
		today    time.Time
		preparer string
		gateErr  error
		wantErr  error
		wantIDs  []string
	}{
		{name: "retention run out: gate opened, nothing purged", closedOn: closed, today: day(2025, time.March, 1), preparer: "agent:retention", wantIDs: []string{"d1", "d2"}},
		{name: "one day short of 15 years", closedOn: closed, today: day(2025, time.February, 28), preparer: "u1", wantErr: ErrNothingToPurge},
		{name: "matter still open", today: day(2025, time.March, 1), preparer: "u1", wantErr: ErrMatterOpen},
		{name: "closed in the future", closedOn: day(2026, time.January, 1), today: day(2025, time.March, 1), preparer: "u1", wantErr: ErrMatterOpen},
		{name: "no preparer", closedOn: closed, today: day(2025, time.March, 1), wantErr: ErrInvalid},
		{name: "gate failure surfaces", closedOn: closed, today: day(2025, time.March, 1), preparer: "u1", gateErr: gateErr, wantErr: gateErr},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := &stubGates{err: tc.gateErr}
			p := NewProposer(stubDocs{docs: matterDocs}, g)
			got, err := p.ProposeDestruction(context.Background(), nil, "m1", tc.closedOn, tc.today, tc.preparer)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				if g.opened != 0 {
					t.Fatalf("gate opened %d times on error", g.opened)
				}
				return
			}
			if g.opened != 1 || got.GateID != "gate-1" || !slices.Equal(got.DocumentIDs, tc.wantIDs) || !slices.Equal(g.ids, tc.wantIDs) {
				t.Fatalf("proposal = %+v, gate ids %v opened %d; want ids %v", got, g.ids, g.opened, tc.wantIDs)
			}
			if g.preparer != tc.preparer {
				t.Fatalf("preparer = %q, want %q", g.preparer, tc.preparer)
			}
		})
	}
}

func TestPastRetentionSkipsNeverPurgeClasses(t *testing.T) {
	classes := map[string]RetentionClass{
		"fin":    {ID: RetentionFinancial, MinYears: 1},
		"audit":  {ID: RetentionAudit, MinYears: 1},
		"conf":   {ID: RetentionConflict, NeverPurge: true},
		"nogate": {ID: RetentionOps, MinYears: 1},
		"ops":    {ID: RetentionOps, MinYears: 1, DestructionGate: DestructionGate},
	}
	cases := []struct {
		kind string
		want bool
	}{
		{"fin", false}, {"audit", false}, {"conf", false}, {"nogate", false}, {"ops", true},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			ids := pastRetention([]Document{{ID: "d", MatterID: "m", Kind: tc.kind}}, "m", classes, day(2000, time.January, 1), day(2025, time.January, 1))
			if got := len(ids) == 1; got != tc.want {
				t.Fatalf("proposed = %v, want %v", got, tc.want)
			}
		})
	}
}
