package trust_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/idem/idemfake"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

const (
	userClerk  = "0191d000-0000-7000-8000-00000000000a" // records the receipt
	userLawyer = "0191d000-0000-7000-8000-00000000000b" // classifies it
	matterNone = "0191b000-0000-7000-8000-000000000002" // no ledger on the account
)

// idemKey is a valid (UUIDv7) Idempotency-Key.
const idemKey = "0191f100-0000-7000-8000-000000000001"

var receiptDay = time.Date(2026, 10, 9, 15, 0, 0, 0, time.UTC)

// memJournal is an in-memory trust.ReceiptJournal.
type memJournal struct {
	mu   sync.Mutex
	rows map[string]trust.JournalReceipt
	seq  int
}

func (j *memJournal) Record(_ context.Context, _ pgx.Tx, r trust.JournalReceipt) (string, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.seq++
	r.ID = fmt.Sprintf("0191a100-0000-7000-8000-%012d", j.seq)
	r.Status = trust.ReceiptPending
	j.rows[r.ID] = r
	return r.ID, nil
}

func (j *memJournal) SetGate(_ context.Context, _ pgx.Tx, id, gateID string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	r, ok := j.rows[id]
	if !ok {
		return trust.ErrNotFound
	}
	r.GateID = gateID
	j.rows[id] = r
	return nil
}

func (j *memJournal) Lock(_ context.Context, _ pgx.Tx, id string) (trust.JournalReceipt, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	r, ok := j.rows[id]
	if !ok {
		return trust.JournalReceipt{}, trust.ErrNotFound
	}
	return r, nil
}

func (j *memJournal) Classify(_ context.Context, _ pgx.Tx, id string, s trust.ReceiptStatus, txn trust.TxnID) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	r, ok := j.rows[id]
	if !ok || r.Status != trust.ReceiptPending {
		return trust.ErrReceiptState
	}
	r.Status, r.TxnID = s, txn
	j.rows[id] = r
	return nil
}

func (j *memJournal) only(t *testing.T) trust.JournalReceipt {
	t.Helper()
	j.mu.Lock()
	defer j.mu.Unlock()
	if len(j.rows) != 1 {
		t.Fatalf("journal rows = %d, want 1", len(j.rows))
	}
	for _, r := range j.rows {
		return r
	}
	return trust.JournalReceipt{}
}

// memPoster stands for the client ledger: every receipt Receive posted.
type memPoster struct {
	mu     sync.Mutex
	posted []trust.Receipt
}

func (p *memPoster) Receive(_ context.Context, _ pgx.Tx, r trust.Receipt) (trust.TxnID, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.posted = append(p.posted, r)
	return trust.TxnID(fmt.Sprintf("txn-%d", len(p.posted))), nil
}

func (p *memPoster) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.posted)
}

type recordAccess struct{}

func (recordAccess) Can(_ context.Context, p registry.Principal, capability string, _ registry.Subject) (bool, error) {
	return (p.UserID == userClerk || p.UserID == userLawyer) && capability == trust.CapRecord, nil
}

type txNone struct{}

func (txNone) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return fn(ctx, nil)
}

type receiptRig struct {
	journal *memJournal
	poster  *memPoster
	gates   *gatefake.Fake
	handler http.Handler
}

func newReceiptRig(t *testing.T) *receiptRig {
	t.Helper()
	rg := &receiptRig{journal: &memJournal{rows: map[string]trust.JournalReceipt{}}, poster: &memPoster{}}
	rg.gates = gatefake.New(func() time.Time { return receiptDay },
		gatefake.Caps{userClerk: {trust.CapRecord}, userLawyer: {trust.CapRecord}})
	fx := (&trust.ReceiptClassificationEffect{Journal: rg.journal, Ledgers: rg.poster}).Effects()
	if err := rg.gates.Register(gate.GateKind{Slug: trust.GateReceiptClassification, Approver: trust.CapRecord,
		Risk: gate.RiskHigh, MinApprovers: 1, TTL: 72 * time.Hour, Effect: fx[trust.GateReceiptClassification]}); err != nil {
		t.Fatal(err)
	}
	st := &stubTrust{ledgers: []trust.Ledger{{ID: ledgerID(1), AccountID: acctID, MatterID: matterID, ClientID: clientID}}}
	h := &trust.ReceiptHTTP{Lists: st, Journal: rg.journal, Access: recordAccess{}, DB: txNone{}, Gates: rg.gates,
		Idem: idemfake.New(), Problems: &server.Problems{},
		Principal: func(r *http.Request) (registry.Principal, bool) {
			u := r.Header.Get("X-Test-User")
			return registry.Principal{Kind: registry.PrincipalUser, UserID: u}, u != ""
		}}
	m := server.New(nil)
	if err := m.Mount(h.Routes); err != nil {
		t.Fatal(err)
	}
	rg.handler = m.Handler()
	return rg
}

