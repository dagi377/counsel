package boot

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/comms"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/db"
)

// tmplKey is one comms.message_template primary key.
type tmplKey struct {
	key     string
	version int
}

// tmplDB is an in-memory db.DB holding comms.message_template rows (key, version) -> body_ref.
// Its Tx answers only the two statements TemplateStore.Seed runs. A failed tx leaves the
// committed rows untouched.
type tmplDB struct {
	rows    map[tmplKey]string
	txs     int
	failErr error // returned by every query when set
}

func newTmplDB() *tmplDB { return &tmplDB{rows: map[tmplKey]string{}} }

func (d *tmplDB) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	d.txs++
	tx := &tmplTx{db: d, rows: maps.Clone(d.rows)}
	if err := fn(ctx, tx); err != nil {
		return err
	}
	d.rows = tx.rows
	return nil
}

func (*tmplDB) Bind(context.Context, pgx.Tx, db.Session) error { return nil }
func (*tmplDB) Ping(context.Context) error                     { return nil }
func (*tmplDB) Close()                                         {}

type tmplTx struct {
	pgx.Tx
	db   *tmplDB
	rows map[tmplKey]string
}

type tmplRow struct {
	version int
	ref     string
	err     error
}

func (r tmplRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*int), *dest[1].(*string) = r.version, r.ref
	return nil
}

// QueryRow answers "latest version of key".
func (t *tmplTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	if t.db.failErr != nil {
		return tmplRow{err: t.db.failErr}
	}
	if !strings.Contains(sql, "FROM comms.message_template") {
		return tmplRow{err: errors.New("unexpected query")}
	}
	key := args[0].(string)
	best := tmplRow{err: pgx.ErrNoRows}
	for k, ref := range t.rows {
		if k.key == key && (best.err != nil || k.version > best.version) {
			best = tmplRow{version: k.version, ref: ref}
		}
	}
	return best
}

// Exec answers the insert; ON CONFLICT DO NOTHING keeps an existing row.
func (t *tmplTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if !strings.Contains(sql, "INSERT INTO comms.message_template") {
		return pgconn.CommandTag{}, errors.New("unexpected statement")
	}
	k := tmplKey{key: args[0].(string), version: args[1].(int)}
	if _, ok := t.rows[k]; !ok {
		t.rows[k] = args[3].(string)
	}
	return pgconn.CommandTag{}, nil
}

func versionsOf(rows map[tmplKey]string) map[string][]int {
	out := map[string][]int{}
	for k := range rows {
		out[k.key] = append(out[k.key], k.version)
	}
	for _, v := range out {
		slices.Sort(v)
	}
	return out
}

func TestSeedTemplatesVersions(t *testing.T) {
	ids := []string{comms.TemplateBillDelivered, comms.TemplateCourtFiling, comms.TemplatePortalShared, comms.TemplateTrustReceipt}
	staleKey := tmplKey{key: comms.TemplateBillDelivered, version: 1}
	tests := []struct {
		name  string
		pre   map[tmplKey]string
		runs  int
		want  map[string][]int
		check func(t *testing.T, rows map[tmplKey]string)
	}{
		{name: "first seed writes version 1 of each", runs: 1,
			want: map[string][]int{ids[0]: {1}, ids[1]: {1}, ids[2]: {1}, ids[3]: {1}}},
		{name: "seeding twice leaves the same versions", runs: 2,
			want: map[string][]int{ids[0]: {1}, ids[1]: {1}, ids[2]: {1}, ids[3]: {1}}},
		{name: "changed body adds version+1 and keeps the old row", runs: 2,
			pre:  map[tmplKey]string{staleKey: "app:comms/templates/" + comms.TemplateBillDelivered + "#sha256=old"},
			want: map[string][]int{ids[0]: {1, 2}, ids[1]: {1}, ids[2]: {1}, ids[3]: {1}},
			check: func(t *testing.T, rows map[tmplKey]string) {
				if got := rows[staleKey]; !strings.HasSuffix(got, "#sha256=old") {
					t.Fatalf("version 1 body_ref overwritten: %q", got)
				}
				if rows[tmplKey{key: comms.TemplateBillDelivered, version: 2}] == rows[staleKey] {
					t.Fatal("version 2 has the old body_ref")
				}
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newTmplDB()
			maps.Copy(d.rows, tt.pre)
			start := App(Options{}).Start
			for i := 0; i < tt.runs; i++ {
				if err := start(context.Background(), &kboot.Kernel{DB: d}); err != nil {
					t.Fatalf("Start run %d = %v", i+1, err)
				}
			}
			if d.txs != tt.runs {
				t.Fatalf("transactions = %d, want one per Start (%d)", d.txs, tt.runs)
			}
			got := versionsOf(d.rows)
			if len(got) != len(tt.want) {
				t.Fatalf("versions = %v, want %v", got, tt.want)
			}
			for k, v := range tt.want {
				if !slices.Equal(got[k], v) {
					t.Fatalf("versions = %v, want %v", got, tt.want)
				}
			}
			if tt.check != nil {
				tt.check(t, d.rows)
			}
		})
	}
}

func TestSeedTemplatesErrorStopsStart(t *testing.T) {
	errDB := errors.New("connection reset")
	d := newTmplDB()
	d.failErr = errDB
	err := App(Options{}).Start(context.Background(), &kboot.Kernel{DB: d})
	if !errors.Is(err, errDB) {
		t.Fatalf("Start = %v, want %v", err, errDB)
	}
	if !strings.Contains(err.Error(), "legal boot: seed templates") {
		t.Fatalf("Start error %q not wrapped \"legal boot: seed templates\"", err)
	}
	if len(d.rows) != 0 {
		t.Fatalf("rows after failed seed = %v, want none", d.rows)
	}
}
