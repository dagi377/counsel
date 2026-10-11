package outbox

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/registry"
)

type memDL struct {
	dls     map[string]*DeadLetter
	order   []string
	effects map[string]int
}

func newMemDL(dls ...DeadLetter) *memDL {
	m := &memDL{dls: map[string]*DeadLetter{}, effects: map[string]int{}}
	for i := range dls {
		dl := dls[i]
		m.dls[dl.ID] = &dl
		m.order = append(m.order, dl.ID)
	}
	return m
}

func (m *memDL) Open(context.Context) ([]DeadLetter, error) {
	var out []DeadLetter
	for _, id := range m.order {
		if dl := m.dls[id]; dl.ReplayedAt == nil {
			out = append(out, *dl)
		}
	}
	return out, nil
}

func (m *memDL) Requeue(_ context.Context, id string, at time.Time) (bool, error) {
	dl, ok := m.dls[id]
	if !ok {
		return false, ErrNotFound
	}
	if dl.ReplayedAt != nil {
		return false, nil
	}
	dl.ReplayedAt = &at
	m.effects[id]++
	return true, nil
}

type pages struct{ keys []string }

func (p *pages) Page(_ context.Context, key, _ string) error {
	p.keys = append(p.keys, key)
	return nil
}

type auditRows struct{ rows []string }

func (a *auditRows) Record(_ context.Context, _ registry.Principal, action, id string, _ map[string]string) error {
	a.rows = append(a.rows, action+":"+id)
	return nil
}

func toronto(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestTriageDue(t *testing.T) {
	loc := toronto(t)
	at := func(m time.Month, d, h int) time.Time { return time.Date(2026, m, d, h, 0, 0, 0, loc) }
	thanksgiving := at(10, 12, 0)
	dls := &DeadLetters{Loc: loc, Holiday: func(d time.Time) bool {
		y, m, dd := d.Date()
		return time.Date(y, m, dd, 0, 0, 0, 0, loc).Equal(thanksgiving)
	}}
	// rule: ARC-U-024
	tests := []struct {
		name   string
		deadAt time.Time
		want   time.Time
	}{
		{"tuesday to wednesday", at(10, 6, 14), at(10, 7, 14)},
		{"friday to monday", at(10, 2, 14), at(10, 5, 14)},
		{"saturday counts from monday start", at(10, 3, 10), at(10, 6, 0)},
		{"friday before holiday monday", at(10, 9, 9), at(10, 13, 9)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := dls.TriageDue(tc.deadAt); !got.Equal(tc.want) {
				t.Fatalf("TriageDue(%v) = %v, want %v", tc.deadAt, got, tc.want)
			}
		})
	}
}

func TestPageOverdue(t *testing.T) {
	loc := toronto(t)
	now := time.Date(2026, 10, 7, 15, 0, 0, 0, loc) // Wednesday
	replayed := now
	store := newMemDL(
		DeadLetter{ID: "old", DeadAt: now.AddDate(0, 0, -1).Add(-time.Hour)},
		DeadLetter{ID: "fresh", DeadAt: now.Add(-2 * time.Hour)},
		DeadLetter{ID: "edge", DeadAt: now.AddDate(0, 0, -1)},
		DeadLetter{ID: "done", DeadAt: now.AddDate(0, 0, -5), ReplayedAt: &replayed},
	)
	p := &pages{}
	d := &DeadLetters{Store: store, Pager: p, Loc: loc, Now: func() time.Time { return now }}
	// rule: ARC-U-024
	got, err := d.PageOverdue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"old", "edge"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("paged = %v, want %v", got, want)
	}
	if want := []string{"outbox.deadletter:old", "outbox.deadletter:edge"}; !reflect.DeepEqual(p.keys, want) {
		t.Fatalf("page keys = %v, want %v", p.keys, want)
	}
}

func TestReplay(t *testing.T) {
	admin := registry.Principal{Kind: registry.PrincipalUser, UserID: "u-admin"}
	isAdmin := func(_ context.Context, p registry.Principal) bool { return p.UserID == "u-admin" }
	// rule: ARC-U-024
	tests := []struct {
		name      string
		by        registry.Principal
		calls     [][]string
		wantErr   error
		wantEff   map[string]int
		wantAudit []string
	}{
		{
			name:    "twice is one effect, audited each time",
			by:      admin,
			calls:   [][]string{{"a"}, {"a"}},
			wantEff: map[string]int{"a": 1},
			wantAudit: []string{
				ActionReplayRequested + ":a", ActionReplayDone + ":a",
				ActionReplayRequested + ":a", ActionReplayDone + ":a",
			},
		},
		{
			name:      "batch",
			by:        admin,
			calls:     [][]string{{"a", "b"}},
			wantEff:   map[string]int{"a": 1, "b": 1},
			wantAudit: []string{ActionReplayRequested + ":a", ActionReplayDone + ":a", ActionReplayRequested + ":b", ActionReplayDone + ":b"},
		},
		{name: "non admin user", by: registry.Principal{Kind: registry.PrincipalUser, UserID: "u-2"}, calls: [][]string{{"a"}}, wantErr: ErrNotAdmin, wantEff: map[string]int{}},
		{name: "agent never replays", by: registry.Principal{Kind: registry.PrincipalAgent, RunID: "r", ActingFor: "u-admin", UserID: "u-admin"}, calls: [][]string{{"a"}}, wantErr: ErrNotAdmin, wantEff: map[string]int{}},
		{name: "no ids", by: admin, calls: [][]string{nil}, wantErr: ErrNoDeadLetters, wantEff: map[string]int{}},
		{name: "unknown id", by: admin, calls: [][]string{{"zz"}}, wantErr: ErrNotFound, wantEff: map[string]int{}, wantAudit: []string{ActionReplayRequested + ":zz"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newMemDL(DeadLetter{ID: "a"}, DeadLetter{ID: "b"})
			audit := &auditRows{}
			d := &DeadLetters{Store: store, Audit: audit, IsAdmin: isAdmin}
			var err error
			for _, ids := range tc.calls {
				if _, err = d.Replay(context.Background(), tc.by, ids); err != nil {
					break
				}
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Replay err = %v, want %v", err, tc.wantErr)
			}
			if !reflect.DeepEqual(store.effects, tc.wantEff) {
				t.Errorf("effects = %v, want %v", store.effects, tc.wantEff)
			}
			if !reflect.DeepEqual(audit.rows, tc.wantAudit) {
				t.Errorf("audit = %v, want %v", audit.rows, tc.wantAudit)
			}
		})
	}
}
