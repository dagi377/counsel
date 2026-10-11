package boot

import (
	"context"
	"errors"
	"testing"

	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/identity/identityfake"
	"github.com/dagi377/counsel/kernel/registry"
)

const (
	capRun   = "desk.report_run"
	capOther = "desk.report_other"
)

func reportAccess() *identityfake.Fake {
	f := identityfake.New()
	f.AddCapability(registry.Capability{ID: capRun})
	f.AddCapability(registry.Capability{ID: capOther})
	f.AddRole(registry.Role{ID: "viewer", Capabilities: []string{capRun, capOther}})
	f.Assign("u-viewer", "viewer")
	return f
}

// brokenAccess errors on every check, and claims to allow, to prove errors never allow.
type brokenAccess struct{ identity.Access }

var errChecker = errors.New("checker down")

func (brokenAccess) Can(context.Context, identity.Principal, string, identity.Subject) (bool, error) {
	return true, errChecker
}

// agentResolver resolves "run-1" to an agent acting for u-viewer with grant, everything
// else to a user, and fails for "ghost".
func agentResolver(grant ...string) PrincipalResolver {
	return func(ctx context.Context, id string) (identity.Principal, error) {
		switch id {
		case "run-1":
			return identity.Principal{Kind: registry.PrincipalAgent, RunID: "run-1", ActingFor: "u-viewer", Grant: grant}, nil
		case "run-nobody":
			return identity.Principal{Kind: registry.PrincipalAgent, RunID: "run-nobody", Grant: grant}, nil
		case "ghost":
			return identity.Principal{}, identity.ErrNoPrincipal
		}
		return userPrincipal(ctx, id)
	}
}

func TestReportAuthFailsClosed(t *testing.T) {
	tests := []struct {
		name    string
		access  identity.Access
		resolve PrincipalResolver
		user    string
		cap     string
		want    bool
		wantErr error
	}{
		{"user holding the capability", reportAccess(), nil, "u-viewer", capRun, true, nil},
		{"unknown user", reportAccess(), nil, "u-stranger", capRun, false, nil},
		{"empty user", reportAccess(), nil, "", capRun, false, identity.ErrNoPrincipal},
		{"empty capability", reportAccess(), nil, "u-viewer", "", false, identity.ErrUnknownCapability},
		{"unregistered capability", reportAccess(), nil, "u-viewer", "desk.never_registered", false, identity.ErrUnknownCapability},
		{"checker error never allows", brokenAccess{}, nil, "u-viewer", capRun, false, errChecker},
		{"nil checker", nil, nil, "u-viewer", capRun, false, ErrNoAccess},
		{"resolver fails", reportAccess(), agentResolver(capRun), "ghost", capRun, false, identity.ErrNoPrincipal},
		{"agent with no acting-for user", reportAccess(), agentResolver(capRun), "run-nobody", capRun, false, identity.ErrNoPrincipal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := newReportAuth(tt.access, tt.resolve).Can(context.Background(), tt.user, tt.cap)
			if got != tt.want {
				t.Fatalf("Can = %v, want %v (err %v)", got, tt.want, err)
			}
			if tt.wantErr == nil && err != nil || tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("Can err = %v, want %v", err, tt.wantErr)
			}
			if err != nil && got {
				t.Fatal("allowed on error")
			}
		})
	}
}

// rule: ARC-I-036
// An agent run is authorized only for report capabilities in its grant, even when the user
// it acts for holds more.
func TestARC_I_036_ReportAuthAgentOnlyGranted(t *testing.T) {
	tests := []struct {
		name  string
		grant []string
		cap   string
		want  bool
	}{
		{"granted capability", []string{capRun}, capRun, true},
		{"capability the user holds but the run was not granted", []string{capRun}, capOther, false},
		{"empty grant", nil, capRun, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := newReportAuth(reportAccess(), agentResolver(tt.grant...)).Can(context.Background(), "run-1", tt.cap)
			if err != nil {
				t.Fatalf("Can: %v", err)
			}
			if got != tt.want {
				t.Fatalf("Can = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReportAuthorizerChoice(t *testing.T) {
	tests := []struct {
		name   string
		access identity.Access
		want   bool // whether u-viewer may run capRun
	}{
		{"identity configured", reportAccess(), true},
		{"no identity keeps denyAll", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := reportAuthorizer(tt.access, nil)
			got, err := a.Can(context.Background(), "u-viewer", capRun)
			if err != nil {
				t.Fatalf("Can: %v", err)
			}
			if got != tt.want {
				t.Fatalf("Can = %v, want %v", got, tt.want)
			}
			if _, isDeny := a.(denyAll); isDeny != (tt.access == nil) {
				t.Fatalf("authorizer = %T", a)
			}
		})
	}
}