const receiptJSON = `{"account_id":"` + acctID + `","matter_id":"` + matterID + `","received_on":"2026-10-09",` +
	`"amount_cents":125000,"method":"eft","payer":"A. Client","purpose":"retainer"}`

func (rg *receiptRig) post(t *testing.T, user, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, server.BasePath+"/trust/receipts", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if user != "" {
		req.Header.Set("X-Test-User", user)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rec := httptest.NewRecorder()
	rg.handler.ServeHTTP(rec, req)
	return rec
}

type gateResp struct {
	ID      string                      `json:"id"`
	Kind    string                      `json:"kind"`
	State   string                      `json:"state"`
	Version int                         `json:"version"`
	Payload trust.ClassificationPayload `json:"payload"`
	Hash    string                      `json:"payload_hash"`
}

// rule: ARC-I-012
// POST /trust/receipts records the receipt pending classification the same day, opens the
// classification gate with the proposed classification and posts nothing to the ledger.
func TestRecordReceiptPending(t *testing.T) {
	rg := newReceiptRig(t)
	rec := rg.post(t, userClerk, idemKey, receiptJSON)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body)
	}
	var g gateResp
	if err := json.Unmarshal(rec.Body.Bytes(), &g); err != nil {
		t.Fatal(err)
	}
	row := rg.journal.only(t)
	for _, c := range []struct {
		name      string
		got, want any
	}{
		{"gate kind", g.Kind, trust.GateReceiptClassification},
		{"gate state", g.State, "open"},
		{"proposed classification", g.Payload.Classification, trust.ClassTrust},
		{"payload receipt", g.Payload.ReceiptID, row.ID},
		{"payload amount", g.Payload.AmountCents, int64(125000)},
		{"row status", row.Status, trust.ReceiptPending},
		{"row received on", row.ReceivedOn.Format(time.DateOnly), "2026-10-09"},
		{"row ledger", row.LedgerID, ledgerID(1)},
		{"row gate", row.GateID, g.ID},
		{"row recorded by", row.RecordedBy, userClerk},
		// rule: BIL-I-009
		{"client ledger postings before approval", rg.poster.count(), 0},
	} {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestRecordReceiptRefused(t *testing.T) {
	for _, c := range []struct {
		name, user, key, body string
		want                  int
	}{
		{"unauthenticated", "", idemKey, receiptJSON, http.StatusUnauthorized},
		{"no capability", "someone", idemKey, receiptJSON, http.StatusForbidden},
		{"no idempotency key", userClerk, "", receiptJSON, http.StatusBadRequest},
		{"zero amount", userClerk, idemKey, `{"account_id":"` + acctID + `","matter_id":"` + matterID +
			`","received_on":"2026-10-09","amount_cents":0,"method":"eft","payer":"A"}`, http.StatusBadRequest},
		{"unknown method", userClerk, idemKey, `{"account_id":"` + acctID + `","matter_id":"` + matterID +
			`","received_on":"2026-10-09","amount_cents":5,"method":"bitcoin","payer":"A"}`, http.StatusBadRequest},
		{"no ledger", userClerk, idemKey, `{"account_id":"` + acctID + `","matter_id":"` + matterNone +
			`","received_on":"2026-10-09","amount_cents":5,"method":"eft","payer":"A"}`, http.StatusUnprocessableEntity},
	} {
		t.Run(c.name, func(t *testing.T) {
			rg := newReceiptRig(t)
			if rec := rg.post(t, c.user, c.key, c.body); rec.Code != c.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, c.want, rec.Body)
			}
			if n := len(rg.journal.rows); n != 0 {
				t.Errorf("journal rows = %d, want 0", n)
			}
		})
	}
}

