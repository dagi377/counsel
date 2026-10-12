package boot

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"io/fs"
	"net/http"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal"
	"github.com/dagi377/counsel/app/legal/billing/payment"
	"github.com/dagi377/counsel/app/legal/billing/payment/paymentfake"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/registry"
)

// recorderBooter is a registry.Recorder used as the kernel's Booter; Seal accepts everything.
type recorderBooter struct{ *registry.Recorder }

func (recorderBooter) Seal() error { return nil }

func methods(calls []registry.Call) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.Method)
	}
	return out
}

func TestAppShape(t *testing.T) {
	a := App(Options{})
	tests := []struct {
		name string
		ok   bool
	}{
		{"name is legal", a.Name == "legal"},
		{"define set", a.Define != nil},
		{"register set", a.Register != nil},
		{"start set", a.Start != nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.ok {
				t.Fatalf("App(Options{}) failed: %s", tt.name)
			}
		})
	}
	if err := a.Start(context.Background(), &kboot.Kernel{}); err != nil {
		t.Fatalf("Start = %v, want nil (no start hooks yet)", err)
	}
}

func TestRegisterNoFlagsRecordsOnlyAlwaysSet(t *testing.T) {
	want := &registry.Recorder{}
	if err := legal.Register(want); err != nil {
		t.Fatalf("legal.Register: %v", err)
	}
	tests := []struct {
		name string
		on   []string
	}{
		{name: "no flags on"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &registry.Recorder{}
			k, err := kboot.BuildWith(context.Background(), kboot.Deps{Registry: recorderBooter{rec}}, tt.on, App(Options{}))
			if err != nil {
				t.Fatalf("BuildWith: %v", err)
			}
			if k.Flags.Enabled("trust.bylaw9_verified") {
				t.Errorf("legal flags not defined off by Define")
			}
			got := methods(rec.Calls())
			for _, m := range got {
				switch m {
				// Routes are always mounted so the OpenAPI conformance test sees every route (Q-HTTP-00).
				case registry.MethodChartOfAccounts, registry.MethodGateKind, registry.MethodCapability, registry.MethodRole, registry.MethodRoutes:
				default:
					t.Errorf("feature-gated call %s with no flags on", m)
				}
			}
			// Every ApprovalKinds kind registers with no flags and no ports (GATE-EFFECTS), and
			// so does every money effect that needs no kernel clock (GE2).
			var kinds []string
			for _, m := range got {
				if m != registry.MethodGateKind {
					kinds = append(kinds, m)
				}
			}
			wantKinds := len(ApprovalKinds) + len(moneyEffects(nil, nil)) + appOpsKinds()
			if n := len(got) - len(kinds); n != wantKinds {
				t.Errorf("registered %d gate kinds, want %d", n, wantKinds)
			}
			got = kinds
			if !reflect.DeepEqual(got, methods(want.Calls())) {
				t.Errorf("calls = %v, want exactly legal.Register's always set %v", got, methods(want.Calls()))
			}
		})
	}
}

func TestRegisterErrorsWrapped(t *testing.T) {
	errBoom := errors.New("boom")
	tests := []struct {
		name    string
		app     kboot.App
		k       *kboot.Kernel
		wantErr error
	}{
		{
			name:    "failing registrar",
			app:     newApp([]hook{func(context.Context, *kboot.Kernel) error { return errBoom }}, nil),
			k:       &kboot.Kernel{Registry: recorderBooter{&registry.Recorder{}}},
			wantErr: errBoom,
		},
		{
			name:    "no registry",
			app:     App(Options{}),
			k:       &kboot.Kernel{},
			wantErr: ErrNoRegistry,
		},
		{
			name: "payment hook without decoder",
			app: App(Options{Payment: &PaymentHook{
				Webhook: payment.Webhook{ID: "payments.example", Verify: verifyHeader},
				Service: paymentfake.New(),
			}}),
			k:       &kboot.Kernel{Registry: recorderBooter{&registry.Recorder{}}},
			wantErr: payment.ErrNoDecoder,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.app.Register(context.Background(), tt.k)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Register err = %v, want %v", err, tt.wantErr)
			}
			if !strings.HasPrefix(err.Error(), "legal boot:") {
				t.Fatalf("Register err = %q, want prefix \"legal boot:\"", err)
			}
		})
	}
}

func verifyHeader(r *http.Request, _ []byte) (string, error) { return r.Header.Get("Webhook-Id"), nil }

func TestPaymentHookRegistersInboundProvider(t *testing.T) {
	hook := &PaymentHook{
		Webhook: payment.Webhook{
			ID:     "payments.example",
			Verify: verifyHeader,
			Decode: func([]byte) (payment.Capture, error) { return payment.Capture{}, nil },
		},
		Service: paymentfake.New(),
	}
	tests := []struct {
		name    string
		opts    Options
		wantIDs []string
	}{
		{name: "not configured", opts: Options{}},
		{name: "configured", opts: Options{Payment: hook}, wantIDs: []string{"payments.example"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &registry.Recorder{}
			if err := App(tt.opts).Register(context.Background(), &kboot.Kernel{Registry: recorderBooter{rec}}); err != nil {
				t.Fatalf("Register: %v", err)
			}
			var ids []string
			for _, c := range rec.Method(registry.MethodInboundProvider) {
				p := c.Args[0].(registry.Provider)
				if p.Verify == nil || p.Handle == nil {
					t.Errorf("provider %q has nil Verify or Handle", p.ID)
				}
				ids = append(ids, p.ID)
			}
			if !reflect.DeepEqual(ids, tt.wantIDs) {
				t.Fatalf("inbound providers = %v, want %v", ids, tt.wantIDs)
			}
		})
	}
}

const modulePath = "github.com/dagi377/counsel"

// imports returns, per package dir (relative to the repo root), the import paths of its
// non-test Go files under root.
func imports(t *testing.T, root string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel("../../..", filepath.Dir(p))
		if err != nil {
			return err
		}
		for _, s := range f.Imports {
			v, err := strconv.Unquote(s.Path.Value)
			if err != nil {
				return err
			}
			out[filepath.ToSlash(rel)] = append(out[filepath.ToSlash(rel)], v)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

func TestOnlyBinariesImportLegalBoot(t *testing.T) {
	allowed := map[string]bool{"cmd/counseld": true, "cmd/counsel-worker": true}
	self := modulePath + "/app/legal/boot"
	for _, root := range []string{"../../../cmd", "../../../kernel", "../../../app"} {
		for pkg, imps := range imports(t, root) {
			for _, imp := range imps {
				if imp == self && !allowed[pkg] {
					t.Errorf("%s imports %s; only cmd/counseld and cmd/counsel-worker may", pkg, self)
				}
			}
		}
	}
}

func TestLegalBootImportsNoStore(t *testing.T) {
	for pkg, imps := range imports(t, "../../../app/legal/boot") {
		for _, imp := range imps {
			if strings.HasSuffix(imp, "/store") || strings.Contains(imp, "/store/") {
				t.Errorf("%s imports store package %s", pkg, imp)
			}
		}
	}
}
