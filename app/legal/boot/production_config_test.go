package boot

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/trust"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/db/dbfake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/report/reportfake"
	"github.com/dagi377/counsel/kernel/server/serverfake"
)

const (
	cfgGateID   = "0190e000-0000-7000-8000-000000000001"
	cfgMember   = "0190e000-0000-7000-8000-0000000000a1"
	cfgNoRole   = "0190e000-0000-7000-8000-0000000000a2"
	cfgPreparer = "0190e000-0000-7000-8000-0000000000a3"
	userHeader  = "X-Test-User"
)

// headerPrincipal authenticates the user the test header names.
func headerPrincipal(r *http.Request) (registry.Principal, bool) {
	u := r.Header.Get(userHeader)
	return registry.Principal{Kind: registry.PrincipalUser, UserID: u}, u != ""
}

// TestARC_I_018_ProductionRoles: the production checker sees the roles the app registered,
// so a member whose role holds a gate approver capability reaches the decision route and a
// member with no role is refused with 403.
func TestARC_I_018_ProductionRoles(t *testing.T) {
	key := trust.SealKey{ID: 1, Key: make([]byte, 32)}
	c := compose(t, legalFlagNames(t), Options{Production: &Production{SealKey: &key, Principal: headerPrincipal,
		Assignments: identity.StaticAssignments{cfgMember: {"lawyer"}}}})
	tests := []struct {
		name, user string
		want       int
	}{
		// The capability check passes; the next check, a missing If-Match, answers 428.
		{"member with a role", cfgMember, http.StatusPreconditionRequired},
		{"member without a role", cfgNoRole, http.StatusForbidden},
		{"no session", "", http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/gates/"+cfgGateID+"/decisions", strings.NewReader("{}"))
			req.Header.Set(userHeader, tt.user)
			req.Header.Set("Idempotency-Key", "k")
			rec := httptest.NewRecorder()
			c.server.Handler().ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.want, rec.Body)
			}
		})
	}
}

// stubDecisions is a gates.Engine holding one pending gate.
type stubDecisions struct {
	g       gate.Gate
	decided int
}

func (s *stubDecisions) Decide(context.Context, pgx.Tx, gate.GateID, int, gate.Decision, gate.Principal) error {
	s.decided++
	return nil
}
func (s *stubDecisions) Withdraw(context.Context, pgx.Tx, gate.GateID, gate.Principal) error {
	return nil
}
func (s *stubDecisions) Get(_ context.Context, id gate.GateID) (gate.Gate, error) {
	if id != s.g.ID {
		return gate.Gate{}, gate.ErrNotFound
	}
	return s.g, nil
}

// TestARC_I_017_DecisionNeedsStepUp: the mounted decision route answers 401
// step_up_required without fresh step-up auth and decides with it.
func TestARC_I_017_DecisionNeedsStepUp(t *testing.T) {
	specs, err := gates.ParseKinds(gates.SpecData())
	if err != nil || len(specs) == 0 {
		t.Fatalf("spec kinds: %v", err)
	}
	now := testKernel().Clock.Now()
	tests := []struct {
		name   string
		stepUp time.Time
		want   int
		decide int
	}{
		{"no step-up", time.Time{}, http.StatusUnauthorized, 0},
		{"stale step-up", now.Add(-time.Hour), http.StatusUnauthorized, 0},
		{"fresh step-up", now.Add(-time.Minute), http.StatusOK, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eng := &stubDecisions{g: gate.Gate{ID: cfgGateID, Kind: specs[0].Code, Version: 1, Status: gate.StatusPending,
				Preparer: registry.Principal{Kind: registry.PrincipalUser, UserID: cfgPreparer}}}
			ports := testHTTPPorts()
			ports.Decisions = eng
			ports.StepUp = func(*http.Request) time.Time { return tt.stepUp }
			c := compose(t, nil, Options{Gates: testGatePorts(), Rest: &RestPorts{}, HTTP: ports})
			body := fmt.Sprintf(`{"decision":"approve","version":1,"payload_hash":"sha256:%s"}`, strings.Repeat("0", 64))
			req := httptest.NewRequest(http.MethodPost, "/gates/"+cfgGateID+"/decisions", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", "k-"+tt.name)
			req.Header.Set("If-Match", `"1"`)
			rec := httptest.NewRecorder()
			c.server.Handler().ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.want, rec.Body)
			}
			if tt.want == http.StatusUnauthorized && !strings.Contains(rec.Body.String(), gates.CodeStepUpRequired) {
				t.Errorf("body = %s, want %s", rec.Body, gates.CodeStepUpRequired)
			}
			if eng.decided != tt.decide {
				t.Errorf("Decide calls = %d, want %d", eng.decided, tt.decide)
			}
		})
	}
}

