package restorecheck

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/db/dbfake"
	"github.com/dagi377/counsel/kernel/registry"
)

var errBalance = errors.New("balances differ")

func pass(name string, ran *[]string) Check {
	return Check{Name: name, Run: func(context.Context, db.DB) error { *ran = append(*ran, name); return nil }}
}

func fail(name string, ran *[]string) Check {
	return Check{Name: name, Run: func(context.Context, db.DB) error { *ran = append(*ran, name); return errBalance }}
}

// rule: ARC-U-043
func TestARC_U_043_RunReportsEveryCheck(t *testing.T) {
	tests := []struct {
		name       string
		checks     func(ran *[]string) []Check
		wantRan    []string
		wantPassed bool
		wantFailed []string
		wantErr    error // per failed check, matched with errors.Is; nil means any
	}{
		{
			name:       "all pass",
			checks:     func(r *[]string) []Check { return []Check{pass("a", r), pass("b", r)} },
			wantRan:    []string{"a", "b"},
			wantPassed: true,
		},
		{
			name:       "first fails, rest still run",
			checks:     func(r *[]string) []Check { return []Check{fail("a", r), pass("b", r), pass("c", r)} },
			wantRan:    []string{"a", "b", "c"},
			wantFailed: []string{"a"},
			wantErr:    errBalance,
		},
		{
			name:       "several fail",
			checks:     func(r *[]string) []Check { return []Check{fail("z", r), pass("b", r), fail("a", r)} },
			wantRan:    []string{"z", "b", "a"},
			wantFailed: []string{"a", "z"},
			wantErr:    errBalance,
		},
		{
			name: "panic is a failure and later checks run",
			checks: func(r *[]string) []Check {
				return []Check{{Name: "boom", Run: func(context.Context, db.DB) error { panic("bad row") }}, pass("b", r)}
			},
			wantRan:    []string{"b"},
			wantFailed: []string{"boom"},
			wantErr:    ErrPanic,
		},
		{
			name:       "duplicate names both fail and do not run",
			checks:     func(r *[]string) []Check { return []Check{pass("a", r), pass("a", r), pass("b", r)} },
			wantRan:    []string{"b"},
			wantFailed: []string{"a", "a"},
			wantErr:    ErrDuplicate,
		},
		{
			name:       "missing run function fails",
			checks:     func(r *[]string) []Check { return []Check{{Name: "a"}, pass("b", r)} },
			wantRan:    []string{"b"},
			wantFailed: []string{"a"},
			wantErr:    ErrNoRun,
		},
		{
			name:       "empty name fails",
			checks:     func(r *[]string) []Check { return []Check{{Run: func(context.Context, db.DB) error { return nil }}} },
			wantFailed: []string{""},
			wantErr:    ErrNoName,
		},
		{
			name:   "no checks does not pass",
			checks: func(*[]string) []Check { return nil },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var ran []string
			checks := tc.checks(&ran)
			rep := Run(context.Background(), dbfake.New(), checks)
			if len(rep.Results) != len(checks) {
				t.Fatalf("results = %d, want %d", len(rep.Results), len(checks))
			}
			for i, res := range rep.Results {
				if res.Name != checks[i].Name {
					t.Errorf("result %d name = %q, want %q (order kept)", i, res.Name, checks[i].Name)
				}
				if !res.Passed() && tc.wantErr != nil && !errors.Is(res.Err, tc.wantErr) {
					t.Errorf("result %q err = %v, want %v", res.Name, res.Err, tc.wantErr)
				}
			}
			if strings.Join(ran, ",") != strings.Join(tc.wantRan, ",") {
				t.Errorf("ran = %v, want %v", ran, tc.wantRan)
			}
			if rep.Passed() != tc.wantPassed {
				t.Errorf("Passed = %v, want %v", rep.Passed(), tc.wantPassed)
			}
			if strings.Join(rep.Failed(), ",") != strings.Join(tc.wantFailed, ",") {
				t.Errorf("Failed = %v, want %v", rep.Failed(), tc.wantFailed)
			}
			if err := rep.Err(); (err == nil) != tc.wantPassed || (err != nil && !errors.Is(err, ErrFailed)) {
				t.Errorf("Err = %v, wantPassed %v", err, tc.wantPassed)
			}
		})
	}
}

func TestRunPassesDBToChecks(t *testing.T) {
	d := dbfake.New()
	var got db.DB
	rep := Run(context.Background(), d, []Check{{Name: "a", Run: func(_ context.Context, x db.DB) error { got = x; return nil }}})
	if !rep.Passed() || got != db.DB(d) {
		t.Fatalf("passed = %v, db passed = %v", rep.Passed(), got == db.DB(d))
	}
}

// rule: ARC-U-043
func TestARC_U_043_FromRegistry(t *testing.T) {
	tests := []struct {
		name     string
		fn       registry.CheckFunc
		wantPass bool
		wantErr  error
	}{
		{name: "pass", fn: func(context.Context) error { return nil }, wantPass: true},
		{name: "fail", fn: func(context.Context) error { return errBalance }, wantErr: errBalance},
		{name: "nil", fn: nil, wantErr: ErrNoRun},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rep := Run(context.Background(), dbfake.New(), []Check{FromRegistry(tc.name, tc.fn)})
			if rep.Passed() != tc.wantPass {
				t.Fatalf("Passed = %v, want %v", rep.Passed(), tc.wantPass)
			}
			if tc.wantErr != nil && !errors.Is(rep.Results[0].Err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", rep.Results[0].Err, tc.wantErr)
			}
		})
	}
}

func TestReportWrite(t *testing.T) {
	tests := []struct {
		name string
		rep  Report
		want string
	}{
		{
			name: "pass",
			rep:  Report{Results: []Result{{Name: "a"}, {Name: "b"}}},
			want: "PASS a 0s\nPASS b 0s\nrestore check: PASS 2/2\n",
		},
		{
			name: "fail",
			rep:  Report{Results: []Result{{Name: "a", Err: errBalance}, {Name: "b"}}},
			want: "FAIL a 0s: balances differ\nPASS b 0s\nrestore check: FAIL 1/2\n",
		},
		{
			name: "empty",
			rep:  Report{},
			want: "restore check: FAIL 0/0\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			if err := tc.rep.Write(&b); err != nil {
				t.Fatal(err)
			}
			if b.String() != tc.want {
				t.Fatalf("got %q, want %q", b.String(), tc.want)
			}
		})
	}
}
