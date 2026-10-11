package matter

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// boolRow scans one bool.
type boolRow struct{ v bool }

func (r boolRow) Scan(dest ...any) error {
	*(dest[0].(*bool)) = r.v
	return nil
}

// existsTx answers the existence check with exists; Query fails so a test sees whether it ran.
type existsTx struct {
	pgx.Tx
	exists  bool
	queried bool
}

func (t *existsTx) QueryRow(context.Context, string, ...any) pgx.Row { return boolRow{v: t.exists} }

func (t *existsTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	t.queried = true
	return nil, errors.New("stop")
}

// existsDB hands tx to every InTx.
type existsDB struct {
	stubDB
	tx *existsTx
}

func (d *existsDB) InTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	return fn(ctx, d.tx)
}

// rule: ARC-I-041
func TestARC_I_041_Get(t *testing.T) {
	ctx := context.Background()
	created := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	okRow := stubRow{vals: []any{"m1", "2026-0001", "x", "c1", "pa", "u1", "pending", "firm_open", nil, DefaultRetentionClass, created, 1, "", "", ""}}
	// Q-TAX-FIELDS: the place-of-supply provinces come back through Query.
	taxRow := stubRow{vals: []any{"m1", "2026-0001", "x", "c1", "pa", "u1", "pending", "firm_open", nil, DefaultRetentionClass, created, 1, "NS", "BC", ""}}
	// AGENT-PROD-3: the matter's own rate table comes back through Query.
	rateRow := stubRow{vals: []any{"m1", "2026-0001", "x", "c1", "pa", "u1", "pending", "firm_open", nil, DefaultRetentionClass, created, 1, "", "", "rt-1"}}
	tests := []struct {
		name            string
		id              ID
		row             stubRow
		err             error
		court, property string
		rateTable       string
	}{
		{name: "found", id: "m1", row: okRow},
		{name: "found with court and property provinces", id: "m1", row: taxRow, court: "NS", property: "BC"},
		{name: "found with its own rate table", id: "m1", row: rateRow, rateTable: "rt-1"},
		{name: "not found", id: "m2", row: stubRow{err: pgx.ErrNoRows}, err: ErrNotFound},
		{name: "empty id", id: "", err: ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &stubDB{tx: &stubTx{row: tt.row}}
			got, err := NewStore(d, nil).Get(ctx, tt.id)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if tt.err != nil {
				return
			}
			want := Matter{ID: "m1", Number: "2026-0001", ShortName: "x", ClientID: "c1", PracticeAreaID: "pa",
				ResponsibleLawyerID: "u1", Status: Pending, Visibility: FirmOpen, RetentionClass: DefaultRetentionClass,
				CreatedAt: created, RowVersion: 1, CourtProvince: tt.court, PropertyProvince: tt.property, RateTableID: tt.rateTable}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("matter = %+v, want %+v", got, want)
			}
			if a := d.tx.args; len(a) != 1 || a[0] != "m1" {
				t.Fatalf("args = %v", a)
			}
		})
	}
}

// rule: ARC-I-041
func TestARC_I_041_ListSQL(t *testing.T) {
	tests := []struct {
		name     string
		f        Filter
		args     []any
		contains []string
		where    bool
	}{
		{name: "no filter", f: Filter{}, args: nil},
		{name: "client", f: Filter{ClientID: "c1"}, args: []any{"c1"}, contains: []string{"m.client_id = $1::uuid"}, where: true},
		{name: "all fields", f: Filter{ClientID: "c1", Status: Open, MemberID: "u1"}, args: []any{"c1", "open", "u1"},
			contains: []string{"m.client_id = $1::uuid", "m.status = $2", "mm.user_id = $3::uuid"}, where: true},
		{name: "member only", f: Filter{MemberID: "u1"}, args: []any{"u1"}, contains: []string{"mm.user_id = $1::uuid"}, where: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, args := listSQL(tt.f)
			if !reflect.DeepEqual(args, tt.args) {
				t.Fatalf("args = %v, want %v", args, tt.args)
			}
			if strings.Contains(q, " WHERE ") != tt.where {
				t.Fatalf("WHERE present = %v, want %v: %s", !tt.where, tt.where, q)
			}
			for _, c := range tt.contains {
				if !strings.Contains(q, c) {
					t.Fatalf("query lacks %q: %s", c, q)
				}
			}
		})
	}
}

// rule: ARC-I-041
func TestARC_I_041_ListRejectsUnknownStatus(t *testing.T) {
	d := &stubDB{tx: &stubTx{}}
	if _, err := NewStore(d, nil).List(context.Background(), Filter{Status: "lost"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	if d.calls != 0 {
		t.Fatalf("transactions = %d, want 0", d.calls)
	}
}

// rule: ARC-I-041
func TestARC_I_041_Members(t *testing.T) {
	tests := []struct {
		name    string
		id      ID
		exists  bool
		err     error
		queried bool
	}{
		{name: "unknown matter", id: "m1", exists: false, err: ErrNotFound},
		{name: "known matter reads members", id: "m1", exists: true, queried: true},
		{name: "empty id", id: "", err: ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &existsDB{tx: &existsTx{exists: tt.exists}}
			_, err := NewStore(d, nil).Members(context.Background(), tt.id)
			if tt.err != nil && !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if d.tx.queried != tt.queried {
				t.Fatalf("members queried = %v, want %v", d.tx.queried, tt.queried)
			}
		})
	}
}

// matterTable matches a reference to a matter base table in SQL text.
var matterTable = regexp.MustCompile(`\bmatter\.(matter|matter_member)\b`)

// matterTableRefs returns the string literals in src that name a matter base table.
func matterTableRefs(src string) ([]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
	if err != nil {
		return nil, err
	}
	var refs []string
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		s, err := strconv.Unquote(lit.Value)
		if err == nil && matterTable.MatchString(s) {
			refs = append(refs, s)
		}
		return true
	})
	return refs, nil
}

// rule: ARC-I-041
func TestARC_I_041_MatterTableRefs(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want int
	}{
		{name: "join base table", src: "package p\nconst q = `SELECT 1 FROM billing.bill b JOIN matter.matter m ON m.id = b.matter_id`", want: 1},
		{name: "member table", src: "package p\nvar q = \"SELECT 1 FROM matter.matter_member\"", want: 1},
		{name: "api view", src: "package p\nconst q = `SELECT 1 FROM api_matter.matter`", want: 0},
		{name: "go identifier", src: "package p\nimport \"x/matter\"\nvar _ matter.Matter", want: 0},
		{name: "other matter table", src: "package p\nconst q = `SELECT 1 FROM matter.matter_template`", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := matterTableRefs(tt.src)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tt.want {
				t.Fatalf("refs = %v, want %d", got, tt.want)
			}
		})
	}
}

// TestARC_I_041_NoOtherModuleReadsMatterTables is the layering test: outside app/legal/matter,
// no Go code names matter.matter or matter.matter_member in SQL. Other modules use Query or
// the api_matter view.
// rule: ARC-I-041
func TestARC_I_041_NoOtherModuleReadsMatterTables(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	own, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"app", "kernel", "cmd"} {
		d := filepath.Join(root, dir)
		if _, err := os.Stat(d); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(d, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if e.IsDir() {
				abs, err := filepath.Abs(p)
				if err != nil {
					return err
				}
				if abs == own || e.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(p, ".go") {
				return nil
			}
			src, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			refs, err := matterTableRefs(string(src))
			if err != nil {
				return err
			}
			for _, r := range refs {
				t.Errorf("%s reads a matter base table (%q): use matter.Query or api_matter", p, r)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", d, err)
		}
	}
}
