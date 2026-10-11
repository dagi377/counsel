package conflict_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal"
	"github.com/dagi377/counsel/app/legal/conflict"
	"github.com/dagi377/counsel/app/legal/conflict/conflictfake"
	"github.com/dagi377/counsel/kernel/flags"
)

func flagCtx(t *testing.T, on bool) context.Context {
	t.Helper()
	s := flags.New()
	if err := legal.DefineFlags(s); err != nil {
		t.Fatal(err)
	}
	if on {
		if err := s.Set(legal.FlagIDVerification, true); err != nil {
			t.Fatal(err)
		}
	}
	return flags.WithSet(context.Background(), s)
}

// rule: BIL-L-043 (verify)
func TestBIL_L_043_IDVerificationBeforeTrustMovement(t *testing.T) {
	v := conflictfake.New()
	if err := v.RecordVerification(context.Background(), conflict.IDVerification{
		ClientID: "c-ok", Method: "government_photo_id", VerifiedBy: "u-law", VerifiedAt: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		flagOn    bool
		funds     conflict.Funds
		wantWarn  bool
		wantBlock bool
		wantErr   error
	}{
		{"flag off blocks unverified payout", false, conflict.Funds{ClientID: "c-new", Kind: conflict.FundsThirdPartyPayout}, false, true, conflict.ErrUnverified},
		{"flag off blocks verified payout", false, conflict.Funds{ClientID: "c-ok", Kind: conflict.FundsThirdPartyPayout}, false, true, conflict.ErrUnverified},
		{"flag off blocks receipt", false, conflict.Funds{ClientID: "c-new", Kind: conflict.FundsReceipt}, false, true, conflict.ErrUnverified},
		{"flag off blocks exempt fee transfer", false, conflict.Funds{ClientID: "c-new", Kind: conflict.FundsFeeTransfer}, false, true, conflict.ErrUnverified},
		{"flag off blocks exempt payout", false, conflict.Funds{ClientID: "c-new", Kind: conflict.FundsThirdPartyPayout, Exemption: conflict.ExemptBail}, false, true, conflict.ErrUnverified},
		{"unverified receipt is flagged", true, conflict.Funds{ClientID: "c-new", Kind: conflict.FundsReceipt}, true, false, nil},
		{"unverified payout to vendor blocked", true, conflict.Funds{ClientID: "c-new", Kind: conflict.FundsThirdPartyPayout}, false, true, conflict.ErrIDNotVerified},
		{"fee transfer to operating exempt", true, conflict.Funds{ClientID: "c-new", Kind: conflict.FundsFeeTransfer}, false, false, nil},
		{"exempt payout allowed", true, conflict.Funds{ClientID: "c-new", Kind: conflict.FundsThirdPartyPayout, Exemption: conflict.ExemptBail}, false, false, nil},
		{"receipt from financial institution exempt", true, conflict.Funds{ClientID: "c-new", Kind: conflict.FundsReceipt, Exemption: conflict.ExemptFinancialInstitution}, false, false, nil},
		{"verified payout allowed", true, conflict.Funds{ClientID: "c-ok", Kind: conflict.FundsThirdPartyPayout}, false, false, nil},
		{"verified receipt not flagged", true, conflict.Funds{ClientID: "c-ok", Kind: conflict.FundsReceipt}, false, false, nil},
		{"unknown kind", true, conflict.Funds{ClientID: "c-ok", Kind: "wire"}, false, false, conflict.ErrUnknownFunds},
		{"unknown exemption", false, conflict.Funds{ClientID: "c-ok", Kind: conflict.FundsReceipt, Exemption: "friend"}, false, false, conflict.ErrUnknownFunds},
		{"no client", true, conflict.Funds{Kind: conflict.FundsReceipt}, false, false, conflict.ErrUnknownFunds},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, err := conflict.CheckFunds(flagCtx(t, tc.flagOn), v, tc.funds)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantBlock && !strings.Contains(err.Error(), conflict.CodeIDVerificationRequired) {
				t.Fatalf("err = %v, want code %s", err, conflict.CodeIDVerificationRequired)
			}
			if d.Warn != tc.wantWarn {
				t.Fatalf("warn = %v, want %v", d.Warn, tc.wantWarn)
			}
			if (tc.wantWarn || tc.wantBlock) && d.Rule != "BIL-L-043" {
				t.Fatalf("rule = %q, want BIL-L-043", d.Rule)
			}
		})
	}
}

// rule: BIL-L-043 (verify)
func TestBIL_L_043_FlagOffByDefault(t *testing.T) {
	s := flags.New()
	if err := legal.DefineFlags(s); err != nil {
		t.Fatal(err)
	}
	if s.Enabled(legal.FlagIDVerification) {
		t.Fatal("conflict.id_verification is on by default")
	}
	d, err := conflict.CheckFunds(context.Background(), conflictfake.New(), conflict.Funds{ClientID: "c", Kind: conflict.FundsThirdPartyPayout})
	if !errors.Is(err, conflict.ErrUnverified) || d.Rule != "BIL-L-043" {
		t.Fatalf("no flag set: got %+v, %v; want ErrUnverified", d, err)
	}
}

// rule: BIL-L-043 (verify)
func TestBIL_L_043_FlagOffDoesNotAllowPayout(t *testing.T) {
	for _, kind := range []conflict.FundsKind{conflict.FundsReceipt, conflict.FundsThirdPartyPayout, conflict.FundsFeeTransfer} {
		t.Run(string(kind), func(t *testing.T) {
			_, err := conflict.CheckFunds(flagCtx(t, false), conflictfake.New(), conflict.Funds{ClientID: "c", Kind: kind})
			if err == nil || !errors.Is(err, conflict.ErrUnverified) {
				t.Fatalf("err = %v, want ErrUnverified", err)
			}
		})
	}
}
