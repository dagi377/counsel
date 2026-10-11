package legal

import (
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/kernel/inbound"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// servedMounters is every kernel route mounter. Zero-value receivers are enough to list
// routes; handlers are not called. App modules never appear here: they mount only through
// Registry.Routes in their register.go, which the conformance test collects from Register.
func servedMounters() []server.Mounter {
	return []server.Mounter{
		inbound.NewPG(nil, nil).Routes,
	}
}

// kernelWrites declares the writes of kernel mounters, which do not use server.Declare.
var kernelWrites = []server.RouteWrite{
	{Route: server.Route{Method: http.MethodPost, Pattern: server.BasePath + "/hooks/{provider}"}, Sets: []string{"inbound_event.received"}},
}

const appPkg = "github.com/dagi377/counsel/app/"

func TestServedMountersAreKernelOnly(t *testing.T) {
	for _, mt := range servedMounters() {
		name := runtime.FuncForPC(reflect.ValueOf(mt).Pointer()).Name()
		if strings.HasPrefix(name, appPkg) {
			t.Errorf("app mounter %s is mounted outside Registry.Routes", name)
		}
	}
}

// mountAll mounts the kernel mounters and every route Register adds through Registry.Routes.
func mountAll(t *testing.T) *server.Mux {
	t.Helper()
	m := server.New(nil)
	mounters := servedMounters()
	rec := &registry.Recorder{}
	if err := Register(rec); err != nil {
		t.Fatalf("Register: %v", err)
	}
	for _, c := range rec.Calls() {
		if c.Method != registry.MethodRoutes {
			continue
		}
		fn, ok := c.Args[0].(func(chi.Router))
		if !ok {
			t.Fatalf("Routes call carries %T", c.Args[0])
		}
		mounters = append(mounters, fn)
	}
	for _, mt := range mounters {
		if err := m.Mount(mt); err != nil {
			t.Fatalf("Mount: %v", err)
		}
	}
	return m
}

// TestRoutesConformToOpenAPI is the two-way route check: every mounted route (kernel
// modules and routes the app registers through Registry.Routes) is in api/openapi.yaml, and
// every openapi operation is mounted or listed in api/unserved.txt. The unserved list only
// shrinks: a listed operation that is now mounted fails this test. It also runs the
// gate-owned-state guard: no mounted route may set gate-owned state except the decision route.
func TestRoutesConformToOpenAPI(t *testing.T) {
	spec, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	documented, err := server.OpenAPIRoutes(spec)
	if err != nil {
		t.Fatal(err)
	}
	list, err := os.ReadFile("../../api/unserved.txt")
	if err != nil {
		t.Fatal(err)
	}
	unserved, err := server.ParseRouteList(list)
	if err != nil {
		t.Fatal(err)
	}
	m := mountAll(t)

	for _, msg := range server.Conformance(m.Routes(), documented, unserved) {
		t.Error(msg)
	}

	// rule: ARC-I-012
	writes, err := server.DeclaredWrites(m.Handler())
	if err != nil {
		t.Fatal(err)
	}
	writes = append(writes, kernelWrites...)
	for _, msg := range server.CheckWrites(m.Routes(), writes, GateOwned, server.DecisionRoute) {
		t.Error(msg)
	}
}

func TestARC_I_012_GateOwnedFixtureRoutes(t *testing.T) {
	approve := server.Route{Method: http.MethodPost, Pattern: server.BasePath + "/bills/{bill_id}/approve"}
	tests := []struct {
		name  string
		route server.Route
		sets  []string
		fails bool
	}{
		{"plain route sets bill approval", approve, []string{"bill.status=approved"}, true},
		{"decision route sets bill approval", server.DecisionRoute, []string{"bill.status=approved"}, false},
		{"withdrawal execution sets executed", server.Route{Method: http.MethodPost,
			Pattern: server.BasePath + "/trust/withdrawals/{withdrawal_id}/execution"}, []string{"withdrawal.status=executed"}, false},
		{"withdrawal execution sets authorized", server.Route{Method: http.MethodPost,
			Pattern: server.BasePath + "/trust/withdrawals/{withdrawal_id}/execution"}, []string{"withdrawal.status=authorized"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := server.CheckWrites([]server.Route{tc.route},
				[]server.RouteWrite{{Route: tc.route, Sets: tc.sets}}, GateOwned, server.DecisionRoute)
			if (len(got) > 0) != tc.fails {
				t.Errorf("CheckWrites = %q, want fails=%v", got, tc.fails)
			}
		})
	}
}

func TestARC_I_012_WithdrawalExecutionDeclaresExecuted(t *testing.T) {
	writes, err := server.DeclaredWrites(mountAll(t).Handler())
	if err != nil {
		t.Fatal(err)
	}
	exec := server.Route{Method: http.MethodPost, Pattern: server.BasePath + "/trust/withdrawals/{withdrawal_id}/execution"}
	for _, w := range writes {
		if w.Route == exec {
			if len(w.Sets) != 1 || w.Sets[0] != "withdrawal.status=executed" {
				t.Errorf("execution declares %q, want [withdrawal.status=executed]", w.Sets)
			}
			return
		}
	}
	t.Error("withdrawal execution route has no write declaration")
}

// TestGateOwnedKindsRegisteredOrPending: every gate-owned key names a kind that a module
// registers through Registry.GateKind or that is listed pending; the pending set only shrinks.
func TestARC_I_012_GateOwnedKindsRegisteredOrPending(t *testing.T) {
	rec := &registry.Recorder{}
	if err := Register(rec); err != nil {
		t.Fatalf("Register: %v", err)
	}
	registered := map[string]bool{}
	for _, c := range rec.Method(registry.MethodGateKind) {
		registered[c.Args[0].(registry.GateKind).Slug] = true
	}
	data, err := os.ReadFile("../../spec/gates.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec []struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	inSpec := map[string]bool{}
	for _, k := range spec {
		inSpec[k.Slug] = true
	}
	used := map[string]bool{}
	for key, kind := range GateOwned {
		used[kind] = true
		if !registered[kind] && !pendingGateKinds[kind] {
			t.Errorf("gate-owned %s: kind %s is neither registered nor pending", key, kind)
		}
	}
	for kind := range pendingGateKinds {
		if registered[kind] {
			t.Errorf("kind %s is registered: remove it from pendingGateKinds", kind)
		}
		if !used[kind] {
			t.Errorf("pending kind %s owns no state key: remove it", kind)
		}
		if !inSpec[kind] && !unspecifiedKinds[kind] {
			t.Errorf("pending kind %s is not in spec/gates.json", kind)
		}
	}
}

// unspecifiedKinds are gate-owned kinds missing from spec/gates.json, each with a
// docs/QUESTIONS.md entry.
var unspecifiedKinds = map[string]bool{}
