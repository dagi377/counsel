package conflict

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
)

// scriptRow is one scripted QueryRow answer: values to scan, or err.
type scriptRow struct {
	vals []any
	err  error
}

func (r scriptRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	return scanInto(r.vals, dest)
}

func scanInto(vals, dest []any) error {
	if len(vals) != len(dest) {
		return fmt.Errorf("scan: %d values into %d targets", len(vals), len(dest))
	}
	for i, v := range vals {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(v))
	}
	return nil
}

// scriptRows is a scripted pgx.Rows.
type scriptRows struct {
	pgx.Rows
	rows    [][]any
	i       int
	scanErr error
	err     error
}

func (r *scriptRows) Next() bool { r.i++; return r.i <= len(r.rows) }
func (r *scriptRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	return scanInto(r.rows[r.i-1], dest)
}
func (r *scriptRows) Err() error { return r.err }
func (r *scriptRows) Close()     {}

// call is one statement the scripted tx saw.
type call struct {
	sql  string
	args []any
}

// searchTxFake answers by SQL: the index version, the contacts, the roles and the sealed
// count. It records every statement and its args.
type searchTxFake struct {
	pgx.Tx
	version     scriptRow
	contacts    []string
	contactsErr error
	roles       [][]any
	rolesErr    error
	sealed      scriptRow
	calls       []call
}

func (t *searchTxFake) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	t.calls = append(t.calls, call{sql, args})
	switch {
	case sql == searchVersionSQL:
		return t.version
	case sql == searchSealedSQL:
		return t.sealed
	}
	return scriptRow{err: fmt.Errorf("unexpected QueryRow %q", sql)}
}

func (t *searchTxFake) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	t.calls = append(t.calls, call{sql, args})
	switch sql {
	case searchContactsSQL:
		if t.contactsErr != nil {
			return nil, t.contactsErr
		}
		rows := make([][]any, 0, len(t.contacts))
		for _, c := range t.contacts {
			rows = append(rows, []any{c})
		}
		return &scriptRows{rows: rows}, nil
	case searchRolesSQL:
		if t.rolesErr != nil {
			return &scriptRows{err: t.rolesErr}, nil
		}
		return &scriptRows{rows: t.roles}, nil
	}
	return nil, fmt.Errorf("unexpected Query %q", sql)
}

// searchDB runs fn on its scripted tx.
type searchDB struct {
	db.DB
	tx *searchTxFake
}

func (d searchDB) InTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	return fn(ctx, d.tx)
}

// indexTx scripts the nameIndex of check_test.go: "acme" is p1, adverse on m9, and "beta"
// is p2, adverse on m8.
func indexTx(term string) *searchTxFake {
	tx := &searchTxFake{version: scriptRow{vals: []any{int64(42)}}, sealed: scriptRow{vals: []any{int64(0)}}}
	switch NormaliseName(term) {
	case "acme":
		tx.contacts = []string{"p1"}
		tx.roles = [][]any{{"p1", "m9", "adverse"}}
	case "beta":
		tx.contacts = []string{"p2"}
		tx.roles = [][]any{{"p2", "m8", "adverse"}}
	}
	return tx
}

func TestNormaliseName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Acme", "acme"},
		{"  ACME   Holdings  Inc ", "acme holdings inc"},
		{"Société Générale", "societe generale"},
		{"BJÖRK Ångström", "bjork angstrom"},
		{"Łukasz Żółć", "lukasz zolc"},
		{"François\tDupré", "francois dupre"},
		{" \t ", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := NormaliseName(tt.in); got != tt.want {
				t.Fatalf("NormaliseName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
	if len([]rune(foldFrom)) != len([]rune(foldTo)) {
		t.Fatalf("translate arguments differ in length: %d vs %d", len([]rune(foldFrom)), len([]rune(foldTo)))
	}
}

// The nameIndex cases of check_test.go, run against scripted rows.
func TestPGSearcherNameIndexCases(t *testing.T) {
	tests := []struct {
		name string
		term string
		want []Hit
	}{
		{"hit", "Acme", []Hit{{Term: "Acme", PartyID: "p1", MatterID: "m9", Role: "adverse"}}},
		{"lower case hit", "acme", []Hit{{Term: "acme", PartyID: "p1", MatterID: "m9", Role: "adverse"}}},
		{"diacritics and spacing fold", "  ÂCMÉ ", []Hit{{Term: "  ÂCMÉ ", PartyID: "p1", MatterID: "m9", Role: "adverse"}}},
		{"second party", "beta", []Hit{{Term: "beta", PartyID: "p2", MatterID: "m8", Role: "adverse"}}},
		{"no hit", "Other", []Hit{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := indexTx(tt.term)
			got, err := NewPGSearcher(searchDB{tx: tx}).Search(context.Background(), tt.term)
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("hits = %+v, want %+v", got, tt.want)
			}
		})
	}
	t.Run("drives Checks like nameIndex", func(t *testing.T) {
		s := NewPGSearcher(searchDB{tx: indexTx("acme")})
		c := NewChecks(s, func() time.Time { return time.Date(2026, 3, 2, 14, 0, 0, 0, time.UTC) })
		r, err := c.Check(context.Background(), Request{MatterID: "m1", Terms: []string{"Acme"}, Preparer: preparer})
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Hits) != 1 || r.Hits[0].PartyID != "p1" || r.Status != StatusOpen {
			t.Fatalf("run = %+v", r)
		}
	})
}

