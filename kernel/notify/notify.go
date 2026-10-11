package notify

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/outbox"
)

// ErrInvalidTemplate is returned by RegisterTemplate for a malformed template.
var ErrInvalidTemplate = errors.New("notify: invalid template")

// ErrInvalidData is returned by Send when data does not fit the template: an unknown
// placeholder, or a value long enough or multi-line enough to be document text.
var ErrInvalidData = errors.New("notify: invalid data")

// EventRequested is the outbox event type Send emits, one per channel. A channel worker
// renders and delivers it.
const EventRequested = "notify.requested"

// SubjectKindUser is the outbox subject kind for a notification's recipient.
const SubjectKindUser = "user"

// MaxValueLen caps one data value in runes. Data fills short placeholders (a name, a
// number, a link); anything longer is treated as document text and refused.
const MaxValueLen = 200

var (
	templateID  = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_.]*$`)
	placeholder = regexp.MustCompile(`\{\{\s*([a-z][a-z0-9_]*)\s*\}\}`)
)

// Requested is the payload of an EventRequested outbox event. It names the template and
// carries only the placeholder data, never rendered text.
type Requested struct {
	To       string            `json:"to"`
	Template string            `json:"template"`
	Channel  Channel           `json:"channel"`
	Data     map[string]string `json:"data,omitempty"`
}

// PreferenceStore keeps core.notification_preference rows.
type PreferenceStore interface {
	// Load returns the stored preferences of one user for one template. tx may be nil.
	Load(ctx context.Context, tx pgx.Tx, userID, event string) ([]Preference, error)
	// Save upserts a preference. It returns ErrLocked if the stored row is locked and p
	// turns it off.
	Save(ctx context.Context, p Preference) error
}

// Service is the Notifier. Templates live in memory (modules register them at boot);
// preferences live in a PreferenceStore; Send emits through the outbox inside the caller's tx.
type Service struct {
	store PreferenceStore
	out   outbox.Emitter

	mu        sync.RWMutex
	templates map[string]Template
	keys      map[string][]string // template id -> sorted placeholder names
}

var _ Notifier = (*Service)(nil)

// New returns a Service.
func New(store PreferenceStore, out outbox.Emitter) *Service {
	return &Service{store: store, out: out, templates: map[string]Template{}, keys: map[string][]string{}}
}

// Template returns a registered template.
func (s *Service) Template(id string) (Template, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.templates[id]
	return t, ok
}

// RegisterTemplate adds a template. The id is module-prefixed ("module.event"); the
// template names at least one channel, each channel once.
func (s *Service) RegisterTemplate(t Template) error {
	if !templateID.MatchString(t.ID) {
		return fmt.Errorf("register %q: id must be module.event: %w", t.ID, ErrInvalidTemplate)
	}
	if len(t.Channels) == 0 {
		return fmt.Errorf("register %q: no channel: %w", t.ID, ErrInvalidTemplate)
	}
	seen := map[Channel]bool{}
	for _, c := range t.Channels {
		if !validChannel(c) {
			return fmt.Errorf("register %q: %q: %w", t.ID, c, ErrUnknownChannel)
		}
		if seen[c] {
			return fmt.Errorf("register %q: channel %q twice: %w", t.ID, c, ErrInvalidTemplate)
		}
		seen[c] = true
	}
	var keys []string
	for _, m := range placeholder.FindAllStringSubmatch(t.Subject+"\n"+t.Body, -1) {
		if !slices.Contains(keys, m[1]) {
			keys = append(keys, m[1])
		}
	}
	slices.Sort(keys)
	t.Channels = slices.Clone(t.Channels)

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.templates[t.ID]; ok {
		return fmt.Errorf("register %q: %w", t.ID, ErrDuplicateTemplate)
	}
	s.templates[t.ID] = t
	s.keys[t.ID] = keys
	return nil
}

// SetPreference stores a preference. A locked preference must stay enabled, and a stored
// locked preference cannot be turned off.
func (s *Service) SetPreference(ctx context.Context, p Preference) error {
	if !validChannel(p.Channel) {
		return fmt.Errorf("set preference: %q: %w", p.Channel, ErrUnknownChannel)
	}
	if p.UserID == "" || p.Event == "" {
		return fmt.Errorf("set preference: user and event are required: %w", ErrInvalidTemplate)
	}
	if p.Locked && !p.Enabled {
		return fmt.Errorf("set preference %s/%s: %w", p.Event, p.Channel, ErrLocked)
	}
	if err := s.store.Save(ctx, p); err != nil {
		return fmt.Errorf("set preference %s/%s: %w", p.Event, p.Channel, err)
	}
	return nil
}

// Send queues the template to the user on each of its channels the user has not turned
// off: one EventRequested outbox event per channel, inside tx. A channel with no stored
// preference is on.
func (s *Service) Send(ctx context.Context, tx pgx.Tx, to, template string, data map[string]string) error {
	s.mu.RLock()
	t, ok := s.templates[template]
	keys := s.keys[template]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("send %q: %w", template, ErrUnknownTemplate)
	}
	if to == "" {
		return fmt.Errorf("send %q: no recipient: %w", template, ErrInvalidData)
	}
	if err := checkData(keys, data); err != nil {
		return fmt.Errorf("send %q: %w", template, err)
	}
	prefs, err := s.store.Load(ctx, tx, to, template)
	if err != nil {
		return fmt.Errorf("send %q: load preferences: %w", template, err)
	}
	off := map[Channel]bool{}
	for _, p := range prefs {
		if !p.Enabled && !p.Locked {
			off[p.Channel] = true
		}
	}
	for _, c := range t.Channels {
		if off[c] {
			continue
		}
		ev := Requested{To: to, Template: template, Channel: c, Data: maps.Clone(data)}
		if err := s.out.Emit(ctx, tx, outbox.Subject{Kind: SubjectKindUser, ID: to}, EventRequested, ev); err != nil {
			return fmt.Errorf("send %q on %s: %w", template, c, err)
		}
	}
	return nil
}

// checkData refuses keys the template has no placeholder for, and values that look like
// document text. Errors name the key, never the value.
func checkData(keys []string, data map[string]string) error {
	for k, v := range data {
		if !slices.Contains(keys, k) {
			return fmt.Errorf("key %q is not a placeholder: %w", k, ErrInvalidData)
		}
		if utf8.RuneCountInString(v) > MaxValueLen || strings.ContainsAny(v, "\r\n") {
			return fmt.Errorf("value of %q is too long or multi-line: %w", k, ErrInvalidData)
		}
	}
	return nil
}

func validChannel(c Channel) bool {
	switch c {
	case InApp, Email, SMS, Push:
		return true
	}
	return false
}

// PGStore keeps preferences in core.notification_preference.
type PGStore struct {
	DB db.DB
}

var _ PreferenceStore = PGStore{}

// Load implements PreferenceStore. With a nil tx it opens its own.
func (s PGStore) Load(ctx context.Context, tx pgx.Tx, userID, event string) ([]Preference, error) {
	if tx == nil {
		var out []Preference
		err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			var err error
			out, err = s.Load(ctx, tx, userID, event)
			return err
		})
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT channel, enabled, locked FROM core.notification_preference
		WHERE user_id = $1 AND event = $2`, userID, event)
	if err != nil {
		return nil, fmt.Errorf("load preferences: %w", err)
	}
	defer rows.Close()
	var out []Preference
	for rows.Next() {
		p := Preference{UserID: userID, Event: event}
		if err := rows.Scan(&p.Channel, &p.Enabled, &p.Locked); err != nil {
			return nil, fmt.Errorf("scan preference: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Save implements PreferenceStore. A stored lock is sticky: the upsert never clears it, and
// it skips a locked row that p would turn off, reporting that as ErrLocked.
func (s PGStore) Save(ctx context.Context, p Preference) error {
	return s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO core.notification_preference (user_id, event, channel, enabled, locked)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (user_id, event, channel) DO UPDATE
			SET enabled = EXCLUDED.enabled, locked = core.notification_preference.locked OR EXCLUDED.locked
			WHERE NOT (core.notification_preference.locked AND NOT EXCLUDED.enabled)`,
			p.UserID, p.Event, string(p.Channel), p.Enabled, p.Locked)
		if err != nil {
			return fmt.Errorf("save preference: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrLocked
		}
		return nil
	})
}
