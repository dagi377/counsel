package gate

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"
)

var decideNow = time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)

func i64(v int64) *int64 { return &v }

// baseDecide is a valid approval by human M on a pending high-risk gate prepared by B.
func baseDecide() decideIn {
	return decideIn{
		gate: Gate{
			ID: "g1", Kind: "desk.refund", Version: 1, Status: StatusPending,
			PayloadHash: "sha256:aa", ExpiresAt: decideNow.Add(time.Hour),
		},
		kind:      GateKind{Slug: "desk.refund", Risk: RiskHigh, MinApprovers: 1},
		preparers: []string{"B"},
		version:   1,
		d: Decision{
			Verdict: VerdictApprove, PayloadHash: "sha256:aa",
			MFAVerifiedAt: decideNow.Add(-time.Minute),
		},
		by:  Principal{Kind: PrincipalUser, UserID: "M"},
		now: decideNow,
	}
}

func allowSelf(context.Context, Gate, Principal) (bool, string) { return true, "sole" }

func TestARC_I_016_SeparationOfDuties(t *testing.T) {
	// rule: ARC-I-016
	tests := []struct {
		name     string
		mod      func(*decideIn)
		wantErr  error
		wantSelf bool
	}{
		{"other human approves", func(*decideIn) {}, nil, false},
		{"creator approves", func(in *decideIn) { in.by.UserID = "B" }, ErrSelfApproval, false},
		{"editor of a later version approves", func(in *decideIn) {
			in.preparers = []string{"B", "L"}
			in.by.UserID = "L"
		}, ErrSelfApproval, false},
		{"agent acting-for user approves", func(in *decideIn) {
			in.preparers = []string{"U"} // Open stores an agent's acting-for user as requester
			in.by.UserID = "U"
		}, ErrSelfApproval, false},
		{"preparer rejects own gate", func(in *decideIn) {
			in.by.UserID = "B"
			in.d.Verdict, in.d.Reason = VerdictReject, "no"
		}, ErrSelfApproval, false},
		{"policy denies", func(in *decideIn) {
			in.by.UserID = "B"
			in.policy = func(context.Context, Gate, Principal) (bool, string) { return false, "" }
			in.d.SelfApprovalReason = "only one"
		}, ErrSelfApproval, false},
		{"policy allows but no declared reason", func(in *decideIn) {
			in.by.UserID = "B"
			in.policy = allowSelf
		}, ErrSelfApproval, false},
		{"policy allows with reason", func(in *decideIn) {
			in.by.UserID = "B"
			in.policy = allowSelf
			in.d.SelfApprovalReason = "only one"
		}, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := baseDecide()
			tt.mod(&in)
			out, err := checkDecision(context.Background(), in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if out.selfApproved != tt.wantSelf {
				t.Fatalf("selfApproved = %v, want %v", out.selfApproved, tt.wantSelf)
			}
		})
	}
}

