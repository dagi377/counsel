// Package notifyfake is an in-memory implementation of the kernel/notify contract for tests.
package notifyfake

import (
	"context"
	"fmt"
	"maps"
	"sync"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/notify"
)

var _ notify.Notifier = (*Fake)(nil)

// Message is one notification the Fake would have delivered.
type Message struct {
	To       string
	Template string
	Channel  notify.Channel
	Data     map[string]string
}

type prefKey struct {
	user, event string
	ch          notify.Channel
}

// Fake keeps templates, preferences and sent messages in memory. A channel with no
// stored preference is enabled. A Fake is safe for concurrent use.
type Fake struct {
	mu        sync.Mutex
	templates map[string]notify.Template
	prefs     map[prefKey]notify.Preference
	sent      []Message
}

// New returns an empty Fake.
func New() *Fake {
	return &Fake{templates: map[string]notify.Template{}, prefs: map[prefKey]notify.Preference{}}
}

// RegisterTemplate adds a template.
func (f *Fake) RegisterTemplate(t notify.Template) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.templates[t.ID]; ok {
		return fmt.Errorf("register %q: %w", t.ID, notify.ErrDuplicateTemplate)
	}
	for _, c := range t.Channels {
		if !valid(c) {
			return fmt.Errorf("register %q: %q: %w", t.ID, c, notify.ErrUnknownChannel)
		}
	}
	f.templates[t.ID] = t
	return nil
}

// SetPreference stores a preference.
func (f *Fake) SetPreference(_ context.Context, p notify.Preference) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !valid(p.Channel) {
		return fmt.Errorf("set preference: %q: %w", p.Channel, notify.ErrUnknownChannel)
	}
	k := prefKey{p.UserID, p.Event, p.Channel}
	if old, ok := f.prefs[k]; ok && old.Locked && !p.Enabled {
		return fmt.Errorf("set preference %s/%s: %w", p.Event, p.Channel, notify.ErrLocked)
	}
	if p.Locked && !p.Enabled {
		return fmt.Errorf("set preference %s/%s: %w", p.Event, p.Channel, notify.ErrLocked)
	}
	f.prefs[k] = p
	return nil
}

// Send records one message per enabled channel. tx is ignored.
func (f *Fake) Send(_ context.Context, _ pgx.Tx, to, template string, data map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.templates[template]
	if !ok {
		return fmt.Errorf("send %q: %w", template, notify.ErrUnknownTemplate)
	}
	for _, c := range t.Channels {
		if p, ok := f.prefs[prefKey{to, template, c}]; ok && !p.Enabled {
			continue
		}
		f.sent = append(f.sent, Message{To: to, Template: template, Channel: c, Data: maps.Clone(data)})
	}
	return nil
}

// Sent returns a copy of the messages sent so far.
func (f *Fake) Sent() []Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Message(nil), f.sent...)
}

func valid(c notify.Channel) bool {
	switch c {
	case notify.InApp, notify.Email, notify.SMS, notify.Push:
		return true
	}
	return false
}
