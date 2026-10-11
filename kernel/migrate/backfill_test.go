package migrate

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/clock/clockfake"
)

var errReset = errors.New("connection reset")

// fakeTable is a table of n rows keyed 1..n whose new column starts NULL.
type fakeTable struct {
	filled    []bool
	fills     int      // rows written, counting repeats
	calls     int      // FillBatch calls
	afters    []string // cursor passed to each call
	maxLimit  int      // largest limit asked for
	clk       *clockfake.Fake
	batchCost time.Duration // fake time one batch takes
	failAt    int           // FillBatch call that fails (0: never)
}

func newFakeTable(n int, clk *clockfake.Fake) *fakeTable {
	return &fakeTable{filled: make([]bool, n), clk: clk}
}

func (f *fakeTable) Name() string { return "work_item.subject_kind" }

func (f *fakeTable) FillBatch(_ context.Context, after string, limit int) (string, int, error) {
	f.calls++
	f.afters = append(f.afters, after)
	if f.failAt > 0 && f.calls == f.failAt {
		return "", 0, errReset
	}
	if limit > f.maxLimit {
		f.maxLimit = limit
	}
	start := 0
	if after != "" {
		k, err := strconv.Atoi(after)
		if err != nil {
			return "", 0, err
		}
		start = k // keys are 1-based; index k is the row after key k
	}
	end := min(start+limit, len(f.filled))
	for i := start; i < end; i++ {
		if !f.filled[i] { // WHERE new_col IS NULL
			f.filled[i] = true
			f.fills++
		}
	}
	f.clk.Advance(f.batchCost)
	if end == start {
		return after, 0, nil
	}
	return strconv.Itoa(end), end - start, nil
}

func (f *fakeTable) nulls() int {
	n := 0
	for _, ok := range f.filled {
		if !ok {
			n++
		}
	}
	return n
}

// memCheckpoints keeps checkpoints in memory; killAt cancels the run right after that batch
// is checkpointed, like a pod killed between batches.
type memCheckpoints struct {
	cps    map[string]Checkpoint
	killAt int
	cancel context.CancelFunc
}

func (m *memCheckpoints) Load(_ context.Context, step string) (Checkpoint, bool, error) {
	cp, ok := m.cps[step]
	return cp, ok, nil
}

func (m *memCheckpoints) Save(_ context.Context, step string, cp Checkpoint) error {
	m.cps[step] = cp
	if m.killAt > 0 && cp.Batches == m.killAt && m.cancel != nil {
		m.cancel()
	}
	return nil
}

// fakeSleeper advances the fake clock instead of sleeping.
type fakeSleeper struct {
	clk   *clockfake.Fake
	slept []time.Duration
}

func (s *fakeSleeper) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.slept = append(s.slept, d)
	s.clk.Advance(d)
	return nil
}

// scriptedLag returns the values in order, then the last one forever.
type scriptedLag struct{ vals []time.Duration }

func (l *scriptedLag) ReplicationLag(context.Context) (time.Duration, error) {
	v := l.vals[0]
	if len(l.vals) > 1 {
		l.vals = l.vals[1:]
	}
	return v, nil
}

type harness struct {
	clk   *clockfake.Fake
	table *fakeTable
	cps   *memCheckpoints
	sleep *fakeSleeper
}

func newHarness(rows int) *harness {
	clk := clockfake.New(time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC), time.UTC)
	return &harness{
		clk:   clk,
		table: newFakeTable(rows, clk),
		cps:   &memCheckpoints{cps: map[string]Checkpoint{}},
		sleep: &fakeSleeper{clk: clk},
	}
}

func (h *harness) opts() Options {
	return Options{Checkpoints: h.cps, Clock: h.clk, Sleep: h.sleep}
}

