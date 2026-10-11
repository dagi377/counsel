package legal

import (
	"os"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	"github.com/dagi377/counsel/kernel/inbound"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// servedMounters is every route mounter that exists, kernel and app. Zero-value receivers
// are enough to list routes; handlers are not called. Add a module's mounter here in the
// change that writes it, and remove its operations from api/unserved.txt.
func servedMounters() []server.Mounter {
	return []server.Mounter{
		inbound.NewPG(nil, nil).Routes,
		(&withdraw.Execution{}).Routes,
	}
}

// TestRoutesConformToOpenAPI is the two-way route check: every mounted route (kernel
// modules, app handlers and routes the app registers through Registry.Routes) is in
// api/openapi.yaml, and every openapi operation is mounted or listed in
// api/unserved.txt. The unserved list only shrinks: a listed operation that is
// now mounted fails this test.
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

	for _, msg := range server.Conformance(m.Routes(), documented, unserved) {
		t.Error(msg)
	}
}
