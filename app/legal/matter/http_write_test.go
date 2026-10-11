package matter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/idem/idemfake"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

type txStub struct{}

func (txStub) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return fn(ctx, nil)
}

// stubGates is a gate engine that records Open calls and never runs an effect.
type stubGates struct {
	mu    sync.Mutex
	gates map[gate.GateID]gate.Gate
	opens []string
}

func (g *stubGates) Open(_ context.Context, _ pgx.Tx, kind string, s gate.Subject, payload json.RawMessage, p gate.Principal) (gate.GateID, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.opens = append(g.opens, kind)
	id := gate.GateID(fmt.Sprintf("0190b000-0000-7000-8000-%012d", len(g.opens)))
	sum := sha256.Sum256(payload)
	at := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	g.gates[id] = gate.Gate{ID: id, Kind: kind, Version: 1, Subject: s, Payload: payload,
		PayloadHash: "sha256:" + hex.EncodeToString(sum[:]), Preparer: p, Status: gate.StatusPending,
		CreatedAt: at, ExpiresAt: at.Add(72 * time.Hour)}
	return id, nil
}

func (g *stubGates) Get(_ context.Context, id gate.GateID) (gate.Gate, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	out, ok := g.gates[id]
	if !ok {
		return gate.Gate{}, gate.ErrNotFound
	}
	return out, nil
}

func (g *stubGates) Opens() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.opens...)
}

// stubUpdates records plain-field updates against the stub query's matters.
type stubUpdates struct {
	q     *stubQuery
	calls int
}

func (u *stubUpdates) Update(_ context.Context, id ID, version int, c PlainChange, _ registry.Principal) (Matter, error) {
	u.calls++
	u.q.mu.Lock()
	defer u.q.mu.Unlock()
	for i, m := range u.q.matters {
		if m.ID != id {
			continue
		}
		if m.RowVersion != version {
			return Matter{}, server.ErrPreconditionFailed
		}
		if c.ShortName != nil {
			m.ShortName = *c.ShortName
		}
		if c.PracticeAreaID != nil {
			m.PracticeAreaID = *c.PracticeAreaID
		}
		m.RowVersion++
		u.q.matters[i] = m
		return m, nil
	}
	return Matter{}, ErrNotFound
}

// stubLifecycle checks transitions; matters in cleared have a cleared conflict run.
type stubLifecycle struct{ cleared map[ID]bool }

func (l stubLifecycle) Transition(_ context.Context, m Matter, _ string, to Status) (Matter, error) {
	if !CanTransition(m.Status, to) {
		return Matter{}, ErrBadTransition
	}
	if to == Open && !l.cleared[m.ID] {
		return Matter{}, ErrNotCleared
	}
	m.Status = to
	return m, nil
}

// effectOnlyMatters stands for the effect-owned matter writes (Create: matter.opening;
// AddMember: matter.team). A call outside a gate effect fails the test.
type effectOnlyMatters struct {
	t     *testing.T
	calls int
}

func (e *effectOnlyMatters) guard(ctx context.Context, method string) error {
	e.calls++
	// rule: ARC-I-012
	if err := gate.RequireEffect(ctx); err != nil {
		e.t.Errorf("effect-owned %s called outside a gate effect", method)
		return err
	}
	return nil
}

func (e *effectOnlyMatters) Create(ctx context.Context, _ NewMatter) (Matter, error) {
	return Matter{}, e.guard(ctx, "Create")
}

func (e *effectOnlyMatters) AddMember(ctx context.Context, _ NewMember) (Member, error) {
	return Member{}, e.guard(ctx, "AddMember")
}

type writeRig struct {
	h       http.Handler
	q       *stubQuery
	gates   *stubGates
	updates *stubUpdates
	effects *effectOnlyMatters
}

func newWriteRig(t *testing.T) writeRig {
	t.Helper()
	base, q, _ := newRig(t)
	rig := writeRig{q: q, gates: &stubGates{gates: map[gate.GateID]gate.Gate{}}, updates: &stubUpdates{q: q},
		effects: &effectOnlyMatters{t: t}}
	wr := &Writes{HTTP: *base, DB: txStub{}, Gates: rig.gates, Updates: rig.updates,
		Lifecycle: stubLifecycle{cleared: map[ID]bool{idB: true}}, Idem: idemfake.New()}
	m := server.New(nil)
	if err := m.Mount(wr.WriteRoutes); err != nil {
		t.Fatal(err)
	}
	rig.h = m.Handler()
	t.Cleanup(func() {
		if rig.effects.calls != 0 {
			t.Errorf("effect-owned matter writes = %d, want 0", rig.effects.calls)
		}
	})
	return rig
}

const openingJSON = `{"number":"2026-0042","short_name":"Delta","client_id":"0190c000-0000-7000-8000-000000000001","practice_area_id":"pa","responsible_lawyer_id":"u9"}`