func TestPGSearcherKindsAndVersion(t *testing.T) {
	tests := []struct {
		name     string
		contacts []string
		roles    [][]any
		sealed   int64
		want     []IndexHit
	}{
		{
			name:     "one hit per conflict-relevant role",
			contacts: []string{"p1"},
			roles:    [][]any{{"p1", "m9", "adverse"}, {"p1", "", "client"}},
			want: []IndexHit{
				{Hit{Term: "acme", PartyID: "p1", MatterID: "m9", Role: "adverse"}, HitKindPartyRole},
				{Hit{Term: "acme", PartyID: "p1", Role: "client"}, HitKindPartyRole},
			},
		},
		{
			name:     "contact with no role is still a hit",
			contacts: []string{"p1", "p3"},
			roles:    [][]any{{"p1", "m9", "adverse"}},
			want: []IndexHit{
				{Hit{Term: "acme", PartyID: "p1", MatterID: "m9", Role: "adverse"}, HitKindPartyRole},
				{Hit{Term: "acme", PartyID: "p3"}, HitKindContact},
			},
		},
		{
			name:   "sealed prospect matched but not disclosed",
			sealed: 2,
			want: []IndexHit{
				{Hit{Term: "acme", Role: HitKindSealedProspect}, HitKindSealedProspect},
				{Hit{Term: "acme", Role: HitKindSealedProspect}, HitKindSealedProspect},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &searchTxFake{version: scriptRow{vals: []any{int64(7)}}, contacts: tt.contacts, roles: tt.roles,
				sealed: scriptRow{vals: []any{tt.sealed}}}
			res, err := NewPGSearcher(searchDB{tx: tx}).SearchIndex(context.Background(), "acme")
			if err != nil {
				t.Fatalf("SearchIndex: %v", err)
			}
			if res.IndexVersion != 7 {
				t.Fatalf("index version = %d, want 7", res.IndexVersion)
			}
			if !reflect.DeepEqual(res.Hits, tt.want) {
				t.Fatalf("hits = %+v, want %+v", res.Hits, tt.want)
			}
			for _, h := range res.Hits {
				if h.Kind == HitKindSealedProspect && (h.PartyID != "" || h.MatterID != "") {
					t.Fatalf("sealed prospect disclosed: %+v", h)
				}
			}
			if len(tt.contacts) == 0 {
				for _, c := range tx.calls {
					if c.sql == searchRolesSQL {
						t.Fatal("roles read with no matching contact")
					}
				}
			}
		})
	}
}

func TestPGSearcherFailsClosed(t *testing.T) {
	boom := errors.New("connection reset")
	tests := []struct {
		name string
		term string
		tx   *searchTxFake
		err  error
	}{
		{"blank term", "  ", &searchTxFake{}, ErrNoTerms},
		{"version error", "acme", &searchTxFake{version: scriptRow{err: boom}}, boom},
		{"no version", "acme", &searchTxFake{version: scriptRow{vals: []any{int64(0)}}}, nil},
		{"contacts error", "acme", &searchTxFake{version: scriptRow{vals: []any{int64(1)}}, contactsErr: boom}, boom},
		{"roles error", "acme", &searchTxFake{version: scriptRow{vals: []any{int64(1)}}, contacts: []string{"p1"}, rolesErr: boom}, boom},
		{"sealed error", "acme", &searchTxFake{version: scriptRow{vals: []any{int64(1)}}, sealed: scriptRow{err: boom}}, boom},
		{"negative sealed count", "acme", &searchTxFake{version: scriptRow{vals: []any{int64(1)}}, sealed: scriptRow{vals: []any{int64(-1)}}}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hits, err := NewPGSearcher(searchDB{tx: tt.tx}).Search(context.Background(), tt.term)
			if err == nil {
				t.Fatalf("Search err = nil, hits %+v", hits)
			}
			if tt.err != nil && !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if hits != nil {
				t.Fatalf("hits on error = %+v", hits)
			}
			if strings.Contains(err.Error(), "acme") {
				t.Fatalf("error leaks the term: %v", err)
			}
		})
	}
	t.Run("no database", func(t *testing.T) {
		if _, err := (&PGSearcher{}).Search(context.Background(), "acme"); err == nil {
			t.Fatal("Search with no database succeeded")
		}
	})
}

