package gatefake

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/gate"
	"github.com/jackc/pgx/v5"
)

var (
	t0      = time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC)
	alice   = gate.Principal{Kind: gate.PrincipalUser, UserID: "alice"}
	bob     = gate.Principal{Kind: gate.PrincipalUser, UserID: "bob"}
	carol   = gate.Principal{Kind: gate.PrincipalUser, UserID: "carol"}
	agent   = gate.Principal{Kind: gate.PrincipalAgent, RunID: "run-1", ActingFor: "alice"}
	payload = json.RawMessage(`{"amount":1000,"to":"x"}`)
	subject = gate.Subject{Kind: "thing", ID: "t-1"}
)

type env struct {
	f     *Fake
	now   time.Time
	ran   int
	fails bool
}

func newEnv(t *testing.T, kinds ...gate.GateKind) *env {
	t.Helper()
	e := &env{now: t0}
	e.f = New(func() time.Time { return e.now }, Caps{"alice": {"x.approve"}, "bob": {"x.approve"}, "carol": {"x.approve"}})
	for _, k := range kinds {
		k.Effect = func(context.Context, pgx.Tx, gate.Approved) error {
			if e.fails {
				return errors.New("boom")
			}
			e.ran++
			return nil
		}
		if err := e.f.Register(k); err != nil {
			t.Fatalf("Register: %v", err)
		}
	}
	return e
}

func kind(slug string, minApprovers int, coolingOff time.Duration) gate.GateKind {
	return gate.GateKind{Slug: slug, Approver: "x.approve", Risk: gate.RiskMedium, MinApprovers: minApprovers, TTL: 48 * time.Hour, CoolingOff: coolingOff}
}

func approve(hash string) gate.Decision {
	return gate.Decision{Verdict: gate.VerdictApprove, PayloadHash: hash, MFAVerifiedAt: t0}
}

func TestRegister(t *testing.T) {
	noop := func(context.Context, pgx.Tx, gate.Approved) error { return nil }
	valid := gate.GateKind{Slug: "x.pay", Approver: "x.approve", Risk: gate.RiskLow, MinApprovers: 1, TTL: time.Hour, Effect: noop}
	tests := []struct {
		name string
		mod  func(k *gate.GateKind)
		want error
	}{
		{name: "valid", mod: func(*gate.GateKind) {}},
		{name: "bad slug", mod: func(k *gate.GateKind) { k.Slug = "pay" }, want: gate.ErrInvalidKind},
		{name: "no approver", mod: func(k *gate.GateKind) { k.Approver = "" }, want: gate.ErrInvalidKind},
		{name: "no effect", mod: func(k *gate.GateKind) { k.Effect = nil }, want: gate.ErrInvalidKind},
		{name: "zero approvers", mod: func(k *gate.GateKind) { k.MinApprovers = 0 }, want: gate.ErrInvalidKind},
		{name: "four approvers", mod: func(k *gate.GateKind) { k.MinApprovers = 4 }, want: gate.ErrInvalidKind},
		{name: "irreversible needs two", mod: func(k *gate.GateKind) { k.Risk = gate.RiskIrreversible }, want: gate.ErrInvalidKind},
		{name: "irreversible with two", mod: func(k *gate.GateKind) { k.Risk = gate.RiskIrreversible; k.MinApprovers = 2 }},
		{name: "ttl too long", mod: func(k *gate.GateKind) { k.TTL = 721 * time.Hour }, want: gate.ErrInvalidKind},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := valid
			tt.mod(&k)
			f := New(time.Now, Caps{})
			if err := f.Register(k); !errors.Is(err, tt.want) {
				t.Fatalf("Register err = %v, want %v", err, tt.want)
			}
		})
	}
	f := New(time.Now, Caps{})
	if err := f.Register(valid); err != nil {
		t.Fatal(err)
	}
	if err := f.Register(valid); !errors.Is(err, gate.ErrDuplicateKind) {
		t.Fatalf("duplicate Register err = %v", err)
	}
}

