//go:build integration

package outbox_test

import (
	"context"
	"errors"
	"os"
	"sort"
	"sync"
	"testing"

	"github.com/dagi377/counsel/kernel/outbox"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const seqDDL = "../../migrations/0500_outbox_subject_seq.sql"

// seqDB is testDB plus the subject_seq migration.
func seqDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := testDB(t)
	ddl, err := os.ReadFile(seqDDL)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Conn().PgConn().Exec(context.Background(), string(ddl)).ReadAll(); err != nil {
		t.Fatalf("apply %s: %v", seqDDL, err)
	}
	return pool
}

var errRollback = errors.New("rollback on purpose")

func TestEmitSeqGapFreeUnderConcurrentWriters(t *testing.T) {
	const writers = 20
	const perWriter = 5
	subjects := []outbox.Subject{
		{Kind: "invoice", ID: "0190a1b2-0000-7000-8000-000000000001"},
		{Kind: "invoice", ID: "0190a1b2-0000-7000-8000-000000000002"},
	}
	tests := []struct {
		name string
		// rollbackEvery makes every nth emit of a writer roll back; 0 never does.
		rollbackEvery int
	}{
		{"all commit", 0},
		{"some roll back", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			pool := seqDB(t)
			ob := outbox.NewPG(pool)
			var wg sync.WaitGroup
			errs := make(chan error, writers*perWriter*len(subjects))
			for w := 0; w < writers; w++ {
				wg.Add(1)
				go func(w int) {
					defer wg.Done()
					for i := 0; i < perWriter; i++ {
						for _, s := range subjects {
							rollback := tt.rollbackEvery > 0 && (w+i)%tt.rollbackEvery == 0
							err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
								if _, err := ob.EmitSeq(ctx, tx, s, "invoice.issued", map[string]int{"writer": w}); err != nil {
									return err
								}
								if rollback {
									return errRollback
								}
								return nil
							})
							if err != nil && !errors.Is(err, errRollback) {
								errs <- err
							}
						}
					}
				}(w)
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				t.Fatalf("emit: %v", err)
			}
			for _, s := range subjects {
				rows, err := pool.Query(ctx, `SELECT subject_seq FROM platform.outbox
WHERE aggregate = $1 AND aggregate_id = $2`, s.Kind, s.ID)
				if err != nil {
					t.Fatal(err)
				}
				seqs, err := pgx.CollectRows(rows, pgx.RowTo[int64])
				if err != nil {
					t.Fatal(err)
				}
				if len(seqs) == 0 {
					t.Fatalf("%s: no rows", s.ID)
				}
				sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })
				for i, got := range seqs {
					if got != int64(i+1) {
						t.Fatalf("%s: seq[%d] = %d, want %d (all %v)", s.ID, i, got, i+1, seqs)
					}
				}
				if tt.rollbackEvery == 0 && len(seqs) != writers*perWriter {
					t.Errorf("%s: %d rows, want %d", s.ID, len(seqs), writers*perWriter)
				}
			}
		})
	}
}

func TestEmitSeqRejectsInvalidEvent(t *testing.T) {
	ctx := context.Background()
	pool := seqDB(t)
	ob := outbox.NewPG(pool)
	ok := outbox.Subject{Kind: "invoice", ID: "0190a1b2-0000-7000-8000-000000000003"}
	tests := []struct {
		name string
		s    outbox.Subject
		typ  string
		body any
	}{
		{"empty type", ok, "", map[string]int{}},
		{"empty subject kind", outbox.Subject{ID: ok.ID}, "invoice.issued", map[string]int{}},
		{"empty subject id", outbox.Subject{Kind: ok.Kind}, "invoice.issued", map[string]int{}},
		{"body not object", ok, "invoice.issued", []int{1}},
		{"body not encodable", ok, "invoice.issued", make(chan int)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
				_, err := ob.EmitSeq(ctx, tx, tt.s, tt.typ, tt.body)
				return err
			})
			if !errors.Is(err, outbox.ErrInvalidEvent) {
				t.Fatalf("err = %v, want ErrInvalidEvent", err)
			}
		})
	}
}
