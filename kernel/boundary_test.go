// Package kernel holds the kernel boundary ratchet tests (T-001b, k1-kernel.html): the
// kernel never imports app/, carries no legal vocabulary, adds no new FK into app tables,
// and app/legal never registers ids under a kernel module prefix.
package kernel

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"unicode"
)

const (
	modulePath = "github.com/dagi377/counsel"
	repoRoot   = ".."
	termsFile  = "tools/kernel_terms.txt"
	allowFile  = "tools/kernel_allow.txt"
	bindFile   = "tools/kernel_bindings.txt"
	dataModel  = "spec/eng/a4-data-model.json"
)

// readList returns the non-blank, non-comment lines of a list file.
func readList(data []byte) []string {
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		out = append(out, l)
	}
	return out
}

func readListFile(t *testing.T, rel string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return readList(b)
}

// --- 1. kernel never imports app/ ---

// appDeps returns the packages in deps that live under the module's app/ tree.
func appDeps(deps []string) []string {
	var bad []string
	for _, d := range deps {
		if d == modulePath+"/app" || strings.HasPrefix(d, modulePath+"/app/") {
			bad = append(bad, d)
		}
	}
	return bad
}

func TestAppDeps(t *testing.T) {
	m := modulePath
	tests := []struct {
		name string
		deps []string
		want int
	}{
		{name: "kernel only", deps: []string{m + "/kernel/money", "fmt"}, want: 0},
		{name: "app/legal/matter", deps: []string{m + "/kernel/money", m + "/app/legal/matter"}, want: 1},
		{name: "app root", deps: []string{m + "/app"}, want: 1},
		{name: "lookalike", deps: []string{m + "/application", "example.com/app/x"}, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := appDeps(tt.deps); len(got) != tt.want {
				t.Fatalf("appDeps(%v) = %v, want %d hits", tt.deps, got, tt.want)
			}
		})
	}
}

func goBinary() string {
	if p := filepath.Join(runtime.GOROOT(), "bin", "go"); fileExists(p) {
		return p
	}
	return "go"
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// TestKernelDoesNotImportApp runs go list -deps ./kernel/... and fails on any app/ package.
func TestKernelDoesNotImportApp(t *testing.T) {
	cmd := exec.Command(goBinary(), "list", "-deps", "-test=false", "./kernel/...")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("go list -deps ./kernel/...: %v\n%s", err, stderr)
	}
	for _, p := range appDeps(strings.Fields(string(out))) {
		t.Errorf("kernel depends on app package %s: the kernel never imports app/", p)
	}
}

// --- 2. legal-term scan ---

// normalize lowercases text and splits identifiers into words (camelCase, snake_case,
// kebab-case), so "matterID", "matter_id" and "Matter-Id" all read "matter id".
func normalize(s string) string {
	var b strings.Builder
	b.WriteByte(' ')
	rs := []rune(s)
	for i, r := range rs {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if i > 0 && unicode.IsUpper(r) {
				prev := rs[i-1]
				nextLower := i+1 < len(rs) && unicode.IsLower(rs[i+1])
				if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextLower) {
					b.WriteByte(' ')
				}
			}
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteByte(' ')
		}
	}
	b.WriteByte(' ')
	return strings.Join(strings.Fields(b.String()), " ")
}

// containsTerm reports whether text holds term as whole words (plural "s" allowed).
func containsTerm(text, term string) bool {
	n := " " + normalize(text) + " "
	t := normalize(term)
	return strings.Contains(n, " "+t+" ") || strings.Contains(n, " "+t+"s ")
}

// scannedExt are the file kinds the term scan reads.
var scannedExt = map[string]bool{
	".go": true, ".sql": true, ".yaml": true, ".yml": true, ".json": true,
	".ts": true, ".tsx": true, ".md": true, ".txt": true,
}

