package gate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// decideTx answers Decide's statements for one pending gate "g1" prepared by "prep". After
// the decision insert, the gate reads back as status.
type decideTx struct {
	pgx.Tx
	jcs      []byte
	expires  time.Time
	status   Status
	inserted int
}

func (f *decideTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	sum := sha256.Sum256(f.jcs)
	switch {
	case strings.Contains(sql, "SELECT gate_code, version, status"):
		return engineRow{vals: []any{"desk.refund", 1, string(StatusPending), sum[:], "desk.refund_request", "s1",
			"prep", f.expires, f.jcs, sum[:]}}
	case strings.Contains(sql, "SELECT status FROM gate.approval_request"):
		return engineRow{vals: []any{string(f.status)}}
	}
	return engineRow{err: fmt.Errorf("decideTx: unexpected QueryRow %q", sql)}
}

func (f *decideTx) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "WITH RECURSIVE chain") {
		return &engineRows{rows: [][]any{{"prep"}}}, nil
	}
	return nil, fmt.Errorf("decideTx: unexpected Query %q", sql)
}

func (f *decideTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, "INSERT INTO gate.gate_decision") {
		f.inserted++
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	}
	return pgconn.CommandTag{}, fmt.Errorf("decideTx: unexpected Exec %q", sql)
}

func TestARC_I_030_DecidePublishesEveryDecision(t *testing.T) {
	// rule: ARC-I-030
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	jcs := []byte(`{"amount_cents":5}`)
	sum := sha256.Sum256(jcs)
	hash := "sha256:" + hex.EncodeToString(sum[:])
	errHook := errors.New("outbox down")
	tests := []struct {
		name    string
		verdict Verdict
		reason  string
		after   Status
		hookErr error
		want    []Decided
		wantErr error
	}{
		{"approval that leaves the gate pending", VerdictApprove, "", StatusPending, nil,
			[]Decided{{GateID: "g1", Version: 1, Decision: "approve", DeciderID: "M"}}, nil},
		{"rejection closes the gate", VerdictReject, "no", StatusRejected, nil,
			[]Decided{{GateID: "g1", Version: 1, Decision: "reject", DeciderID: "M", Final: true}}, nil},
		{"a failing publish fails the decision", VerdictReject, "no", StatusRejected, errHook,
			[]Decided{{GateID: "g1", Version: 1, Decision: "reject", DeciderID: "M", Final: true}}, errHook},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := NewPG()
			kind := validKind("desk.refund")
			kind.MinApprovers = 2
			if err := e.Register(kind); err != nil {
				t.Fatal(err)
			}
			var got []Decided
			e.SetDecided(func(_ context.Context, tx pgx.Tx, d Decided) error {
				if tx == nil {
					t.Fatal("hook ran outside the decision tx")
				}
				got = append(got, d)
				return tt.hookErr
			})
			tx := &decideTx{jcs: jcs, expires: now.Add(time.Hour), status: tt.after}
			err := e.Decide(engineCtx(t, now), tx, "g1", 1, Decision{
				Verdict: tt.verdict, Reason: tt.reason, PayloadHash: hash, MFAVerifiedAt: now.Add(-time.Minute),
			}, Principal{Kind: PrincipalUser, UserID: "M"})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Decide = %v, want %v", err, tt.wantErr)
			}
			if tx.inserted != 1 {
				t.Fatalf("decisions inserted = %d, want 1", tx.inserted)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("published %+v, want %+v", got, tt.want)
			}
		})
	}
}
