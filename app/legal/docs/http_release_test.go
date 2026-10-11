package docs_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/docs"
	"github.com/dagi377/counsel/kernel/api"
	kdocs "github.com/dagi377/counsel/kernel/docs"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/idem/idemfake"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

func init() {
	users["releaser"] = []string{docs.CapRelease, docs.CapView}
}

type txStub struct{}

func (txStub) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return fn(ctx, nil)
}

// countingGates counts Open calls and keeps the opened payloads.
type countingGates struct {
	*gatefake.Fake
	opens    int
	payloads []json.RawMessage
}

func (g *countingGates) Open(ctx context.Context, tx pgx.Tx, kind string, s gate.Subject, payload json.RawMessage, p gate.Principal) (gate.GateID, error) {
	g.opens++
	g.payloads = append(g.payloads, payload)
	return g.Fake.Open(ctx, tx, kind, s, payload, p)
}

// Get returns the gate with a uuid id, as the Postgres engine has (the fake numbers them).
func (g *countingGates) Get(ctx context.Context, id gate.GateID) (gate.Gate, error) {
	out, err := g.Fake.Get(ctx, id)
	out.ID = "0190f000-0000-7000-8000-000000000001"
	return out, err
}

// fakeDrafts lists a fixed set of drafts and counts calls.
type fakeDrafts struct {
	all   []docs.Draft
	calls int
}

func (f *fakeDrafts) List(_ context.Context, fl docs.DraftFilter) ([]docs.Draft, error) {
	f.calls++
	var out []docs.Draft
	for _, d := range f.all {
		if d.DocumentID > fl.After && (fl.MatterID == "" || d.MatterID == fl.MatterID) && len(out) < fl.Limit {
			out = append(out, d)
		}
	}
	return out, nil
}

type releaseRig struct {
	h       http.Handler
	svc     *fakeDocs
	gates   *countingGates
	drafts  *fakeDrafts
	effects *int
}

