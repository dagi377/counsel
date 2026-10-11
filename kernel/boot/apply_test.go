package boot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/report"
	"github.com/dagi377/counsel/kernel/report/reportfake"
	"github.com/dagi377/counsel/kernel/server"
	"github.com/dagi377/counsel/kernel/server/serverfake"
	"github.com/dagi377/counsel/kernel/webhook"
)

// sealRecorder is a Booter over registry.Recorder whose Seal returns err.
type sealRecorder struct {
	registry.Recorder
	err error
}

func (s *sealRecorder) Seal() error { return s.err }

func testGate(slug string) registry.GateKind {
	return registry.GateKind{
		Slug: slug, Approver: "desk.approve", Risk: registry.RiskHigh, MinApprovers: 1,
		TTL: 72 * time.Hour, PayloadSchema: []byte(`{"type":"object"}`),
		Effect: func(context.Context, pgx.Tx, registry.Approved) error { return nil },
	}
}

func testReport(id string) registry.ReportDef {
	return registry.ReportDef{
		ID: id, Capability: "desk.approve", Params: []byte(`{"type":"object"}`),
		Run: func(context.Context, pgx.Tx, json.RawMessage) (registry.Rows, error) { return registry.Rows{}, nil },
	}
}

func mountGet(pattern string) func(chi.Router) {
	return func(r chi.Router) { r.Get(pattern, func(http.ResponseWriter, *http.Request) {}) }
}

// engines is one set of fakes for a test case.
type engines struct {
	gates   *gatefake.Fake
	reports *reportfake.Fake
	server  *serverfake.Fake
	events  *webhook.Types
}

func newEngines() engines {
	return engines{
		gates:   gatefake.New(time.Now, gatefake.Caps{}),
		reports: reportfake.New(time.Now, reportfake.Caps{}),
		server:  serverfake.New(),
		events:  webhook.NewTypes(),
	}
}

func (e engines) deps(r registry.Booter) Deps {
	return Deps{Registry: r, Gates: e.gates, Reports: e.reports, Server: e.server, Events: e.events}
}

// hasGate reports whether slug is registered in the gate fake: a second Register of a known
// kind fails with ErrDuplicateKind.
func hasGate(t *testing.T, g *gatefake.Fake, slug string) bool {
	t.Helper()
	err := g.Register(testGate(slug))
	return errors.Is(err, gate.ErrDuplicateKind)
}

