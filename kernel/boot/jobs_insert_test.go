package boot

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/dagi377/counsel/kernel/db"
)

// nopInserter accepts every insert.
type nopInserter struct{}

func (nopInserter) Insert(context.Context, river.JobArgs, *river.InsertOpts) (*rivertype.JobInsertResult, error) {
	return &rivertype.JobInsertResult{}, nil
}

// TestRunHandsAppsTheEnqueuer checks both roles open the insert-only job client on the direct
// DSN before Register, hand it to apps as Kernel.Enqueuer and close it when the role ends.
func TestRunHandsAppsTheEnqueuer(t *testing.T) {
	errDial := errors.New("dial refused")
	tests := []struct {
		name    string
		role    Role
		openErr error
		wantErr error
	}{
		{"server role", RoleServer, nil, nil},
		{"worker role", RoleWorker, nil, nil},
		{"open error stops run", RoleServer, errDial, errDial},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rig := &runRig{}
			e := rig.env(t)
			var direct string
			closed := false
			e.inserter = func(_ context.Context, cfg db.Config) (JobInserter, func(), error) {
				direct = cfg.DirectDSN
				if tc.openErr != nil {
					return nil, nil, tc.openErr
				}
				return nopInserter{}, func() { closed = true }, nil
			}
			var got JobInserter
			app := App{Name: "enq", Register: func(_ context.Context, k *Kernel) error {
				got = k.Enqueuer
				return nil
			}}
			cfg := baseConfig(tc.role)
			err := run(context.Background(), cfg, e, app)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("run() = %v, want %v", err, tc.wantErr)
			}
			if err != nil && strings.Contains(err.Error(), "s3cr3t") {
				t.Errorf("run() error carries the database URL: %v", err)
			}
			// rule: ARC-U-011
			if direct != cfg.DirectDatabaseURL {
				t.Errorf("inserter opened %q, want the direct DSN", direct)
			}
			if tc.wantErr != nil {
				return
			}
			if got == nil {
				t.Fatal("Register saw no Kernel.Enqueuer")
			}
			if !closed {
				t.Error("inserter pool not closed when the role ended")
			}
		})
	}
}
