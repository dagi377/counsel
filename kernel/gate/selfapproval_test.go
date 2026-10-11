package gate

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelfApprovalDefaultDeny(t *testing.T) {
	ctx := context.Background()
	g := Gate{ID: "g1", Kind: "desk.refund"}
	human := Principal{Kind: PrincipalUser, UserID: "B"}
	agent := Principal{Kind: PrincipalAgent, ActingFor: "B", RunID: "r1"}
	tests := []struct {
		name   string
		policy SelfApprovalPolicy // installed with SetSelfApprovalPolicy; nil means none
		by     Principal
		want   bool
	}{
		{"no policy denies a human", nil, human, false},
		{"app policy allows a human", allowSelf, human, true},
		{"app policy never allows an agent", allowSelf, agent, false},
		{"app policy never allows a guest", allowSelf, Principal{Kind: PrincipalGuest, UserID: "B"}, false},
		{"app policy never allows an empty user", allowSelf, Principal{Kind: PrincipalUser}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := NewPG()
			if tc.policy != nil {
				e.SetSelfApprovalPolicy(tc.policy)
			}
			if e.SelfApproval() == nil {
				t.Fatal("SelfApproval returned nil; the hook must always be set")
			}
			got, _ := MaySelfApprove(ctx, e.SelfApproval(), g, tc.by)
			if got != tc.want {
				t.Fatalf("MaySelfApprove = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSelfApprovalResetToNilDenies(t *testing.T) {
	e := NewPG()
	e.SetSelfApprovalPolicy(allowSelf)
	e.SetSelfApprovalPolicy(nil)
	if ok, reason := MaySelfApprove(context.Background(), e.SelfApproval(), Gate{}, Principal{Kind: PrincipalUser, UserID: "B"}); ok || reason == "" {
		t.Fatalf("after clearing the policy: allowed=%v reason=%q, want denied with a reason", ok, reason)
	}
	if ok, _ := MaySelfApprove(context.Background(), nil, Gate{}, Principal{Kind: PrincipalUser, UserID: "B"}); ok {
		t.Fatal("nil policy allowed self-approval")
	}
}

// The engine's default hook keeps the preparer out of Decide.
func TestSelfApprovalDefaultBlocksDecide(t *testing.T) {
	tests := []struct {
		name    string
		install SelfApprovalPolicy
		wantErr error
	}{
		{"default hook", nil, ErrSelfApproval},
		{"app policy", allowSelf, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := NewPG()
			if tc.install != nil {
				e.SetSelfApprovalPolicy(tc.install)
			}
			in := baseDecide()
			in.by.UserID = "B" // the preparer
			in.d.SelfApprovalReason = "sole practitioner"
			in.policy = e.SelfApproval()
			_, err := checkDecision(context.Background(), in)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("checkDecision err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// Only the app supplies a self-approval policy: no non-test kernel file calls
// SetSelfApprovalPolicy (the composition root passes the app's policy in).
func TestSelfApprovalOnlyAppInstalls(t *testing.T) {
	kernel, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	var callers []string
	err = filepath.WalkDir(kernel, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "SetSelfApprovalPolicy" {
				rel, _ := filepath.Rel(kernel, path)
				callers = append(callers, rel)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range callers {
		t.Errorf("kernel/%s calls SetSelfApprovalPolicy; only the app may install a self-approval policy", c)
	}
}
