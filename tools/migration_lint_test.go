package tools

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The migration lint is a Python script (tools/migration_lint.py) so CI and developers can run
// it without building Go. These tests run it over tools/testdata/migration_lint: every file in
// good/ must pass, and every file in bad/ must fail. A bad file's first line is
// "-- expect: <rule> <fragment>"; the lint output must name that rule and contain the fragment,
// and every rule it reports must be one the file expects.

const migrationLintData = "testdata/migration_lint"

var (
	expectLine = regexp.MustCompile(`^-- expect: ([A-Z]+-\d+) (.+)$`)
	lintLine   = regexp.MustCompile(`^(\S+):(\d+): ([A-Z]+-\d+): (.+)$`)
)

type lintExpect struct {
	rule, fragment string
}

// lintFile runs the lint on one file and returns its output lines and exit code.
func lintFile(t *testing.T, path string) ([]string, int) {
	t.Helper()
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Fatalf("python3 is required by make check: %v", err)
	}
	cmd := exec.Command(py, "-I", "migration_lint.py", path)
	out, err := cmd.Output()
	code := 0
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code = exit.ExitCode()
		if code != 1 {
			t.Fatalf("lint %s: exit %d: %s", path, code, exit.Stderr)
		}
	} else if err != nil {
		t.Fatalf("lint %s: %v", path, err)
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines, code
}

// expectations reads the leading "-- expect:" lines of a bad file.
func expectations(t *testing.T, path string) []lintExpect {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	var out []lintExpect
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		m := expectLine.FindStringSubmatch(sc.Text())
		if m == nil {
			break
		}
		out = append(out, lintExpect{rule: m[1], fragment: m[2]})
	}
	if len(out) == 0 {
		t.Fatalf("%s: bad file has no '-- expect: <rule> <fragment>' header", path)
	}
	return out
}

func lintCases(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(migrationLintData, dir, "*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no %s migration lint cases: %v", dir, err)
	}
	sort.Strings(files)
	return files
}

// checkLintCases runs every good file and every bad file whose expectations include rule.
func checkLintCases(t *testing.T, rule string) {
	t.Helper()
	ran := 0
	for _, path := range lintCases(t, "bad") {
		exp := expectations(t, path)
		wanted := map[string]bool{}
		mine := false
		for _, e := range exp {
			wanted[e.rule] = true
			mine = mine || e.rule == rule
		}
		if !mine {
			continue
		}
		ran++
		t.Run("bad/"+filepath.Base(path), func(t *testing.T) {
			lines, code := lintFile(t, path)
			if code != 1 {
				t.Fatalf("exit %d, want 1; output:\n%s", code, strings.Join(lines, "\n"))
			}
			for _, l := range lines {
				m := lintLine.FindStringSubmatch(l)
				if m == nil {
					t.Errorf("malformed lint line %q", l)
					continue
				}
				if m[1] != path {
					t.Errorf("line %q names file %q, want %q", l, m[1], path)
				}
				if !wanted[m[3]] {
					t.Errorf("unexpected rule %s: %q", m[3], l)
				}
			}
			for _, e := range exp {
				found := false
				for _, l := range lines {
					if strings.Contains(l, ": "+e.rule+": ") && strings.Contains(l, e.fragment) {
						found = true
					}
				}
				if !found {
					t.Errorf("want a %s line containing %q; output:\n%s", e.rule, e.fragment, strings.Join(lines, "\n"))
				}
			}
		})
	}
	if ran == 0 {
		t.Fatalf("no bad case expects %s", rule)
	}
	for _, path := range lintCases(t, "good") {
		t.Run("good/"+filepath.Base(path), func(t *testing.T) {
			lines, code := lintFile(t, path)
			if code != 0 || len(lines) != 0 {
				t.Fatalf("exit %d, want 0 and no output; got:\n%s", code, strings.Join(lines, "\n"))
			}
		})
	}
}

// rule: MIG-060
// rule: ARC-U-017
func TestMIG_060_ExpandContract(t *testing.T) {
	checkLintCases(t, "MIG-060")
}
