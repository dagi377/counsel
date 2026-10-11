package payment_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	legal "github.com/dagi377/counsel/app/legal"
	"github.com/dagi377/counsel/app/legal/billing/payment"
	"github.com/dagi377/counsel/app/legal/billing/payment/paymentfake"
	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/idem/idemfake"
	"github.com/dagi377/counsel/kernel/server"
)

const (
	gateSchema   = "#/components/schemas/Gate"
	trustLedgerA = "0190c000-0000-7000-8000-000000000001"
)

type txStub struct{}

func (txStub) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return fn(ctx, nil)
}

// stubGates records Open calls and never runs an effect.
type stubGates struct {
	mu    sync.Mutex
	gates map[gate.GateID]gate.Gate
	opens []string
}

func (g *stubGates) Open(_ context.Context, _ pgx.Tx, kind string, s gate.Subject, payload json.RawMessage, p gate.Principal) (gate.GateID, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.opens = append(g.opens, kind)
	id := gate.GateID(fmt.Sprintf("0190d000-0000-7000-8000-%012d", len(g.opens)))
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
	return g.gates[id], nil
}

// stubProposals is the effect-free Proposals port: it records calls and splits each bill row
// into fee only.
type stubProposals struct {
	calls int
	err   error
}

func (s *stubProposals) Propose(_ context.Context, _ pgx.Tx, _ string, _ int, rows []payment.ProposedAllocation, _ string) ([]payment.Allocation, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	var out []payment.Allocation
	for _, r := range rows {
		out = append(out, payment.Allocation{BillID: r.TargetID, FeeCents: r.AmountCents})
	}
	return out, nil
}

type gatedRig struct {
	h     http.Handler
	svc   *countingPayments
	gates *stubGates
	props *stubProposals
	pay   string // a seeded payment of 1000 cents
}

func newGatedRig(t *testing.T) gatedRig {
	t.Helper()
	fake := paymentfake.New()
	n := 0
	fake.NewID = func() string { n++; return fmt.Sprintf("0190f000-0000-7000-8000-%012d", n) }
	fake.AddBill(billA, clientA, 10000, 1300)
	fake.AddBill(billB, clientA, 5000, 650)
	codec, err := api.NewCodec(bytes.Repeat([]byte("k"), api.MinCursorKeyLen))
	if err != nil {
		t.Fatal(err)
	}
	svc := &countingPayments{Fake: fake}
	p, _, err := fake.Capture(context.Background(), payment.Capture{ClientID: clientA, BillID: billA,
		Kind: payment.KindBillPayment, AmountCents: 1000, Method: "eft", IdempotencyKey: "seed"})
	if err != nil {
		t.Fatal(err)
	}
	gates := &stubGates{gates: map[gate.GateID]gate.Gate{}}
	props := &stubProposals{}
	h := &payment.Gated{
		HTTP: payment.HTTP{Payments: svc, Access: testAccess{}, Cursors: codec, Idem: idemfake.New(),
			Problems: &server.Problems{}, Principal: testPrincipal},
		DB: txStub{}, Gates: gates, Proposals: props,
	}
	m := server.New(nil)
	if err := m.Mount(h.Routes); err != nil {
		t.Fatal(err)
	}
	return gatedRig{h: m.Handler(), svc: svc, gates: gates, props: props, pay: p.ID}
}

func (g gatedRig) calls() int { return g.svc.calls + len(g.gates.opens) + g.props.calls }

func allocPath(id string) string  { return "/v1/payments/" + id + "/allocations" }
func refundPath(id string) string { return "/v1/payments/" + id + "/refunds" }

func hdrs(key, ifMatch string) map[string]string {
	h := map[string]string{}
	if key != "" {
		h[idem.HeaderName] = key
	}
	if ifMatch != "" {
		h["If-Match"] = ifMatch
	}
	return h
}

const (
	billSet   = `{"allocations":[{"target_type":"bill","target_id":"` + billA + `","amount_cents":600},{"target_type":"bill","target_id":"` + billB + `","amount_cents":400}]}`
	trustSet  = `{"allocations":[{"target_type":"bill","target_id":"` + billA + `","amount_cents":600},{"target_type":"trust_ledger","target_id":"` + trustLedgerA + `","amount_cents":400}]}`
	creditSet = `{"allocations":[{"target_type":"unapplied","amount_cents":1000}]}`
	shortSet  = `{"allocations":[{"target_type":"bill","target_id":"` + billA + `","amount_cents":999}]}`
	refund300 = `{"amount_cents":300,"reason_code":"client_request"}`
)

