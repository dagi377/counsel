package boot

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/db/dbfake"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/report/reportfake"
	"github.com/dagi377/counsel/kernel/server"
	"github.com/dagi377/counsel/kernel/server/serverfake"
)

var update = flag.Bool("update", false, "rewrite the boot golden files")

// errNoConn is what the never-connected pool answers: the composition must build without
// running a statement.
var errNoConn = errors.New("compose test: no connection")

// noConnPool is a pool with plain statements that never connects.
type noConnPool struct{ *dbfake.Fake }

func (noConnPool) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errNoConn
}
func (noConnPool) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, errNoConn }
func (noConnPool) QueryRow(context.Context, string, ...any) pgx.Row        { return nil }

// composed is a kernel built by BuildWith over the fakes, with its registrations applied.
type composed struct {
	k      *kboot.Kernel
	boot   *registry.Boot
	gates  *gatefake.Fake
	server *serverfake.Fake
}

// compose builds the legal app over the fakes and applies the sealed registrations the way
// kernel boot does: gate kinds to the gate engine, reports to the report engine and routes
// to the server.
func compose(t *testing.T, on []string, opts Options) composed {
	t.Helper()
	schemas, err := registry.LoadSchemas(os.DirFS("../../../spec/kernel"))
	if err != nil {
		t.Fatal(err)
	}
	k0 := testKernel()
	now := func() time.Time { return k0.Clock.Now() }
	c := composed{
		boot:   registry.NewBoot(schemas),
		gates:  gatefake.New(now, gatefake.Caps{}),
		server: serverfake.New(),
	}
	deps := kboot.Deps{
		Registry: c.boot, DB: noConnPool{dbfake.New()}, Clock: k0.Clock, Gates: c.gates,
		Reports: reportfake.New(now, reportfake.Caps{}), Server: c.server, Emitter: outboxfake.New(),
	}
	// BuildWith seals the registry; a Seal failure is its error.
	k, err := kboot.BuildWith(context.Background(), deps, on, App(opts))
	if err != nil {
		t.Fatalf("BuildWith: %v", err)
	}
	c.k = k
	for i, call := range c.boot.Calls() {
		if err := applyCall(k, call); err != nil {
			t.Fatalf("apply registration %d (%s): %v", i, call.Method, err)
		}
	}
	return c
}

// applyCall hands one sealed call to its engine, as kernel boot's apply does.
func applyCall(k *kboot.Kernel, c registry.Call) error {
	switch c.Method {
	case registry.MethodGateKind:
		return k.Gates.Register(c.Args[0].(registry.GateKind))
	case registry.MethodReport:
		return k.Reports.Register(c.Args[0].(registry.ReportDef))
	case registry.MethodRoutes:
		return k.Server.Mount(server.Mounter(c.Args[0].(func(chi.Router))))
	}
	return nil
}

// wantWiredGates is the gate kinds with an effect over testGatePorts.
const wantWiredGates = 10

func TestComposeOverFakes(t *testing.T) {
	all := legalFlagNames(t)
	tests := []struct {
		name string
		on   []string
	}{
		{name: "no flags"},
		{name: "every legal flag", on: all},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := compose(t, tt.on, Options{Gates: testGatePorts(), Rest: &RestPorts{}, HTTP: testHTTPPorts()})
			// Every wired gate kind reaches the gate engine with its effect.
			var wired int
			for _, call := range c.boot.Method(registry.MethodGateKind) {
				g := call.Args[0].(registry.GateKind)
				if g.Effect == nil {
					t.Errorf("gate kind %s has a nil Effect", g.Slug)
				}
				wired++
			}
			// The ten effects testGatePorts and the rest step build (BOOT-16): signatory,
			// withdrawal, trust transfer, bill approval and void, the write-off pair,
			// matter.opening (EVENTS-WIRE) and matter.stage_move (STAGE-EFFECT).
			if wired != wantWiredGates {
				t.Fatalf("wired %d gate kinds, want %d", wired, wantWiredGates)
			}
			if err := c.k.Jobs.Validate(); err != nil {
				t.Fatalf("Jobs.Validate: %v", err)
			}
			golden(t, filepath.Join("testdata", "job_kinds.golden"), strings.Join(c.k.Jobs.Kinds(), "\n")+"\n")
			// Start seeds the notification templates in one transaction.
			c.k.DB = newTmplDB()
			if err := App(Options{}).Start(context.Background(), c.k); err != nil {
				t.Fatalf("App.Start: %v", err)
			}
		})
	}
}

// TestProductionRoutesHaveTheirPorts builds the real Options graph the binaries run, with
// Postgres ports over a pool that never connects, and calls every mounted route. A route that
// answers 503 is a route whose ports boot did not supply; testdata/unwired_routes.golden
// lists the groups no Postgres implementation exists for yet, and no other may answer 503.
func TestProductionRoutesHaveTheirPorts(t *testing.T) {
	// The gate effects run on the Postgres GatePorts (gatePortsPG), not on fakes.
	// The agent runs on the mock model (AGENT-PROD).
	c := compose(t, legalFlagNames(t), Options{Production: mockProduction(nil), Agent: noLawyerWiring()})
	var wired int
	for _, call := range c.boot.Method(registry.MethodGateKind) {
		if call.Args[0].(registry.GateKind).Effect == nil {
			t.Errorf("gate kind %s has a nil Effect", call.Args[0].(registry.GateKind).Slug)
		}
		wired++
	}
	if wired != wantWiredGates {
		t.Fatalf("production wired %d gate kinds, want %d", wired, wantWiredGates)
	}
	h := c.server.Handler()
	param := regexp.MustCompile(`\{[^}]+\}`)
	var unserved []string
	for _, r := range c.server.Routes() {
		path := param.ReplaceAllString(r.Pattern, "00000000-0000-0000-0000-000000000001")
		req := httptest.NewRequest(r.Method, path, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "k")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusServiceUnavailable {
			unserved = append(unserved, r.Method+" "+r.Pattern)
		}
	}
	slices.Sort(unserved)
	golden(t, filepath.Join("testdata", "unwired_routes.golden"), strings.Join(unserved, "\n")+"\n")
	// rule: ARC-I-040
	// The routes this task wires must be served.
	for _, must := range []string{"GET /matters", "GET /contacts", "GET /firm/tax-registrations", "GET /bills",
		"POST /matters", "PATCH /matters/{matter_id}", "POST /agent/runs", "GET /agent/runs/{run_id}", "POST /agent/actions/{feature_id}"} {
		if slices.Contains(unserved, must) {
			t.Errorf("%s answers 503", must)
		}
	}
}

// golden compares got with the file at path, or rewrites it under -update.
func golden(t *testing.T, path, got string) {
	t.Helper()
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update)", err)
	}
	if string(want) != got {
		t.Fatalf("%s differs (run with -update)\ngot:\n%s\nwant:\n%s", path, got, want)
	}
}
