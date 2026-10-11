package tools

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/dagi377/counsel"

// storeOwner returns the module that owns imp when imp is a store package
// (".../<m>/store" or below it), and "" otherwise.
func storeOwner(imp string) string {
	if strings.HasSuffix(imp, "/store") {
		return strings.TrimSuffix(imp, "/store")
	}
	if i := strings.Index(imp, "/store/"); i >= 0 {
		return imp[:i]
	}
	return ""
}

// layeringViolation reports whether a file in package pkg may not import imp: a module's
// store package is importable only from that module (the owner and its subpackages).
func layeringViolation(pkg, imp string) bool {
	owner := storeOwner(imp)
	if owner == "" || !strings.HasPrefix(owner, modulePath+"/") {
		return false
	}
	return pkg != owner && !strings.HasPrefix(pkg, owner+"/")
}

func TestLayeringViolation(t *testing.T) {
	m := modulePath
	tests := []struct {
		name string
		pkg  string
		imp  string
		want bool
	}{
		{name: "own store", pkg: m + "/kernel/ledger", imp: m + "/kernel/ledger/store", want: false},
		{name: "own subpackage", pkg: m + "/kernel/ledger/post", imp: m + "/kernel/ledger/store", want: false},
		{name: "store itself", pkg: m + "/kernel/ledger/store", imp: m + "/kernel/ledger/store/sqlc", want: false},
		{name: "other kernel store", pkg: m + "/kernel/gate", imp: m + "/kernel/ledger/store", want: true},
		{name: "app into kernel store", pkg: m + "/app/legal/billing", imp: m + "/kernel/ledger/store", want: true},
		{name: "sibling app store", pkg: m + "/app/legal/billing/bill", imp: m + "/app/legal/trust/store", want: true},
		{name: "prefix lookalike", pkg: m + "/kernel/ledgerx", imp: m + "/kernel/ledger/store", want: true},
		{name: "not a store", pkg: m + "/kernel/gate", imp: m + "/kernel/ledger", want: false},
		{name: "external store", pkg: m + "/kernel/gate", imp: "example.com/x/store", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := layeringViolation(tt.pkg, tt.imp); got != tt.want {
				t.Fatalf("layeringViolation(%q, %q) = %v, want %v", tt.pkg, tt.imp, got, tt.want)
			}
		})
	}
}

// TestNoCrossModuleStoreImports scans kernel/ and app/legal/: no file imports another
// module's store package.
func TestNoCrossModuleStoreImports(t *testing.T) {
	for _, dir := range []string{"../kernel", "../app/legal"} {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
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
			f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel("..", filepath.Dir(p))
			if err != nil {
				return err
			}
			pkg := path.Join(modulePath, filepath.ToSlash(rel))
			for _, imp := range f.Imports {
				ip, _ := strconv.Unquote(imp.Path.Value)
				if layeringViolation(pkg, ip) {
					t.Errorf("%s imports %s: only the owning module may import its store", p, ip)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
}