func (r writeRig) serviceCalls() int { return r.q.Calls() + len(r.gates.Opens()) + r.updates.calls }

func TestARC_I_051_MatterWritesAuthzAndValidation(t *testing.T) {
	key := map[string]string{idem.HeaderName: "key-00000001"}
	ifm := func(v string) map[string]string { return map[string]string{"If-Match": v} }
	tests := []struct {
		name   string
		method string
		path   string
		user   string
		body   string
		hdr    map[string]string
		status int
		code   string
	}{
		{"post without principal", "POST", "/v1/matters", "", openingJSON, key, 401, CodeUnauthenticated},
		{"patch without principal", "PATCH", "/v1/matters/" + idA, "", `{"short_name":"x"}`, ifm(`"3"`), 401, CodeUnauthenticated},
		{"post without matters.open", "POST", "/v1/matters", "viewer", openingJSON, key, 403, CodeMissingCapability},
		{"patch without matters.edit", "PATCH", "/v1/matters/" + idA, "viewer", `{"short_name":"x"}`, ifm(`"3"`), 403, CodeMissingCapability},
		{"patch walled matter is 404", "PATCH", "/v1/matters/" + idHidden, "editor", `{"short_name":"x"}`, ifm(`"1"`), 404, CodeNotFound},
		{"patch walled matter without capability is 404", "PATCH", "/v1/matters/" + idHidden, "viewer", `{"short_name":"x"}`, ifm(`"1"`), 404, CodeNotFound},
		{"post missing idempotency key", "POST", "/v1/matters", "editor", openingJSON, nil, 400, idem.CodeKeyRequired},
		{"post unknown body field", "POST", "/v1/matters", "editor", `{"short_name":"x","colour":"red"}`, key, 400, server.CodeValidationFailed},
		{"patch malformed id", "PATCH", "/v1/matters/nope", "editor", `{"short_name":"x"}`, ifm(`"3"`), 400, server.CodeValidationFailed},
		{"patch unknown body field", "PATCH", "/v1/matters/" + idA, "editor", `{"colour":"red"}`, ifm(`"3"`), 400, server.CodeValidationFailed},
		{"patch missing If-Match", "PATCH", "/v1/matters/" + idA, "editor", `{"short_name":"x"}`, nil, 428, server.CodePreconditionRequired},
		{"patch stale version", "PATCH", "/v1/matters/" + idA, "editor", `{"short_name":"x"}`, ifm(`"2"`), 412, server.CodeVersionConflict},
		{"patch open an open matter", "PATCH", "/v1/matters/" + idC, "editor", `{"status":"open"}`, ifm(`"1"`), 409, CodeInvalidTransition},
		{"patch close a pending matter", "PATCH", "/v1/matters/" + idB, "editor", `{"status":"closed"}`, ifm(`"1"`), 409, CodeInvalidTransition},
		{"patch mixes plain and gated fields", "PATCH", "/v1/matters/" + idA, "editor", `{"short_name":"x","restricted":true}`, ifm(`"3"`), 400, server.CodeValidationFailed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rig := newWriteRig(t)
			rec := do(rig.h, tc.method, tc.path, tc.user, []byte(tc.body), tc.hdr)
			checkProblem(t, rec, tc.status, tc.code)
			if tc.status == 401 || tc.status == 403 || tc.code == idem.CodeKeyRequired {
				if n := rig.serviceCalls(); n != 0 {
					t.Errorf("service and gate calls = %d, want 0", n)
				}
			}
			if len(rig.gates.Opens()) != 0 {
				t.Errorf("gates opened = %v, want none", rig.gates.Opens())
			}
		})
	}
}

func TestMatterOpeningNotClearedIs409(t *testing.T) {
	rig := newWriteRig(t)
	rig.q.mu.Lock()
	rig.q.matters = append(rig.q.matters, Matter{ID: "0190a000-0000-7000-8000-00000000000d", Number: "2026-005",
		ShortName: "Pending", Status: Pending, RowVersion: 1})
	rig.q.mu.Unlock()
	rec := do(rig.h, "PATCH", "/v1/matters/0190a000-0000-7000-8000-00000000000d", "editor", []byte(`{"status":"open"}`),
		map[string]string{"If-Match": `"1"`})
	checkProblem(t, rec, 409, CodeConflictNotCleared)
}