// scanTerms walks roots inside fsys and returns "path: term" for every term hit not in allow.
func scanTerms(fsys fs.FS, roots, terms []string, allow map[string]bool) ([]string, error) {
	var hits []string
	for _, root := range roots {
		if _, err := fs.Stat(fsys, root); err != nil {
			continue
		}
		err := fs.WalkDir(fsys, root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !scannedExt[path.Ext(p)] || p == "kernel/boundary_test.go" {
				return nil
			}
			b, err := fs.ReadFile(fsys, p)
			if err != nil {
				return err
			}
			for _, term := range terms {
				key := p + ": " + term
				if containsTerm(string(b), term) && !allow[key] {
					hits = append(hits, key)
				}
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk %s: %w", root, err)
		}
	}
	sort.Strings(hits)
	return hits, nil
}

var termRoots = []string{"kernel", "web/src/kernel"}

func TestScanTerms(t *testing.T) {
	terms := []string{"matter", "retainer", "conflict check", "By-Law", "HST"}
	tests := []struct {
		name  string
		files map[string]string
		allow []string
		want  []string
	}{
		{
			name:  "planted retainer in kernel/money fails",
			files: map[string]string{"kernel/money/split.go": "package money\n// retainer split\n"},
			want:  []string{"kernel/money/split.go: retainer"},
		},
		{
			name:  "allowlisted pair passes",
			files: map[string]string{"kernel/money/split.go": "package money\n// retainer split\n"},
			allow: []string{"kernel/money/split.go: retainer"},
		},
		{
			name:  "identifier forms",
			files: map[string]string{"kernel/work/x.go": "var matterID, hstRate int\nconst q = \"conflict_check\"\n"},
			want:  []string{"kernel/work/x.go: HST", "kernel/work/x.go: conflict check", "kernel/work/x.go: matter"},
		},
		{
			name:  "sql and yaml",
			files: map[string]string{"kernel/db/a.sql": "SELECT matters FROM t;", "kernel/flags/f.yaml": "rule: by_law_9\n"},
			want:  []string{"kernel/db/a.sql: matter", "kernel/flags/f.yaml: By-Law"},
		},
		{
			name:  "web kernel scanned",
			files: map[string]string{"web/src/kernel/shell.tsx": "const Matter = 1"},
			want:  []string{"web/src/kernel/shell.tsx: matter"},
		},
		{
			name:  "no substring hits",
			files: map[string]string{"kernel/x/x.go": "// mattered not; whst; no retainerx\n"},
		},
		{
			name:  "app tree not scanned",
			files: map[string]string{"app/legal/matter/m.go": "package matter"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsys := fstest.MapFS{}
			for p, c := range tt.files {
				fsys[p] = &fstest.MapFile{Data: []byte(c)}
			}
			allow := map[string]bool{}
			for _, a := range tt.allow {
				allow[a] = true
			}
			got, err := scanTerms(fsys, termRoots, terms, allow)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Fatalf("scanTerms = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestKernelHasNoLegalTerms scans the real tree against tools/kernel_terms.txt.
func TestKernelHasNoLegalTerms(t *testing.T) {
	terms := readListFile(t, termsFile)
	if len(terms) == 0 {
		t.Fatalf("%s is empty", termsFile)
	}
	allow := map[string]bool{}
	for _, a := range readListFile(t, allowFile) {
		allow[a] = true
	}
	fsys := os.DirFS(repoRoot)
	hits, err := scanTerms(fsys, termRoots, terms, allow)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		t.Errorf("legal term in kernel: %s (rename it, or add the exact line to %s)", h, allowFile)
	}
	// Stale allow entries must be removed so the list keeps shrinking.
	for a := range allow {
		p, term, ok := strings.Cut(a, ": ")
		if !ok {
			t.Errorf("%s: malformed line %q, want <path>: <term>", allowFile, a)
			continue
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil || !containsTerm(string(b), term) {
			t.Errorf("%s: stale entry %q, remove it", allowFile, a)
		}
	}
}

// --- 3. ratchet: allow and bindings lists only shrink ---

// ratchetGrew reports whether a list file grew relative to its base version.
func ratchetGrew(base, head []byte) (bool, string) {
	b, h := len(readList(base)), len(readList(head))
	if h > b {
		return true, fmt.Sprintf("kernel ratchet grew: %d entries, base has %d", h, b)
	}
	return false, ""
}

func TestRatchetGrew(t *testing.T) {
	tests := []struct {
		name       string
		base, head string
		want       bool
	}{
		{name: "same", base: "a\nb\n", head: "a\nb\n", want: false},
		{name: "shrank", base: "a\nb\n", head: "a\n", want: false},
		{name: "grew", base: "a\n", head: "a\nb\n", want: true},
		{name: "comments ignored", base: "# x\na\n", head: "# x\n# y\na\n\n", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, msg := ratchetGrew([]byte(tt.base), []byte(tt.head))
			if got != tt.want {
				t.Fatalf("ratchetGrew = %v, want %v", got, tt.want)
			}
			if got && !strings.Contains(msg, "kernel ratchet grew") {
				t.Fatalf("message %q lacks 'kernel ratchet grew'", msg)
			}
		})
	}
}

// TestKernelRatchet compares the list files with the base ref (COUNSEL_RATCHET_BASE,
// default origin/main). It skips when git or the base file is unavailable.
func TestKernelRatchet(t *testing.T) {
	base := os.Getenv("COUNSEL_RATCHET_BASE")
	if base == "" {
		base = "origin/main"
	}
	for _, f := range []string{allowFile, bindFile} {
		cmd := exec.Command("git", "show", base+":"+f)
		cmd.Dir = repoRoot
		old, err := cmd.Output()
		if err != nil {
			t.Logf("ratchet: no %s at %s, skipped", f, base)
			continue
		}
		head, err := os.ReadFile(filepath.Join(repoRoot, f))
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if grew, msg := ratchetGrew(old, head); grew {
			t.Errorf("%s: %s", f, msg)
		}
	}
}

// --- 4. kernel_bindings.txt covers every kernel-to-app FK ---

type dataModelDoc struct {
	Tables []struct {
		Name  string `json:"name"`
		Layer string `json:"layer"`
		FKs   []struct {
			Cols []string `json:"cols"`
			Ref  string   `json:"ref"`
		} `json:"fks"`
	} `json:"tables"`
}

// kernelToAppFKs lists "schema.table.column -> schema.table" for every kernel-layer
// column whose FK points at an app-layer table.
func kernelToAppFKs(raw []byte) ([]string, error) {
	var dm dataModelDoc
	if err := json.Unmarshal(raw, &dm); err != nil {
		return nil, fmt.Errorf("parse data model: %w", err)
	}
	layer := map[string]string{}
	for _, t := range dm.Tables {
		layer[t.Name] = t.Layer
	}
	var out []string
	for _, t := range dm.Tables {
		if t.Layer != "kernel" {
			continue
		}
		for _, fk := range t.FKs {
			if layer[fk.Ref] != "app" {
				continue
			}
			for _, c := range fk.Cols {
				out = append(out, t.Name+"."+c+" -> "+fk.Ref)
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

func TestKernelToAppFKs(t *testing.T) {
	raw := `{"tables":[
	 {"name":"k.a","layer":"kernel","fks":[{"cols":["owner_id"],"ref":"app.x"},{"cols":["user_id"],"ref":"k.b"}]},
	 {"name":"k.b","layer":"kernel","fks":[]},
	 {"name":"app.x","layer":"app","fks":[{"cols":["b_id"],"ref":"k.b"}]}]}`
	got, err := kernelToAppFKs([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if want := "k.a.owner_id -> app.x"; len(got) != 1 || got[0] != want {
		t.Fatalf("kernelToAppFKs = %q, want [%q]", got, want)
	}
}

// TestKernelBindingsListed fails when a kernel-to-app FK is missing from kernel_bindings.txt,
// or when the file lists a binding that no longer exists.
func TestKernelBindingsListed(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot, dataModel))
	if err != nil {
		t.Fatalf("read %s: %v", dataModel, err)
	}
	fks, err := kernelToAppFKs(raw)
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	for _, l := range readListFile(t, bindFile) {
		listed[l] = true
	}
	actual := map[string]bool{}
	for _, fk := range fks {
		actual[fk] = true
		if !listed[fk] {
			t.Errorf("new kernel-to-app FK %q is not in %s: use an opaque subject reference instead", fk, bindFile)
		}
	}
	for l := range listed {
		if !actual[l] {
			t.Errorf("%s: stale binding %q, remove it", bindFile, l)
		}
	}
}

// --- 5. app/legal ids never use a kernel module prefix ---

var kernelPrefix = regexp.MustCompile(`^(core|gate|platform|audit)\.[a-z0-9_.]+$`)

// grandfathered ids under question Q-K1 (k1-kernel.html, CF-07).
var prefixAllow = map[string]bool{"core.trust_signatory": true}

// prefixViolation reports whether an app-registered id uses a kernel module prefix.
func prefixViolation(id string) bool {
	return kernelPrefix.MatchString(id) && !prefixAllow[id]
}

func TestPrefixViolation(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{id: "billing.invoice_approve", want: false},
		{id: "trust.signatory_change", want: false},
		{id: "core.trust_signatory", want: false},
		{id: "core.other", want: true},
		{id: "gate.x", want: true},
		{id: "platform.report", want: true},
		{id: "audit.export", want: true},
		{id: "corex.y", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			if got := prefixViolation(tt.id); got != tt.want {
				t.Fatalf("prefixViolation(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}

// idFields are the struct fields whose string values are registered ids.
var idFields = map[string]bool{"ID": true, "Kind": true, "Slug": true, "Cap": true, "Capability": true, "PackID": true, "ReportID": true}

var yamlID = regexp.MustCompile(`(?m)^\s*-?\s*(id|kind|slug|capability):\s*["']?([A-Za-z0-9_.]+)["']?\s*$`)

// registeredIDs extracts candidate registered ids from one app file.
func registeredIDs(name string, src []byte) ([]string, error) {
	var ids []string
	switch path.Ext(name) {
	case ".go":
		f, err := parser.ParseFile(token.NewFileSet(), name, src, 0)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			kv, ok := n.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			k, ok := kv.Key.(*ast.Ident)
			if !ok || !idFields[k.Name] {
				return true
			}
			if lit, ok := kv.Value.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if s, err := strconv.Unquote(lit.Value); err == nil {
					ids = append(ids, s)
				}
			}
			return true
		})
	case ".yaml", ".yml":
		for _, m := range yamlID.FindAllSubmatch(src, -1) {
			ids = append(ids, string(m[2]))
		}
	}
	return ids, nil
}

func TestRegisteredIDs(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{name: "g.go", src: "package g\nvar k = GateKind{ID: \"gate.x\", Title: \"core.y\"}\n", want: []string{"gate.x"}},
		{name: "p.yaml", src: "id: core.pack\nrules:\n  - id: rule_1\n", want: []string{"core.pack", "rule_1"}},
		{name: "n.txt", src: "id: core.x", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := registeredIDs(tt.name, []byte(tt.src))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Fatalf("registeredIDs = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestAppIDsAvoidKernelPrefix scans app/legal for registered ids under a kernel prefix.
func TestAppIDsAvoidKernelPrefix(t *testing.T) {
	root := filepath.Join(repoRoot, "app", "legal")
	if !fileExists(root) {
		t.Skip("app/legal not present yet")
	}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		ids, err := registeredIDs(p, src)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if prefixViolation(id) {
				t.Errorf("%s registers %q under a kernel module prefix; use the app module prefix", p, id)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
