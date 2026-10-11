package recon_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"

	"github.com/dagi377/counsel/app/legal/trust/recon"
	"github.com/dagi377/counsel/app/legal/trust/recon/reconfake"
	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/idem/idemfake"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

const (
	acctA       = "0190a000-0000-7000-8000-0000000000aa"
	wAcctHidden  = "0190a000-0000-7000-8000-0000000000ff"
	recBalanced = "0190d000-0000-7000-8000-000000000001"
	recOff      = "0190d000-0000-7000-8000-000000000002"
	recHidden   = "0190d000-0000-7000-8000-000000000003"
)

var (
	sep = recon.Period{Year: 2026, Month: time.September}
	aug = recon.Period{Year: 2026, Month: time.August}
	jul = recon.Period{Year: 2026, Month: time.July}
)

// svc counts every call on the fake and hands out uuid reconciliation ids.
type svc struct {
	*reconfake.Fake
	mu    sync.Mutex
	calls map[string]int
	ids   map[string]string // uuid -> fake id
}

func (s *svc) count(m string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[m]++
}

func (s *svc) total() int {
	n := 0
	for _, c := range s.calls {
		n += c
	}
	return n
}

func (s *svc) ImportStatement(ctx context.Context, st recon.Statement) (string, error) {
	s.count("ImportStatement")
	return s.Fake.ImportStatement(ctx, st)
}

func (s *svc) Prepare(ctx context.Context, a string, p recon.Period, items []recon.ReconcilingItem, by string) (recon.Reconciliation, error) {
	s.count("Prepare")
	rec, err := s.Fake.Prepare(ctx, a, p, items, by)
	if err == nil {
		id := "0190d000-0000-7000-8000-0000000001" + strconv.Itoa(10+len(s.ids))
		s.ids[id], rec.ID = rec.ID, id
	}
	return rec, err
}

// Approve is effect-owned: only the sign-off gate's effect approves.
func (s *svc) Approve(ctx context.Context, id string, by recon.Approver) error {
	s.count("Approve")
	return s.Fake.Approve(ctx, s.ids[id], by)
}

func (s *svc) Get(ctx context.Context, id string) (recon.Reconciliation, error) {
	s.count("Get")
	fid, ok := s.ids[id]
	if !ok {
		return recon.Reconciliation{}, recon.ErrNotFound
	}
	rec, err := s.Fake.Get(ctx, fid)
	rec.ID = id
	return rec, err
}

func (s *svc) Locked(ctx context.Context, a string, p recon.Period) (bool, error) {
	s.count("Locked")
	return s.Fake.Locked(ctx, a, p)
}

// parser reads "id,amount" lines after an "opening,closing" header line.
type parser struct{}

func (parser) Parse(_ context.Context, accountID string, file []byte) (recon.Statement, error) {
	lines := strings.Split(strings.TrimSpace(string(file)), "\n")
	var open, closing int64
	if _, err := fmtSscan(lines[0], &open, &closing); err != nil {
		return recon.Statement{}, recon.ErrUnparseable
	}
	st := recon.Statement{AccountID: accountID, Period: sep, OpeningCents: open, ClosingCents: closing}
	for _, l := range lines[1:] {
		id, amt, ok := strings.Cut(l, ",")
		n, err := strconv.ParseInt(amt, 10, 64)
		if !ok || err != nil {
			return recon.Statement{}, recon.ErrUnparseable
		}
		st.Lines = append(st.Lines, recon.StatementLine{ID: id, AmountCents: n})
	}
	return st, nil
}

func fmtSscan(s string, a, b *int64) (int, error) {
	x, y, ok := strings.Cut(s, ",")
	if !ok {
		return 0, recon.ErrUnparseable
	}
	var err error
	if *a, err = strconv.ParseInt(x, 10, 64); err != nil {
		return 0, err
	}
	if *b, err = strconv.ParseInt(y, 10, 64); err != nil {
		return 1, err
	}
	return 2, nil
}

type versions struct{}

func (versions) Version(context.Context, string) (int64, error) { return 1, nil }

type countingGates struct {
	*gatefake.Fake
	opens int
	kinds []string
	ids   map[gate.GateID]gate.GateID
}

