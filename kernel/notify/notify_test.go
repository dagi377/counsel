package notify_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/notify"
	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
)

type key struct {
	user, event string
	ch          notify.Channel
}

// memStore is an in-memory PreferenceStore with the same sticky-lock rule as PGStore.
type memStore struct {
	mu sync.Mutex
	m  map[key]notify.Preference
}

func (s *memStore) Load(_ context.Context, _ pgx.Tx, user, event string) ([]notify.Preference, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []notify.Preference
	for k, p := range s.m {
		if k.user == user && k.event == event {
			out = append(out, p)
		}
	}
	return out, nil
}

func (s *memStore) Save(_ context.Context, p notify.Preference) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key{p.UserID, p.Event, p.Channel}
	if old, ok := s.m[k]; ok && old.Locked {
		if !p.Enabled {
			return notify.ErrLocked
		}
		p.Locked = true
	}
	s.m[k] = p
	return nil
}

func newService(t *testing.T) (*notify.Service, *outboxfake.Fake) {
	t.Helper()
	out := outboxfake.New()
	s := notify.New(&memStore{m: map[key]notify.Preference{}}, out)
	err := s.RegisterTemplate(notify.Template{
		ID:       "billing.bill_ready",
		Channels: []notify.Channel{notify.InApp, notify.Email},
		Subject:  "Bill {{number}} is ready",
		Body:     "Hello {{name}}, bill {{number}} is ready: {{link}}",
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, out
}

func sent(t *testing.T, out *outboxfake.Fake) []notify.Requested {
	t.Helper()
	msgs, err := out.Pending(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	var rs []notify.Requested
	for _, m := range msgs {
		if m.Type != notify.EventRequested {
			t.Fatalf("type = %q", m.Type)
		}
		var r notify.Requested
		if err := json.Unmarshal(m.Payload, &r); err != nil {
			t.Fatal(err)
		}
		rs = append(rs, r)
	}
	return rs
}

func TestRegisterTemplate(t *testing.T) {
	tests := []struct {
		name string
		tpl  notify.Template
		want error
	}{
		{"ok", notify.Template{ID: "docs.shared", Channels: []notify.Channel{notify.InApp}}, nil},
		{"duplicate", notify.Template{ID: "billing.bill_ready", Channels: []notify.Channel{notify.InApp}}, notify.ErrDuplicateTemplate},
		{"no module prefix", notify.Template{ID: "bill_ready", Channels: []notify.Channel{notify.InApp}}, notify.ErrInvalidTemplate},
		{"no channel", notify.Template{ID: "docs.x"}, notify.ErrInvalidTemplate},
		{"unknown channel", notify.Template{ID: "docs.x", Channels: []notify.Channel{"fax"}}, notify.ErrUnknownChannel},
		{"channel twice", notify.Template{ID: "docs.x", Channels: []notify.Channel{notify.Email, notify.Email}}, notify.ErrInvalidTemplate},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newService(t)
			if err := s.RegisterTemplate(tc.tpl); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestSendFollowsPreferences(t *testing.T) {
	tests := []struct {
		name  string
		prefs []notify.Preference
		want  []notify.Channel
	}{
		{"no preference: every channel", nil, []notify.Channel{notify.InApp, notify.Email}},
		{"email off", []notify.Preference{{UserID: "u1", Event: "billing.bill_ready", Channel: notify.Email}}, []notify.Channel{notify.InApp}},
		{"both off", []notify.Preference{
			{UserID: "u1", Event: "billing.bill_ready", Channel: notify.Email},
			{UserID: "u1", Event: "billing.bill_ready", Channel: notify.InApp},
		}, nil},
		{"other user's preference ignored", []notify.Preference{{UserID: "u2", Event: "billing.bill_ready", Channel: notify.Email}}, []notify.Channel{notify.InApp, notify.Email}},
		{"other event's preference ignored", []notify.Preference{{UserID: "u1", Event: "docs.shared", Channel: notify.Email}}, []notify.Channel{notify.InApp, notify.Email}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, out := newService(t)
			ctx := context.Background()
			for _, p := range tc.prefs {
				if err := s.SetPreference(ctx, p); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.Send(ctx, nil, "u1", "billing.bill_ready", map[string]string{"number": "42"}); err != nil {
				t.Fatal(err)
			}
			got := sent(t, out)
			if len(got) != len(tc.want) {
				t.Fatalf("sent %d, want %d: %+v", len(got), len(tc.want), got)
			}
			for i, r := range got {
				if r.Channel != tc.want[i] || r.To != "u1" || r.Template != "billing.bill_ready" || r.Data["number"] != "42" {
					t.Fatalf("message %d = %+v", i, r)
				}
			}
		})
	}
}

func TestLockedPreference(t *testing.T) {
	ctx := context.Background()
	locked := notify.Preference{UserID: "u1", Event: "billing.bill_ready", Channel: notify.Email, Enabled: true, Locked: true}
	tests := []struct {
		name  string
		first *notify.Preference
		set   notify.Preference
		want  error
	}{
		{"locked must be enabled", nil, notify.Preference{UserID: "u1", Event: "billing.bill_ready", Channel: notify.Email, Locked: true}, notify.ErrLocked},
		{"cannot turn off locked", &locked, notify.Preference{UserID: "u1", Event: "billing.bill_ready", Channel: notify.Email}, notify.ErrLocked},
		{"can re-enable locked", &locked, notify.Preference{UserID: "u1", Event: "billing.bill_ready", Channel: notify.Email, Enabled: true}, nil},
		{"unknown channel", nil, notify.Preference{UserID: "u1", Event: "billing.bill_ready", Channel: "fax"}, notify.ErrUnknownChannel},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, out := newService(t)
			if tc.first != nil {
				if err := s.SetPreference(ctx, *tc.first); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.SetPreference(ctx, tc.set); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if tc.first == nil {
				return
			}
			// A locked channel still goes out.
			if err := s.Send(ctx, nil, "u1", "billing.bill_ready", nil); err != nil {
				t.Fatal(err)
			}
			if got := sent(t, out); len(got) != 2 {
				t.Fatalf("sent %d, want 2 (locked email stays on)", len(got))
			}
		})
	}
}

func TestSendCarriesNoDocumentText(t *testing.T) {
	tests := []struct {
		name     string
		template string
		data     map[string]string
		want     error
	}{
		{"placeholders only", "billing.bill_ready", map[string]string{"name": "A", "number": "7", "link": "https://x/b/7"}, nil},
		{"unregistered template", "billing.nope", nil, notify.ErrUnknownTemplate},
		{"key not in template", "billing.bill_ready", map[string]string{"body": "x"}, notify.ErrInvalidData},
		{"long value", "billing.bill_ready", map[string]string{"name": strings.Repeat("a", notify.MaxValueLen+1)}, notify.ErrInvalidData},
		{"multi-line value", "billing.bill_ready", map[string]string{"name": "line one\nline two"}, notify.ErrInvalidData},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, out := newService(t)
			err := s.Send(context.Background(), nil, "u1", tc.template, tc.data)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if err != nil {
				if len(sent(t, out)) != 0 {
					t.Fatal("refused send still emitted")
				}
				for _, v := range tc.data {
					if strings.Contains(err.Error(), v) {
						t.Fatal("error carries data value")
					}
				}
			}
		})
	}
}
