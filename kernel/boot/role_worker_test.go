package boot

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/dagi377/counsel/kernel/db"
)

// fakeStarter records Start and Stop calls in place of a River client.
type fakeStarter struct {
	mu       sync.Mutex
	starts   int
	stops    int
	startErr error
	started  chan struct{}
}

func (f *fakeStarter) Start(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts++
	if f.started != nil {
		close(f.started)
	}
	return f.startErr
}

func (f *fakeStarter) Stop(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
	return ctx.Err()
}

func (f *fakeStarter) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.starts, f.stops
}

// testDB points at distinct pooled and direct endpoints. pgxpool connects lazily, so no
// server is needed while the fake starter never runs a query.
var testDB = db.Config{
	PooledDSN: "postgres://app@127.0.0.1:6432/counsel",
	DirectDSN: "postgres://app@127.0.0.1:5432/counsel",
}

func jobsWith(t *testing.T, periodicKind string) *Jobs {
	t.Helper()
	j := NewJobs()
	if err := AddWorker(j, pingWorker{}); err != nil {
		t.Fatal(err)
	}
	if periodicKind != "" {
		j.AddPeriodic(hourly(periodicKind), periodicKind)
	}
	return j
}

func TestRiverConfig(t *testing.T) {
	tests := []struct {
		name       string
		maxWorkers int
		want       int
	}{
		{"default", 0, 10},
		{"negative is default", -1, 10},
		{"set", 3, 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			j := jobsWith(t, "test.ping")
			got := WorkerConfig{MaxWorkers: tc.maxWorkers}.riverConfig(j)
			if got.Workers != j.Workers {
				t.Error("Workers is not the job set's bundle")
			}
			if len(got.PeriodicJobs) != 1 || got.PeriodicJobs[0] != j.Periodic()[0] {
				t.Errorf("PeriodicJobs = %v, want the job set's", got.PeriodicJobs)
			}
			if len(got.Queues) != 1 {
				t.Errorf("queues = %v, want only the default queue", got.Queues)
			}
			if q := got.Queues[river.QueueDefault]; q.MaxWorkers != tc.want {
				t.Errorf("default MaxWorkers = %d, want %d", q.MaxWorkers, tc.want)
			}
		})
	}
}

func TestRunWorker(t *testing.T) {
	tests := []struct {
		name      string
		periodic  string
		dbCfg     db.Config
		buildErr  error
		startErr  error
		wantErr   error // errors.Is target; nil means a clean stop
		anyErr    bool  // an error without a sentinel to match
		wantStart int
		wantStop  int
		wantBuild bool
	}{
		{name: "start then stop on cancel", periodic: "test.ping", dbCfg: testDB,
			wantStart: 1, wantStop: 1, wantBuild: true},
		{name: "invalid job set stops before start", periodic: "test.missing", dbCfg: testDB,
			wantErr: ErrJobs},
		// rule: ARC-U-011
		{name: "direct dsn equal to pooler is refused", dbCfg: db.Config{
			PooledDSN: testDB.PooledDSN, DirectDSN: testDB.PooledDSN}, anyErr: true},
		{name: "client build error", dbCfg: testDB, buildErr: errors.New("boom"),
			anyErr: true, wantBuild: true},
		{name: "start error", dbCfg: testDB, startErr: errors.New("boom"),
			anyErr: true, wantStart: 1, wantBuild: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			k := &Kernel{Jobs: jobsWith(t, tc.periodic)}
			fake := &fakeStarter{startErr: tc.startErr, started: make(chan struct{})}
			var built bool
			var gotPool *pgxpool.Pool
			build := func(pool *pgxpool.Pool, cfg *river.Config) (starter, error) {
				built, gotPool = true, pool
				if cfg.Workers != k.Jobs.Workers {
					t.Error("client config does not carry the job set's workers")
				}
				if tc.buildErr != nil {
					return nil, tc.buildErr
				}
				return fake, nil
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- runWorker(ctx, k, WorkerConfig{DB: tc.dbCfg}, build) }()

			if tc.wantStart > 0 && tc.startErr == nil {
				select {
				case <-fake.started:
				case err := <-done:
					t.Fatalf("runWorker returned before Start: %v", err)
				case <-time.After(5 * time.Second):
					t.Fatal("Start not called")
				}
				if _, stops := fake.counts(); stops != 0 {
					t.Fatalf("Stop called %d times before cancel", stops)
				}
				cancel()
			}

			var err error
			select {
			case err = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("runWorker did not return")
			}
			switch {
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("err = %v, want %v", err, tc.wantErr)
				}
			case tc.anyErr:
				if err == nil {
					t.Error("err = nil, want an error")
				}
			default:
				if err != nil {
					t.Errorf("err = %v, want nil", err)
				}
			}
			if built != tc.wantBuild {
				t.Errorf("client built = %v, want %v", built, tc.wantBuild)
			}
			if built && gotPool == nil {
				t.Error("client built without the direct pool")
			}
			if starts, stops := fake.counts(); starts != tc.wantStart || stops != tc.wantStop {
				t.Errorf("starts, stops = %d, %d; want %d, %d", starts, stops, tc.wantStart, tc.wantStop)
			}
		})
	}
}

func TestRunWorkerNoJobs(t *testing.T) {
	build := func(*pgxpool.Pool, *river.Config) (starter, error) {
		t.Fatal("client built without a job set")
		return nil, nil
	}
	if err := runWorker(context.Background(), &Kernel{}, WorkerConfig{DB: testDB}, build); !errors.Is(err, ErrConfig) {
		t.Errorf("err = %v, want ErrConfig", err)
	}
}

// The production builder must satisfy newStarter.
var _ newStarter = newRiverClient
