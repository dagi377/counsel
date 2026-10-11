//go:build integration

package contract

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	initDDLPath   = "../../migrations/0001_init.sql"
	smokeSQLPath  = "../../migrations/smoke.sql"
	specDDLPath   = "../../spec/eng/a4-data-model.sql"
	specSmokePath = "../../spec/eng/a4-data-model.smoke.sql"
	specModelPath = "../../spec/eng/a4-data-model.json"
)

// dataModelLayers is the "layers" object of a4-data-model.json.
type dataModelLayers struct {
	KernelSchemas            []string            `json:"kernel_schemas"`
	AppSchemas               []string            `json:"app_schemas"`
	MixedSchemas             map[string][]string `json:"mixed_schemas"`
	AppTablesInKernelSchemas []string            `json:"app_tables_in_kernel_schemas"`
}

// ddlHeaderLayers is what the "-- Layers" comment block of 0001_init.sql declares.
type ddlHeaderLayers struct {
	kernel, app, appTablesInKernel []string
	mixed                          map[string][]string
}

var (
	headerKernelRe = regexp.MustCompile(`^--\s+kernel schemas \([^)]*\):\s*(.+)$`)
	headerAppRe    = regexp.MustCompile(`^--\s+app schemas \([^)]*\):\s*(.+)$`)
	headerMixedRe  = regexp.MustCompile(`^--\s+(\w+)\s+app:\s*([\w, ]+?)\s*(\(.*)?$`)
	headerInKernel = regexp.MustCompile(`^--\s+app table in a kernel schema:\s*(.+)$`)
)

func splitList(s string) []string {
	var out []string
	for _, f := range strings.Split(s, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	slices.Sort(out)
	return out
}

// parseDDLHeader reads the leading comment block of a DDL file and returns the layers it lists.
func parseDDLHeader(ddl string) ddlHeaderLayers {
	h := ddlHeaderLayers{mixed: map[string][]string{}}
	for _, line := range strings.Split(ddl, "\n") {
		line = strings.TrimRight(line, " \r")
		if line != "" && !strings.HasPrefix(line, "--") {
			break // end of the header comment block
		}
		if m := headerKernelRe.FindStringSubmatch(line); m != nil {
			h.kernel = splitList(m[1])
		} else if m := headerAppRe.FindStringSubmatch(line); m != nil {
			h.app = splitList(m[1])
		} else if m := headerInKernel.FindStringSubmatch(line); m != nil {
			h.appTablesInKernel = splitList(m[1])
		} else if m := headerMixedRe.FindStringSubmatch(line); m != nil {
			h.mixed[m[1]] = splitList(m[2])
		}
	}
	return h
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.FromSlash(p))
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

func loadLayers(t *testing.T) dataModelLayers {
	t.Helper()
	var doc struct {
		Layers dataModelLayers `json:"layers"`
	}
	if err := json.Unmarshal([]byte(readFile(t, specModelPath)), &doc); err != nil {
		t.Fatalf("decode %s: %v", specModelPath, err)
	}
	return doc.Layers
}

func sorted(s []string) []string {
	out := slices.Clone(s)
	slices.Sort(out)
	return out
}

// TestDDLHeaderListsLayers checks that the 0001_init.sql header lists kernel and app schemas
// exactly as the a4-data-model.json layer tags.
func TestDDLHeaderListsLayers(t *testing.T) {
	want := loadLayers(t)
	got := parseDDLHeader(readFile(t, initDDLPath))
	if !strings.HasPrefix(readFile(t, initDDLPath), "-- CONTRACT") {
		t.Errorf("%s: first line must be the CONTRACT header", initDDLPath)
	}
	tests := []struct {
		name      string
		got, want []string
	}{
		{name: "kernel schemas", got: got.kernel, want: sorted(want.KernelSchemas)},
		{name: "app schemas", got: got.app, want: sorted(want.AppSchemas)},
		{name: "app tables in kernel schemas", got: got.appTablesInKernel, want: sorted(want.AppTablesInKernelSchemas)},
		{name: "mixed schema names", got: sorted(mapKeys(got.mixed)), want: sorted(mapKeys(want.MixedSchemas))},
	}
	for s, tables := range want.MixedSchemas {
		tests = append(tests, struct {
			name      string
			got, want []string
		}{name: "mixed schema " + s, got: got.mixed[s], want: sorted(tables)})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.want) == 0 {
				t.Fatalf("a4-data-model.json lists nothing for %s", tt.name)
			}
			if !slices.Equal(tt.got, tt.want) {
				t.Fatalf("header lists %v, a4-data-model.json lists %v", tt.got, tt.want)
			}
		})
	}
}

func mapKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestDDLImportMatchesSpec checks that the migrations are the spec files unchanged: 0001_init.sql is
// the spec DDL behind a header block, and smoke.sql is the spec smoke script byte-for-byte.
func TestDDLImportMatchesSpec(t *testing.T) {
	initDDL := readFile(t, initDDLPath)
	_, body, ok := strings.Cut(initDDL, "\n\n")
	tests := []struct {
		name      string
		ok        bool
		got, want string
	}{
		{name: "0001_init.sql body", ok: ok, got: body, want: readFile(t, specDDLPath)},
		{name: "smoke.sql", ok: true, got: readFile(t, smokeSQLPath), want: readFile(t, specSmokePath)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.ok {
				t.Fatal("no blank line after the header block")
			}
			if tt.got != tt.want {
				t.Fatal("differs from the spec file; re-import it unchanged")
			}
		})
	}
}

// splitSQL splits a script into top-level statements, the way psql does: semicolons inside
// quotes, quoted identifiers, comments and dollar-quoted bodies do not end a statement.
// Statements that are only whitespace and comments are dropped.
func splitSQL(script string) []string {
	var (
		out     []string
		start   int
		code    bool // statement has non-comment text
		dollar  string
		inQuote byte
	)
	flush := func(end int) {
		if code {
			out = append(out, strings.TrimSpace(script[start:end]))
		}
		start, code = end+1, false
	}
	for i := 0; i < len(script); i++ {
		c := script[i]
		switch {
		case dollar != "":
			if strings.HasPrefix(script[i:], dollar) {
				i += len(dollar) - 1
				dollar = ""
			}
		case inQuote != 0:
			if c == inQuote {
				inQuote = 0
			}
		case c == '-' && strings.HasPrefix(script[i:], "--"):
			if j := strings.IndexByte(script[i:], '\n'); j >= 0 {
				i += j
			} else {
				i = len(script)
			}
		case c == '/' && strings.HasPrefix(script[i:], "/*"):
			if j := strings.Index(script[i+2:], "*/"); j >= 0 {
				i += j + 3
			} else {
				i = len(script)
			}
		case c == '\'' || c == '"':
			inQuote, code = c, true
		case c == '$':
			code = true
			if m := dollarTagRe.FindString(script[i:]); m != "" {
				dollar = m
				i += len(m) - 1
			}
		case c == ';':
			flush(i)
		case c != ' ' && c != '\t' && c != '\n' && c != '\r':
			code = true
		}
	}
	flush(len(script))
	return out
}

var dollarTagRe = regexp.MustCompile(`^\$([A-Za-z_][A-Za-z0-9_]*)?\$`)