// rule: MIG-062
// rule: ARC-U-018
func TestMIG_062_BatchedBackfill(t *testing.T) {
	const rows = 1_000_000

	t.Run("kill at batch 120 resumes at 120", func(t *testing.T) {
		h := newHarness(rows)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		h.cps.killAt, h.cps.cancel = 120, cancel

		res, err := Backfill(ctx, h.table, h.opts())
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("first run err = %v, want context.Canceled", err)
		}
		if res.Batches != 120 || h.table.fills != 120*MaxBatchSize {
			t.Fatalf("first run: batches %d fills %d, want 120 and %d", res.Batches, h.table.fills, 120*MaxBatchSize)
		}

		h.cps.killAt = 0
		callsBefore := h.table.calls
		res, err = Backfill(context.Background(), h.table, h.opts())
		if err != nil {
			t.Fatalf("restart: %v", err)
		}
		if res.StartBatch != 120 {
			t.Errorf("restart StartBatch = %d, want 120", res.StartBatch)
		}
		if got, want := h.table.afters[callsBefore], strconv.Itoa(120*MaxBatchSize); got != want {
			t.Errorf("restart first cursor = %q, want %q", got, want)
		}
		if res.Batches < 200 {
			t.Errorf("batches = %d, want >= 200", res.Batches)
		}
		if h.table.maxLimit > MaxBatchSize {
			t.Errorf("largest batch = %d, want <= %d", h.table.maxLimit, MaxBatchSize)
		}
		if n := h.table.nulls(); n != 0 {
			t.Errorf("NULL rows after backfill = %d, want 0", n)
		}
		if h.table.fills != rows {
			t.Errorf("rows written = %d, want %d (no batch redone)", h.table.fills, rows)
		}
		if res.Rows != rows {
			t.Errorf("Result.Rows = %d, want %d", res.Rows, rows)
		}
		// A finished step is a no-op on the next run.
		calls := h.table.calls
		if _, err := Backfill(context.Background(), h.table, h.opts()); err != nil || h.table.calls != calls {
			t.Errorf("rerun after done: err %v, %d new calls", err, h.table.calls-calls)
		}
	})

	tests := []struct {
		name        string
		rows        int
		edit        func(*Options, *harness)
		wantErr     error
		wantBatches int
		check       func(t *testing.T, h *harness, res Result)
	}{
		{
			name: "batch size above 1000 refused", rows: 10,
			edit:    func(o *Options, _ *harness) { o.BatchSize = 5000 },
			wantErr: ErrBatchTooLarge,
		},
		{
			name: "missing sleeper refused", rows: 10,
			edit:    func(o *Options, _ *harness) { o.Sleep = nil },
			wantErr: ErrBadOptions,
		},
		{
			name: "pause between batches", rows: 2500,
			wantBatches: 3,
			check: func(t *testing.T, h *harness, _ Result) {
				if len(h.sleep.slept) != 3 {
					t.Fatalf("sleeps = %v, want one pause per committed batch", h.sleep.slept)
				}
				for _, d := range h.sleep.slept {
					if d != DefaultPause {
						t.Errorf("pause = %v, want %v", d, DefaultPause)
					}
				}
			},
		},
		{
			name: "paused while replication lag above 5s", rows: 1500,
			edit: func(o *Options, _ *harness) {
				o.Lag = &scriptedLag{vals: []time.Duration{6 * time.Second, 9 * time.Second, 5 * time.Second, time.Second}}
			},
			wantBatches: 2,
			check: func(t *testing.T, h *harness, res Result) {
				if res.LagPauses != 2 {
					t.Errorf("lag pauses = %d, want 2", res.LagPauses)
				}
				if h.table.calls < 1 || h.sleep.slept[0] != DefaultLagPoll || h.sleep.slept[1] != DefaultLagPoll {
					t.Errorf("sleeps = %v, want two lag polls before the first batch", h.sleep.slept)
				}
			},
		},
		{
			name: "slow batch halves the next", rows: 2000,
			edit: func(_ *Options, h *harness) { h.table.batchCost = 2 * time.Second },
			check: func(t *testing.T, h *harness, _ Result) {
				if len(h.table.afters) < 2 || h.table.afters[1] != "1000" {
					t.Fatalf("cursors = %v", h.table.afters)
				}
				// 1000, then 500, 250, 125, ... so more than two batches.
				if h.table.calls <= 3 {
					t.Errorf("calls = %d, want the batch size to shrink after slow batches", h.table.calls)
				}
			},
		},
		{
			name: "batch error keeps the last checkpoint", rows: 5000,
			edit:    func(_ *Options, h *harness) { h.table.failAt = 3 },
			wantErr: errReset,
			check: func(t *testing.T, h *harness, res Result) {
				if cp := h.cps.cps[h.table.Name()]; cp.Batches != 2 || cp.After != "2000" || cp.Done {
					t.Errorf("checkpoint = %+v, want 2 batches after 2000", cp)
				}
				if res.Batches != 2 {
					t.Errorf("batches = %d, want 2", res.Batches)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(tt.rows)
			o := h.opts()
			if tt.edit != nil {
				tt.edit(&o, h)
			}
			res, err := Backfill(context.Background(), h.table, o)
			switch {
			case tt.wantErr == nil && err != nil:
				t.Fatalf("err = %v", err)
			case tt.wantErr != nil && err == nil:
				t.Fatalf("err = nil, want %v", tt.wantErr)
			case tt.wantErr != nil && !errors.Is(err, tt.wantErr):
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && h.table.nulls() != 0 {
				t.Errorf("NULL rows = %d, want 0", h.table.nulls())
			}
			if tt.wantBatches > 0 && res.Batches != tt.wantBatches {
				t.Errorf("batches = %d, want %d", res.Batches, tt.wantBatches)
			}
			if tt.check != nil {
				tt.check(t, h, res)
			}
		})
	}
}
