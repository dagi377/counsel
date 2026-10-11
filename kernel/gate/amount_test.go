package gate

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"
)

const billSchema = `{"type":"object","required":["bill_id","total_cents"],"properties":{"bill_id":{"type":"string"},"total_cents":{"type":"integer"}}}`

func billKind() GateKind {
	k := validKind("billing.bill_approval")
	k.PayloadSchema = []byte(billSchema)
	k.AmountPath = "total_cents"
	return k
}

// rule: ARC-I-020
// A money kind must name its amount field; registration fails closed without it.
func TestARC_I_020_KindDeclaresAmountPath(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*GateKind)
		wantErr error
	}{
		{"bill kind with total_cents path", func(*GateKind) {}, nil},
		{"money kind with no path", func(k *GateKind) { k.AmountPath = "" }, ErrInvalidKind},
		{"path not in the schema", func(k *GateKind) { k.AmountPath = "amount_cents" }, ErrInvalidKind},
		{"limit without a path", func(k *GateKind) {
			k.PayloadSchema = []byte(`{"type":"object"}`)
			k.AmountPath, k.LimitKind = "", "payout_max"
		}, ErrInvalidKind},
		{"nested money property", func(k *GateKind) {
			k.PayloadSchema = []byte(`{"type":"object","properties":{"line":{"type":"object","properties":{"fee_cents":{"type":"integer"}}}}}`)
			k.AmountPath = ""
		}, ErrInvalidKind},
		{"nested path declared", func(k *GateKind) {
			k.PayloadSchema = []byte(`{"type":"object","properties":{"line":{"type":"object","properties":{"fee_cents":{"type":"integer"}}}}}`)
			k.AmountPath = "line.fee_cents"
		}, nil},
		{"kind without money needs no path", func(k *GateKind) {
			k.PayloadSchema = []byte(`{"type":"object","properties":{"item_id":{"type":"string"}}}`)
			k.AmountPath = ""
		}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := billKind()
			tt.mutate(&k)
			if err := NewKinds().Register("billing", k); !errors.Is(err, tt.wantErr) {
				t.Fatalf("Register = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// rule: ARC-I-020
// The limit check runs on every Decide: the amount comes from the kind's declared path
// (total_cents for a bill, never a fixed amount_cents), with or without app hooks.
func TestARC_I_020_LimitInputsOnEveryDecide(t *testing.T) {
	capped := func(k GateKind) GateKind { k.LimitKind = "bill_max"; return k }
	tests := []struct {
		name       string
		kind       GateKind
		hooks      DecideHooks
		payload    string
		verdict    Verdict
		wantLimit  string
		wantAmount *int64
		wantErr    error
	}{
		{"bill amount read from total_cents", capped(billKind()), DecideHooks{}, `{"bill_id":"b","total_cents":250000}`, VerdictApprove, "bill_max", i64(250000), nil},
		{"amount_cents is not a bill's amount", capped(billKind()), DecideHooks{}, `{"bill_id":"b","amount_cents":1,"total_cents":250000}`, VerdictApprove, "bill_max", i64(250000), nil},
		{"kind limit used with no hooks installed", capped(billKind()), DecideHooks{}, `{"total_cents":5}`, VerdictApprove, "bill_max", i64(5), nil},
		{"hook limit kind still honoured", billKind(), DecideHooks{LimitKinds: map[string]string{"billing.bill_approval": "hook_max"}}, `{"total_cents":7}`, VerdictApprove, "hook_max", i64(7), nil},
		{"uncapped money kind still reads its amount", billKind(), DecideHooks{}, `{"total_cents":9}`, VerdictApprove, "", i64(9), nil},
		{"reject is never capped", capped(billKind()), DecideHooks{}, `{"total_cents":9}`, VerdictReject, "", nil, nil},
		{"capped kind with no amount fails closed", capped(billKind()), DecideHooks{}, `{"bill_id":"b"}`, VerdictApprove, "", nil, ErrInvalidPayload},
		{"capped kind with no payload fails closed", capped(billKind()), DecideHooks{}, ``, VerdictApprove, "", nil, ErrInvalidPayload},
		{"fractional amount fails closed", capped(billKind()), DecideHooks{}, `{"total_cents":1.5}`, VerdictApprove, "", nil, ErrInvalidPayload},
		{"money kind with no path fails closed", func() GateKind { k := billKind(); k.AmountPath = ""; return k }(), DecideHooks{}, `{"total_cents":9}`, VerdictApprove, "", nil, ErrInvalidKind},
		{"no path fails closed even on reject", func() GateKind { k := billKind(); k.AmountPath = ""; return k }(), DecideHooks{}, `{"total_cents":9}`, VerdictReject, "", nil, ErrInvalidKind},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p json.RawMessage
			if tt.payload != "" {
				p = json.RawMessage(tt.payload)
			}
			lk, amount, err := limitInputs(tt.kind, tt.hooks, p, tt.verdict)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if lk != tt.wantLimit {
				t.Fatalf("limit kind = %q, want %q", lk, tt.wantLimit)
			}
			if (amount == nil) != (tt.wantAmount == nil) || (amount != nil && *amount != *tt.wantAmount) {
				t.Fatalf("amount = %v, want %v", amount, tt.wantAmount)
			}
		})
	}
}

// rule: ARC-I-020
// With the amount read from total_cents, a bill above the decider's limit only recommends.
func TestARC_I_020_BillAboveLimitRecommends(t *testing.T) {
	k := billKind()
	k.LimitKind = "bill_max"
	_, amount, err := limitInputs(k, DecideHooks{}, json.RawMessage(`{"total_cents":250000}`), VerdictApprove)
	if err != nil {
		t.Fatal(err)
	}
	in := baseDecide()
	in.kind, in.amount, in.limit = k, amount, i64(100000)
	out, err := checkDecision(context.Background(), in)
	if err != nil || !out.recommend {
		t.Fatalf("checkDecision = %+v, %v; want a recommendation", out, err)
	}
}

// rule: ARC-I-016
// The kind's editor lookup feeds the preparer set on every Decide, with no app hooks; an
// editor who approves is refused.
func TestARC_I_016_KindEditorsArePreparers(t *testing.T) {
	editors := func(_ context.Context, _ pgx.Tx, s Subject, p json.RawMessage) ([]string, error) {
		if s.ID != "bill-1" || string(p) != `{"total_cents":1}` {
			return nil, errors.New("wrong subject or payload")
		}
		return []string{"L"}, nil
	}
	failing := func(context.Context, pgx.Tx, Subject, json.RawMessage) ([]string, error) {
		return nil, errors.New("db down")
	}
	hook := func(context.Context, pgx.Tx, Gate) ([]string, error) { return []string{"H"}, nil }
	tests := []struct {
		name    string
		kindFn  PreparersFunc
		hooks   DecideHooks
		decider string
		want    []string
		wantSoD bool
		wantErr bool
	}{
		{"editor L approves", editors, DecideHooks{}, "L", []string{"L"}, true, false},
		{"other decider M approves", editors, DecideHooks{}, "M", []string{"L"}, false, false},
		{"kind and hook editors both count", editors, DecideHooks{Preparers: hook}, "H", []string{"L", "H"}, true, false},
		{"lookup error fails closed", failing, DecideHooks{}, "M", nil, false, true},
		{"no lookup adds nobody", nil, DecideHooks{}, "M", nil, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := billKind()
			k.Preparers = tt.kindFn
			g := Gate{ID: "g1", Subject: Subject{Kind: "billing.bill", ID: "bill-1"}, Payload: json.RawMessage(`{"total_cents":1}`)}
			got, err := itemPreparers(context.Background(), nil, g, k, tt.hooks)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, want error %v", err, tt.wantErr)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("preparers = %v, want %v", got, tt.want)
			}
			in := baseDecide()
			in.preparers = append([]string{"B"}, got...)
			in.by.UserID = tt.decider
			_, err = checkDecision(context.Background(), in)
			if errors.Is(err, ErrSelfApproval) != tt.wantSoD {
				t.Fatalf("checkDecision = %v, want sod %v", err, tt.wantSoD)
			}
		})
	}
}
