package schemas

import (
	"bytes"
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dagi377/counsel/kernel/registry"
)

// specDir is the source of truth the embedded copy must match.
const specDir = "../../../spec/kernel"

func schemaNames(t *testing.T, fsys fs.FS) []string {
	t.Helper()
	names, err := fs.Glob(fsys, "*.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(names)
	return names
}

// diff names every file that is missing, extra or different between spec and embedded.
func diff(spec, embedded fs.FS) ([]string, error) {
	var problems []string
	specNames, err := fs.Glob(spec, "*.schema.json")
	if err != nil {
		return nil, err
	}
	embNames, err := fs.Glob(embedded, "*.schema.json")
	if err != nil {
		return nil, err
	}
	for _, n := range specNames {
		if !slices.Contains(embNames, n) {
			problems = append(problems, n+": in spec/kernel, not embedded")
			continue
		}
		a, err := fs.ReadFile(spec, n)
		if err != nil {
			return nil, err
		}
		b, err := fs.ReadFile(embedded, n)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(a, b) {
			problems = append(problems, n+": embedded copy differs from spec/kernel")
		}
	}
	for _, n := range embNames {
		if !slices.Contains(specNames, n) {
			problems = append(problems, n+": embedded, not in spec/kernel")
		}
	}
	slices.Sort(problems)
	return problems, nil
}

func TestEmbeddedSchemasMatchSpec(t *testing.T) {
	problems, err := diff(os.DirFS(specDir), FS)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		t.Error(p)
	}
	if len(schemaNames(t, FS)) == 0 {
		t.Fatal("no schemas embedded")
	}
}

// The drift check itself names the file for each kind of drift.
func TestDiffNamesTheFile(t *testing.T) {
	spec := os.DirFS(specDir)
	names := schemaNames(t, spec)
	first := names[0]
	tests := []struct {
		name   string
		mutate func(m fstestMap)
		want   string
	}{
		{"identical", func(fstestMap) {}, ""},
		{"changed bytes", func(m fstestMap) { m[first] = append(m[first], '\n') }, first + ": embedded copy differs"},
		{"missing file", func(m fstestMap) { delete(m, first) }, first + ": in spec/kernel, not embedded"},
		{"extra file", func(m fstestMap) { m["extra.schema.json"] = []byte("{}") }, "extra.schema.json: embedded, not in spec/kernel"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := fstestMap{}
			for _, n := range names {
				b, err := fs.ReadFile(spec, n)
				if err != nil {
					t.Fatal(err)
				}
				m[n] = b
			}
			tt.mutate(m)
			problems, err := diff(spec, m.fs())
			if err != nil {
				t.Fatal(err)
			}
			got := strings.Join(problems, "; ")
			if tt.want == "" && got != "" || tt.want != "" && !strings.Contains(got, tt.want) {
				t.Fatalf("diff = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEmbeddedSchemasLoad(t *testing.T) {
	if _, err := registry.LoadSchemas(FS); err != nil {
		t.Fatalf("LoadSchemas(FS): %v", err)
	}
}

// fstestMap is a mutable set of schema files for the drift tests.
type fstestMap map[string][]byte

func (m fstestMap) fs() fs.FS {
	out := fstest.MapFS{}
	for n, b := range m {
		out[n] = &fstest.MapFile{Data: b}
	}
	return out
}
