package sync

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/clock/clockfake"
)

type memStore struct {
	rows map[string]*Subscription
}

func (m *memStore) Expiring(_ context.Context, before time.Time) ([]Subscription, error) {
	var out []Subscription
	for _, id := range []string{"s1", "s2", "s3"} {
		if r, ok := m.rows[id]; ok && !r.ExpiresAt.After(before) {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (m *memStore) MarkRenewed(_ context.Context, id string, exp, at time.Time) error {
	r := m.rows[id]
	r.ExpiresAt, r.LastRenewedAt, r.LastError = exp, &at, ""
	return nil
}

func (m *memStore) MarkFailed(_ context.Context, id, lastError string) error {
	m.rows[id].LastError = lastError
	return nil
}

type fakeSource struct {
	fail    bool
	patches []string
}

func (f *fakeSource) Renew(_ context.Context, remoteID string) (time.Time, error) {
	f.patches = append(f.patches, remoteID)
	if f.fail {
		return time.Time{}, errors.New("source answered 500")
	}
	return time.Date(2026, 12, 24, 0, 0, 0, 0, time.UTC), nil
}

type recNotifier struct {
	events []string
	alerts []RenewalFailure
}

func (r *recNotifier) Emit(_ context.Context, typ string, _ RenewalFailure) error {
	r.events = append(r.events, typ)
	return nil
}

func (r *recNotifier) Alert(_ context.Context, f RenewalFailure) error {
	r.alerts = append(r.alerts, f)
	return nil
}

// rule: MIG-042
// rule: MIG-041
// rule: ARC-I-049
func TestMIG_042_SubscriptionRenewal(t *testing.T) {
	expires := time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC)
	newExp := time.Date(2026, 12, 24, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		now         time.Time
		fail        bool
		wantPatches int
		wantExp     time.Time
		wantRenewed bool
		wantErr     bool
		wantEvents  int
		wantAlerts  int
	}{
		{"8 days left: not due", time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC), false, 0, expires, false, false, 0, 0},
		{"7 days left: renewed", time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC), false, 1, newExp, true, false, 0, 0},
		{"6 days left, source fails: last_error, event, no alert", time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC), true, 1, expires, false, true, 1, 0},
		{"exactly 72 h left, source fails: no alert yet", time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC), true, 1, expires, false, true, 1, 0},
		{"under 72 h left, source fails: alert", time.Date(2026, 11, 27, 1, 0, 0, 0, time.UTC), true, 1, expires, false, true, 1, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := &memStore{rows: map[string]*Subscription{
				"s1": {ID: "s1", Source: "clio", RemoteID: "r1", Events: []string{"matter"}, ExpiresAt: expires},
			}}
			src := &fakeSource{fail: tc.fail}
			note := &recNotifier{}
			job := &RenewJob{Store: store, Source: src, Notifier: note, Clock: clockfake.New(tc.now, time.UTC)}
			if _, err := job.Run(context.Background()); err != nil {
				t.Fatalf("Run: %v", err)
			}
			row := store.rows["s1"]
			if len(src.patches) != tc.wantPatches {
				t.Errorf("patches = %d, want %d", len(src.patches), tc.wantPatches)
			}
			if !row.ExpiresAt.Equal(tc.wantExp) {
				t.Errorf("expires_at = %v, want %v", row.ExpiresAt, tc.wantExp)
			}
			if (row.LastRenewedAt != nil) != tc.wantRenewed || (tc.wantRenewed && !row.LastRenewedAt.Equal(tc.now)) {
				t.Errorf("last_renewed_at = %v, want set=%v at %v", row.LastRenewedAt, tc.wantRenewed, tc.now)
			}
			if (row.LastError != "") != tc.wantErr {
				t.Errorf("last_error = %q, want set=%v", row.LastError, tc.wantErr)
			}
			if len(note.events) != tc.wantEvents {
				t.Errorf("events = %v, want %d", note.events, tc.wantEvents)
			}
			for _, e := range note.events {
				if e != EventRenewalFailed {
					t.Errorf("event = %q, want %q", e, EventRenewalFailed)
				}
			}
			if len(note.alerts) != tc.wantAlerts {
				t.Errorf("alerts = %d, want %d", len(note.alerts), tc.wantAlerts)
			}
		})
	}
}

// rule: MIG-042
func TestMIG_042_HourlyRetryAfterFailure(t *testing.T) {
	expires := time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC)
	clk := clockfake.New(time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC), time.UTC)
	store := &memStore{rows: map[string]*Subscription{
		"s1": {ID: "s1", Source: "clio", RemoteID: "r1", ExpiresAt: expires},
	}}
	src := &fakeSource{fail: true}
	note := &recNotifier{}
	job := &RenewJob{Store: store, Source: src, Notifier: note, Clock: clk}
	steps := []struct {
		fail                 bool
		wantRenewed, wantErr bool
	}{
		{true, false, true},
		{true, false, true},
		{false, true, false},
	}
	for i, st := range steps {
		src.fail = st.fail
		res, err := job.Run(context.Background())
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if (res.Renewed == 1) != st.wantRenewed || (store.rows["s1"].LastError != "") != st.wantErr {
			t.Errorf("run %d: res=%+v last_error=%q", i, res, store.rows["s1"].LastError)
		}
		clk.Advance(RenewInterval)
	}
	if len(src.patches) != 3 {
		t.Errorf("patches = %d, want 3", len(src.patches))
	}
}
