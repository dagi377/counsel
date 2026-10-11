package work

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/outbox"
)

// ErrInvalidEvent: an event body lacks a required id.
var ErrInvalidEvent = errors.New("work: invalid event")

// Outbox subject kinds of the work events.
const (
	SubjectAutomationRun = "work.automation_run"
	SubjectAbsence       = "work.absence"
)

// AutomationRun is the body of EventAutomationRunFailed and EventAutomationRunSkipped: ids
// and a reason code only, never the item's text. UserID is the automation's owner, whom the
// explaining run acts for.
type AutomationRun struct {
	RunID        string `json:"run_id"`
	AutomationID string `json:"automation_id"`
	WorkItemID   string `json:"work_item_id,omitempty"`
	ReasonCode   string `json:"reason_code"`
	UserID       string `json:"user_id"`
}

// AbsenceRequested is the body of EventAbsenceRequested: a user asked for time away, from
// and to inclusive (YYYY-MM-DD, America/Toronto). No reason text.
type AbsenceRequested struct {
	AbsenceID string `json:"absence_id"`
	UserID    string `json:"user_id"`
	From      string `json:"from"`
	To        string `json:"to"`
}

// PublishAutomationRun publishes EventAutomationRunFailed or EventAutomationRunSkipped (typ)
// in tx (nil: the emitter opens its own). Minimal publisher: main has no automation runner
// yet (Q-EVENTS-7).
func PublishAutomationRun(ctx context.Context, em outbox.Emitter, tx pgx.Tx, typ string, b AutomationRun) error {
	if typ != EventAutomationRunFailed && typ != EventAutomationRunSkipped {
		return fmt.Errorf("publish %s: %w", typ, ErrInvalidEvent)
	}
	if b.RunID == "" || b.AutomationID == "" || b.ReasonCode == "" {
		return fmt.Errorf("publish %s: %w", typ, ErrInvalidEvent)
	}
	return publish(ctx, em, tx, outbox.Subject{Kind: SubjectAutomationRun, ID: b.RunID}, typ, b)
}

// PublishAbsenceRequested publishes EventAbsenceRequested. Minimal publisher: main has no
// team-availability service yet (Q-EVENTS-7).
func PublishAbsenceRequested(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b AbsenceRequested) error {
	if b.AbsenceID == "" || b.UserID == "" || b.From == "" || b.To == "" {
		return fmt.Errorf("publish %s: %w", EventAbsenceRequested, ErrInvalidEvent)
	}
	return publish(ctx, em, tx, outbox.Subject{Kind: SubjectAbsence, ID: b.AbsenceID}, EventAbsenceRequested, b)
}

// publish emits one event; a nil emitter publishes nothing.
func publish(ctx context.Context, em outbox.Emitter, tx pgx.Tx, s outbox.Subject, typ string, body any) error {
	if em == nil {
		return nil
	}
	// rule: ARC-I-030
	if err := em.Emit(ctx, tx, s, typ, body); err != nil {
		return fmt.Errorf("publish %s: %w", typ, err)
	}
	return nil
}