func newReleaseRig(t *testing.T) releaseRig {
	t.Helper()
	base := newRig(t)
	codec, err := api.NewCodec(bytes.Repeat([]byte("k"), api.MinCursorKeyLen))
	if err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }
	effects := 0
	eng := gatefake.New(now, gatefake.Caps{})
	err = eng.Register(gate.GateKind{Slug: docs.GateOutgoing, Approver: docs.CapRelease, Risk: gate.RiskHigh, MinApprovers: 1,
		TTL: 72 * time.Hour, Effect: func(context.Context, pgx.Tx, registry.Approved) error { effects++; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	gates := &countingGates{Fake: eng}
	created := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	drafts := &fakeDrafts{all: []docs.Draft{
		{DocumentID: docA, VersionID: versionA, MatterID: matterA, Kind: docs.KindResearchMemo, RunID: "run-1", ActingFor: "editor", CreatedAt: created},
		{DocumentID: docB, VersionID: versionB, MatterID: matterA, Kind: docs.KindPrecedent, RunID: "run-2", ActingFor: "editor", CreatedAt: created},
	}}
	h := &docs.ReleaseHTTP{Documents: base.svc, Drafts: drafts, Gates: gates, DB: txStub{}, Access: testAccess{},
		Cursors: codec, Idem: idemfake.New(), Problems: &server.Problems{}, Principal: testPrincipal}
	m := server.New(nil)
	if err := m.Mount(h.Routes); err != nil {
		t.Fatal(err)
	}
	return releaseRig{h: m.Handler(), svc: base.svc, gates: gates, drafts: drafts, effects: &effects}
}

func releasePath(id string) string { return "/v1/documents/" + id + "/release" }

func releaseBody(channel string, versions ...string) string {
	return `{"channel":"` + channel + `","version_ids":["` + strings.Join(versions, `","`) + `"]}`
}

func TestARC_I_051_ReleaseProblems(t *testing.T) {
	ok := releaseBody("email", versionA)
	tests := []struct {
		name      string
		method    string
		path      string
		user      string
		body      string
		hdr       map[string]string
		status    int
		code      string
		zeroCalls bool
	}{
		{"release without principal", "POST", releasePath(docA), "", ok, withKey, 401, docs.CodeUnauthenticated, true},
		{"drafts without principal", "GET", "/v1/drafts", "", "", nil, 401, docs.CodeUnauthenticated, true},
		// rule: ARC-I-051
		{"release without documents.release", "POST", releasePath(docA), "editor", ok, withKey, 403, docs.CodeMissingCapability, true},
		{"drafts without documents.view", "GET", "/v1/drafts", "reader", "", nil, 403, docs.CodeMissingCapability, true},
		// rule: ARC-I-042
		{"release of walled document is 404", "POST", releasePath(docHidden), "releaser", ok, withKey, 404, docs.CodeNotFound, true},
		{"drafts of walled matter is 404", "GET", "/v1/drafts?matter_id=" + matterWall, "viewer", "", nil, 404, docs.CodeNotFound, true},
		{"release unknown document", "POST", releasePath("0190d000-0000-7000-8000-0000000000ee"), "releaser", ok, withKey, 404, docs.CodeNotFound, false},
		{"release malformed id", "POST", releasePath("nope"), "releaser", ok, withKey, 400, server.CodeValidationFailed, true},
		{"drafts tampered cursor", "GET", "/v1/drafts?cursor=abc.def", "viewer", "", nil, 400, api.CodeBadCursor, true},
		{"drafts malformed matter_id", "GET", "/v1/drafts?matter_id=nope", "viewer", "", nil, 400, server.CodeValidationFailed, true},
		// rule: ARC-I-007
		{"release missing Idempotency-Key", "POST", releasePath(docA), "releaser", ok, nil, 400, idem.CodeKeyRequired, true},
		{"release unknown field", "POST", releasePath(docA), "releaser", `{"channel":"email","version_ids":["` + versionA + `"],"to":"x"}`, withKey, 400, server.CodeValidationFailed, true},
		{"release bad channel", "POST", releasePath(docA), "releaser", releaseBody("fax", versionA), withKey, 400, server.CodeValidationFailed, true},
		{"release no versions", "POST", releasePath(docA), "releaser", `{"channel":"email","version_ids":[]}`, withKey, 400, server.CodeValidationFailed, true},
		{"release another document's version", "POST", releasePath(docA), "releaser", releaseBody("email", versionB), withKey, 422, server.CodeValidationFailed, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newReleaseRig(t)
			rec := do(r.h, tc.method, tc.path, tc.user, tc.body, tc.hdr)
			checkProblem(t, rec, tc.status, tc.code)
			if tc.zeroCalls && (r.svc.calls != 0 || r.drafts.calls != 0) {
				t.Errorf("service calls = %d/%d, want 0", r.svc.calls, r.drafts.calls)
			}
			if r.gates.opens != 0 {
				t.Errorf("gate opens = %d, want 0", r.gates.opens)
			}
		})
	}
}

func TestReleaseUnconfiguredAnswers503(t *testing.T) {
	m := server.New(nil)
	if err := m.Mount((&docs.ReleaseHTTP{}).Routes); err != nil {
		t.Fatal(err)
	}
	checkProblem(t, do(m.Handler(), "GET", "/v1/drafts", "viewer", "", nil), 503, docs.CodeUnavailable)
}

// rule: ARC-I-012
func TestARC_I_012_ReleaseOpensOutgoingGate(t *testing.T) {
	r := newReleaseRig(t)
	rec := do(r.h, "POST", releasePath(docA), "releaser", releaseBody("portal", versionA), withKey)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
	}
	validate(t, "#/components/schemas/Gate", rec.Body.Bytes())
	var g struct {
		Kind  string `json:"kind"`
		State string `json:"state"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &g); err != nil {
		t.Fatal(err)
	}
	if g.Kind != docs.GateOutgoing || g.State != "open" {
		t.Errorf("gate = %+v, want open %s", g, docs.GateOutgoing)
	}
	if r.gates.opens != 1 {
		t.Errorf("gate opens = %d, want 1", r.gates.opens)
	}
	var payload struct {
		MatterID   string   `json:"matter_id"`
		VersionIDs []string `json:"document_version_ids"`
		Channel    string   `json:"channel"`
	}
	if err := json.Unmarshal(r.gates.payloads[0], &payload); err != nil {
		t.Fatal(err)
	}
	if payload.MatterID != matterA || !slices.Equal(payload.VersionIDs, []string{versionA}) || payload.Channel != "portal" {
		t.Errorf("payload = %+v", payload)
	}
	// The release itself is the gate effect's: opening runs no effect and uploads nothing.
	if *r.effects != 0 || len(r.svc.uploads) != 0 {
		t.Errorf("effects = %d uploads = %d, want 0", *r.effects, len(r.svc.uploads))
	}
}

// rule: ARC-I-007
func TestARC_I_007_ReleaseIdempotency(t *testing.T) {
	tests := []struct {
		name       string
		second     string
		wantStatus int
	}{
		{"same key same body replays", releaseBody("email", versionA), http.StatusAccepted},
		{"same key different body refused", releaseBody("portal", versionA), http.StatusUnprocessableEntity},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newReleaseRig(t)
			first := do(r.h, "POST", releasePath(docA), "releaser", releaseBody("email", versionA), withKey)
			if first.Code != http.StatusAccepted {
				t.Fatalf("first = %d (%s)", first.Code, first.Body)
			}
			second := do(r.h, "POST", releasePath(docA), "releaser", tc.second, withKey)
			if second.Code != tc.wantStatus {
				t.Fatalf("second = %d, want %d (%s)", second.Code, tc.wantStatus, second.Body)
			}
			if tc.wantStatus == http.StatusAccepted && second.Body.String() != first.Body.String() {
				t.Errorf("replay body differs")
			}
			if r.gates.opens != 1 {
				t.Errorf("gate opens = %d, want 1", r.gates.opens)
			}
		})
	}
}

func TestDraftList(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		wantIDs  []string
		wantNext bool
	}{
		{"all", "", []string{docA, docB}, false},
		{"page of one", "?limit=1", []string{docA}, true},
		{"limit capped", "?limit=100000", []string{docA, docB}, false},
		{"by matter", "?matter_id=" + matterA, []string{docA, docB}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newReleaseRig(t)
			rec := do(r.h, "GET", "/v1/drafts"+tc.query, "viewer", "", nil)
			if rec.Code != 200 {
				t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
			}
			var page struct {
				Data []struct {
					DocumentID string `json:"document_id"`
					VersionID  string `json:"version_id"`
				} `json:"data"`
				NextCursor *string `json:"next_cursor"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, d := range page.Data {
				ids = append(ids, d.DocumentID)
			}
			if !slices.Equal(ids, tc.wantIDs) || (page.NextCursor != nil) != tc.wantNext {
				t.Errorf("ids = %v next = %v, want %v next %v", ids, page.NextCursor, tc.wantIDs, tc.wantNext)
			}
			if page.Data[0].VersionID != string(kdocs.VersionID(versionA)) {
				t.Errorf("version = %q", page.Data[0].VersionID)
			}
		})
	}
}
