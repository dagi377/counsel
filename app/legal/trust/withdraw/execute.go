package withdraw

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/legalflags"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// ExecutionPath is the one route that records a withdrawal's execution. No other route moves
// a withdrawal to executed.
const ExecutionPath = "/trust/withdrawals/{withdrawal_id}/execution"

// CodeUnverified is the problem code while FlagBylaw9Verified is off.
const CodeUnverified = "trust_rules_unverified"

// CodeTrustOverdraw is the problem code for an amount above the available balance.
const CodeTrustOverdraw = "trust_overdraw"

// TxRunner runs fn in one transaction; kernel/db's pool implements it.
type TxRunner interface {
	InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error
}

// BankMatch pairs a posted withdrawal with the bank's confirmation of it.
type BankMatch struct {
	WithdrawalID string
	TxnID        trust.TxnID
	ExecutedOn   time.Time
	BankRef      string
	InstrumentNo string
}

// BankMatcher records which bank line confirms a posted trust transaction. It only matches;
// it never posts or changes an amount.
type BankMatcher interface {
	Match(ctx context.Context, tx pgx.Tx, m BankMatch) error
}

// Confirmation is the bank's record that an authorised withdrawal was paid.
type Confirmation struct {
	ExecutedOn   time.Time // business date in America/Toronto
	BankRef      string
	InstrumentNo string
}

// Execution records the bank payment of an authorised withdrawal. The trust transaction posts
// here, dated ExecutedOn, through Effect.Execute; the bank confirmation only matches it.
type Execution struct {
	DB       TxRunner
	Effect   *Effect
	Bank     BankMatcher
	Problems server.ProblemWriter
	// Principal returns who the request acts as; false means unauthenticated.
	Principal func(r *http.Request) (registry.Principal, bool)
}

// Record posts the withdrawal and matches the bank line in one transaction. It fails closed:
// on any error nothing is posted or matched. It returns ErrUnverified while
// FlagBylaw9Verified is off.
func (x *Execution) Record(ctx context.Context, id string, by registry.Principal, c Confirmation) (trust.TxnID, error) {
	if !legalflags.Enabled(ctx, FlagBylaw9Verified) {
		return "", ErrUnverified
	}
	if id == "" || c.ExecutedOn.IsZero() || c.BankRef == "" || len(c.BankRef) > 80 || len(c.InstrumentNo) > 40 {
		return "", fmt.Errorf("record execution: %w", ErrInvalid)
	}
	var txn trust.TxnID
	err := x.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		// rule: ARC-I-012
		// Posting goes only through the gate's Execute path: it requires an approved,
		// unexpired authorisation and an executor other than the authoriser (ARC-I-027).
		t, err := x.Effect.Execute(ctx, tx, id, by, c.ExecutedOn)
		if err != nil {
			return err
		}
		if err := x.Bank.Match(ctx, tx, BankMatch{WithdrawalID: id, TxnID: t, ExecutedOn: c.ExecutedOn,
			BankRef: c.BankRef, InstrumentNo: c.InstrumentNo}); err != nil {
			return fmt.Errorf("bank match: %w", err)
		}
		txn = t
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("record execution: %w", err)
	}
	return txn, nil
}

// Routes mounts the execution route. It is the only withdrawal route that writes.
func (x *Execution) Routes(r chi.Router) {
	r.Post(ExecutionPath, x.handle)
}

type executionBody struct {
	ExecutedOn   string `json:"executed_on"`
	BankRef      string `json:"bank_ref"`
	InstrumentNo string `json:"instrument_no"`
}

func (x *Execution) handle(w http.ResponseWriter, r *http.Request) {
	by, ok := x.Principal(r)
	if !ok {
		x.Problems.WriteProblem(w, r, server.NewProblem(http.StatusUnauthorized, "unauthenticated", "Authentication required"))
		return
	}
	var b executionBody
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		x.Problems.WriteProblem(w, r, server.NewProblem(http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid"))
		return
	}
	on, err := server.ParseBusinessDate(b.ExecutedOn)
	if err != nil {
		x.Problems.WriteProblem(w, r, server.NewProblem(http.StatusBadRequest, server.CodeInvalidDate, "executed_on must be YYYY-MM-DD"))
		return
	}
	executedOn := time.Date(on.Year, on.Month, on.Day, 0, 0, 0, 0, time.UTC)
	id := chi.URLParam(r, "withdrawal_id")
	txn, err := x.Record(r.Context(), id, by, Confirmation{ExecutedOn: executedOn, BankRef: b.BankRef, InstrumentNo: b.InstrumentNo})
	if err != nil {
		x.Problems.WriteProblem(w, r, problemFor(err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"id": string(txn), "kind": "disbursement", "posted_on": b.ExecutedOn})
}

// problemFor maps a Record error to its problem. Unknown errors are a 500 with no detail.
func problemFor(err error) api.Problem {
	var ee *EffectError
	switch {
	case errors.Is(err, ErrUnverified):
		return server.NewProblem(http.StatusConflict, CodeUnverified, "Trust withdrawal rules are not verified")
	case errors.Is(err, ErrNotFound):
		return server.NewProblem(http.StatusNotFound, "not_found", "Withdrawal not found")
	case errors.Is(err, ErrInvalid):
		return server.NewProblem(http.StatusUnprocessableEntity, server.CodeValidationFailed, "Execution is not valid")
	case errors.As(err, &ee) && ee.Code == CodeInsufficientTrustFunds:
		p := server.NewProblem(http.StatusConflict, CodeTrustOverdraw, "Amount exceeds the available trust balance")
		p.Rule = "ARC-I-024"
		return p
	case errors.As(err, &ee) && ee.Code == CodeUnclearedFunds:
		return server.NewProblem(http.StatusConflict, CodeUnclearedFunds, "Withdrawal draws on uncleared funds")
	case errors.Is(err, ErrNotAuthorized):
		// rule: ARC-I-012
		// rule: ARC-I-027
		p := server.NewProblem(http.StatusConflict, CodeNotAuthorized, "Withdrawal is not authorized for this execution")
		p.Rule = "ARC-I-012"
		return p
	default:
		return server.NewProblem(http.StatusInternalServerError, server.CodeInternal, "Internal error")
	}
}
