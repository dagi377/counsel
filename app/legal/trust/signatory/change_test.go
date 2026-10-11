package signatory_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/trust/signatory"
	"github.com/dagi377/counsel/app/legal/trust/signatory/signatoryfake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/registry"
)

func user(id string) gate.Principal { return gate.Principal{Kind: registry.PrincipalUser, UserID: id} }

var t0 = time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC)

// setup returns a gate engine with both signatory kinds registered and a service where
// "owner" and "second" already sign on account "acc1".
func setup(t *testing.T) (*gatefake.Fake, *signatoryfake.Fake) {
	t.Helper()
	ctx := context.Background()
	svc := signatoryfake.New()
	for _, id := range []string{"owner", "second", "newbie", "boot"} {
		svc.PutUser(signatory.User{ID: id, Licensee: true, Active: true})
	}
	svc.PutUser(signatory.User{ID: "clerk", Active: true})
	c, err := svc.Propose(ctx, signatory.Change{AccountID: "acc1", PreparedBy: "boot", Add: []signatory.Signatory{
		{UserID: "owner", Kind: signatory.Licensee}, {UserID: "second", Kind: signatory.Licensee},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Approve(ctx, c.ID, signatory.Approver{UserID: "owner"}); err != nil {
		t.Fatal(err)
	}

	eng := gatefake.New(func() time.Time { return t0 }, gatefake.Caps{
		"owner": {"trust.signatories"}, "second": {"trust.signatories"},
	})
	for slug, effect := range signatory.Effects(svc) {
		if err := eng.Register(registry.GateKind{Slug: slug, Approver: "trust.signatories", Risk: registry.RiskHigh,
			MinApprovers: 2, TTL: 72 * time.Hour, Effect: effect}); err != nil {
			t.Fatal(err)
		}
	}
	return eng, svc
}

func approve(t *testing.T, eng *gatefake.Fake, id gate.GateID, by string) error {
	t.Helper()
	g, err := eng.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return eng.Decide(context.Background(), nil, id, g.Version,
		gate.Decision{Verdict: gate.VerdictApprove, PayloadHash: g.PayloadHash, MFAVerifiedAt: t0}, user(by))
}

func TestRoute(t *testing.T) {
	cases := []struct {
		kind    string
		want    string
		wantErr error
	}{
		{signatory.KindChange, signatory.KindChange, nil},
		{signatory.KindSigningAuthority, signatory.KindChange, nil},
		{"trust.withdrawal", "", gate.ErrUnknownKind},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			got, err := signatory.Route(tc.kind)
			if got != tc.want || !errors.Is(err, tc.wantErr) {
				t.Fatalf("Route = %q, %v; want %q, %v", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestOpenChangeAppliesOnlyViaCanonicalKind(t *testing.T) {
	cases := []struct {
		name      string
		kind      string
		payload   signatory.Payload
		approvers []string
		wantOpen  error
		wantApply error
		canSign   map[string]bool
	}{
		{
			name:      "add via canonical kind",
			kind:      signatory.KindChange,
			payload:   signatory.Payload{TrustAccountID: "acc1", UserID: "newbie", Change: signatory.PayloadAdd},
			approvers: []string{"owner", "second"},
			canSign:   map[string]bool{"newbie": true, "owner": true},
		},
		{
			name:      "alias routes to canonical",
			kind:      signatory.KindSigningAuthority,
			payload:   signatory.Payload{TrustAccountID: "acc1", UserID: "second", Change: signatory.PayloadRemove},
			approvers: []string{"owner", "second"},
			canSign:   map[string]bool{"second": false, "owner": true},
		},
		{
			name:      "one approval does not apply",
			kind:      signatory.KindChange,
			payload:   signatory.Payload{TrustAccountID: "acc1", UserID: "newbie", Change: signatory.PayloadAdd},
			approvers: []string{"owner"},
			canSign:   map[string]bool{"newbie": false},
		},
		{
			name:      "ineligible signatory fails in effect",
			kind:      signatory.KindChange,
			payload:   signatory.Payload{TrustAccountID: "acc1", UserID: "clerk", Change: signatory.PayloadAdd},
			approvers: []string{"owner", "second"},
			wantApply: signatory.ErrNotEligible,
			canSign:   map[string]bool{"clerk": false},
		},
		{
			name:     "unknown kind",
			kind:     "trust.withdrawal",
			payload:  signatory.Payload{TrustAccountID: "acc1", UserID: "newbie", Change: signatory.PayloadAdd},
			wantOpen: gate.ErrUnknownKind,
		},
		{
			name:     "bad change value",
			kind:     signatory.KindChange,
			payload:  signatory.Payload{TrustAccountID: "acc1", UserID: "newbie", Change: "promote"},
			wantOpen: signatory.ErrInvalidPayload,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			eng, svc := setup(t)
			id, err := signatory.OpenChange(ctx, nil, eng, tc.kind, tc.payload, user("clerk"))
			if tc.wantOpen != nil {
				if !errors.Is(err, tc.wantOpen) {
					t.Fatalf("open err = %v, want %v", err, tc.wantOpen)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			g, _ := eng.Get(ctx, id)
			if g.Kind != signatory.KindChange || g.Subject != (gate.Subject{Kind: signatory.SubjectAccount, ID: "acc1"}) {
				t.Fatalf("gate = %+v", g)
			}
			var last error
			for _, a := range tc.approvers {
				last = approve(t, eng, id, a)
			}
			if !errors.Is(last, tc.wantApply) {
				t.Fatalf("apply err = %v, want %v", last, tc.wantApply)
			}
			for u, want := range tc.canSign {
				if got, _ := svc.CanSign(ctx, "acc1", u); got != want {
					t.Fatalf("CanSign(%s) = %v, want %v", u, got, want)
				}
			}
		})
	}
}

func TestAliasEffectNeverApplies(t *testing.T) {
	_, svc := setup(t)
	payload, _ := json.Marshal(signatory.Payload{TrustAccountID: "acc1", UserID: "newbie", Change: signatory.PayloadAdd})
	effects := signatory.Effects(svc)
	cases := []struct {
		name string
		slug string
		kind string
		want error
	}{
		{"alias effect", signatory.KindSigningAuthority, signatory.KindSigningAuthority, signatory.ErrAliasKind},
		{"canonical effect fed alias kind", signatory.KindChange, signatory.KindSigningAuthority, signatory.ErrAliasKind},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := effects[tc.slug](context.Background(), nil, registry.Approved{GateID: "g1", Kind: tc.kind, Payload: payload,
				Preparer: user("clerk"), Approvers: []registry.Principal{user("owner"), user("second")}})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if ok, _ := svc.CanSign(context.Background(), "acc1", "newbie"); ok {
				t.Fatal("alias applied a change")
			}
		})
	}
}
