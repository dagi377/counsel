//go:build integration

package ledger

import (
	"context"
	"testing"
	"time"
)

func TestGLPostReverseBalanceOnPostgres(t *testing.T) {
	conn, user := freshDB(t)
	ctx := context.Background()
	gl, err := NewGL(&PG{PostedBy: func(context.Context) (string, error) { return user, nil }}, Balances{Q: conn})
	if err != nil {
		t.Fatalf("NewGL: %v", err)
	}
	var l Ledger = gl
	day := func(d int) Date { return Date{Year: 2026, Month: time.March, Day: d} }

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id, err := l.Post(ctx, tx, Entry{Book: "main", Date: day(10), SourceKind: "manual", SourceID: uuidish(41), Memo: "t",
		Lines: []Line{line("1000", Debit, 900), line("4000", Credit, 900)}})
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit post: %v", err)
	}

	tests := []struct {
		name    string
		account string
		asOf    Date
		want    Cents
	}{
		{"debit side", "1000", day(31), 900},
		{"credit side", "4000", day(31), -900},
		{"before entry", "1000", day(9), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := l.Balance(ctx, tt.account, tt.asOf)
			if err != nil || got != tt.want {
				t.Fatalf("Balance = %d, %v; want %d", got, err, tt.want)
			}
		})
	}

	tx, err = conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Reverse(ctx, tx, id, "test"); err != nil {
		t.Fatalf("Reverse: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit reverse: %v", err)
	}
	far := Date{Year: 2100, Month: time.January, Day: 1}
	if got, err := l.Balance(ctx, "1000", far); err != nil || got != 0 {
		t.Fatalf("Balance after reverse = %d, %v; want 0", got, err)
	}
}
