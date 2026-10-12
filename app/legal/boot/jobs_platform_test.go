package boot

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/platform"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/db/dbfake"
	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
)

func TestAddPlatformJobs(t *testing.T) {
	full := func() *kboot.Kernel {
		return &kboot.Kernel{Jobs: kboot.NewJobs(), DB: noConnPool{dbfake.New()}, Clock: clockfake.New(time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC), time.UTC),
			Emitter: outboxfake.New()}
	}
	tests := []struct {
		name      string
		kernel    func() *kboot.Kernel
		wantKinds []string
		wantPer   int
		wantErr   error
	}{
		{name: "both runners and their periodic jobs", kernel: full,
			wantKinds: []string{platform.KindDestructionRun, platform.KindReleaseSend}, wantPer: 2},
		{name: "no emitter -> destruction runner only", kernel: func() *kboot.Kernel { k := full(); k.Emitter = nil; return k },
			wantKinds: []string{platform.KindDestructionRun}, wantPer: 1},
		{name: "no clock -> missing dependency", kernel: func() *kboot.Kernel { k := full(); k.Clock = nil; return k },
			wantErr: ErrJobDependency},
		{name: "no job set -> missing dependency", kernel: func() *kboot.Kernel { k := full(); k.Jobs = nil; return k },
			wantErr: ErrJobDependency},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := tt.kernel()
			err := addPlatformJobs(k, PlatformJobPorts{})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("addPlatformJobs() = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if got := k.Jobs.Kinds(); !reflect.DeepEqual(got, tt.wantKinds) {
				t.Fatalf("Kinds() = %v, want %v", got, tt.wantKinds)
			}
			if got := len(k.Jobs.Periodic()); got != tt.wantPer {
				t.Fatalf("periodic jobs = %d, want %d", got, tt.wantPer)
			}
			if err := k.Jobs.Validate(); err != nil {
				t.Fatalf("Validate() = %v", err)
			}
		})
	}
}
