package identity

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/dagi377/counsel/kernel/registry"
)

var errResolve = errors.New("resolver down")

// wallResolver is an app resolver that hides the subjects in walled from everyone and
// records every call.
type wallResolver struct {
	walled map[string]bool
	fail   bool
	calls  []Subject
}

func (w *wallResolver) Visible(_ context.Context, _ Principal, s Subject) (bool, error) {
	w.calls = append(w.calls, s)
	if w.fail {
		return false, errResolve
	}
	return !w.walled[s.ID], nil
}

func TestCanCallsSubjectResolver(t *testing.T) {
	tier1 := Principal{Kind: registry.PrincipalUser, UserID: "u1"}
	tests := []struct {
		name       string
		capability string
		subject    Subject
		fail       bool
		wantOK     bool
		wantErr    error
		wantCalls  int
		wantStatus int
	}{
		{"visible and held", "ticket.read", Subject{Kind: "item", ID: "open"}, false, true, nil, 1, 0},
		{"visible, not held is 403", "ticket.close", Subject{Kind: "item", ID: "open"}, false, false, ErrForbidden, 1, http.StatusForbidden},
		{"walled and held is 404", "ticket.read", Subject{Kind: "item", ID: "walled"}, false, false, ErrNotVisible, 1, http.StatusNotFound},
		{"walled, not held is 404 not 403", "ticket.close", Subject{Kind: "item", ID: "walled"}, false, false, ErrNotVisible, 1, http.StatusNotFound},
		{"zero subject skips resolver", "ticket.read", Subject{}, false, true, nil, 0, 0},
		{"resolver error surfaces", "ticket.read", Subject{Kind: "item", ID: "open"}, true, false, errResolve, 1, 0},
		{"unregistered kind", "ticket.read", Subject{Kind: "nobody", ID: "x"}, false, false, ErrUnknownSubjectKind, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			roles, _ := bootHelpdesk(t)
			wall := &wallResolver{walled: map[string]bool{"walled": true}, fail: tt.fail}
			roles.SubjectKind(registry.SubjectKind{ID: "item"}, wall)
			c := NewChecker(roles, StaticAssignments{"u1": {"agent_tier1"}})
			ctx := context.Background()

			err := Require(ctx, c, tier1, tt.capability, tt.subject)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil && err != nil) {
				t.Fatalf("Require err = %v, want %v", err, tt.wantErr)
			}
			if len(wall.calls) != tt.wantCalls {
				t.Fatalf("resolver calls = %d, want %d", len(wall.calls), tt.wantCalls)
			}
			if tt.wantCalls > 0 && wall.calls[0] != tt.subject {
				t.Fatalf("resolver saw %v, want %v", wall.calls[0], tt.subject)
			}
			ok, _ := c.Can(ctx, tier1, tt.capability, tt.subject)
			if ok != tt.wantOK {
				t.Fatalf("Can = %v, want %v", ok, tt.wantOK)
			}
			p := Problem(err)
			switch {
			case tt.wantStatus == 0 && p != nil:
				t.Fatalf("Problem = %+v, want nil", p)
			case tt.wantStatus != 0 && (p == nil || p.Status != tt.wantStatus):
				t.Fatalf("Problem = %+v, want status %d", p, tt.wantStatus)
			}
		})
	}
}

func TestProblemMapping(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"not visible", ErrNotVisible, http.StatusNotFound},
		{"wrapped not visible", errors.Join(errors.New("ctx"), ErrNotVisible), http.StatusNotFound},
		{"forbidden", ErrForbidden, http.StatusForbidden},
		{"no principal", ErrNoPrincipal, http.StatusUnauthorized},
		{"unknown capability", ErrUnknownCapability, 0},
		{"nil", nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Problem(tt.err)
			got := 0
			if p != nil {
				got = p.Status
			}
			if got != tt.want {
				t.Fatalf("Problem(%v) status = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}
