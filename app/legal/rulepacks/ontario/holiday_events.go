package ontario

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/outbox"
)

// EventHolidayAdded is the outbox event type of a court holiday the calendar did not list
// before. F-DL-03 (app/legal/deadlines) recomputes the deadlines that may now roll.
const EventHolidayAdded = "holiday.added"

// SubjectHoliday is the outbox subject kind of a holiday; the subject id is its date.
const SubjectHoliday = "cal.holiday"

// HolidayAdded is the body of EventHolidayAdded.
type HolidayAdded struct {
	Date     string `json:"date"`
	Calendar string `json:"calendar"`
}

// calendarOntario names this package's holiday calendar in events.
const calendarOntario = "ontario"

// AddedHolidays returns the holidays in next that known does not list, in next's order, each
// once.
func AddedHolidays(known, next []clock.Date) []clock.Date {
	seen := make(map[clock.Date]bool, len(known)+len(next))
	for _, d := range known {
		seen[d] = true
	}
	var out []clock.Date
	for _, d := range next {
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return out
}

// PublishAddedHolidays emits EventHolidayAdded in tx for every holiday in this package's
// calendar (Holidays) that known does not list, and returns them. The caller passes the
// holidays the deadline engine used until now and loads the new calendar in the same tx.
func PublishAddedHolidays(ctx context.Context, tx pgx.Tx, em outbox.Emitter, known []clock.Date) ([]clock.Date, error) {
	if em == nil {
		return nil, fmt.Errorf("publish holidays: no event emitter")
	}
	next, err := Holidays()
	if err != nil {
		return nil, fmt.Errorf("publish holidays: %w", err)
	}
	added := AddedHolidays(known, next)
	for _, d := range added {
		ev := HolidayAdded{Date: d.String(), Calendar: calendarOntario}
		if err := em.Emit(ctx, tx, outbox.Subject{Kind: SubjectHoliday, ID: d.String()}, EventHolidayAdded, ev); err != nil {
			return nil, fmt.Errorf("publish holiday %s: %w", d, err)
		}
	}
	return added, nil
}
