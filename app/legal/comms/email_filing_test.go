package comms

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/outbox"
)

// scriptRow is one scripted QueryRow result: a value to scan or an error.
type scriptRow struct {
	val any
	err error
}

func (r scriptRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	switch d := dest[0].(type) {
	case *string:
		*d = r.val.(string)
	case *bool:
		*d = r.val.(bool)
	}
	return nil
}

// scriptTx answers QueryRow from rows in order and records each statement. Any other pgx.Tx
// method panics through the nil embedded interface.
type scriptTx struct {
	pgx.Tx
	rows []scriptRow
	sqls []string
}

func (t *scriptTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	t.sqls = append(t.sqls, sql)
	r := t.rows[0]
	t.rows = t.rows[1:]
	return r
}

// scriptDB runs every transaction on tx.
type scriptDB struct{ tx *scriptTx }

func (d scriptDB) InTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	return fn(ctx, d.tx)
}
func (scriptDB) Bind(context.Context, pgx.Tx, db.Session) error { return nil }
func (scriptDB) Ping(context.Context) error                     { return nil }
func (scriptDB) Close()                                         {}

type emitted struct {
	subject outbox.Subject
	typ     string
	body    any
}

type recEmitter struct {
	got []emitted
	err error
}

func (e *recEmitter) Emit(_ context.Context, _ pgx.Tx, s outbox.Subject, typ string, body any) error {
	e.got = append(e.got, emitted{s, typ, body})
	return e.err
}

func TestFileEmail(t *testing.T) {
	const item, m, u = "0192f000-0000-7000-8000-0000000000e1", "0192f000-0000-7000-8000-0000000000a1", "0192f000-0000-7000-8000-0000000000b1"
	errEmit := errors.New("emit failed")
	cases := []struct {
		name               string
		item, matter, user string
		rows               []scriptRow
		emitErr            error
		wantFiled          bool
		wantErr            error
		wantEvents         int
	}{
		{"files and emits email.filed", item, m, u, []scriptRow{{val: item}}, nil, true, nil, 1},
		{"already filed emits nothing", item, m, u, []scriptRow{{err: pgx.ErrNoRows}, {val: true}}, nil, false, nil, 0},
		{"unknown or non-email item", item, m, u, []scriptRow{{err: pgx.ErrNoRows}, {val: false}}, nil, false, ErrNotFound, 0},
		{"emit failure fails the filing", item, m, u, []scriptRow{{val: item}}, errEmit, false, errEmit, 1},
		{"no comm item", "", m, u, nil, nil, false, ErrInvalid, 0},
		{"no matter", item, " ", u, nil, nil, false, ErrInvalid, 0},
		{"no filer", item, m, "", nil, nil, false, ErrInvalid, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tx := &scriptTx{rows: tc.rows}
			out := &recEmitter{err: tc.emitErr}
			filed, err := NewEmailFilingStore(scriptDB{tx}, out).FileEmail(context.Background(), tc.item, tc.matter, tc.user)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil && filed != tc.wantFiled {
				t.Fatalf("filed = %v, want %v", filed, tc.wantFiled)
			}
			if len(out.got) != tc.wantEvents {
				t.Fatalf("events = %d, want %d", len(out.got), tc.wantEvents)
			}
			if tc.wantEvents == 0 {
				return
			}
			// rule: ARC-I-030
			ev := out.got[0]
			want := EmailFiled{CommItemID: item, MatterID: m, FiledBy: u}
			if ev.typ != EventEmailFiled || ev.subject != (outbox.Subject{Kind: "comm_item", ID: item}) || ev.body != want {
				t.Fatalf("event = %+v, want %s on comm_item %s with %+v", ev, EventEmailFiled, item, want)
			}
			if !strings.Contains(tx.sqls[0], "channel = 'email'") {
				t.Fatalf("filing must be limited to email items: %s", tx.sqls[0])
			}
		})
	}
}