func (g *countingGates) Open(ctx context.Context, tx pgx.Tx, kind string, s gate.Subject, payload json.RawMessage, p gate.Principal) (gate.GateID, error) {
	g.opens++
	g.kinds = append(g.kinds, kind)
	return g.Fake.Open(ctx, tx, kind, s, payload, p)
}

// Get returns the gate with a uuid id, as the Postgres engine has (the fake numbers them).
func (g *countingGates) Get(ctx context.Context, id gate.GateID) (gate.Gate, error) {
	out, err := g.Fake.Get(ctx, id)
	out.ID = "0190f000-0000-7000-8000-000000000001"
	return out, err
}

type txStub struct{}

func (txStub) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return fn(ctx, nil)
}

type wStubAccess map[string][]string

func (a wStubAccess) Can(_ context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error) {
	if s.ID == wAcctHidden {
		return false, identity.ErrNotVisible
	}
	for _, c := range a[p.UserID] {
		if c == capability {
			return true, nil
		}
	}
	return false, nil
}

var testAccess = wStubAccess{"alice": {recon.CapReconcile}, "viewer": {"trust.view"}}

func testPrincipal(r *http.Request) (registry.Principal, bool) {
	u := r.Header.Get("X-Test-User")
	return registry.Principal{Kind: registry.PrincipalUser, UserID: u}, u != ""
}

type rig struct {
	h     http.Handler
	svc   *svc
	gates *countingGates
}

func wNewRig(t *testing.T) rig {
	t.Helper()
	ctx := context.Background()
	f := reconfake.New()
	s := &svc{Fake: f, calls: map[string]int{}, ids: map[string]string{}}
	seed := func(uuid, acct string, p recon.Period, book, clients int64) {
		if _, err := f.ImportStatement(ctx, recon.Statement{AccountID: acct, Period: p, OpeningCents: 0, ClosingCents: 1000,
			Lines: []recon.StatementLine{{ID: "l1", AmountCents: 1000}}}); err != nil {
			t.Fatal(err)
		}
		f.SetBook(acct, p, book, clients)
		rec, err := f.Prepare(ctx, acct, p, nil, "bob")
		if err != nil {
			t.Fatal(err)
		}
		s.ids[uuid] = rec.ID
	}
	seed(recBalanced, acctA, aug, 1000, 1000)
	seed(recOff, acctA, jul, 900, 900)
	seed(recHidden, wAcctHidden, aug, 1000, 1000)
	// September's statement is imported but not prepared.
	if _, err := f.ImportStatement(ctx, recon.Statement{AccountID: acctA, Period: recon.Period{Year: 2026, Month: time.June}, ClosingCents: 0}); err != nil {
		t.Fatal(err)
	}
	f.SetBook(acctA, recon.Period{Year: 2026, Month: time.June}, 0, 0)
	now := func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }
	eng := gatefake.New(now, gatefake.Caps{})
	if err := eng.Register(gate.GateKind{Slug: recon.GateSignOff, Approver: "trust.approve", Risk: gate.RiskHigh, MinApprovers: 1,
		TTL: 168 * time.Hour, Effect: func(context.Context, pgx.Tx, registry.Approved) error { return nil }}); err != nil {
		t.Fatal(err)
	}
	g := &countingGates{Fake: eng}
	h := &recon.WriteHTTP{Recons: s, Parser: parser{}, Statements: f, Versions: versions{}, Gates: g, DB: txStub{}, Access: testAccess,
		Idem: idemfake.New(), Problems: &server.Problems{}, Principal: testPrincipal}
	m := server.New(nil)
	if err := m.Mount(h.Routes); err != nil {
		t.Fatal(err)
	}
	return rig{h: m.Handler(), svc: s, gates: g}
}