func TestARC_I_017_HumanFreshMFA(t *testing.T) {
	// rule: ARC-I-017
	tests := []struct {
		name    string
		mod     func(*decideIn)
		wantErr error
	}{
		{"agent principal", func(in *decideIn) {
			in.by = Principal{Kind: PrincipalAgent, RunID: "r1", ActingFor: "M"}
		}, ErrAgentApprover},
		{"agent with self-approval policy", func(in *decideIn) {
			in.by = Principal{Kind: PrincipalAgent, RunID: "r1", ActingFor: "B"}
			in.policy = allowSelf
			in.d.SelfApprovalReason = "x"
		}, ErrAgentApprover},
		{"guest", func(in *decideIn) { in.by = Principal{Kind: PrincipalGuest, UserID: "G"} }, ErrNotAuthorized},
		{"user without id", func(in *decideIn) { in.by.UserID = "" }, ErrNotAuthorized},
		{"no mfa", func(in *decideIn) { in.d.MFAVerifiedAt = time.Time{} }, ErrMFARequired},
		{"mfa in the future", func(in *decideIn) { in.d.MFAVerifiedAt = decideNow.Add(time.Minute) }, ErrMFARequired},
		{"high, mfa 15 min old", func(in *decideIn) { in.d.MFAVerifiedAt = decideNow.Add(-15 * time.Minute) }, nil},
		{"high, mfa 20 min old", func(in *decideIn) { in.d.MFAVerifiedAt = decideNow.Add(-20 * time.Minute) }, ErrMFARequired},
		{"irreversible, mfa 20 min old", func(in *decideIn) {
			in.kind.Risk = RiskIrreversible
			in.d.MFAVerifiedAt = decideNow.Add(-20 * time.Minute)
		}, ErrMFARequired},
		{"medium, mfa 20 min old", func(in *decideIn) {
			in.kind.Risk = RiskMedium
			in.d.MFAVerifiedAt = decideNow.Add(-20 * time.Minute)
		}, nil},
		{"stale version", func(in *decideIn) { in.version = 2 }, ErrStaleVersion},
		{"payload changed", func(in *decideIn) { in.d.PayloadHash = "sha256:bb" }, ErrPayloadChanged},
		{"not pending", func(in *decideIn) { in.gate.Status = StatusSuperseded }, ErrNotPending},
		{"expired", func(in *decideIn) { in.gate.ExpiresAt = decideNow }, ErrExpired},
		{"reject without reason", func(in *decideIn) { in.d.Verdict = VerdictReject }, ErrReasonRequired},
		{"changes requested with reason", func(in *decideIn) {
			in.d.Verdict, in.d.Reason = VerdictChangesRequested, "fix line 2"
		}, nil},
		{"unknown verdict", func(in *decideIn) { in.d.Verdict = "recommend" }, ErrInvalidPayload},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := baseDecide()
			tt.mod(&in)
			_, err := checkDecision(context.Background(), in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestARC_I_020_AuthorityLimitRecommends(t *testing.T) {
	// rule: ARC-I-020
	tests := []struct {
		name          string
		amount, limit *int64
		verdict       Verdict
		wantRecommend bool
	}{
		{"associate limit below amount", i64(250000), i64(100000), VerdictApprove, true},
		{"one cent over", i64(100001), i64(100000), VerdictApprove, true},
		{"exactly at limit", i64(100000), i64(100000), VerdictApprove, false},
		{"no limit", i64(250000), nil, VerdictApprove, false},
		{"no amount", nil, i64(100), VerdictApprove, false},
		{"zero limit", i64(1), i64(0), VerdictApprove, true},
		{"reject is never a recommendation", i64(250000), i64(100000), VerdictReject, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := baseDecide()
			in.amount, in.limit = tt.amount, tt.limit
			in.d.Verdict = tt.verdict
			if tt.verdict != VerdictApprove {
				in.d.Reason = "no"
			}
			out, err := checkDecision(context.Background(), in)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if out.recommend != tt.wantRecommend {
				t.Fatalf("recommend = %v, want %v", out.recommend, tt.wantRecommend)
			}
		})
	}
}

func TestARC_I_020_PayloadAmountCents(t *testing.T) {
	// rule: ARC-I-020
	tests := []struct {
		name    string
		payload string
		want    *int64
		wantErr error
	}{
		{"integer cents", `{"amount_cents":250000}`, i64(250000), nil},
		{"large integer exact", `{"amount_cents":9007199254740993}`, i64(9007199254740993), nil},
		{"absent", `{"other":1}`, nil, nil},
		{"null", `{"amount_cents":null}`, nil, nil},
		{"fraction", `{"amount_cents":10.5}`, nil, ErrInvalidPayload},
		{"string", `{"amount_cents":"100"}`, nil, ErrInvalidPayload},
		{"not an object", `[1]`, nil, ErrInvalidPayload},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := payloadAmount(json.RawMessage(tt.payload))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if (got == nil) != (tt.want == nil) || (got != nil && *got != *tt.want) {
				t.Fatalf("amount = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestARC_I_024_DecideProblems(t *testing.T) {
	// rule: ARC-I-016
	// rule: ARC-I-017
	// rule: ARC-I-024
	tests := []struct {
		err    error
		status int
		code   string
	}{
		{ErrSelfApproval, http.StatusForbidden, "sod_violation"},
		{ErrAgentApprover, http.StatusForbidden, "agent_cannot_decide"},
		{ErrMFARequired, http.StatusUnauthorized, "step_up_required"},
		{ErrEffectFailed, http.StatusConflict, "effect_failed"},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			p := Problem(errors.Join(errors.New("ctx"), tt.err))
			if p == nil || p.Status != tt.status || p.Code != tt.code {
				t.Fatalf("Problem(%v) = %+v, want %d %s", tt.err, p, tt.status, tt.code)
			}
		})
	}
}