func TestDecide(t *testing.T) {
	policyAllow := func(context.Context, gate.Gate, gate.Principal) (bool, string) { return true, "sole" }
	tests := []struct {
		name       string
		kind       gate.GateKind
		preparer   gate.Principal
		policy     gate.SelfApprovalPolicy
		by         gate.Principal
		mod        func(d *gate.Decision, version *int)
		want       error
		wantStatus gate.Status
		wantRan    int
	}{
		{name: "other user approves", kind: kind("x.pay", 1, 0), preparer: alice, by: bob, wantStatus: gate.StatusApproved, wantRan: 1},
		{name: "preparer never approves", kind: kind("x.pay", 1, 0), preparer: alice, by: alice, want: gate.ErrSelfApproval, wantStatus: gate.StatusPending},
		{name: "agent's acting-for user is preparer", kind: kind("x.pay", 1, 0), preparer: agent, by: alice, want: gate.ErrSelfApproval, wantStatus: gate.StatusPending},
		{name: "agent never approves", kind: kind("x.pay", 1, 0), preparer: bob, by: agent, want: gate.ErrAgentApprover, wantStatus: gate.StatusPending},
		{name: "self-approval policy without reason", kind: kind("x.pay", 1, 0), preparer: alice, policy: policyAllow, by: alice, want: gate.ErrSelfApproval, wantStatus: gate.StatusPending},
		{name: "self-approval policy waits cooling-off", kind: kind("x.pay", 1, 24*time.Hour), preparer: alice, policy: policyAllow, by: alice,
			mod: func(d *gate.Decision, _ *int) { d.SelfApprovalReason = "sole" }, wantStatus: gate.StatusApprovedCooling},
		{name: "no capability", kind: kind("x.pay", 1, 0), preparer: alice, by: gate.Principal{Kind: gate.PrincipalUser, UserID: "dave"}, want: gate.ErrNotAuthorized, wantStatus: gate.StatusPending},
		{name: "stale version", kind: kind("x.pay", 1, 0), preparer: alice, by: bob, mod: func(_ *gate.Decision, v *int) { *v = 2 }, want: gate.ErrStaleVersion, wantStatus: gate.StatusPending},
		{name: "payload changed", kind: kind("x.pay", 1, 0), preparer: alice, by: bob, mod: func(d *gate.Decision, _ *int) { d.PayloadHash = "sha256:00" }, want: gate.ErrPayloadChanged, wantStatus: gate.StatusPending},
		{name: "no mfa", kind: kind("x.pay", 1, 0), preparer: alice, by: bob, mod: func(d *gate.Decision, _ *int) { d.MFAVerifiedAt = time.Time{} }, want: gate.ErrMFARequired, wantStatus: gate.StatusPending},
		{name: "reject needs reason", kind: kind("x.pay", 1, 0), preparer: alice, by: bob, mod: func(d *gate.Decision, _ *int) { d.Verdict = gate.VerdictReject }, want: gate.ErrReasonRequired, wantStatus: gate.StatusPending},
		{name: "reject", kind: kind("x.pay", 1, 0), preparer: alice, by: bob, mod: func(d *gate.Decision, _ *int) { d.Verdict = gate.VerdictReject; d.Reason = "no" }, wantStatus: gate.StatusRejected},
		{name: "quorum of two waits", kind: kind("x.pay", 2, 0), preparer: alice, by: bob, wantStatus: gate.StatusPending},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, tt.kind)
			e.f.SetSelfApprovalPolicy(tt.policy)
			ctx := context.Background()
			id, err := e.f.Open(ctx, nil, tt.kind.Slug, subject, payload, tt.preparer)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			g, _ := e.f.Get(ctx, id)
			d, version := approve(g.PayloadHash), 1
			if tt.mod != nil {
				tt.mod(&d, &version)
			}
			if err := e.f.Decide(ctx, nil, id, version, d, tt.by); !errors.Is(err, tt.want) {
				t.Fatalf("Decide err = %v, want %v", err, tt.want)
			}
			g, _ = e.f.Get(ctx, id)
			if g.Status != tt.wantStatus || e.ran != tt.wantRan {
				t.Fatalf("status %s ran %d, want %s ran %d", g.Status, e.ran, tt.wantStatus, tt.wantRan)
			}
		})
	}
}

