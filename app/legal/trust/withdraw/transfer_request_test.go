package withdraw_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	"github.com/dagi377/counsel/app/legal/trust/withdraw/withdrawfake"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/flags"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/ledger/ledgerfake"
)

const (
	xAccount = "acct-1"
	xFrom    = "matter-from"
	xTo      = "matter-to"
	xClerk   = "u-clerk"
	xLawyer  = "u-lawyer"
	xOther   = "u-other" // approves trust gates but holds no signing authority
)

var xNow = time.Date(2026, 10, 9, 14, 0, 0, 0, time.UTC)

func xVerified(t *testing.T) context.Context {
	t.Helper()
	s := flags.New()
	if err := s.Define(flags.Def{Name: withdraw.FlagBylaw9Verified}); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(withdraw.FlagBylaw9Verified, true); err != nil {
		t.Fatal(err)
	}
	return flags.WithSet(context.Background(), s)
}

type xSigners map[string]bool

func (s xSigners) IsSignatory(_ context.Context, _ pgx.Tx, _ trust.AccountID, u string) (bool, error) {
	return s[u], nil
}

// xRig is a transfer store, a gate engine whose trust.transfer effect is TransferEffect over
// that store, and the trust book.
type xRig struct {
	store      *withdrawfake.Transfers
	gates      *gatefake.Fake
	gl         *ledgerfake.Fake
	from, to   trust.LedgerID
	requests   withdraw.TransferRequests
	sourceCash int64
}

func newXRig(t *testing.T, held int64) xRig {
	t.Helper()
	r := xRig{store: withdrawfake.NewTransfers(), sourceCash: held}
	r.from = r.store.AddLedger(xAccount, xFrom, held)
	r.to = r.store.AddLedger(xAccount, xTo, 0)
	r.gl = ledgerfake.New(clock.DateOf(xNow, time.UTC))
	r.gates = gatefake.New(func() time.Time { return xNow },
		gatefake.Caps{xLawyer: {"trust.approve"}, xOther: {"trust.approve"}, xClerk: {"trust.approve"}})
	eff := &withdraw.TransferEffect{
		Store:     r.store,
		Transfers: &withdraw.Transfers{Store: r.store, GL: r.gl, Loc: time.UTC},
		Signers:   xSigners{xLawyer: true, xClerk: true},
		Now:       func() time.Time { return xNow },
	}
	if err := r.gates.Register(gate.GateKind{Slug: withdraw.TransferGateKind, Approver: "trust.approve",
		Risk: gate.RiskHigh, MinApprovers: 1, TTL: 72 * time.Hour, Effect: eff.Apply}); err != nil {
		t.Fatalf("register: %v", err)
	}
	r.requests = r.store.Requests(r.gates)
	return r
}

func xRequest(cents int64, preparer gate.Principal) withdraw.TransferRequest {
	return withdraw.TransferRequest{TrustAccountID: xAccount, FromMatterID: xFrom, ToMatterID: xTo,
		AmountCents: cents, Reason: "matter split", PreparedBy: xClerk, Preparer: preparer}
}

func (r xRig) decide(ctx context.Context, t *testing.T, id string, by gate.Principal) error {
	t.Helper()
	g, err := r.gates.Get(ctx, gate.GateID(id))
	if err != nil {
		t.Fatalf("gate: %v", err)
	}
	// The decision runs in one tx with the effect: an effect error rolls the transfer back.
	return r.store.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return r.gates.Decide(ctx, tx, g.ID, g.Version, gate.Decision{Verdict: gate.VerdictApprove,
			PayloadHash: g.PayloadHash, MFAVerifiedAt: xNow.Add(-time.Minute)}, by)
	})
}

var (
	xUser    = func(id string) gate.Principal { return gate.Principal{Kind: gate.PrincipalUser, UserID: id} }
	xAgent   = gate.Principal{Kind: gate.PrincipalAgent, RunID: "run-1", ActingFor: xClerk}
	xLawyerP = xUser(xLawyer)
)

