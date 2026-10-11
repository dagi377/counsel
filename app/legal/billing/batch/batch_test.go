package batch

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/clock"
)

// memDrafts stores drafts by idempotency key, as the unique batch draft key does.
type memDrafts struct {
	byKey map[string]string
	fail  map[string]bool // matter ids whose draft fails
}

func newMemDrafts() *memDrafts {
	return &memDrafts{byKey: map[string]string{}, fail: map[string]bool{}}
}

func (m *memDrafts) Create(_ context.Context, key string, t Target, _ clock.Date) (string, bool, error) {
	if id, ok := m.byKey[key]; ok {
		return id, false, nil
	}
	if m.fail[t.MatterID] {
		return "", false, errors.New("boom")
	}
	id := fmt.Sprintf("d%d", len(m.byKey)+1)
	m.byKey[key] = id
	return id, true, nil
}

// rule: BIL-C-110
func TestBatchRerunCreatesNoDuplicateDraft(t *testing.T) {
	sep := clock.Date{Year: 2026, Month: time.September, Day: 30}
	oct := clock.Date{Year: 2026, Month: time.October, Day: 31}
	targets := []Target{{MatterID: "m1", UnbilledCents: 50000}, {ClientID: "c1", UnbilledCents: 90000}}
	tests := []struct {
		name         string
		second       Run
		wantCreated  int
		wantExisting int
	}{
		{"same run and period", Run{ID: "R1", PeriodTo: sep, Targets: targets}, 0, 2},
		{"new run", Run{ID: "R2", PeriodTo: sep, Targets: targets}, 2, 0},
		{"same run, next period", Run{ID: "R1", PeriodTo: oct, Targets: targets}, 2, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := newMemDrafts()
			first, err := Execute(context.Background(), d, Run{ID: "R1", PeriodTo: sep, Targets: targets})
			if err != nil || len(first.Created) != 2 {
				t.Fatalf("first run: %+v, %v", first, err)
			}
			got, err := Execute(context.Background(), d, tc.second)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Created) != tc.wantCreated || len(got.Existing) != tc.wantExisting {
				t.Fatalf("created %v existing %v, want %d and %d", got.Created, got.Existing, tc.wantCreated, tc.wantExisting)
			}
		})
	}
}

// rule: BIL-C-110
func TestBatchSkipsAndIsolatesFailures(t *testing.T) {
	d := newMemDrafts()
	d.fail["bad"] = true
	run := Run{ID: "R1", PeriodTo: clock.Date{Year: 2026, Month: time.September, Day: 30}, MinimumBillCents: 10000,
		Targets: []Target{{MatterID: "small", UnbilledCents: 9999}, {MatterID: "bad", UnbilledCents: 20000}, {MatterID: "ok", UnbilledCents: 10000}}}
	got, err := Execute(context.Background(), d, run)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Skipped) != 1 || got.Skipped[0].MatterID != "small" {
		t.Errorf("skipped %+v", got.Skipped)
	}
	if len(got.Failed) != 1 || got.Failed[0].Target.MatterID != "bad" {
		t.Errorf("failed %+v", got.Failed)
	}
	if len(got.Created) != 1 {
		t.Errorf("created %v, want the ok matter", got.Created)
	}
}

func TestBatchInvalid(t *testing.T) {
	tests := []struct {
		name string
		run  Run
	}{
		{"no run id", Run{Targets: []Target{{MatterID: "m"}}}},
		{"target with neither", Run{ID: "R", Targets: []Target{{}}}},
		{"target with both", Run{ID: "R", Targets: []Target{{MatterID: "m", ClientID: "c"}}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Execute(context.Background(), newMemDrafts(), tc.run); !errors.Is(err, ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}
		})
	}
}