func TestLifecycle(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		run  func(t *testing.T, e *env)
	}{
		{name: "quorum of two runs effect once", run: func(t *testing.T, e *env) {
			id, _ := e.f.Open(ctx, nil, "x.two", subject, payload, alice)
			g, _ := e.f.Get(ctx, id)
			if err := e.f.Decide(ctx, nil, id, 1, approve(g.PayloadHash), bob); err != nil {
				t.Fatal(err)
			}
			if err := e.f.Decide(ctx, nil, id, 1, approve(g.PayloadHash), bob); !errors.Is(err, gate.ErrAlreadyDecided) {
				t.Fatalf("second decision by bob err = %v", err)
			}
			if err := e.f.Decide(ctx, nil, id, 1, approve(g.PayloadHash), carol); err != nil {
				t.Fatal(err)
			}
			if err := e.f.Decide(ctx, nil, id, 1, approve(g.PayloadHash), carol); !errors.Is(err, gate.ErrNotPending) {
				t.Fatalf("decision after approval err = %v", err)
			}
			if e.ran != 1 {
				t.Fatalf("effect ran %d times, want 1", e.ran)
			}
		}},
		{name: "expiry", run: func(t *testing.T, e *env) {
			id, _ := e.f.Open(ctx, nil, "x.pay", subject, payload, alice)
			g, _ := e.f.Get(ctx, id)
			e.now = t0.Add(48 * time.Hour)
			if err := e.f.Decide(ctx, nil, id, 1, approve(g.PayloadHash), bob); !errors.Is(err, gate.ErrExpired) {
				t.Fatalf("Decide err = %v, want expired", err)
			}
			if g, _ := e.f.Get(ctx, id); g.Status != gate.StatusExpired {
				t.Fatalf("status %s", g.Status)
			}
		}},
		{name: "revise supersedes and resets version", run: func(t *testing.T, e *env) {
			id, _ := e.f.Open(ctx, nil, "x.pay", subject, payload, alice)
			old, _ := e.f.Get(ctx, id)
			if _, err := e.f.Revise(ctx, nil, id, payload, bob); !errors.Is(err, gate.ErrNotPreparer) {
				t.Fatalf("Revise by bob err = %v", err)
			}
			next, err := e.f.Revise(ctx, nil, id, json.RawMessage(`{"amount":900}`), alice)
			if err != nil {
				t.Fatal(err)
			}
			if err := e.f.Decide(ctx, nil, id, 1, approve(old.PayloadHash), bob); !errors.Is(err, gate.ErrNotPending) {
				t.Fatalf("Decide old err = %v", err)
			}
			g, _ := e.f.Get(ctx, next)
			if g.Version != 2 || g.Supersedes != id || g.PayloadHash == old.PayloadHash {
				t.Fatalf("revised gate %+v", g)
			}
			if err := e.f.Decide(ctx, nil, next, 2, approve(g.PayloadHash), bob); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "cooling-off effect runs only when due", run: func(t *testing.T, e *env) {
			e.f.SetSelfApprovalPolicy(func(context.Context, gate.Gate, gate.Principal) (bool, string) { return true, "sole" })
			id, _ := e.f.Open(ctx, nil, "x.cool", subject, payload, alice)
			g, _ := e.f.Get(ctx, id)
			d := approve(g.PayloadHash)
			d.SelfApprovalReason = "sole"
			if err := e.f.Decide(ctx, nil, id, 1, d, alice); err != nil {
				t.Fatal(err)
			}
			e.now = t0.Add(24*time.Hour - time.Second)
			if n, _ := e.f.RunDue(ctx, nil); n != 0 {
				t.Fatalf("ran %d before cooling-off ended", n)
			}
			e.now = t0.Add(24 * time.Hour)
			if n, _ := e.f.RunDue(ctx, nil); n != 1 || e.ran != 1 {
				t.Fatalf("ran %d/%d at cooling-off end, want 1", n, e.ran)
			}
			if n, _ := e.f.RunDue(ctx, nil); n != 0 || e.ran != 1 {
				t.Fatalf("effect ran again")
			}
		}},
		{name: "failed effect leaves gate pending", run: func(t *testing.T, e *env) {
			e.fails = true
			id, _ := e.f.Open(ctx, nil, "x.pay", subject, payload, alice)
			g, _ := e.f.Get(ctx, id)
			if err := e.f.Decide(ctx, nil, id, 1, approve(g.PayloadHash), bob); !errors.Is(err, gate.ErrEffectFailed) {
				t.Fatalf("Decide err = %v", err)
			}
			if g, _ := e.f.Get(ctx, id); g.Status != gate.StatusPending || len(g.Approvers) != 0 {
				t.Fatalf("gate after failed effect %+v", g)
			}
		}},
		{name: "withdraw", run: func(t *testing.T, e *env) {
			id, _ := e.f.Open(ctx, nil, "x.pay", subject, payload, agent)
			if err := e.f.Withdraw(ctx, nil, id, bob); !errors.Is(err, gate.ErrNotPreparer) {
				t.Fatalf("Withdraw by bob err = %v", err)
			}
			if err := e.f.Withdraw(ctx, nil, id, alice); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "open errors", run: func(t *testing.T, e *env) {
			if _, err := e.f.Open(ctx, nil, "x.nope", subject, payload, alice); !errors.Is(err, gate.ErrUnknownKind) {
				t.Fatalf("unknown kind err = %v", err)
			}
			if _, err := e.f.Open(ctx, nil, "x.pay", subject, json.RawMessage(`[1]`), alice); !errors.Is(err, gate.ErrInvalidPayload) {
				t.Fatalf("bad payload err = %v", err)
			}
			if _, err := e.f.Get(ctx, "missing"); !errors.Is(err, gate.ErrNotFound) {
				t.Fatalf("Get err = %v", err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, kind("x.pay", 1, 0), kind("x.two", 2, 0), kind("x.cool", 1, 24*time.Hour))
			tt.run(t, e)
		})
	}
}

func TestPayloadHashKeyOrder(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		same bool
	}{
		{name: "key order ignored", a: `{"a":1,"b":2}`, b: `{ "b":2, "a":1 }`, same: true},
		{name: "value differs", a: `{"a":1}`, b: `{"a":2}`, same: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ha, _ := PayloadHash(json.RawMessage(tt.a))
			hb, _ := PayloadHash(json.RawMessage(tt.b))
			if (ha == hb) != tt.same {
				t.Fatalf("hash(%s)=%s hash(%s)=%s", tt.a, ha, tt.b, hb)
			}
		})
	}
}