// The term reaches SQL only as a bound argument, never concatenated into the statement.
func TestPGSearcherParameterised(t *testing.T) {
	const term = "O'Brien; DROP TABLE party.contact --"
	tx := &searchTxFake{version: scriptRow{vals: []any{int64(1)}}, contacts: []string{"p1"},
		roles: [][]any{{"p1", "m1", "adverse"}}, sealed: scriptRow{vals: []any{int64(0)}}}
	if _, err := NewPGSearcher(searchDB{tx: tx}).Search(context.Background(), term); err != nil {
		t.Fatal(err)
	}
	norm := NormaliseName(term)
	want := []call{
		{searchVersionSQL, nil},
		{searchContactsSQL, []any{norm, foldFrom, foldTo}},
		{searchRolesSQL, []any{[]string{"p1"}}},
		{searchSealedSQL, []any{norm, foldFrom, foldTo}},
	}
	if len(tx.calls) != len(want) {
		t.Fatalf("calls = %+v", tx.calls)
	}
	for i, w := range want {
		got := tx.calls[i]
		if got.sql != w.sql {
			t.Fatalf("call %d sql = %q, want %q", i, got.sql, w.sql)
		}
		if len(got.args) != len(w.args) || (len(w.args) > 0 && !reflect.DeepEqual(got.args, w.args)) {
			t.Fatalf("call %d args = %#v, want %#v", i, got.args, w.args)
		}
		if strings.Contains(got.sql, "O'Brien") || strings.Contains(got.sql, "obrien") || strings.Contains(got.sql, norm) {
			t.Fatalf("call %d concatenates the term: %q", i, got.sql)
		}
	}
}

// The search reads names, ids and roles only: never notes, descriptions or other text.
func TestPGSearcherReadsNoText(t *testing.T) {
	for _, sql := range []string{searchVersionSQL, searchContactsSQL, searchRolesSQL, searchSealedSQL} {
		for _, banned := range []string{"notes", "nature", "description", "custom_fields", "emails", "addresses", "message", "body", "comms.", "docs."} {
			if strings.Contains(sql, banned) {
				t.Errorf("search SQL reads %q:\n%s", banned, sql)
			}
		}
	}
	if !strings.Contains(searchRolesSQL, "conflict_relevant") {
		t.Error("roles SQL lacks the conflict_relevant predicate")
	}
	for _, sql := range []string{searchContactsSQL, searchRolesSQL, searchSealedSQL} {
		if strings.Contains(strings.ToUpper(sql), " JOIN ") {
			t.Errorf("search SQL joins another module's table:\n%s", sql)
		}
	}
}

// The index version is the conflict index version row, not a snapshot xmin, and it is read
// before any hit, so it never names a write the hits do not include.
func TestPGSearcherIndexVersionReadFirst(t *testing.T) {
	tests := []struct {
		name    string
		version int64
	}{
		{"first version", 1},
		{"later version", 9001},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := indexTx("acme")
			tx.version = scriptRow{vals: []any{tt.version}}
			res, err := NewPGSearcher(searchDB{tx: tx}).SearchIndex(context.Background(), "acme")
			if err != nil {
				t.Fatalf("SearchIndex: %v", err)
			}
			if res.IndexVersion != tt.version {
				t.Fatalf("index version = %d, want %d", res.IndexVersion, tt.version)
			}
			if len(tx.calls) == 0 || tx.calls[0].sql != searchVersionSQL {
				t.Fatalf("first statement = %+v, want the index version", tx.calls)
			}
			if !strings.Contains(searchVersionSQL, "conflict.index_version") || strings.Contains(searchVersionSQL, "xmin") {
				t.Fatalf("version SQL %q does not read the conflict index version", searchVersionSQL)
			}
		})
	}
}
