package tools

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The restore scripts run here with DRY_RUN=1 and a fake kubectl first on PATH, so no
// cluster is needed. The fake logs every call, saves what `apply` reads, fails any call
// that contains $FAKE_FAIL, and sleeps $FAKE_SLEEP seconds on a `wait` that contains
// $FAKE_SLOW. It execs sleep, so stopping the call stops the sleep.

const cnpgDir = "../deploy/cnpg"

const fakeKubectl = `#!/bin/sh
printf '%s\n' "$*" >> "$KUBECTL_LOG"
case "$1" in apply) cat >> "$KUBECTL_LOG.apply" ;; esac
if [ -n "$FAKE_FAIL" ]; then case "$*" in *"$FAKE_FAIL"*) exit 1 ;; esac; fi
if [ -n "$FAKE_SLOW" ]; then case "$*" in wait*"$FAKE_SLOW"*) exec sleep "$FAKE_SLEEP" ;; esac; fi
exit 0
`

// fakeBin holds the fake kubectl. It is written once per test binary: macOS checks every
// new executable on its first run, which costs seconds per call on a fresh file.
var fakeBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "fake-kubectl")
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "kubectl"), []byte(fakeKubectl), 0o755)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake kubectl:", err)
		os.Exit(1)
	}
	fakeBin = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

type drillRun struct {
	calls  []string // one kubectl call per entry
	apply  string   // everything piped into kubectl apply
	stdout string
	stderr string
	code   int
}

// runScript runs a deploy/cnpg script with the fake kubectl and returns what happened.
func runScript(t *testing.T, script string, env []string, args ...string) drillRun {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not on PATH")
	}
	log := filepath.Join(t.TempDir(), "kubectl.log")
	cmd := exec.Command(bash, append([]string{filepath.Join(cnpgDir, script)}, args...)...)
	cmd.Env = append([]string{
		"PATH=" + fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"KUBECTL_LOG=" + log,
		"DRY_RUN=1",
		"DRILL_ID=t1",
		"HOME=" + t.TempDir(),
	}, env...)
	var out, errb strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errb
	run := drillRun{}
	if err := cmd.Run(); err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run %s: %v", script, err)
		}
		run.code = ee.ExitCode()
	}
	run.stdout, run.stderr = out.String(), errb.String()
	if b, err := os.ReadFile(log); err == nil {
		run.calls = strings.Split(strings.TrimSpace(string(b)), "\n")
	}
	if b, err := os.ReadFile(log + ".apply"); err == nil {
		run.apply = string(b)
	}
	return run
}

// rule: ARC-U-043
func TestARC_U_043_RestoreDrillSequence(t *testing.T) {
	const ns = "scratch-acme-t1"
	full := []string{
		"create namespace " + ns,
		"label namespace " + ns + " counsel.io/cell=acme counsel.io/drill=restore",
		"apply -n " + ns + " -f -",
		"wait -n " + ns + " --for=condition=Ready cluster/counsel-acme-pg-restore --timeout=45m",
		"run restore-check -n " + ns + " --image=ghcr.io/dagi377/counsel:0.1.0 --restart=Never --rm -i --overrides=",
		"delete namespace " + ns + " --ignore-not-found --wait=false",
	}
	tests := []struct {
		name     string
		args     []string
		fail     string
		wantCode int
		want     []string // prefixes of the kubectl calls, in order
		wantLast string
	}{
		{name: "pass", args: []string{"acme", "2026-10-11T03:00:00Z"}, want: full, wantLast: "restore drill acme: PASS"},
		{
			name: "cluster never ready: no check, namespace still deleted", args: []string{"acme", "2026-10-11T03:00:00Z"},
			fail: "condition=Ready", wantCode: 1,
			want: []string{full[0], full[1], full[2], full[3], full[5]}, wantLast: "restore drill acme: FAIL",
		},
		{
			name: "restore check fails", args: []string{"acme", "2026-10-11T03:00:00Z"},
			fail: "restore-check", wantCode: 1, want: full, wantLast: "restore drill acme: FAIL",
		},
		{name: "missing target time", args: []string{"acme"}, wantCode: 2},
		{name: "bad cell", args: []string{"Acme;rm", "2026-10-11T03:00:00Z"}, wantCode: 2},
		{name: "bad target time", args: []string{"acme", "yesterday"}, wantCode: 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			run := runScript(t, "restore-drill.sh", []string{"FAKE_FAIL=" + tc.fail}, tc.args...)
			if run.code != tc.wantCode {
				t.Fatalf("exit = %d, want %d\nstderr: %s", run.code, tc.wantCode, run.stderr)
			}
			if len(run.calls) != len(tc.want) {
				t.Fatalf("kubectl calls = %q, want %d calls", run.calls, len(tc.want))
			}
			for i, w := range tc.want {
				if !strings.HasPrefix(run.calls[i], w) {
					t.Errorf("call %d = %q, want prefix %q", i, run.calls[i], w)
				}
			}
			if tc.wantLast != "" && !strings.HasPrefix(lastLine(run.stdout), tc.wantLast) {
				t.Errorf("last line = %q, want %q", lastLine(run.stdout), tc.wantLast)
			}
			if tc.wantCode == 0 && !strings.Contains(run.stderr, "+ kubectl create namespace "+ns) {
				t.Errorf("DRY_RUN did not echo commands: %s", run.stderr)
			}
		})
	}
}

