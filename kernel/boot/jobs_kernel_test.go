package boot

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/db/dbfake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/outbox"
	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
)

var errRunDue = errors.New("run due exploded")

// failingGates is a gate engine whose RunDue always fails.
type failingGates struct{ gate.Engine }

func (failingGates) RunDue(context.Context, pgx.Tx) (int, error) { return 0, errRunDue }

func TestAddKernelJobsKinds(t *testing.T) {
	tests := []struct {
		name       string
		kernel     func() *Kernel
		wantKinds  []string
		wantPeriod int
		wantErr    error
	}{
		{name: "no dependencies -> no kernel kinds", kernel: func() *Kernel {
			return &Kernel{Jobs: NewJobs()}
		}, wantKinds: []string{}},
		{name: "gates without DB -> gate.run_due absent", kernel: func() *Kernel {
			return &Kernel{Jobs: NewJobs(), Gates: gatefake.New(time.Now, gatefake.Caps{})}
		}, wantKinds: []string{}},
		{name: "gates and DB and outbox -> both kinds", kernel: func() *Kernel {
			ob := outboxfake.New()
			return &Kernel{
				Jobs: NewJobs(), DB: dbfake.New(), Gates: gatefake.New(time.Now, gatefake.Caps{}),
				Outbox: &outbox.Deliverer{Relay: ob, Inbox: ob},
			}
		}, wantKinds: []string{KindGateRunDue, KindOutboxRelay}, wantPeriod: 2},
		{name: "outbox without relay -> config error", kernel: func() *Kernel {
			return &Kernel{Jobs: NewJobs(), DB: dbfake.New(), Outbox: &outbox.Deliverer{}}
		}, wantErr: ErrConfig},
		{name: "no job set -> config error", kernel: func() *Kernel { return &Kernel{} }, wantErr: ErrConfig},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := tt.kernel()
			err := addKernelJobs(k)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("addKernelJobs() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if got := k.Jobs.Kinds(); !reflect.DeepEqual(got, tt.wantKinds) {
				t.Fatalf("Kinds() = %v, want %v", got, tt.wantKinds)
			}
			if got := len(k.Jobs.Periodic()); got != tt.wantPeriod {
				t.Fatalf("periodic jobs = %d, want %d", got, tt.wantPeriod)
			}
			if err := k.Jobs.Validate(); err != nil {
				t.Fatalf("Validate() = %v", err)
			}
		})
	}
}

func TestGateRunDueJob(t *testing.T) {
	start := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		advance    time.Duration
		failing    bool
		wantStatus gate.Status
		wantErr    error
	}{
		{name: "pending gate past its TTL is expired", advance: 73 * time.Hour, wantStatus: gate.StatusExpired},
		{name: "pending gate inside its TTL stays pending", advance: time.Hour, wantStatus: gate.StatusPending},
		{name: "RunDue error is returned wrapped", advance: 73 * time.Hour, failing: true, wantErr: errRunDue},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			clk := clockfake.New(start, time.UTC)
			gates := gatefake.New(clk.Now, gatefake.Caps{})
			if err := gates.Register(testGate("desk.sign")); err != nil {
				t.Fatal(err)
			}
			id, err := gates.Open(ctx, nil, "desk.sign", gate.Subject{Kind: "desk", ID: "d1"},
				json.RawMessage(`{"n":1}`), gate.Principal{Kind: gate.PrincipalUser, UserID: "u1"})
			if err != nil {
				t.Fatal(err)
			}
			pool := dbfake.New()
			k := &Kernel{Jobs: NewJobs(), DB: pool, Gates: gates}
			if tt.failing {
				k.Gates = failingGates{gates}
			}
			if err := addKernelJobs(k); err != nil {
				t.Fatal(err)
			}
			clk.Advance(tt.advance)

			err = gateRunDueWorker{k: k}.Work(ctx, &river.Job[gateRunDueArgs]{})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Work() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if pool.Rollbacks != 1 {
					t.Fatalf("rollbacks = %d, want 1", pool.Rollbacks)
				}
				return
			}
			if pool.Commits != 1 {
				t.Fatalf("commits = %d, want one transaction", pool.Commits)
			}
			g, err := gates.Get(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if g.Status != tt.wantStatus {
				t.Fatalf("status = %s, want %s", g.Status, tt.wantStatus)
			}
		})
	}
}

// TestARC_I_030_OutboxRelayJob: an emitted message reaches a registered consumer once, and
// a second relay run does not redeliver it.
func TestARC_I_030_OutboxRelayJob(t *testing.T) {
	tests := []struct {
		name      string
		runs      int
		republish bool // clear published_at between runs, as a relay that died before MarkPublished
		wantCalls int
	}{
		{name: "one run delivers once", runs: 1, wantCalls: 1},
		{name: "second run does not redeliver", runs: 2, wantCalls: 1},
		// rule: ARC-I-030
		{name: "redelivery after crash is skipped by the inbox claim", runs: 2, republish: true, wantCalls: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			ob := outboxfake.New()
			calls := 0
			k := &Kernel{Jobs: NewJobs(), DB: dbfake.New(), Outbox: &outbox.Deliverer{Relay: ob, Inbox: ob}}
			if err := k.AddConsumer(outbox.Consumer{Name: "count", Handle: func(context.Context, pgx.Tx, outbox.Message) error {
				calls++
				return nil
			}}); err != nil {
				t.Fatal(err)
			}
			if err := addKernelJobs(k); err != nil {
				t.Fatal(err)
			}
			if err := ob.Emit(ctx, nil, outbox.Subject{Kind: "desk", ID: "d1"}, "desk.signed", map[string]int{"n": 1}); err != nil {
				t.Fatal(err)
			}
			relay := outboxRelayWorker{k: k}
			for i := 0; i < tt.runs; i++ {
				if i > 0 && tt.republish {
					k.Outbox.Relay = unpublished{ob}
				}
				if err := relay.Work(ctx, &river.Job[outboxRelayArgs]{}); err != nil {
					t.Fatalf("run %d: %v", i, err)
				}
			}
			if calls != tt.wantCalls {
				t.Fatalf("consumer calls = %d, want %d", calls, tt.wantCalls)
			}
		})
	}
}

// unpublished is a relay that reports every message as still pending, as after a relay
// crash between the consumer's commit and MarkPublished.
type unpublished struct{ *outboxfake.Fake }

func (u unpublished) Pending(context.Context, int) ([]outbox.Message, error) {
	return u.Messages(), nil
}

func TestAddConsumer(t *testing.T) {
	handle := func(context.Context, pgx.Tx, outbox.Message) error { return nil }
	tests := []struct {
		name    string
		outbox  bool
		add     []outbox.Consumer
		wantErr error
	}{
		{name: "no outbox", add: []outbox.Consumer{{Name: "a", Handle: handle}}, wantErr: ErrConfig},
		{name: "empty name", outbox: true, add: []outbox.Consumer{{Handle: handle}}, wantErr: ErrConfig},
		{name: "nil handler", outbox: true, add: []outbox.Consumer{{Name: "a"}}, wantErr: ErrConfig},
		{name: "duplicate name", outbox: true, add: []outbox.Consumer{{Name: "a", Handle: handle}, {Name: "a", Handle: handle}}, wantErr: ErrConfig},
		{name: "two consumers", outbox: true, add: []outbox.Consumer{{Name: "a", Handle: handle}, {Name: "b", Handle: handle}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := &Kernel{}
			if tt.outbox {
				k.Outbox = &outbox.Deliverer{}
			}
			var err error
			for _, c := range tt.add {
				if err = k.AddConsumer(c); err != nil {
					break
				}
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("AddConsumer() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
