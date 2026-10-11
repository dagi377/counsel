package ontario

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
)

func TestPublishAddedHolidays(t *testing.T) {
	all, err := Holidays()
	if err != nil || len(all) < 2 {
		t.Fatalf("Holidays = %v, %v", all, err)
	}
	tests := []struct {
		name  string
		known []clock.Date
		want  []clock.Date
	}{
		{name: "first load announces every holiday", want: AddedHolidays(nil, all)},
		{name: "all known announces nothing", known: all},
		{name: "one new holiday", known: all[1:], want: all[:1]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := outboxfake.New()
			got, err := PublishAddedHolidays(context.Background(), nil, ev, tt.known)
			if err != nil {
				t.Fatalf("PublishAddedHolidays: %v", err)
			}
			msgs := ev.Messages()
			if len(got) != len(tt.want) || len(msgs) != len(tt.want) {
				t.Fatalf("added %d, events %d, want %d", len(got), len(msgs), len(tt.want))
			}
			for i, m := range msgs {
				var body HolidayAdded
				if err := json.Unmarshal(m.Payload, &body); err != nil || m.Type != EventHolidayAdded ||
					m.Subject.Kind != SubjectHoliday || m.Subject.ID != tt.want[i].String() || body.Date != tt.want[i].String() {
					t.Fatalf("event %d = %+v (%v)", i, m, err)
				}
			}
		})
	}
	if _, err := PublishAddedHolidays(context.Background(), nil, nil, nil); err == nil {
		t.Fatal("nil emitter: want an error")
	}
}

func TestAddedHolidaysDedups(t *testing.T) {
	d1 := clock.Date{Year: 2026, Month: 1, Day: 1}
	d2 := clock.Date{Year: 2026, Month: 12, Day: 25}
	tests := []struct {
		name        string
		known, next []clock.Date
		want        int
	}{
		{"duplicate in next", nil, []clock.Date{d1, d1, d2}, 2},
		{"known removed", []clock.Date{d1}, []clock.Date{d1, d2}, 1},
		{"empty", nil, nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AddedHolidays(tt.known, tt.next); len(got) != tt.want {
				t.Fatalf("AddedHolidays = %v, want %d", got, tt.want)
			}
		})
	}
}
