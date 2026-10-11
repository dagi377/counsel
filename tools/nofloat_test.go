package tools

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// floatIdents are the predeclared floating-point and complex types.
var floatIdents = map[string]bool{"float32": true, "float64": true, "complex64": true, "complex128": true}

// floatCalls are selector names that read or write a float.
var floatCalls = map[string]bool{"ParseFloat": true, "FormatFloat": true, "AppendFloat": true, "Float64": true, "Float32": true}

// floatUses reports each float use in one Go source: a float type name, a float-producing
// call, or a literal with a fractional part. Integral literals such as 1e6 are integers.
func floatUses(fset *token.FileSet, name string, src any) ([]string, error) {
	f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}
	var out []string
	add := func(n ast.Node, what string) {
		out = append(out, fmt.Sprintf("%s: %s", fset.Position(n.Pos()), what))
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Ident:
			if floatIdents[x.Name] {
				add(x, x.Name)
			}
		case *ast.SelectorExpr:
			if floatCalls[x.Sel.Name] {
				add(x, x.Sel.Name)
			}
		case *ast.BasicLit:
			if x.Kind == token.FLOAT {
				if r, ok := new(big.Rat).SetString(x.Value); !ok || !r.IsInt() {
					add(x, "float literal "+x.Value)
				}
			} else if x.Kind == token.IMAG {
				add(x, "imaginary literal "+x.Value)
			}
		}
		return true
	})
	return out, nil
}

func TestFloatUses(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want int
	}{
		{name: "int only", src: "package p\nvar a int64 = 1e6\nvar b = 2_000\n", want: 0},
		{name: "float64 var", src: "package p\nvar a float64\n", want: 1},
		{name: "float32 conversion", src: "package p\nvar a = float32(1)\n", want: 1},
		{name: "fraction literal", src: "package p\nvar a = 12.5\n", want: 1},
		{name: "integral exponent literal", src: "package p\nconst a = 1.5e2\n", want: 0},
		{name: "parse float", src: "package p\nimport \"strconv\"\nvar a, _ = strconv.ParseFloat(\"1\", 64)\n", want: 1},
		{name: "json number Float64", src: "package p\nimport \"encoding/json\"\nvar a, _ = json.Number(\"1\").Float64()\n", want: 1},
		{name: "complex", src: "package p\nvar a complex128 = 1i\n", want: 2},
		{name: "field named after float in a string", src: "package p\nvar a = \"float64\"\n", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := floatUses(token.NewFileSet(), "x.go", tt.src)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tt.want {
				t.Fatalf("floatUses = %v, want %d uses", got, tt.want)
			}
		})
	}
}

func readFloatAllow(t *testing.T) map[string]bool {
	t.Helper()
	f, err := os.Open("nofloat_allow.txt")
	if err != nil {
		t.Fatalf("open allowlist: %v", err)
	}
	defer f.Close()
	allow := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line, _, _ := strings.Cut(sc.Text(), "#")
		if line = strings.TrimSpace(line); line != "" {
			allow[line] = true
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read allowlist: %v", err)
	}
	return allow
}

// TestNoFloatInKernelOrApp: money is int64 cents and rates are int ppm, so no file in
// kernel/ or app/ uses floating point unless tools/nofloat_allow.txt lists it.
func TestNoFloatInKernelOrApp(t *testing.T) {
	allow := readFloatAllow(t)
	used := map[string]bool{}
	fset := token.NewFileSet()
	for _, dir := range []string{"kernel", "app"} {
		root := filepath.Join("..", dir)
		if _, err := os.Stat(root); os.IsNotExist(err) {
			continue
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
			if !strings.HasSuffix(p, ".go") {
				return nil
			}
			rel, err := filepath.Rel("..", p)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			uses, err := floatUses(fset, p, nil)
			if err != nil {
				return err
			}
			if len(uses) == 0 {
				return nil
			}
			used[rel] = true
			if !allow[rel] {
				for _, u := range uses {
					t.Errorf("%s: floats are not allowed in kernel/ or app/; use money.Cents or money.PPM (or list a display-only file in tools/nofloat_allow.txt)", u)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	var stale []string
	for p := range allow {
		if !used[p] {
			stale = append(stale, p)
		}
	}
	sort.Strings(stale)
	for _, p := range stale {
		t.Errorf("tools/nofloat_allow.txt: %s no longer uses floats; remove the entry (the list only shrinks)", p)
	}
}