// rule: ARC-I-012
// Only an approved classification gate posts the receipt; the recorder and the agent can not
// approve it.
func TestReceiptClassificationDecision(t *testing.T) {
	for _, c := range []struct {
		name       string
		by         gate.Principal
		wantErr    error
		wantPosted int
		wantStatus trust.ReceiptStatus
	}{
		{"preparer cannot approve", gate.Principal{Kind: gate.PrincipalUser, UserID: userClerk},
			gate.ErrSelfApproval, 0, trust.ReceiptPending},
		{"agent cannot decide", gate.Principal{Kind: gate.PrincipalAgent, RunID: "run-1", ActingFor: userLawyer},
			gate.ErrAgentApprover, 0, trust.ReceiptPending},
		{"posted after approval", gate.Principal{Kind: gate.PrincipalUser, UserID: userLawyer},
			nil, 1, trust.ReceiptPosted},
	} {
		t.Run(c.name, func(t *testing.T) {
			rg := newReceiptRig(t)
			rec := rg.post(t, userClerk, idemKey, receiptJSON)
			var g gateResp
			if err := json.Unmarshal(rec.Body.Bytes(), &g); err != nil {
				t.Fatal(err)
			}
			err := rg.gates.Decide(context.Background(), nil, gate.GateID(g.ID), g.Version,
				gate.Decision{Verdict: gate.VerdictApprove, PayloadHash: g.Hash, MFAVerifiedAt: receiptDay}, c.by)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("Decide = %v, want %v", err, c.wantErr)
			}
			row := rg.journal.only(t)
			if got := rg.poster.count(); got != c.wantPosted {
				t.Errorf("client ledger postings = %d, want %d", got, c.wantPosted)
			}
			if row.Status != c.wantStatus {
				t.Errorf("row status = %s, want %s", row.Status, c.wantStatus)
			}
			if c.wantPosted == 1 {
				p := rg.poster.posted[0]
				if p.LedgerID != ledgerID(1) || p.AmountCents != 125000 || p.PostedBy != userLawyer ||
					p.ClearedOn.IsZero() || row.TxnID == "" {
					t.Errorf("posted %+v, row txn %q", p, row.TxnID)
				}
			}
		})
	}
}

// rule: ARC-I-012
func TestReceiptClassificationEffect(t *testing.T) {
	lawyer := registry.Principal{Kind: registry.PrincipalUser, UserID: userLawyer}
	clerk := registry.Principal{Kind: registry.PrincipalUser, UserID: userClerk}
	agent := registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-1", ActingFor: userLawyer}
	for _, c := range []struct {
		name       string
		class      string
		amount     int64
		approvers  []registry.Principal
		runs       int
		wantErr    bool
		wantPosted int
		wantStatus trust.ReceiptStatus
	}{
		{"trust posts", trust.ClassTrust, 125000, []registry.Principal{lawyer}, 1, false, 1, trust.ReceiptPosted},
		{"replay posts once", trust.ClassTrust, 125000, []registry.Principal{lawyer}, 2, false, 1, trust.ReceiptPosted},
		{"general posts nothing to trust", trust.ClassGeneral, 125000, []registry.Principal{lawyer}, 1, false, 0, trust.ReceiptGeneral},
		{"amount mismatch", trust.ClassTrust, 1, []registry.Principal{lawyer}, 1, true, 0, trust.ReceiptPending},
		{"no approver", trust.ClassTrust, 125000, nil, 1, true, 0, trust.ReceiptPending},
		{"agent approver", trust.ClassTrust, 125000, []registry.Principal{agent}, 1, true, 0, trust.ReceiptPending},
		{"preparer approver", trust.ClassTrust, 125000, []registry.Principal{clerk}, 1, true, 0, trust.ReceiptPending},
	} {
		t.Run(c.name, func(t *testing.T) {
			j := &memJournal{rows: map[string]trust.JournalReceipt{}}
			p := &memPoster{}
			id, _ := j.Record(context.Background(), nil, trust.JournalReceipt{LedgerID: ledgerID(1), MatterID: matterID,
				AmountCents: 125000, Method: trust.MethodCheque, Payer: "A", Purpose: "retainer", ReceivedOn: receiptDay})
			payload, _ := json.Marshal(trust.ClassificationPayload{ReceiptID: id, MatterID: matterID, AmountCents: c.amount, Classification: c.class})
			e := &trust.ReceiptClassificationEffect{Journal: j, Ledgers: p}
			var err error
			for range c.runs {
				err = e.Apply(context.Background(), nil, registry.Approved{GateID: "g", Kind: trust.GateReceiptClassification,
					Subject: registry.Subject{Kind: trust.SubjectReceipt, ID: id}, Payload: payload, Preparer: clerk, Approvers: c.approvers})
			}
			if (err != nil) != c.wantErr {
				t.Fatalf("Apply = %v, wantErr %v", err, c.wantErr)
			}
			if got := p.count(); got != c.wantPosted {
				t.Errorf("postings = %d, want %d", got, c.wantPosted)
			}
			if got := j.only(t).Status; got != c.wantStatus {
				t.Errorf("status = %s, want %s", got, c.wantStatus)
			}
			// A cheque starts uncleared.
			if c.wantPosted == 1 && !p.posted[0].ClearedOn.IsZero() {
				t.Errorf("cheque cleared on %v, want zero", p.posted[0].ClearedOn)
			}
		})
	}
}
