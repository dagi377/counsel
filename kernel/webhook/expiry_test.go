package webhook

import (
	"context"
	"errors"
	"testing"
	"time"
)

type noticeKey struct {
	id   string
	exp  time.Time
	lead time.Duration
}

type memExpiry struct {
	eps     map[string]*EndpointExpiry
	notices map[noticeKey]bool
}

func newMemExpiry(eps ...EndpointExpiry) *memExpiry {
	m := &memExpiry{eps: map[string]*EndpointExpiry{}, notices: map[noticeKey]bool{}}
	for i := range eps {
		m.eps[eps[i].ID] = &eps[i]
	}
	return m
}

func (m *memExpiry) Expiring(_ context.Context, before time.Time) ([]EndpointExpiry, error) {
	var out []EndpointExpiry
	for _, e := range m.eps {
		if e.ExpiresAt != nil && !e.ExpiresAt.After(before) {
			out = append(out, *e)
		}
	}
	return out, nil
}

func (m *memExpiry) ClaimNotice(_ context.Context, id string, exp time.Time, lead time.Duration) (bool, error) {
	k := noticeKey{id, exp.UTC(), lead}
	if m.notices[k] {
		return false, nil
	}
	m.notices[k] = true
	return true, nil
}

func (m *memExpiry) SetExpiry(_ context.Context, id string, exp *time.Time) error {
	e, ok := m.eps[id]
	if !ok {
		return ErrUnknownEndpoint
	}
	e.ExpiresAt = exp
	return nil
}

func ptr(t time.Time) *time.Time { return &t }

// rule: ARC-I-049
func TestARC_I_049_EndpointExpiringEvent(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	tests := []struct {
		name    string
		ep      EndpointExpiry
		runs    int
		want    int
		wantLed string
	}{
		{"expires in 14 days: one event", EndpointExpiry{ID: "e1", ExpiresAt: ptr(now.Add(14 * day))}, 3, 1, "336h0m0s"},
		{"expires in 15 days: none yet", EndpointExpiry{ID: "e1", ExpiresAt: ptr(now.Add(15 * day))}, 1, 0, ""},
		{"expires in 1 day: one-day notice", EndpointExpiry{ID: "e1", ExpiresAt: ptr(now.Add(day))}, 2, 1, "24h0m0s"},
		{"tenant-owned: never", EndpointExpiry{ID: "e1"}, 1, 0, ""},
		{"already expired: none", EndpointExpiry{ID: "e1", ExpiresAt: ptr(now.Add(-time.Hour))}, 1, 0, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newMemExpiry(tc.ep)
			var got []ExpiringNotice
			emit := func(_ context.Context, typ string, n ExpiringNotice) error {
				if typ != EventEndpointExpiring {
					t.Errorf("type = %q", typ)
				}
				got = append(got, n)
				return nil
			}
			for range tc.runs {
				if _, err := NotifyExpiring(context.Background(), store, emit, now); err != nil {
					t.Fatal(err)
				}
			}
			if len(got) != tc.want {
				t.Fatalf("events = %d, want %d", len(got), tc.want)
			}
			if tc.want > 0 && got[0].Lead != tc.wantLed {
				t.Errorf("lead = %q, want %q", got[0].Lead, tc.wantLed)
			}
		})
	}
}

// rule: ARC-I-049
func TestARC_I_049_RenewEndpoint(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	later := now.Add(60 * 24 * time.Hour)
	tests := []struct {
		name    string
		id      string
		exp     *time.Time
		wantErr error
		wantExp *time.Time
	}{
		{"later expiry", "e1", &later, nil, &later},
		{"null: never expires", "e1", nil, nil, nil},
		{"past expiry rejected", "e1", ptr(now.Add(-time.Minute)), ErrPastExpiry, ptr(now.Add(14 * 24 * time.Hour))},
		{"unknown endpoint", "nope", &later, ErrUnknownEndpoint, ptr(now.Add(14 * 24 * time.Hour))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newMemExpiry(EndpointExpiry{ID: "e1", ExpiresAt: ptr(now.Add(14 * 24 * time.Hour))})
			err := RenewEndpoint(context.Background(), store, tc.id, tc.exp, now)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			got := store.eps["e1"].ExpiresAt
			if (got == nil) != (tc.wantExp == nil) || (got != nil && !got.Equal(*tc.wantExp)) {
				t.Errorf("expires_at = %v, want %v", got, tc.wantExp)
			}
			if tc.wantErr == nil {
				n, _ := NotifyExpiring(context.Background(), store, func(context.Context, string, ExpiringNotice) error { return nil }, now)
				if n != 0 {
					t.Errorf("renewed endpoint still emits %d expiring events", n)
				}
			}
		})
	}
}

// rule: ARC-I-049
func TestARC_I_049_TenantOwnedHasNoExpiry(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		owned bool
		want  bool
	}{{true, false}, {false, true}}
	for _, tc := range tests {
		got := InitialExpiry(tc.owned, now, 30*24*time.Hour)
		if (got != nil) != tc.want {
			t.Errorf("owned=%v: expires_at=%v", tc.owned, got)
		}
	}
}
