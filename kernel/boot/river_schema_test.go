package boot

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The River step is not a numbered file, so make migration-lint never sees it. This runs
// the same lint over its grant SQL as if it were one, and checks the lint would catch a
// step that dropped its timeouts.
// rule: MIG-060
func TestMIG_060_RiverGrantStepPassesLint(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Fatalf("python3 is required by make check: %v", err)
	}
	lint, err := filepath.Abs("../../tools/migration_lint.py")
	if err != nil {
		t.Fatal(err)
	}
	noTimeouts := strings.NewReplacer(
		"SET lock_timeout = '2s';", "", "SET statement_timeout = '5min';", "").Replace(riverGrantSQL)
	tests := []struct {
		name     string
		sql      string
		wantPass bool
	}{
		{name: "grant step as shipped", sql: riverGrantSQL, wantPass: true},
		{name: "grant step without timeouts", sql: noTimeouts, wantPass: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "2300_river_grants.sql")
			if err := os.WriteFile(path, []byte("-- module: kernel/boot\n"+tt.sql), 0o600); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command(py, "-I", lint, path).CombinedOutput()
			var exit *exec.ExitError
			if err != nil && !errors.As(err, &exit) {
				t.Fatalf("lint: %v", err)
			}
			pass := err == nil && strings.TrimSpace(string(out)) == ""
			if pass != tt.wantPass {
				t.Fatalf("lint pass = %v, want %v; output:\n%s", pass, tt.wantPass, out)
			}
		})
	}
}