// rule: ARC-U-043
func TestARC_U_043_RestoreClusterIsPITR(t *testing.T) {
	run := runScript(t, "restore-drill.sh", nil, "acme", "2026-10-11T03:00:00Z")
	if run.code != 0 {
		t.Fatalf("exit = %d: %s", run.code, run.stderr)
	}
	var c map[string]any
	if err := yaml.Unmarshal([]byte(run.apply), &c); err != nil {
		t.Fatalf("applied manifest: %v\n%s", err, run.apply)
	}
	tests := []struct {
		path []string
		want any
	}{
		{[]string{"kind"}, "Cluster"},
		{[]string{"metadata", "name"}, "counsel-acme-pg-restore"},
		{[]string{"metadata", "namespace"}, "scratch-acme-t1"},
		{[]string{"metadata", "labels", "counsel.io/cell"}, "acme"},
		{[]string{"spec", "bootstrap", "recovery", "source"}, "origin"},
		{[]string{"spec", "bootstrap", "recovery", "recoveryTarget", "targetTime"}, "2026-10-11T03:00:00Z"},
		{[]string{"spec", "storage", "storageClass"}, "counsel-acme-encrypted"},
	}
	for _, tc := range tests {
		if got := dig(c, tc.path...); got != tc.want {
			t.Errorf("%s = %v, want %v", strings.Join(tc.path, "."), got, tc.want)
		}
	}
	ext, _ := dig(c, "spec", "externalClusters").([]any)
	if len(ext) != 1 {
		t.Fatalf("externalClusters = %v, want one", ext)
	}
	store := dig(ext[0], "barmanObjectStore")
	for _, tc := range []struct {
		path []string
		want any
	}{
		{[]string{"destinationPath"}, "s3://counsel-acme-wal-dr/"},
		{[]string{"endpointURL"}, "https://s3.ca-west-1.amazonaws.com"},
		{[]string{"serverName"}, "counsel-acme-pg"},
	} {
		if got := dig(store, tc.path...); got != tc.want {
			t.Errorf("barmanObjectStore.%s = %v, want %v", strings.Join(tc.path, "."), got, tc.want)
		}
	}
	if strings.Contains(run.apply, "@") {
		t.Errorf("unfilled placeholder in applied manifest:\n%s", run.apply)
	}
	// The check pod reads the restored database through the CloudNativePG app secret.
	for _, call := range run.calls {
		if strings.HasPrefix(call, "run restore-check") && !strings.Contains(call, `"name":"counsel-acme-pg-restore-app","key":"uri"`) {
			t.Errorf("restore-check pod does not read the restored cluster secret: %s", call)
		}
	}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

// rule: ARC-U-043
func TestARC_U_043_ParallelBudget(t *testing.T) {
	tests := []struct {
		name      string
		cells     string
		env       []string
		wantCode  int
		wantStart []string // start order, which is the file's priority order
		wantCells []string // report lines, prefix match, in priority order
		wantMax   int      // most drills running at once
		wantGone  []string // namespaces the drills deleted
	}{
		{
			name:      "slow cell over budget fails, others pass",
			cells:     "# priority order\nfirst\n\nslow\nlast # trailing comment\n",
			env:       []string{"BUDGET_SECONDS=2", "POLL_SECONDS=0.2", "FAKE_SLOW=cluster/counsel-slow-", "FAKE_SLEEP=30"},
			wantCode:  1,
			wantStart: []string{"first", "slow", "last"},
			wantCells: []string{"first PASS", "slow FAIL", "last PASS", "restore all: FAIL 2/3"},
			wantGone:  []string{"scratch-first-t1", "scratch-slow-t1", "scratch-last-t1"},
		},
		{
			name:      "ten cells, at most eight at once",
			cells:     "c1\nc2\nc3\nc4\nc5\nc6\nc7\nc8\nc9\nc10\n",
			env:       []string{"BUDGET_SECONDS=60", "POLL_SECONDS=0.2", "FAKE_SLOW=cluster/", "FAKE_SLEEP=1"},
			wantStart: []string{"c1", "c2", "c3", "c4", "c5", "c6", "c7", "c8", "c9", "c10"},
			wantCells: []string{"c1 PASS", "c2 PASS", "c3 PASS", "c4 PASS", "c5 PASS", "c6 PASS", "c7 PASS", "c8 PASS", "c9 PASS", "c10 PASS", "restore all: PASS 10/10"},
			wantMax:   8,
		},
		{
			name:      "PARALLEL=1 runs one at a time",
			cells:     "a\nb\n",
			env:       []string{"PARALLEL=1", "POLL_SECONDS=0.2"},
			wantStart: []string{"a", "b"},
			wantCells: []string{"a PASS", "b PASS", "restore all: PASS 2/2"},
			wantMax:   1,
		},
		{name: "PARALLEL above 8 refused", cells: "a\n", env: []string{"PARALLEL=9"}, wantCode: 2},
		{name: "duplicate cell refused", cells: "a\nb\na\n", wantCode: 2},
		{name: "empty list refused", cells: "# none\n", wantCode: 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cells := filepath.Join(dir, "cells.txt")
			if err := os.WriteFile(cells, []byte(tc.cells), 0o644); err != nil {
				t.Fatal(err)
			}
			env := append([]string{"LOG_DIR=" + dir}, tc.env...)
			run := runScript(t, "restore-all.sh", env, cells, "2026-10-11T03:00:00Z")
			if run.code != tc.wantCode {
				t.Fatalf("exit = %d, want %d\nstdout: %s\nstderr: %s", run.code, tc.wantCode, run.stdout, run.stderr)
			}
			var starts, report []string
			running, most := 0, 0
			for _, l := range strings.Split(strings.TrimSpace(run.stdout), "\n") {
				switch {
				case strings.HasPrefix(l, "start "):
					starts = append(starts, strings.TrimPrefix(l, "start "))
					running++
					most = max(most, running)
				case strings.HasPrefix(l, "done "):
					running--
				case l != "":
					report = append(report, l)
				}
			}
			if strings.Join(starts, ",") != strings.Join(tc.wantStart, ",") {
				t.Errorf("start order = %v, want %v", starts, tc.wantStart)
			}
			if len(report) != len(tc.wantCells) {
				t.Fatalf("report = %q, want %d lines", report, len(tc.wantCells))
			}
			for i, w := range tc.wantCells {
				if !strings.HasPrefix(report[i], w) {
					t.Errorf("report line %d = %q, want prefix %q", i, report[i], w)
				}
			}
			if tc.wantMax != 0 && most != tc.wantMax {
				t.Errorf("most running at once = %d, want %d", most, tc.wantMax)
			}
			for _, l := range report {
				if !strings.HasPrefix(l, "restore all:") && strings.Contains(l, " FAIL ") && !strings.HasSuffix(l, " over budget") {
					t.Errorf("failed cell not marked over budget: %q", l)
				}
			}
			for _, ns := range tc.wantGone {
				if !slices.Contains(run.calls, "delete namespace "+ns+" --ignore-not-found --wait=false") {
					t.Errorf("namespace %s not deleted; calls: %q", ns, run.calls)
				}
			}
		})
	}
}
