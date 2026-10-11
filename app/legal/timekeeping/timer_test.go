package timekeeping

import (
	"errors"
	"testing"
	"time"
)

var timerStart = time.Date(2026, time.October, 5, 9, 0, 0, 0, time.UTC)

func TestStartTimer(t *testing.T) {
	running := Timer{ID: "t1", UserID: "u1", MatterID: "m1", StartedAt: timerStart, State: TimerRunning}
	tests := []struct {
		name     string
		existing []Timer
		user     string
		matter   string
		wantErr  error
	}{
		{"first timer starts", nil, "u1", "m1", nil},
		{"second running timer for same user refused", []Timer{running}, "u1", "m2", ErrTimerRunning},
		{"other user's running timer does not block", []Timer{running}, "u2", "m1", nil},
		{"stopped timer does not block", []Timer{{ID: "t0", UserID: "u1", State: TimerStopped}}, "u1", "m1", nil},
		{"paused timer does not block", []Timer{{ID: "t0", UserID: "u1", State: TimerPaused}}, "u1", "m1", nil},
		{"missing user", nil, "", "m1", ErrInvalid},
		{"missing matter", nil, "u1", "", ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := StartTimer(tc.existing, "t9", tc.user, tc.matter, timerStart)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}
			want := Timer{ID: "t9", UserID: tc.user, MatterID: tc.matter, StartedAt: timerStart, State: TimerRunning}
			if got != want {
				t.Fatalf("got %+v, want %+v", got, want)
			}
		})
	}
}

func TestStopTimer(t *testing.T) {
	run := Timer{ID: "t1", UserID: "u1", MatterID: "m1", StartedAt: timerStart, State: TimerRunning}
	paused := run
	paused.PausedMinutes = 10
	tests := []struct {
		name        string
		timer       Timer
		elapsed     time.Duration
		wantMinutes int
		wantErr     error
	}{
		{"raw whole minutes", run, 47 * time.Minute, 47, nil},
		{"partial minute truncated, not rounded", run, 47*time.Minute + 59*time.Second, 47, nil},
		{"under a minute is zero", run, 30 * time.Second, 0, nil},
		{"paused minutes subtracted", paused, 70 * time.Minute, 60, nil},
		{"never below zero", paused, 5 * time.Minute, 0, nil},
		{"stopped timer refused", Timer{ID: "t1", State: TimerStopped, StartedAt: timerStart}, time.Hour, 0, ErrTimerStopped},
		{"stop before start refused", run, -time.Minute, 0, ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, minutes, err := StopTimer(tc.timer, timerStart.Add(tc.elapsed))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}
			if minutes != tc.wantMinutes || got.State != TimerStopped {
				t.Fatalf("got %d minutes state %s, want %d stopped", minutes, got.State, tc.wantMinutes)
			}
		})
	}
}
