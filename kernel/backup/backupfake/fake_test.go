package backupfake

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/backup"
)

func TestExportMonth(t *testing.T) {
	m := backup.Month{Year: 2026, Month: 9}
	down := errors.New("bucket down")
	tests := []struct {
		name    string
		failPut error
		twice   bool
		err     error
		record  bool
	}{
		{name: "object then record", record: true},
		{name: "bucket failure writes no record", failPut: down, err: down},
		{name: "second export refused", twice: true, err: backup.ErrAlreadyExported, record: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			f := New("dr-cell-1")
			if tt.twice {
				if _, err := f.ExportMonth(ctx, m, strings.NewReader("x")); err != nil {
					t.Fatal(err)
				}
			}
			f.FailPut = tt.failPut
			rec, err := f.ExportMonth(ctx, m, strings.NewReader("journal export"))
			if !errors.Is(err, tt.err) {
				t.Fatalf("ExportMonth err = %v, want %v", err, tt.err)
			}
			if err == nil {
				obj, herr := f.Head(ctx, rec.Object.Key)
				if herr != nil || obj.SHA256 != rec.Object.SHA256 || obj.Size != 14 {
					t.Fatalf("object %+v, %v; record %+v", obj, herr, rec)
				}
			}
			_, gerr := f.Export(ctx, m)
			if (gerr == nil) != tt.record {
				t.Fatalf("record present = %v, want %v", gerr == nil, tt.record)
			}
		})
	}
}

func TestRestoreChecks(t *testing.T) {
	ok := func(context.Context, pgx.Tx) error { return nil }
	bad := func(context.Context, pgx.Tx) error { return errors.New("totals differ") }
	tests := []struct {
		name   string
		checks map[string]backup.CheckFunc
		order  []string
		err    error
	}{
		{name: "all pass", checks: map[string]backup.CheckFunc{"a": ok, "b": ok}, order: []string{"a", "b"}},
		{name: "one fails", checks: map[string]backup.CheckFunc{"a": ok, "b": bad}, order: []string{"a", "b"}, err: backup.ErrCheckFailed},
		{name: "none", order: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New("b")
			for _, n := range tt.order {
				if err := f.Register(n, tt.checks[n]); err != nil {
					t.Fatal(err)
				}
			}
			res, err := f.Run(context.Background(), nil)
			if !errors.Is(err, tt.err) || len(res) != len(tt.order) {
				t.Fatalf("Run = %v, %v; want %d results, %v", res, err, len(tt.order), tt.err)
			}
		})
	}
	f := New("b")
	_ = f.Register("a", ok)
	if err := f.Register("a", ok); !errors.Is(err, backup.ErrDuplicateCheck) {
		t.Fatalf("duplicate err = %v", err)
	}
	if _, err := f.Head(context.Background(), "nope"); !errors.Is(err, backup.ErrNotFound) {
		t.Fatalf("Head err = %v", err)
	}
}
