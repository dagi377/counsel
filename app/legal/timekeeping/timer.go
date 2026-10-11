package timekeeping

import (
	"fmt"
	"time"
)

// StartTimer returns a new running timer for userID on matterID, started at now. existing
// are the user's current timers; a running one returns ErrTimerRunning, because a user runs
// at most one timer at a time (timekeeping.timer_one_running).
func StartTimer(existing []Timer, id TimerID, userID, matterID string, now time.Time) (Timer, error) {
	if id == "" || userID == "" || matterID == "" || now.IsZero() {
		return Timer{}, fmt.Errorf("start timer: %w", ErrInvalid)
	}
	for _, t := range existing {
		if t.UserID == userID && t.State == TimerRunning {
			return Timer{}, fmt.Errorf("start timer for %s: %s running: %w", userID, t.ID, ErrTimerRunning)
		}
	}
	return Timer{ID: id, UserID: userID, MatterID: matterID, StartedAt: now, State: TimerRunning}, nil
}

// StopTimer stops t at now and returns the stopped timer and its raw minutes: whole minutes
// since StartedAt less PausedMinutes, never below zero. Rounding happens at billing, not here.
func StopTimer(t Timer, now time.Time) (Timer, int, error) {
	if t.State == TimerStopped {
		return t, 0, fmt.Errorf("stop timer %s: %w", t.ID, ErrTimerStopped)
	}
	if now.Before(t.StartedAt) || t.PausedMinutes < 0 {
		return t, 0, fmt.Errorf("stop timer %s: %w", t.ID, ErrInvalid)
	}
	minutes := int(now.Sub(t.StartedAt)/time.Minute) - t.PausedMinutes
	if minutes < 0 {
		minutes = 0
	}
	t.State = TimerStopped
	return t, minutes, nil
}