func TestApply(t *testing.T) {
	tests := []struct {
		name       string
		register   func(r registry.Registry)
		gates      []string
		reports    []string
		events     []string
		wantRoutes []server.Route
		wantErr    error
	}{
		{name: "nothing registered"},
		{name: "gate kinds", register: func(r registry.Registry) {
			for _, s := range []string{"a", "b", "c"} {
				r.GateKind(testGate(fmt.Sprintf("desk.close_%s", s)))
			}
		}, gates: []string{"desk.close_a", "desk.close_b", "desk.close_c"}},
		{name: "report", register: func(r registry.Registry) {
			r.Report(testReport("desk.open"))
		}, reports: []string{"desk.open"}},
		{name: "routes", register: func(r registry.Registry) {
			r.Routes(mountGet("/v1/x"))
		}, wantRoutes: []server.Route{{Method: http.MethodGet, Pattern: "/v1/x"}}},
		{name: "event type", register: func(r registry.Registry) {
			r.EventType("desk.opened", []byte(`{"type":"object"}`))
		}, events: []string{"desk.opened"}},
		{name: "duplicate route", register: func(r registry.Registry) {
			r.Routes(mountGet("/v1/x"))
			r.Routes(mountGet("/v1/x"))
		}, wantRoutes: []server.Route{{Method: http.MethodGet, Pattern: "/v1/x"}}, wantErr: server.ErrDuplicateRoute},
		{name: "duplicate gate kind", register: func(r registry.Registry) {
			r.GateKind(testGate("desk.close"))
			r.GateKind(testGate("desk.close"))
		}, gates: []string{"desk.close"}, wantErr: gate.ErrDuplicateKind},
		{name: "methods without an engine are skipped", register: func(r registry.Registry) {
			r.Capability(registry.Capability{ID: "desk.approve", Description: "approve"})
			r.NumberSeries("desk.ticket", "T-{NNNN}")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEngines()
			app := App{Name: "desk", Register: func(_ context.Context, k *Kernel) error {
				if tt.register != nil {
					tt.register(k.Registry)
				}
				return nil
			}}
			k, err := BuildWith(context.Background(), e.deps(&sealRecorder{}), nil, app)
			if err != nil {
				t.Fatalf("BuildWith() = %v", err)
			}
			if err := apply(k); !errors.Is(err, tt.wantErr) {
				t.Fatalf("apply() = %v, want %v", err, tt.wantErr)
			}
			for _, slug := range tt.gates {
				if !hasGate(t, e.gates, slug) {
					t.Errorf("gate kind %q not registered", slug)
				}
			}
			for _, id := range tt.reports {
				if err := e.reports.Register(testReport(id)); !errors.Is(err, report.ErrDuplicate) {
					t.Errorf("report %q not registered (re-register = %v)", id, err)
				}
			}
			for _, name := range tt.events {
				err := e.events.Register(webhook.EventType{Name: name, Schema: []byte(`{"type":"object"}`)})
				if !errors.Is(err, webhook.ErrDuplicateType) {
					t.Errorf("event type %q not registered (re-register = %v)", name, err)
				}
			}
			got := e.server.Routes()
			if len(got) == 0 && len(tt.wantRoutes) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.wantRoutes) {
				t.Errorf("Routes() = %v, want %v", got, tt.wantRoutes)
			}
		})
	}
}

func TestApplyNeedsSeal(t *testing.T) {
	errSeal := errors.New("seal failed")
	register := func(_ context.Context, k *Kernel) error {
		k.Registry.GateKind(testGate("desk.close"))
		return nil
	}
	tests := []struct {
		name string
		run  func(e engines) error
		want error
	}{
		{name: "failing seal stops boot before apply", run: func(e engines) error {
			k, err := BuildWith(context.Background(), e.deps(&sealRecorder{err: errSeal}), nil,
				App{Name: "desk", Register: register})
			if err != nil {
				return err
			}
			return apply(k)
		}, want: errSeal},
		{name: "unsealed kernel", run: func(e engines) error {
			r := &sealRecorder{}
			r.GateKind(testGate("desk.close"))
			return apply(&Kernel{Registry: r, Gates: e.gates})
		}, want: ErrNotSealed},
		{name: "no registry", run: func(e engines) error {
			return apply(&Kernel{Gates: e.gates})
		}, want: ErrNotSealed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEngines()
			if err := tt.run(e); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if hasGate(t, e.gates, "desk.close") {
				t.Fatal("gate kind registered although Seal did not pass")
			}
		})
	}
}

func TestApplyMissingEngine(t *testing.T) {
	tests := []struct {
		name     string
		register func(r registry.Registry)
	}{
		{name: "gate kind", register: func(r registry.Registry) { r.GateKind(testGate("desk.close")) }},
		{name: "report", register: func(r registry.Registry) { r.Report(testReport("desk.open")) }},
		{name: "routes", register: func(r registry.Registry) { r.Routes(mountGet("/v1/x")) }},
		{name: "event type", register: func(r registry.Registry) { r.EventType("desk.opened", []byte(`{}`)) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := App{Name: "desk", Register: func(_ context.Context, k *Kernel) error {
				tt.register(k.Registry)
				return nil
			}}
			k, err := BuildWith(context.Background(), Deps{Registry: &sealRecorder{}}, nil, app)
			if err != nil {
				t.Fatalf("BuildWith() = %v", err)
			}
			if err := apply(k); !errors.Is(err, ErrConfig) {
				t.Fatalf("apply() = %v, want %v", err, ErrConfig)
			}
		})
	}
}
