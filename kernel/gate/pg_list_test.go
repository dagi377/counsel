package gate

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

// noRunner fails any transaction: an empty kind list must return before reading.
type noRunner struct{}

func (noRunner) InTx(context.Context, func(context.Context, pgx.Tx) error) error {
	return errors.New("no database")
}

func TestPGListNeedsDB(t *testing.T) {
	_, err := PGList{}.List(context.Background(), ListFilter{})
	if !errors.Is(err, ErrNoDB) {
		t.Fatalf("err = %v, want ErrNoDB", err)
	}
}

func TestPGListEmptyKindsKeepsNone(t *testing.T) {
	got, err := PGList{DB: noRunner{}}.List(context.Background(), ListFilter{Kinds: []string{}})
	if err != nil || len(got) != 0 {
		t.Fatalf("List = %v, %v; want none", got, err)
	}
}

func TestListLimit(t *testing.T) {
	tests := []struct{ in, want int }{
		{0, DefaultListLimit},
		{-3, DefaultListLimit},
		{7, 7},
		{MaxListLimit + 1, MaxListLimit},
	}
	for _, tt := range tests {
		if got := listLimit(tt.in); got != tt.want {
			t.Errorf("listLimit(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}