func TestARC_I_008_MatterOpeningIdempotency(t *testing.T) {
	rig := newWriteRig(t)
	hdr := map[string]string{idem.HeaderName: "key-00000042"}
	first := do(rig.h, "POST", "/v1/matters", "editor", []byte(openingJSON), hdr)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first status = %d (%s)", first.Code, first.Body)
	}
	validate(t, "#/components/schemas/Gate", first.Body.Bytes())
	tests := []struct {
		name   string
		body   string
		status int
		code   string
	}{
		{"same key, same body replays", openingJSON, http.StatusAccepted, ""},
		// rule: ARC-I-008
		// The rule sets 422 for a reused key with a different body (the task text said 409).
		{"same key, different body", `{"short_name":"Other","client_id":"0190c000-0000-7000-8000-000000000001","practice_area_id":"pa","responsible_lawyer_id":"u9"}`,
			http.StatusUnprocessableEntity, idem.CodeKeyReused},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(rig.h, "POST", "/v1/matters", "editor", []byte(tc.body), hdr)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.status, rec.Body)
			}
			if tc.code == "" {
				if rec.Body.String() != first.Body.String() || rec.Header().Get(idem.ReplayedHeader) != "true" {
					t.Errorf("replay body = %s, want %s", rec.Body, first.Body)
				}
				return
			}
			var p struct{ Code string }
			if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil || p.Code != tc.code {
				t.Errorf("code = %q (%v), want %q", p.Code, err, tc.code)
			}
		})
	}
	if opens := rig.gates.Opens(); len(opens) != 1 || opens[0] != GateOpening {
		t.Errorf("gates opened = %v, want one %s", opens, GateOpening)
	}
}

func TestARC_I_012_MatterGatedChanges(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		path     string
		body     string
		hdr      map[string]string
		wantKind string
	}{
		{"post opens matter.opening", "POST", "/v1/matters", openingJSON, map[string]string{idem.HeaderName: "key-00000007"}, GateOpening},
		{"patch team", "PATCH", "/v1/matters/" + idA, `{"team":[{"user_id":"u2","role":"member"}]}`, map[string]string{"If-Match": `"3"`}, GateWidenAccess},
		{"patch restricted", "PATCH", "/v1/matters/" + idA, `{"restricted":true}`, map[string]string{"If-Match": `"3"`}, GateWidenAccess},
		{"patch status closing", "PATCH", "/v1/matters/" + idA, `{"status":"closing"}`, map[string]string{"If-Match": `"3"`}, GateClosing},
		{"patch status open (cleared)", "PATCH", "/v1/matters/" + idB, `{"status":"open"}`, map[string]string{"If-Match": `"1"`}, GateOpening},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rig := newWriteRig(t)
			before := append([]Matter(nil), rig.q.matters...)
			rec := do(rig.h, tc.method, tc.path, "editor", []byte(tc.body), tc.hdr)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
			}
			validate(t, "#/components/schemas/Gate", rec.Body.Bytes())
			var g struct {
				Kind  string `json:"kind"`
				State string `json:"state"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &g); err != nil || g.Kind != tc.wantKind || g.State != "open" {
				t.Errorf("gate = %+v (%v), want kind %s open", g, err, tc.wantKind)
			}
			if rig.updates.calls != 0 {
				t.Errorf("plain updates = %d, want 0", rig.updates.calls)
			}
			for i, m := range rig.q.matters {
				if m != before[i] {
					t.Errorf("matter %s changed: %+v", m.ID, m)
				}
			}
		})
	}
}

func TestMatterPatchPlainFields(t *testing.T) {
	rig := newWriteRig(t)
	rec := do(rig.h, "PATCH", "/v1/matters/"+idA, "editor", []byte(`{"short_name":"Alpha 2"}`), map[string]string{"If-Match": `"3"`})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
	}
	if etag := rec.Header().Get("ETag"); etag != `"4"` {
		t.Errorf("ETag = %q, want \"4\"", etag)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got["short_name"] != "Alpha 2" {
		t.Errorf("body = %v (%v)", got, err)
	}
	if len(rig.gates.Opens()) != 0 {
		t.Errorf("gates opened = %v, want none", rig.gates.Opens())
	}
	// The old version is now stale.
	rec = do(rig.h, "PATCH", "/v1/matters/"+idA, "editor", []byte(`{"short_name":"Alpha 3"}`), map[string]string{"If-Match": `"3"`})
	checkProblem(t, rec, 412, server.CodeVersionConflict)
}

func TestARC_I_012_MatterWriteDeclarationsOwnNoGateState(t *testing.T) {
	m := server.New(nil)
	if err := m.Mount((&Writes{}).WriteRoutes); err != nil {
		t.Fatal(err)
	}
	writes, err := server.DeclaredWrites(m.Handler())
	if err != nil {
		t.Fatal(err)
	}
	owned := map[string]string{"matter.status=open": GateOpening, "matter.team": GateWidenAccess,
		"matter.restricted": GateWidenAccess, "matter.status=closed": GateClosing}
	if mis := server.CheckWrites(m.Routes(), writes, owned, server.DecisionRoute); len(mis) != 0 {
		t.Errorf("CheckWrites = %q", mis)
	}
	for _, path := range []string{"/v1/matters", "/v1/matters/" + idA} {
		method := http.MethodPatch
		if path == "/v1/matters" {
			method = http.MethodPost
		}
		rec := httptest.NewRecorder()
		m.Handler().ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		checkProblem(t, rec, 503, CodeUnavailable)
	}
}
