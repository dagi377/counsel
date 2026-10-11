package payment_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/billing/payment"
	"github.com/dagi377/counsel/app/legal/billing/payment/paymentfake"
	"github.com/dagi377/counsel/kernel/inbound/inboundfake"
)

// counting wraps a Service and records the distinct payment ids Capture returns.
type counting struct {
	payment.Service
	mu  sync.Mutex
	ids map[string]bool
}

func (c *counting) Capture(ctx context.Context, cp payment.Capture) (payment.Payment, []payment.Allocation, error) {
	p, as, err := c.Service.Capture(ctx, cp)
	if err == nil {
		c.mu.Lock()
		c.ids[p.ID] = true
		c.mu.Unlock()
	}
	return p, as, err
}

type body struct {
	Client, Bill, EventID string
	Cents                 int64
}

func testWebhook() payment.Webhook {
	return payment.Webhook{
		ID: "payments.example",
		Verify: func(r *http.Request, _ []byte) (string, error) {
			return r.Header.Get("Webhook-Id"), nil
		},
		Decode: func(b []byte) (payment.Capture, error) {
			var v body
			if err := json.Unmarshal(b, &v); err != nil {
				return payment.Capture{}, err
			}
			// A forged body event id must not change the dedup key.
			return payment.Capture{ClientID: v.Client, BillID: v.Bill, Kind: payment.KindBillPayment,
				AmountCents: v.Cents, ProviderEventID: v.EventID, RecordedBy: "webhook"}, nil
		},
	}
}

func deliver(t *testing.T, ctx context.Context, rx *inboundfake.Fake, eventID string, b body) bool {
	t.Helper()
	raw, _ := json.Marshal(b)
	r := httptest.NewRequest(http.MethodPost, "/hooks/payments.example", strings.NewReader(string(raw)))
	r.Header.Set("Webhook-Id", eventID)
	res, err := rx.Receive(ctx, "payments.example", r, raw)
	if err != nil {
		t.Fatalf("receive %s: %v", eventID, err)
	}
	return res.Duplicate
}

// rule: BIL-I-020
func TestBIL_I_020_DuplicateWebhookOnePayment(t *testing.T) {
	tests := []struct {
		name     string
		events   []string // delivered event ids, in order
		bodyIDs  []string // event id the body claims, per delivery
		payments int
		dups     int
	}{
		{"single delivery", []string{"ev1"}, []string{"ev1"}, 1, 0},
		{"same event twice", []string{"ev1", "ev1"}, []string{"ev1", "ev1"}, 1, 1},
		{"same event three times", []string{"ev1", "ev1", "ev1"}, []string{"ev1", "ev1", "ev1"}, 1, 2},
		{"two events", []string{"ev1", "ev2"}, []string{"ev1", "ev2"}, 2, 0},
		{"body claims another event id", []string{"ev1", "ev2"}, []string{"evX", "evX"}, 2, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			pf := paymentfake.New()
			pf.AddBill("b1", "c1", 100000, 13000)
			svc := &counting{Service: pf, ids: map[string]bool{}}
			p, err := payment.NewProvider(testWebhook(), svc)
			if err != nil {
				t.Fatal(err)
			}
			rx := inboundfake.New(func() time.Time { return time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC) })
			if err := rx.Register(p); err != nil {
				t.Fatal(err)
			}
			dups := 0
			for i, ev := range tc.events {
				if deliver(t, ctx, rx, ev, body{Client: "c1", Bill: "b1", EventID: tc.bodyIDs[i], Cents: 5000}) {
					dups++
				}
			}
			if len(svc.ids) != tc.payments || dups != tc.dups {
				t.Fatalf("payments=%d dups=%d, want %d and %d", len(svc.ids), dups, tc.payments, tc.dups)
			}
		})
	}
}

// rule: BIL-I-020
func TestBIL_I_020_RedeliveryPastReceiverReturnsFirstPayment(t *testing.T) {
	ctx := context.Background()
	pf := paymentfake.New()
	pf.AddBill("b1", "c1", 100000, 13000)
	svc := &counting{Service: pf, ids: map[string]bool{}}
	p, err := payment.NewProvider(testWebhook(), svc)
	if err != nil {
		t.Fatal(err)
	}
	// Two receivers model a lost dedup row: the second delivery reaches Handle again.
	for _, rx := range []*inboundfake.Fake{inboundfake.New(nil), inboundfake.New(nil)} {
		if err := rx.Register(p); err != nil {
			t.Fatal(err)
		}
		deliver(t, ctx, rx, "ev1", body{Client: "c1", Bill: "b1", Cents: 5000})
	}
	if len(svc.ids) != 1 {
		t.Fatalf("payments=%d, want 1", len(svc.ids))
	}
}

func TestNewProviderRefusesIncompleteWebhook(t *testing.T) {
	full := testWebhook()
	tests := []struct {
		name string
		w    payment.Webhook
		svc  payment.Service
	}{
		{"no id", payment.Webhook{Verify: full.Verify, Decode: full.Decode}, paymentfake.New()},
		{"no verify", payment.Webhook{ID: "x", Decode: full.Decode}, paymentfake.New()},
		{"no decode", payment.Webhook{ID: "x", Verify: full.Verify}, paymentfake.New()},
		{"no service", full, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := payment.NewProvider(tc.w, tc.svc); !errors.Is(err, payment.ErrNoDecoder) {
				t.Fatalf("err=%v, want ErrNoDecoder", err)
			}
		})
	}
}