// rule: ARC-I-051
func TestGatedPaymentRoutesProblems(t *testing.T) {
	tests := []struct {
		name      string
		path      func(pay string) string
		user      string
		body      string
		hdr       map[string]string
		status    int
		code      string
		zeroCalls bool
	}{
		{"allocations without principal", allocPath, "", billSet, hdrs(key1, `"1"`), 401, payment.CodeUnauthenticated, true},
		{"refunds without principal", refundPath, "", refund300, hdrs(key1, ""), 401, payment.CodeUnauthenticated, true},
		{"allocations without billing.payments", allocPath, "viewer", billSet, hdrs(key1, `"1"`), 403, payment.CodeMissingCapability, true},
		{"refunds without billing.payments", refundPath, "viewer", refund300, hdrs(key1, ""), 403, payment.CodeMissingCapability, true},
		// rule: ARC-I-042
		{"hidden payment allocations are 404", func(string) string { return allocPath(payHidden) }, "clerk", billSet, hdrs(key1, `"1"`), 404, payment.CodeNotFound, true},
		{"hidden payment refunds are 404", func(string) string { return refundPath(payHidden) }, "clerk", refund300, hdrs(key1, ""), 404, payment.CodeNotFound, true},
		{"unknown payment", func(string) string { return refundPath(payMissing) }, "clerk", refund300, hdrs(key1, ""), 404, payment.CodeNotFound, false},
		{"malformed payment id", func(string) string { return refundPath("nope") }, "clerk", refund300, hdrs(key1, ""), 400, server.CodeValidationFailed, true},
		{"unknown body field", refundPath, "clerk", `{"amount_cents":300,"reason_code":"x","extra":1}`, hdrs(key1, ""), 400, server.CodeValidationFailed, true},
		{"unknown allocation field", allocPath, "clerk", `{"allocations":[{"target_type":"unapplied","amount_cents":1000,"fee_cents":1}]}`, hdrs(key1, `"1"`), 400, server.CodeValidationFailed, true},
		// rule: ARC-I-007
		{"allocations without Idempotency-Key", allocPath, "clerk", billSet, hdrs("", `"1"`), 400, idem.CodeKeyRequired, true},
		{"refunds without Idempotency-Key", refundPath, "clerk", refund300, hdrs("", ""), 400, idem.CodeKeyRequired, true},
		// rule: ARC-I-006
		{"allocations without If-Match", allocPath, "clerk", billSet, hdrs(key1, ""), 428, server.CodePreconditionRequired, true},
		{"allocations with a stale version", allocPath, "clerk", billSet, hdrs(key1, `"7"`), 412, server.CodeVersionConflict, false},
		// rule: ARC-I-029
		{"allocations not summing to the payment", allocPath, "clerk", shortSet, hdrs(key1, `"1"`), 422, payment.CodeAllocationSumMismatch, false},
		{"refund above the payment", refundPath, "clerk", `{"amount_cents":1001,"reason_code":"x"}`, hdrs(key1, ""), 409, payment.CodeRefundExceedsPayment, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := newGatedRig(t)
			checkProblem(t, do(g.h, http.MethodPost, tc.path(g.pay), tc.user, tc.body, tc.hdr), tc.status, tc.code)
			if tc.zeroCalls && g.calls() != 0 {
				t.Errorf("service and gate calls = %d, want 0", g.calls())
			}
			if len(g.gates.opens) != 0 || g.props.calls != 0 {
				t.Errorf("opens = %v, proposals = %d, want none", g.gates.opens, g.props.calls)
			}
		})
	}
}