func TestSplitSQL(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   []string
	}{
		{name: "two statements", script: "SELECT 1; SELECT 2;", want: []string{"SELECT 1", "SELECT 2"}},
		{name: "no trailing semicolon", script: "SELECT 1", want: []string{"SELECT 1"}},
		{name: "comment only dropped", script: "-- a; b\n;SELECT 1;", want: []string{"SELECT 1"}},
		{name: "block comment", script: "/* x; y */ SELECT 1;", want: []string{"/* x; y */ SELECT 1"}},
		{name: "string literal", script: "SELECT 'a;b'; SELECT 2", want: []string{"SELECT 'a;b'", "SELECT 2"}},
		{name: "escaped quote", script: "SELECT 'it''s;'; SELECT 2", want: []string{"SELECT 'it''s;'", "SELECT 2"}},
		{name: "quoted identifier", script: `SELECT 1 AS "x;y";`, want: []string{`SELECT 1 AS "x;y"`}},
		{name: "dollar body", script: "DO $$ BEGIN PERFORM 1; END $$; SELECT 2;", want: []string{"DO $$ BEGIN PERFORM 1; END $$", "SELECT 2"}},
		{name: "tagged dollar body", script: "SELECT $f$ a; $$ b $f$; SELECT 2", want: []string{"SELECT $f$ a; $$ b $f$", "SELECT 2"}},
		{name: "positional param", script: "SELECT $1; SELECT 2", want: []string{"SELECT $1", "SELECT 2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := splitSQL(tt.script); !slices.Equal(got, tt.want) {
				t.Fatalf("splitSQL(%q) = %q, want %q", tt.script, got, tt.want)
			}
		})
	}
}

// TestDDLAppliesAndSmokePasses applies 0001_init.sql to a fresh, empty database on the
// COUNSEL_TEST_DB server (Postgres 16 or later), then runs migrations/smoke.sql against it.
func TestDDLAppliesAndSmokePasses(t *testing.T) {
	dsn := os.Getenv("COUNSEL_TEST_DB")
	if dsn == "" {
		t.Skip("COUNSEL_TEST_DB not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	// Registered first so it runs last, after the database is dropped.
	t.Cleanup(func() { admin.Close(context.Background()) })

	var versionNum string
	if err := admin.QueryRow(ctx, "SHOW server_version_num").Scan(&versionNum); err != nil {
		t.Fatalf("server version: %v", err)
	}
	if v, err := strconv.Atoi(versionNum); err != nil || v < 160000 {
		t.Fatalf("server_version_num %s: need Postgres 16 or later", versionNum)
	}

	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatalf("random name: %v", err)
	}
	dbName := "ddl_contract_" + hex.EncodeToString(suffix)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{dbName}.Sanitize()); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{dbName}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Errorf("drop database %s: %v", dbName, err)
		}
	})

	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse COUNSEL_TEST_DB: %v", err)
	}
	cfg.Database = dbName
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect %s: %v", dbName, err)
	}
	defer conn.Close(ctx)

	// One simple-protocol round trip: the whole migration runs in one implicit transaction.
	if _, err := conn.PgConn().Exec(ctx, readFile(t, initDDLPath)).ReadAll(); err != nil {
		t.Fatalf("apply %s: %v", initDDLPath, err)
	}

	// Run the smoke script statement by statement, as psql -v ON_ERROR_STOP=1 does.
	var runErr error
	for i, stmt := range splitSQL(readFile(t, smokeSQLPath)) {
		if _, err := conn.PgConn().Exec(ctx, stmt).ReadAll(); err != nil {
			runErr = fmt.Errorf("smoke statement %d: %w", i+1, err)
			break
		}
	}
	rows, err := conn.Query(ctx, "SELECT label, coalesce(detail, '') FROM smoke.result WHERE NOT ok ORDER BY n")
	if err != nil {
		t.Fatalf("read smoke.result (run error: %v): %v", runErr, err)
	}
	failures, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (string, error) {
		var label, detail string
		err := r.Scan(&label, &detail)
		return label + ": " + detail, err
	})
	if err != nil {
		t.Fatalf("scan smoke.result: %v", err)
	}
	for _, f := range failures {
		t.Errorf("smoke FAIL %s", f)
	}
	if runErr != nil {
		t.Fatal(runErr)
	}
	var passed int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM smoke.result WHERE ok").Scan(&passed); err != nil {
		t.Fatalf("count smoke.result: %v", err)
	}
	if passed == 0 {
		t.Fatal("smoke.sql recorded no passing cases")
	}
	t.Logf("smoke: %d cases passed", passed)
}
