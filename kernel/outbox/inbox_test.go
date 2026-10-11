package outbox_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/dagi377/counsel/kernel/outbox"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// inboxTx is a pgx.Tx that keeps platform.inbox in memory.
type inboxTx struct {
	pgx.Tx
	rows map[string]bool
}

func (t *inboxTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if !strings.Contains(sql, "platform.inbox") {
		return pgconn.CommandTag{}, errors.New("unexpected sql")
	}
	k := fmt.Sprint(args...)
	if t.rows[k] {
		return pgconn.NewCommandTag("INSERT 0 0"), nil
	}
	t.rows[k] = true
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

// rule: ARC-U-022
func TestARC_U_022_InboxRedeliveryIsNoop(t *testing.T) {
	tx := &inboxTx{rows: map[string]bool{}}
	s := outbox.NewPG(nil)
	effects := 0
	for range 3 {
		first, err := s.Claim(context.Background(), tx, "receipts", "m1")
		if err != nil {
			t.Fatal(err)
		}
		if first {
			effects++
		}
	}
	if effects != 1 || len(tx.rows) != 1 {
		t.Fatalf("effects = %d, inbox rows = %d; want 1, 1", effects, len(tx.rows))
	}
}

// rule: ARC-I-030
func TestARC_I_030_ConsumerClaimsInTx(t *testing.T) {
	tests := []struct {
		consumer, msg string
		want          bool
	}{
		{consumer: "a", msg: "m1", want: true},
		{consumer: "a", msg: "m1", want: false},
		{consumer: "b", msg: "m1", want: true},
	}
	tx := &inboxTx{rows: map[string]bool{}}
	s := outbox.NewPG(nil)
	for _, tt := range tests {
		t.Run(tt.consumer+"/"+tt.msg, func(t *testing.T) {
			got, err := s.Claim(context.Background(), tx, tt.consumer, tt.msg)
			if err != nil || got != tt.want {
				t.Fatalf("Claim = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}
