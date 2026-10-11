package boot

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/db/dbfake"
)

type fakeReleaser struct {
	at  []time.Time
	err error
}

func (f *fakeReleaser) ReleaseExpired(_ context.Context, _ pgx.Tx, now time.Time) (int64, error) {
	f.at = append(f.at, now)
	return 2, f.err
}

func TestAddWithdrawJobs(t *testing.T) {
	now := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		kernel    func() *kboot.Kernel
		wantKinds []string
		wantErr   error
	}{
		{name: "db and clock -> release kind", kernel: func() *kboot.Kernel {
			return &kboot.Kernel{Jobs: kboot.NewJobs(), DB: dbfake.New(), Clock: clockfake.New(now, time.UTC)}
		}, wantKinds: []string{KindWithdrawalReleaseExpired}},
		{name: "no db -> dependency error", kernel: func() *kboot.Kernel {
			return &kboot.Kernel{Jobs: kboot.NewJobs(), Clock: clockfake.New(now, time.UTC)}
		}, wantErr: ErrJobDependency},
		{name: "no clock -> dependency error", kernel: func() *kboot.Kernel {
			return &kboot.Kernel{Jobs: kboot.NewJobs(), DB: dbfake.New()}
		}, wantErr: ErrJobDependency},
		{name: "no job set -> dependency error", kernel: func() *kboot.Kernel {
			return &kboot.Kernel{}
		}, wantErr: ErrJobDependency},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := tt.kernel()
			err := addWithdrawJobs(k, &fakeReleaser{})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("addWithdrawJobs() = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if got := k.Jobs.Kinds(); !slices.Equal(got, tt.wantKinds) {
				t.Fatalf("kinds = %v, want %v", got, tt.wantKinds)
			}
			if len(k.Jobs.Periodic()) != 1 {
				t.Fatalf("periodic jobs = %d, want 1", len(k.Jobs.Periodic()))
			}
		})
	}
}

func TestWithdrawReleaseWorkerUsesKernelClock(t *testing.T) {
	now := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	errStore := errors.New("store down")
	tests := []struct {
		name    string
		err     error
		wantErr error
	}{
		{name: "released at kernel now"},
		{name: "store error fails the job", err: errStore, wantErr: errStore},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rel := &fakeReleaser{err: tt.err}
			w := withdrawReleaseWorker{db: dbfake.New(), clock: clockfake.New(now, time.UTC), store: rel}
			err := w.Work(context.Background(), &river.Job[withdrawReleaseArgs]{})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Work() = %v, want %v", err, tt.wantErr)
			}
			if len(rel.at) != 1 || !rel.at[0].Equal(now) {
				t.Fatalf("ReleaseExpired at = %v, want [%v]", rel.at, now)
			}
		})
	}
}
