package db

import (
	"context"
	"errors"
	"testing"
)

func TestPlainStatementsOnClosedPool(t *testing.T) {
	p := &Pool{}
	ctx := context.Background()
	tests := []struct {
		name string
		run  func() error
	}{
		{"exec", func() error { _, err := p.Exec(ctx, "SELECT 1"); return err }},
		{"query", func() error { _, err := p.Query(ctx, "SELECT 1"); return err }},
		{"query row", func() error { var n int; return p.QueryRow(ctx, "SELECT 1").Scan(&n) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); !errors.Is(err, ErrClosed) {
				t.Fatalf("err = %v, want %v", err, ErrClosed)
			}
		})
	}
}
