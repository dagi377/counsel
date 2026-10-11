package boot

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/kernel/db"
)

// bodyTx keeps agent.draft_body rows in memory: INSERT stores the sealed bytes under id,
// SELECT returns them.
type bodyTx struct {
	pgx.Tx
	id    string
	rows  map[string][]byte
	saved [][]byte
}

type bodyRow struct {
	scan func(dest ...any) error
}

func (r bodyRow) Scan(dest ...any) error { return r.scan(dest...) }

func (t *bodyTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	if strings.HasPrefix(sql, "INSERT") {
		b := args[0].([]byte)
		t.saved = append(t.saved, b)
		t.rows[t.id] = b
		return bodyRow{scan: func(dest ...any) error { *dest[0].(*string) = t.id; return nil }}
	}
	b, ok := t.rows[args[0].(string)]
	return bodyRow{scan: func(dest ...any) error {
		if !ok {
			return pgx.ErrNoRows
		}
		*dest[0].(*[]byte) = b
		return nil
	}}
}

func (t *bodyTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errNoDB
}

type bodyDB struct{ tx *bodyTx }

func (d bodyDB) InTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	return fn(ctx, d.tx)
}
func (bodyDB) Bind(context.Context, pgx.Tx, db.Session) error { return nil }
func (bodyDB) Ping(context.Context) error                     { return nil }
func (bodyDB) Close()                                         {}

func TestPGDraftBodies(t *testing.T) {
	ctx := context.Background()
	sealer, err := trust.NewAESSealer(trust.SealKey{ID: 1, Key: bytes.Repeat([]byte{7}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	const id = "00000000-0000-0000-0000-00000000d001"
	const text = "Synthetic draft body for the matter."
	cases := []struct {
		name    string
		store   PGDraftBodies
		getRef  func(put string) string
		wantPut error
		wantGet error
	}{
		{name: "stores sealed and opens back", getRef: func(put string) string { return put }},
		{name: "a ref it did not return is not found", getRef: func(string) string { return "obj-1" }, wantGet: ErrDraftBodyNotFound},
		{name: "an unknown id is not found", getRef: func(string) string { return DraftBodyRefPrefix + "00000000-0000-0000-0000-00000000d999" },
			wantGet: ErrDraftBodyNotFound},
		{name: "no sealer fails closed", store: PGDraftBodies{DB: bodyDB{}}, wantPut: agent.ErrMissingDep},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tx := &bodyTx{id: id, rows: map[string][]byte{}}
			s := tc.store
			if s.DB == nil {
				s = PGDraftBodies{DB: bodyDB{tx: tx}, Sealer: sealer}
			}
			ref, err := s.Put(ctx, text)
			if !errors.Is(err, tc.wantPut) || (tc.wantPut == nil && err != nil) {
				t.Fatalf("Put = %q, %v, want %v", ref, err, tc.wantPut)
			}
			if tc.wantPut != nil {
				return
			}
			// rule: ARC-U-040
			// The row holds ciphertext only; the reference holds no text.
			if ref != DraftBodyRefPrefix+id || len(tx.saved) != 1 || bytes.Contains(tx.saved[0], []byte(text)) {
				t.Fatalf("ref %q saved %q", ref, tx.saved)
			}
			got, err := s.Get(ctx, tc.getRef(ref))
			if !errors.Is(err, tc.wantGet) || (tc.wantGet == nil && (err != nil || got != text)) {
				t.Fatalf("Get = %q, %v, want %v", got, err, tc.wantGet)
			}
			if err != nil && strings.Contains(err.Error(), text) {
				t.Fatal("error carries the body text")
			}
		})
	}
}