func wDo(h http.Handler, path, user, ctype, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if user != "" {
		req.Header.Set("X-Test-User", user)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// upload builds a multipart body with the given fields and an optional file.
func upload(t *testing.T, fields map[string]string, file string) (string, string) {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if file != "" {
		fw, err := mw.CreateFormFile("file", "statement.csv")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(file)); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return mw.FormDataContentType(), b.String()
}

var wOpenAPISchemas = sync.OnceValues(func() (*jsonschema.Compiler, error) {
	raw, err := os.ReadFile("../../../../api/openapi.yaml")
	if err != nil {
		return nil, err
	}
	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	js, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat = true
	if err := c.AddResource("openapi.json", bytes.NewReader(js)); err != nil {
		return nil, err
	}
	return c, nil
})

func wValidate(t *testing.T, ref string, body []byte) {
	t.Helper()
	c, err := wOpenAPISchemas()
	if err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile("openapi.json" + ref)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if err := s.Validate(v); err != nil {
		t.Errorf("body does not wValidate against %s: %v", ref, err)
	}
}

func wCheckProblem(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, status, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != api.ProblemContentType {
		t.Errorf("Content-Type = %q, want %q", ct, api.ProblemContentType)
	}
	wValidate(t, "#/components/schemas/Problem", rec.Body.Bytes())
	var p api.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.Code != code {
		t.Errorf("code = %q, want %q", p.Code, code)
	}
}

const (
	importPath  = "/v1/trust/bank-lines/imports"
	preparePath = "/v1/trust/reconciliations"
	jsonType    = "application/json"
	goodCSV     = "0,1500\nb1,1000\nb2,500"
)

func submitPath(id string) string { return "/v1/trust/reconciliations/" + id + "/submit" }

func key(n string) map[string]string {
	return map[string]string{idem.HeaderName: "0190e000-0000-7000-8000-000000000" + n}
}

func keyMatch(n, v string) map[string]string {
	h := key(n)
	h["If-Match"] = v
	return h
}

func TestARC_I_051_ReconWriteProblems(t *testing.T) {
	upType, upBody := upload(t, map[string]string{"account_id": acctA}, goodCSV)
	hiddenType, hiddenBody := upload(t, map[string]string{"account_id": wAcctHidden}, goodCSV)
	extraType, extraBody := upload(t, map[string]string{"account_id": acctA, "post": "true"}, goodCSV)
	badType, badBody := upload(t, map[string]string{"account_id": acctA}, "garbage")
	offType, offBody := upload(t, map[string]string{"account_id": acctA}, "0,1600\nb1,1000\nb2,500")
	good := `{"account_id":"` + acctA + `","month":"2026-06","bank_balance_cents":0}`
	tests := []struct {
		name      string
		path      string
		user      string
		ctype     string
		body      string
		hdr       map[string]string
		status    int
		code      string
		zeroCalls bool
	}{
		{"import without principal", importPath, "", upType, upBody, key("101"), 401, recon.CodeUnauthenticated, true},
		{"prepare without principal", preparePath, "", jsonType, good, key("102"), 401, recon.CodeUnauthenticated, true},
		{"submit without principal", submitPath(recBalanced), "", "", "", keyMatch("103", `"1"`), 401, recon.CodeUnauthenticated, true},
		// rule: ARC-I-051
		{"import without trust.reconcile", importPath, "viewer", upType, upBody, key("104"), 403, recon.CodeMissingCapability, true},
		{"prepare without trust.reconcile", preparePath, "viewer", jsonType, good, key("105"), 403, recon.CodeMissingCapability, true},
		{"submit without trust.reconcile", submitPath(recBalanced), "viewer", "", "", keyMatch("106", `"1"`), 403, recon.CodeMissingCapability, true},
		// rule: ARC-I-042
		{"import on hidden account is 404", importPath, "alice", hiddenType, hiddenBody, key("107"), 404, recon.CodeNotFound, true},
		{"prepare on hidden account is 404", preparePath, "alice", jsonType, `{"account_id":"` + wAcctHidden + `","month":"2026-08","bank_balance_cents":0}`, key("108"), 404, recon.CodeNotFound, true},
		{"submit hidden reconciliation is 404", submitPath(recHidden), "alice", "", "", keyMatch("109", `"1"`), 404, recon.CodeNotFound, false},
		{"import unknown form field", importPath, "alice", extraType, extraBody, key("110"), 400, server.CodeValidationFailed, true},
		{"prepare unknown body field", preparePath, "alice", jsonType, `{"account_id":"` + acctA + `","month":"2026-06","bank_balance_cents":0,"status":"signed"}`, key("111"), 400, server.CodeValidationFailed, true},
		{"prepare malformed account id", preparePath, "alice", jsonType, `{"account_id":"nope","month":"2026-06","bank_balance_cents":0}`, key("112"), 400, server.CodeValidationFailed, true},
		{"submit malformed id", submitPath("nope"), "alice", "", "", keyMatch("113", `"1"`), 400, server.CodeValidationFailed, true},
		// rule: ARC-I-006
		{"submit missing If-Match", submitPath(recBalanced), "alice", "", "", key("114"), 428, server.CodePreconditionRequired, true},
		{"submit stale version", submitPath(recBalanced), "alice", "", "", keyMatch("115", `"9"`), 412, server.CodeVersionConflict, false},
		// rule: ARC-I-007
		{"import missing idempotency key", importPath, "alice", upType, upBody, nil, 400, idem.CodeKeyRequired, true},
		{"prepare missing idempotency key", preparePath, "alice", jsonType, good, nil, 400, idem.CodeKeyRequired, true},
		{"submit missing idempotency key", submitPath(recBalanced), "alice", "", "", map[string]string{"If-Match": `"1"`}, 400, idem.CodeKeyRequired, true},
		{"import unparseable statement", importPath, "alice", badType, badBody, key("116"), 422, recon.CodeUnparseableStatement, true},
		{"import lines not summing to closing", importPath, "alice", offType, offBody, key("117"), 422, recon.CodeUnparseableStatement, false},
		{"prepare locked or missing month", preparePath, "alice", jsonType, `{"account_id":"` + acctA + `","month":"2026-01","bank_balance_cents":0}`, key("118"), 404, recon.CodeNotFound, false},
		// rule: BIL-I-009
		{"submit unbalanced without explanations", submitPath(recOff), "alice", "", "", keyMatch("119", `"1"`), 422, recon.CodeUnexplainedDifference, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := wNewRig(t)
			rec := wDo(r.h, tc.path, tc.user, tc.ctype, tc.body, tc.hdr)
			wCheckProblem(t, rec, tc.status, tc.code)
			if tc.zeroCalls && r.svc.total() != 0 {
				t.Errorf("service calls = %v, want 0", r.svc.calls)
			}
			if r.gates.opens != 0 || r.svc.calls["Approve"] != 0 {
				t.Errorf("gate opens = %d approve = %d, want 0", r.gates.opens, r.svc.calls["Approve"])
			}
		})
	}
}

func TestReconDuplicateStatement(t *testing.T) {
	r := wNewRig(t)
	ctype, body := upload(t, map[string]string{"account_id": acctA}, goodCSV)
	first := wDo(r.h, importPath, "alice", ctype, body, key("201"))
	if first.Code != http.StatusAccepted {
		t.Fatalf("first = %d %s", first.Code, first.Body)
	}
	ctype, body = upload(t, map[string]string{"account_id": acctA}, goodCSV)
	wCheckProblem(t, wDo(r.h, importPath, "alice", ctype, body, key("202")), 409, recon.CodeDuplicateStatement)
	if r.svc.calls["ImportStatement"] != 2 || r.svc.calls["Prepare"] != 0 || r.gates.opens != 0 {
		t.Errorf("calls = %v opens = %d; import posts nothing else", r.svc.calls, r.gates.opens)
	}
}

func TestReconPrepare(t *testing.T) {
	r := wNewRig(t)
	rec := wDo(r.h, preparePath, "alice", jsonType, `{"account_id":"`+acctA+`","month":"2026-06","bank_balance_cents":0}`, key("301"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
	}
	wValidate(t, "#/components/schemas/Reconciliation", rec.Body.Bytes())
}

func TestBIL_I_009_PrepareBankBalanceMatchesStatement(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		month   string
		balance string
		status  int
		code    string
	}{
		{"matches the statement's closing balance", "601", "2026-06", "0", http.StatusCreated, ""},
		// rule: BIL-I-009
		// The imported statement's closing balance is authoritative; a different figure is refused.
		{"differs from the closing balance", "602", "2026-06", "5", http.StatusUnprocessableEntity, recon.CodeBankBalanceMismatch},
		{"negative and different", "603", "2026-06", "-1", http.StatusUnprocessableEntity, recon.CodeBankBalanceMismatch},
		{"no statement imported", "604", "2026-01", "0", http.StatusNotFound, recon.CodeNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := wNewRig(t)
			body := `{"account_id":"` + acctA + `","month":"` + tc.month + `","bank_balance_cents":` + tc.balance + `}`
			rec := wDo(r.h, preparePath, "alice", jsonType, body, key(tc.key))
			if tc.code == "" {
				if rec.Code != tc.status {
					t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.status, rec.Body)
				}
				return
			}
			wCheckProblem(t, rec, tc.status, tc.code)
			if r.svc.calls["Prepare"] != 0 {
				t.Errorf("Prepare called %d times on a refused balance", r.svc.calls["Prepare"])
			}
		})
	}
}

