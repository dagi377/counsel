// CONTRACT: kernel/notify. Frozen once on main; changes are additive only and need a
// docs/QUESTIONS.md entry. Bodies live in the T-035 subtasks (notify.go, worker.go).

package notify

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Sentinel errors.
var (
	// ErrUnknownTemplate is returned by Send for a template id no module registered.
	ErrUnknownTemplate = errors.New("notify: unknown template")
	// ErrDuplicateTemplate is returned when a template id is registered twice.
	ErrDuplicateTemplate = errors.New("notify: duplicate template")
	// ErrLocked is returned when a user tries to turn off a mandated channel.
	ErrLocked = errors.New("notify: preference is locked")
	// ErrUnknownChannel is returned for a channel outside the Channel constants.
	ErrUnknownChannel = errors.New("notify: unknown channel")
)

// Channel is a delivery channel; it mirrors core.notification_preference.channel.
type Channel string

// Channels.
const (
	InApp Channel = "in_app"
	Email Channel = "email"
	SMS   Channel = "sms"
	Push  Channel = "push"
)

// Template is a notification template a module registers at boot. Data passed to Send
// fills its placeholders; it never carries document text.
type Template struct {
	ID       string    // module-prefixed id, e.g. "billing.bill_ready"
	Channels []Channel // channels the template can go out on
	Subject  string
	Body     string
}

// Preference is one row of core.notification_preference: whether a user gets a template
// on a channel. A locked preference is mandated and stays enabled.
type Preference struct {
	UserID  string
	Event   string // template id
	Channel Channel
	Enabled bool
	Locked  bool
}

// Notifier registers templates, stores preferences and queues notifications.
type Notifier interface {
	// RegisterTemplate adds a template. Duplicate ids are an error.
	RegisterTemplate(t Template) error
	// SetPreference stores a preference. Turning off a locked one returns ErrLocked.
	SetPreference(ctx context.Context, p Preference) error
	// Send queues the template to the user on every channel the user has not turned off,
	// inside tx (through the outbox for external channels).
	Send(ctx context.Context, tx pgx.Tx, to string, template string, data map[string]string) error
}