// TestARC_U_040_SealKeyNeverLogged: no seal key source, valid or not, puts the key into a
// log line, an error or a formatted config; the generated dev key is never shown either.
func TestARC_U_040_SealKeyNeverLogged(t *testing.T) {
	raw := bytes.Repeat([]byte{0xA7}, 32)
	good := base64.StdEncoding.EncodeToString(raw)
	short := base64.StdEncoding.EncodeToString(raw[:16])
	file := filepath.Join(t.TempDir(), "seal.key")
	if err := os.WriteFile(file, []byte(good+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		p       Production
		wantErr bool
		wantKey []byte // nil: any key (generated)
	}{
		{"env key", Production{SealKeyBase64: Secret(good)}, false, raw},
		{"file key", Production{SealKeyFile: file}, false, raw},
		{"short key refused", Production{SealKeyBase64: Secret(short)}, true, nil},
		{"not base64 refused", Production{SealKeyBase64: Secret("!!" + good)}, true, nil},
		{"dev generates a key", Production{Dev: true}, false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := captureLogs(t)
			k, err := tt.p.resolveSealKey()
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			secrets := []string{good, short, string(raw)}
			if k != nil {
				if tt.wantKey != nil && !bytes.Equal(k.Key, tt.wantKey) {
					t.Fatal("resolved key differs from the configured key")
				}
				secrets = append(secrets, base64.StdEncoding.EncodeToString(k.Key), fmt.Sprintf("%x", k.Key))
			}
			// The config itself, formatted or logged, shows only the placeholder.
			slog.Info("config", "production", tt.p, "seal", tt.p.SealKeyBase64)
			out := logs.String() + fmt.Sprintf("%v %+v %#v %s", tt.p, tt.p, tt.p, tt.p.SealKeyBase64)
			if err != nil {
				out += err.Error()
			}
			for _, s := range secrets {
				if strings.Contains(out, s) {
					t.Fatalf("seal key material leaked: %q", out)
				}
			}
			if tt.p.Dev && !strings.Contains(logs.String(), "random per-process seal key") {
				t.Errorf("dev boot did not warn: %q", logs)
			}
		})
	}
}

// TestProductionBootRefusesMissingSealKey: outside dev, boot refuses to start without a
// seal key; a dev boot starts on a generated one.
func TestProductionBootRefusesMissingSealKey(t *testing.T) {
	tests := []struct {
		name    string
		p       Production
		wantErr error
	}{
		{"production without a key", Production{}, ErrSealKeyMissing},
		{"dev without a key", Production{Dev: true}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			captureLogs(t)
			schemas, err := registry.LoadSchemas(os.DirFS("../../../spec/kernel"))
			if err != nil {
				t.Fatal(err)
			}
			k0 := testKernel()
			now := k0.Clock.Now
			deps := kboot.Deps{Registry: registry.NewBoot(schemas), DB: noConnPool{dbfake.New()}, Clock: k0.Clock,
				Gates: gatefake.New(now, gatefake.Caps{}), Reports: reportfake.New(now, reportfake.Caps{}),
				Server: serverfake.New(), Emitter: outboxfake.New()}
			p := tt.p
			_, err = kboot.BuildWith(context.Background(), deps, nil, App(Options{Production: &p}))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("BuildWith err = %v, want %v", err, tt.wantErr)
			}
			if p.SealKey != nil {
				t.Error("boot wrote the generated key into the caller's config")
			}
		})
	}
}