// rule: ARC-I-012
func TestGatedPaymentRoutesOpenGates(t *testing.T) {
	tests := []struct {
		name     string
		path     func(pay string) string
		body     string
		ifMatch  string
		status   int
		wantKind string // gate opened, "" for none
	}{
		{"trust-touching allocation opens the gate", allocPath, trustSet, `"1"`, 202, payment.KindAllocation},
		{"unapplied money opens the gate", allocPath, creditSet, `"1"`, 202, payment.KindAllocation},
		{"bill-only allocation is proposed directly", allocPath, billSet, `"1"`, 200, ""},
		{"refund opens the gate", refundPath, refund300, "", 202, payment.KindRefund},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := newGatedRig(t)
			rec := do(g.h, http.MethodPost, tc.path(g.pay), "clerk", tc.body, hdrs(key1, tc.ifMatch))
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.status, rec.Body)
			}
			if tc.wantKind == "" {
				validate(t, paymentSchema, rec.Body.Bytes())
				var v struct {
					Allocations []struct{ State string } `json:"allocations"`
				}
				_ = json.Unmarshal(rec.Body.Bytes(), &v)
				for _, a := range v.Allocations {
					if a.State != "proposed" {
						t.Errorf("allocation state = %q, want proposed", a.State)
					}
				}
				if len(v.Allocations) != 2 || len(g.gates.opens) != 0 || g.props.calls != 1 {
					t.Errorf("allocations = %d, opens = %v, proposals = %d", len(v.Allocations), g.gates.opens, g.props.calls)
				}
				return
			}
			validate(t, gateSchema, rec.Body.Bytes())
			var body struct {
				Kind    string
				Payload map[string]any
			}
			_ = json.Unmarshal(rec.Body.Bytes(), &body)
			if body.Kind != tc.wantKind || len(g.gates.opens) != 1 || g.gates.opens[0] != tc.wantKind {
				t.Errorf("kind = %q, opens = %v, want %s once", body.Kind, g.gates.opens, tc.wantKind)
			}
			if body.Payload["payment_id"] != g.pay {
				t.Errorf("payload = %v, want payment_id %s", body.Payload, g.pay)
			}
			// The effect-owned work never runs from the route.
			if g.props.calls != 0 {
				t.Errorf("proposals = %d, want 0", g.props.calls)
			}
		})
	}
}

func TestGatedPaymentRoutesDeclareNoGateOwnedState(t *testing.T) {
	g := newGatedRig(t)
	writes, err := server.DeclaredWrites(g.h)
	if err != nil {
		t.Fatal(err)
	}
	if len(writes) != 2 {
		t.Fatalf("writes = %+v, want two routes", writes)
	}
	for _, w := range writes {
		for _, k := range w.Sets {
			if kind, owned := legal.GateOwned[k]; owned {
				t.Errorf("%s %s declares %s, owned by %s", w.Method, w.Pattern, k, kind)
			}
		}
	}
}

// rule: ARC-I-008
func TestGatedPaymentRoutesIdempotency(t *testing.T) {
	tests := []struct {
		name          string
		path          func(pay string) string
		first, second string
		ifMatch       string
		wantStatus    int
		wantCode      string // problem code of the second call, "" for a replay
	}{
		{"refund replay", refundPath, refund300, refund300, "", 202, ""},
		{"allocation replay", allocPath, trustSet, trustSet, `"1"`, 202, ""},
		{"refund key reused with another body", refundPath, refund300, `{"amount_cents":200,"reason_code":"client_request"}`, "", 422, idem.CodeKeyReused},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := newGatedRig(t)
			first := do(g.h, http.MethodPost, tc.path(g.pay), "clerk", tc.first, hdrs(key1, tc.ifMatch))
			if first.Code != http.StatusAccepted {
				t.Fatalf("first status = %d (%s)", first.Code, first.Body)
			}
			second := do(g.h, http.MethodPost, tc.path(g.pay), "clerk", tc.second, hdrs(key1, tc.ifMatch))
			if tc.wantCode != "" {
				checkProblem(t, second, tc.wantStatus, tc.wantCode)
			} else if second.Code != first.Code || !bytes.Equal(second.Body.Bytes(), first.Body.Bytes()) {
				t.Errorf("replay = %d %s, want %d %s", second.Code, second.Body, first.Code, first.Body)
			}
			if len(g.gates.opens) != 1 {
				t.Errorf("gate opens = %d, want 1", len(g.gates.opens))
			}
		})
	}
}

func TestGatedPaymentRoutesUnconfiguredFailClosed(t *testing.T) {
	m := server.New(nil)
	if err := m.Mount((&payment.Gated{}).Routes); err != nil {
		t.Fatal(err)
	}
	checkProblem(t, do(m.Handler(), http.MethodPost, refundPath(payMissing), "clerk", refund300, hdrs(key1, "")), 503, payment.CodeUnavailable)
}
