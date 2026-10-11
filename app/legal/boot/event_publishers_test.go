package boot

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/clock"
)

// publishCall matches the callee names that put an event on the outbox: outbox Emit and
// EmitSeq, and the modules' emit and Publish helpers built on them.
var publishCall = regexp.MustCompile(`^(?i:emit|publish)`)

// srcFile is one parsed non-test Go file and the import path of its package.
type srcFile struct {
	pkg  string
	file *ast.File
}

// parseSources parses every non-test Go file under dirs of the module at root.
func parseSources(t *testing.T, root string, dirs ...string) []srcFile {
	t.Helper()
	fset := token.NewFileSet()
	var out []srcFile
	for _, d := range dirs {
		err := filepath.WalkDir(filepath.Join(root, d), func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if e.IsDir() && e.Name() == "testdata" {
				return filepath.SkipDir
			}
			if e.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, filepath.Dir(p))
			if err != nil {
				return err
			}
			out = append(out, srcFile{pkg: modulePath + "/" + filepath.ToSlash(rel), file: f})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// stringConsts maps package import path to its top-level string constants, following
// constants defined as another constant of the module.
func stringConsts(files []srcFile) map[string]map[string]string {
	type ref struct{ pkg, name string }
	vals := map[string]map[string]string{}
	pending := map[ref]ast.Expr{}
	imports := map[ref]map[string]string{}
	for _, f := range files {
		if vals[f.pkg] == nil {
			vals[f.pkg] = map[string]string{}
		}
		imp := fileImports(f.file)
		for _, decl := range f.file.Decls {
			g, ok := decl.(*ast.GenDecl)
			if !ok || g.Tok != token.CONST {
				continue
			}
			for _, spec := range g.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, n := range vs.Names {
					if i < len(vs.Values) {
						pending[ref{f.pkg, n.Name}] = vs.Values[i]
						imports[ref{f.pkg, n.Name}] = imp
					}
				}
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for r, e := range pending {
			if v, ok := exprString(e, r.pkg, imports[r], vals); ok {
				vals[r.pkg][r.name] = v
				delete(pending, r)
				changed = true
			}
		}
	}
	return vals
}

// fileImports maps each import name of f to its path.
func fileImports(f *ast.File) map[string]string {
	m := map[string]string{}
	for _, s := range f.Imports {
		p, err := strconv.Unquote(s.Path.Value)
		if err != nil {
			continue
		}
		name := p[strings.LastIndex(p, "/")+1:]
		if s.Name != nil {
			name = s.Name.Name
		}
		m[name] = p
	}
	return m
}

// exprString resolves e, in package pkg with imports imp, to a string literal or constant.
func exprString(e ast.Expr, pkg string, imp map[string]string, vals map[string]map[string]string) (string, bool) {
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(x.Value)
		return s, err == nil
	case *ast.Ident:
		v, ok := vals[pkg][x.Name]
		return v, ok
	case *ast.SelectorExpr:
		id, ok := x.X.(*ast.Ident)
		if !ok {
			return "", false
		}
		v, ok := vals[imp[id.Name]][x.Sel.Name]
		return v, ok
	}
	return "", false
}

// funcKey names a function by its package import path and name (methods by method name).
type funcKey struct{ pkg, name string }

// publishedEvents returns every string a publishing function names. A function publishes
// when it calls an outbox emitter, a function named emit or publish, or (transitively) a
// module helper that takes the event type as a string parameter named typ and publishes it.
func publishedEvents(files []srcFile) map[string]bool {
	vals := stringConsts(files)
	type fnInfo struct {
		f       srcFile
		imp     map[string]string
		decl    *ast.FuncDecl
		takeTyp bool
	}
	var fns []fnInfo
	for _, f := range files {
		imp := fileImports(f.file)
		for _, decl := range f.file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
				fns = append(fns, fnInfo{f: f, imp: imp, decl: fn, takeTyp: hasTypParam(fn)})
			}
		}
	}
	helpers := map[funcKey]bool{}
	emits := map[*ast.FuncDecl]bool{}
	for changed := true; changed; {
		changed = false
		for _, fn := range fns {
			if emits[fn.decl] || !callsPublish(fn.decl.Body, fn.f.pkg, fn.imp, helpers) {
				continue
			}
			emits[fn.decl] = true
			changed = true
			if fn.takeTyp {
				helpers[funcKey{fn.f.pkg, fn.decl.Name.Name}] = true
			}
		}
	}
	out := map[string]bool{}
	for _, fn := range fns {
		if !emits[fn.decl] {
			continue
		}
		ast.Inspect(fn.decl.Body, func(n ast.Node) bool {
			if e, ok := n.(ast.Expr); ok {
				if v, ok := exprString(e, fn.f.pkg, fn.imp, vals); ok {
					out[v] = true
				}
			}
			return true
		})
	}
	return out
}

// hasTypParam reports whether fn takes a string parameter named typ: an event type.
func hasTypParam(fn *ast.FuncDecl) bool {
	for _, field := range fn.Type.Params.List {
		id, ok := field.Type.(*ast.Ident)
		if !ok || id.Name != "string" {
			continue
		}
		for _, n := range field.Names {
			if n.Name == "typ" {
				return true
			}
		}
	}
	return false
}

// callsPublish reports whether body calls a function named like an outbox emitter, or one
// of helpers. A method call resolves to the caller's package.
func callsPublish(body *ast.BlockStmt, pkg string, imp map[string]string, helpers map[funcKey]bool) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok || found {
			return !found
		}
		key := funcKey{pkg: pkg}
		switch fn := c.Fun.(type) {
		case *ast.Ident:
			key.name = fn.Name
		case *ast.SelectorExpr:
			key.name = fn.Sel.Name
			if x, ok := fn.X.(*ast.Ident); ok && imp[x.Name] != "" {
				key.pkg = imp[x.Name]
			}
		}
		if (publishCall.MatchString(key.name) && !strings.HasPrefix(strings.ToLower(key.name), "published")) || helpers[key] {
			found = true
		}
		return !found
	})
	return found
}

// boundEvents returns every event type a job feature binds to.
func boundEvents(t *testing.T) []string {
	t.Helper()
	s, err := Features()
	if err != nil {
		t.Fatal(err)
	}
	clk, err := clock.New("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	b := trigger.New(stubEnqueuerNone{}, clk, nil)
	if err := JobBindings(s, b); err != nil {
		t.Fatal(err)
	}
	return b.EventTypes()
}

// moduleRoot is the module root, three levels above this package.
func moduleRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(wd, "..", "..", "..")
}

// TestARC_I_030_EveryBoundEventHasPublisher guards against a feature waiting on an event
// that nothing puts on the outbox (EVENTS-9).
func TestARC_I_030_EveryBoundEventHasPublisher(t *testing.T) {
	// rule: ARC-I-030
	published := publishedEvents(parseSources(t, moduleRoot(t), "app", "kernel", "cmd"))
	tests := []struct {
		name   string
		events []string
	}{
		{"every event a job feature binds to", boundEvents(t)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.events) == 0 {
				t.Fatal("no events to check")
			}
			var missing []string
			for _, ev := range tc.events {
				if !published[ev] {
					missing = append(missing, ev)
				}
			}
			slices.Sort(missing)
			if len(missing) > 0 {
				t.Fatalf("events with no publisher: %v", missing)
			}
		})
	}
}
