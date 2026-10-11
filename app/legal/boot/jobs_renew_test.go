package boot

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/riverqueue/river"

	cliosync "github.com/dagi377/counsel/app/legal/clio/sync"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/flags"
)

var renewNow = time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)

// subStore is a fake SubscriptionStore that records each mark.
type subStore struct {
	subs    []cliosync.Subscription
	renewed []string
	failed  []string
}

func (s *subStore) Expiring(_ context.Context, before time.Time) ([]cliosync.Subscription, error) {
	var out []cliosync.Subscription
	for _, r := range s.subs {
		if !r.ExpiresAt.After(before) {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *subStore) MarkRenewed(_ context.Context, id string, _, _ time.Time) error {
	s.renewed = append(s.renewed, id)
	return nil
}

func (s *subStore) MarkFailed(_ context.Context, id, _ string) error {
	s.failed = append(s.failed, id)
	return nil
}

// renewer is a fake SubscriptionRenewer: it extends by 30 days or fails.
type renewer struct{ err error }

func (r renewer) Renew(context.Context, string) (time.Time, error) {
	if r.err != nil {
		return time.Time{}, r.err
	}
	return renewNow.Add(30 * 24 * time.Hour), nil
}

// notifier is a fake RenewalNotifier that counts events and alerts.
type notifier struct{ events, alerts int }

func (n *notifier) Emit(context.Context, string, cliosync.RenewalFailure) error {
	n.events++
	return nil
}

func (n *notifier) Alert(context.Context, cliosync.RenewalFailure) error {
	n.alerts++
	return nil
}

// renewKernel returns a kernel with a job set, a fake clock and FlagSourceSync defined and on
// or off.
func renewKernel(t *testing.T, on bool) *kboot.Kernel {
	t.Helper()
	s := flags.New()
	if err := s.Define(flags.Def{Name: FlagSourceSync, Owner: "owner", Expiry: renewNow.AddDate(1, 0, 0)}); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(FlagSourceSync, on); err != nil {
		t.Fatal(err)
	}
	return &kboot.Kernel{Flags: s, Jobs: kboot.NewJobs(), Clock: clockfake.New(renewNow, time.UTC)}
}

func TestAddRenewJobsKinds(t *testing.T) {
	full := func() RenewPorts {
		return RenewPorts{Store: &subStore{}, Renewer: renewer{}, Notifier: &notifier{}}
	}
	tests := []struct {
		name         string
		on           bool
		ports        RenewPorts
		wantKinds    []string
		wantPeriodic int
		wantErr      error
	}{
		{name: "flag off -> kind absent", on: false, ports: RenewPorts{}, wantKinds: []string{}},
		{name: "flag on -> worker and hourly periodic", on: true, ports: full(),
			wantKinds: []string{cliosync.RenewJobKind}, wantPeriodic: 1},
		{name: "flag on, no store -> missing dependency", on: true,
			ports: RenewPorts{Renewer: renewer{}, Notifier: &notifier{}}, wantErr: ErrJobDependency},
		{name: "flag on, no notifier -> missing dependency", on: true,
			ports: RenewPorts{Store: &subStore{}, Renewer: renewer{}}, wantErr: ErrJobDependency},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := renewKernel(t, tt.on)
			err := addRenewJobs(k, tt.ports)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("addRenewJobs() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if got := k.Jobs.Kinds(); !reflect.DeepEqual(got, tt.wantKinds) {
				t.Fatalf("Kinds() = %v, want %v", got, tt.wantKinds)
			}
			if got := len(k.Jobs.Periodic()); got != tt.wantPeriodic {
				t.Fatalf("periodic jobs = %d, want %d", got, tt.wantPeriodic)
			}
			if err := k.Jobs.Validate(); err != nil {
				t.Fatalf("Validate() = %v", err)
			}
		})
	}
}

func TestAddRenewJobsUndefinedFlagIsOff(t *testing.T) {
	k := &kboot.Kernel{Flags: flags.New(), Jobs: kboot.NewJobs()}
	if err := addRenewJobs(k, RenewPorts{}); err != nil {
		t.Fatalf("addRenewJobs() = %v", err)
	}
	if got := k.Jobs.Kinds(); len(got) != 0 {
		t.Fatalf("Kinds() = %v, want none", got)
	}
}

func TestRenewWorkerWork(t *testing.T) {
	expiring := cliosync.Subscription{ID: "sub-1", Source: "src", RemoteID: "r-1", ExpiresAt: renewNow.Add(48 * time.Hour)}
	tests := []struct {
		name        string
		renewErr    error
		wantRenewed []string
		wantFailed  []string
		wantEvents  int
	}{
		{name: "one expiring -> MarkRenewed once", wantRenewed: []string{"sub-1"}},
		{name: "renewer error -> MarkFailed, Work returns nil", renewErr: errors.New("source answered 500"),
			wantFailed: []string{"sub-1"}, wantEvents: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &subStore{subs: []cliosync.Subscription{expiring}}
			n := &notifier{}
			w := newRenewWorker(renewKernel(t, true), RenewPorts{Store: store, Renewer: renewer{err: tt.renewErr}, Notifier: n})
			if err := w.Work(context.Background(), &river.Job[renewArgs]{}); err != nil {
				t.Fatalf("Work() = %v, want nil", err)
			}
			if !slices.Equal(store.renewed, tt.wantRenewed) {
				t.Fatalf("renewed = %v, want %v", store.renewed, tt.wantRenewed)
			}
			if !slices.Equal(store.failed, tt.wantFailed) {
				t.Fatalf("failed = %v, want %v", store.failed, tt.wantFailed)
			}
			if n.events != tt.wantEvents {
				t.Fatalf("events = %d, want %d", n.events, tt.wantEvents)
			}
		})
	}
}