// rule: ARC-I-012
func TestARC_I_012_ReconSubmitOpensGateOnly(t *testing.T) {
	r := wNewRig(t)
	rec := wDo(r.h, submitPath(recBalanced), "alice", "", "", keyMatch("401", `"1"`))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
	}
	wValidate(t, "#/components/schemas/Gate", rec.Body.Bytes())
	if r.gates.opens != 1 || r.gates.kinds[0] != recon.GateSignOff {
		t.Errorf("gates = %v, want one %s", r.gates.kinds, recon.GateSignOff)
	}
	if r.svc.calls["Approve"] != 0 {
		t.Errorf("Approve called %d times", r.svc.calls["Approve"])
	}
	got, _ := r.svc.Get(context.Background(), recBalanced)
	if got.Status != recon.StatusPrepared {
		t.Errorf("status = %s, want prepared", got.Status)
	}
}

func TestARC_I_008_ReconIdempotency(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		body  string
		other string
		want  int
	}{
		{"prepare", preparePath, `{"account_id":"` + acctA + `","month":"2026-06","bank_balance_cents":0}`,
			`{"account_id":"` + acctA + `","month":"2026-06","bank_balance_cents":5}`, http.StatusCreated},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := wNewRig(t)
			first := wDo(r.h, tc.path, "alice", jsonType, tc.body, key("501"))
			if first.Code != tc.want {
				t.Fatalf("first = %d %s", first.Code, first.Body)
			}
			again := wDo(r.h, tc.path, "alice", jsonType, tc.body, key("501"))
			if again.Code != tc.want || again.Body.String() != first.Body.String() {
				t.Errorf("replay = %d %s, want %s", again.Code, again.Body, first.Body)
			}
			// rule: ARC-I-008
			// A reused key with a different body answers 422, as kernel/idem does.
			wCheckProblem(t, wDo(r.h, tc.path, "alice", jsonType, tc.other, key("501")), 422, idem.CodeKeyReused)
			if r.svc.calls["Prepare"] != 1 {
				t.Errorf("Prepare called %d times, want 1", r.svc.calls["Prepare"])
			}
		})
	}
	r := wNewRig(t)
	hdr := keyMatch("502", `"1"`)
	first := wDo(r.h, submitPath(recBalanced), "alice", "", "", hdr)
	again := wDo(r.h, submitPath(recBalanced), "alice", "", "", hdr)
	if first.Code != http.StatusAccepted || again.Body.String() != first.Body.String() || r.gates.opens != 1 {
		t.Errorf("submit replay = %d/%d opens = %d", first.Code, again.Code, r.gates.opens)
	}
}

func TestARC_I_012_ReconWritesDeclared(t *testing.T) {
	writes, err := server.DeclaredWrites(wNewRig(t).h)
	if err != nil {
		t.Fatal(err)
	}
	if len(writes) != 3 {
		t.Fatalf("writes = %+v, want import, prepare and submit", writes)
	}
	for _, w := range writes {
		for _, k := range w.Sets {
			if k == "reconciliation.status=signed_off" {
				t.Errorf("%s declares gate-owned %s", w.Pattern, k)
			}
		}
	}
}