// rule: ARC-I-019
// rule: ARC-I-024
// rule: ARC-I-025
// rule: ARC-I-027
// Request records the transfer pending authorization and opens its gate in one tx. Approval
// by a signatory other than the preparer posts two equal and opposite trust lines; an
// overdraw at authorization, the preparer approving and an agent deciding move nothing.
func TestTransferRequestToPosting(t *testing.T) {
	tests := []struct {
		name string
		// drain is taken out of the source ledger after the request and before the decision.
		drain     int64
		approver  gate.Principal
		wantErr   error
		wantCode  string
		wantHTTP  int
		wantState string
	}{
		{"approval posts both lines", 0, xLawyerP, nil, "", 0, "posted"},
		// rule: ARC-I-025
		{"overdraw at authorization is refused", 30_001, xLawyerP, withdraw.ErrTrustOverdraw, withdraw.CodeInsufficientTrustFunds, 0, "pending_authorization"},
		// rule: ARC-I-019
		{"preparer approves", 0, xUser(xClerk), gate.ErrSelfApproval, "", http.StatusForbidden, "pending_authorization"},
		{"approver without signing authority", 0, xUser(xOther), withdraw.ErrSignerNotAuthorized, withdraw.CodeNotAuthorized, 0, "pending_authorization"},
		// rule: ARC-I-017
		{"agent decision", 0, gate.Principal{Kind: gate.PrincipalAgent, RunID: "run-2", ActingFor: xLawyer}, gate.ErrAgentApprover, "", http.StatusForbidden, "pending_authorization"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := xVerified(t)
			r := newXRig(t, 50_000)
			got, err := r.requests.Request(ctx, xRequest(20_000, xAgent))
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			row, ok := r.store.Row(got.ID)
			if !ok || row.Status != "pending_authorization" || row.GateID != got.GateID || row.RequestedBy != xClerk || row.AmountCents != 20_000 {
				t.Fatalf("row = %+v", row)
			}
			r.store.Adjust(r.from, -tt.drain)
			err = r.decide(ctx, t, got.GateID, tt.approver)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("decide = %v, want %v", err, tt.wantErr)
			}
			if tt.wantCode != "" {
				var ee *withdraw.EffectError
				if !errors.As(err, &ee) || ee.Code != tt.wantCode {
					t.Fatalf("decide = %v, want code %s", err, tt.wantCode)
				}
			}
			if tt.wantHTTP != 0 {
				if p := gate.Problem(err); p == nil || p.Status != tt.wantHTTP {
					t.Fatalf("problem = %+v, want %d", p, tt.wantHTTP)
				}
			}
			if row, _ := r.store.Row(got.ID); row.Status != tt.wantState {
				t.Fatalf("status = %s, want %s", row.Status, tt.wantState)
			}
			legs := r.store.Legs()
			if tt.wantErr != nil {
				if len(legs) != 0 || r.store.Balance(r.from) != 50_000-tt.drain || r.store.Balance(r.to) != 0 {
					t.Fatalf("refused transfer moved money: legs %d, from %d, to %d", len(legs), r.store.Balance(r.from), r.store.Balance(r.to))
				}
				return
			}
			// Two lines, equal and opposite, in integer cents: the account total is unchanged.
			if len(legs) != 2 || legs[0].AmountCents+legs[1].AmountCents != 0 ||
				legs[0].Kind != trust.KindTransferOut || legs[1].Kind != trust.KindTransferIn ||
				legs[0].PostedBy != xLawyer || legs[0].ApprovalID != got.GateID {
				t.Fatalf("legs = %+v", legs)
			}
			if r.store.Balance(r.from) != 30_000 || r.store.Balance(r.to) != 20_000 {
				t.Fatalf("balances = %d, %d; want 30000, 20000", r.store.Balance(r.from), r.store.Balance(r.to))
			}
		})
	}
}

// rule: ARC-I-025
// rule: ARC-I-027
// Request refuses an overdraw, a missing preparer and a gate preparer that is not the
// preparer; nothing is recorded and no gate is opened.
func TestTransferRequestRefuses(t *testing.T) {
	tests := []struct {
		name string
		req  withdraw.TransferRequest
		want error
	}{
		{"overdraw", xRequest(50_001, xAgent), withdraw.ErrTrustOverdraw},
		{"whole balance", xRequest(50_000, xUser(xClerk)), nil},
		{"no preparer", func() withdraw.TransferRequest { r := xRequest(100, xAgent); r.PreparedBy = ""; return r }(), withdraw.ErrInvalid},
		{"gate preparer is someone else", xRequest(100, xUser(xLawyer)), withdraw.ErrInvalid},
		{"no reason", func() withdraw.TransferRequest { r := xRequest(100, xAgent); r.Reason = ""; return r }(), withdraw.ErrInvalid},
		{"same matter", func() withdraw.TransferRequest { r := xRequest(100, xAgent); r.ToMatterID = xFrom; return r }(), withdraw.ErrInvalid},
		{"unknown destination", func() withdraw.TransferRequest { r := xRequest(100, xAgent); r.ToMatterID = "matter-x"; return r }(), withdraw.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newXRig(t, 50_000)
			got, err := r.requests.Request(context.Background(), tt.req)
			if !errors.Is(err, tt.want) {
				t.Fatalf("request = %v, want %v", err, tt.want)
			}
			if tt.want == nil {
				if len(r.store.Rows()) != 1 || got.GateID == "" {
					t.Fatalf("rows = %+v, gate %q", r.store.Rows(), got.GateID)
				}
				return
			}
			if len(r.store.Rows()) != 0 || got.GateID != "" {
				t.Fatalf("refused request recorded %+v", r.store.Rows())
			}
		})
	}
}

// A failed gate open rolls the transfer row back: the row and the gate exist together or
// not at all.
func TestTransferRequestGateFailureRecordsNothing(t *testing.T) {
	store := withdrawfake.NewTransfers()
	store.AddLedger(xAccount, xFrom, 50_000)
	store.AddLedger(xAccount, xTo, 0)
	// No trust.transfer kind registered: Open fails.
	reqs := store.Requests(gatefake.New(func() time.Time { return xNow }, gatefake.Caps{}))
	if _, err := reqs.Request(context.Background(), xRequest(100, xAgent)); !errors.Is(err, gate.ErrUnknownKind) {
		t.Fatalf("request = %v, want unknown kind", err)
	}
	if rows := store.Rows(); len(rows) != 0 {
		t.Fatalf("rows = %+v, want none", rows)
	}
}
