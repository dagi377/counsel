package matter

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/conflict"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

func strp(s string) *string { return &s }

// rule: BIL-T-011
// rule: BIL-T-012
func TestBIL_T_011_PGUpdaterValidChange(t *testing.T) {
	tests := []struct {
		name string
		c    PlainChange
		want bool
	}{
		{"empty change", PlainChange{}, false},
		{"short name", PlainChange{ShortName: strp("Smith v Jones")}, true},
		{"blank short name", PlainChange{ShortName: strp("")}, false},
		{"blank practice area", PlainChange{PracticeAreaID: strp("")}, false},
		{"court province", PlainChange{CourtProvince: strp("ON")}, true},
		{"clear court province", PlainChange{CourtProvince: strp("")}, true},
		{"property province", PlainChange{PropertyProvince: strp("BC")}, true},
		{"lower case province", PlainChange{PropertyProvince: strp("on")}, false},
		{"export code refused", PlainChange{CourtProvince: strp("EX")}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validChange(tt.c); got != tt.want {
				t.Fatalf("validChange = %v, want %v", got, tt.want)
			}
		})
	}
}

// rule: ARC-I-042
// rule: ARC-I-006
func TestARC_I_042_PGUpdaterCheckLocked(t *testing.T) {
	tests := []struct {
		name     string
		current  int
		expected int
		walled   bool
		want     error
	}{
		{"current version", 3, 3, false, nil},
		{"stale version", 4, 3, false, server.ErrPreconditionFailed},
		{"walled hides the matter", 3, 3, true, ErrNotFound},
		{"walled wins over stale", 4, 3, true, ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkLocked("m1", tt.current, tt.expected, tt.walled)
			if tt.want == nil && err != nil || tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("checkLocked = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestPGUpdaterSession(t *testing.T) {
	tests := []struct {
		name string
		p    registry.Principal
		user string
		run  string
	}{
		{"user", registry.Principal{Kind: registry.PrincipalUser, UserID: "u1"}, "u1", ""},
		{"agent writes as the user it acts for", registry.Principal{Kind: registry.PrincipalAgent, RunID: "r1", ActingFor: "u2"}, "u2", "r1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := updateSession(tt.p)
			if s.UserID != tt.user || s.RunID != tt.run || s.PrincipalKind != string(tt.p.Kind) {
				t.Fatalf("session = %+v", s)
			}
		})
	}
}

func TestPGUpdaterRefusesBeforeTheDatabase(t *testing.T) {
	user := registry.Principal{Kind: registry.PrincipalUser, UserID: "u1"}
	tests := []struct {
		name string
		id   ID
		c    PlainChange
		by   registry.Principal
	}{
		{"no id", "", PlainChange{ShortName: strp("x")}, user},
		{"no change", "m1", PlainChange{}, user},
		{"bad province", "m1", PlainChange{CourtProvince: strp("XX")}, user},
		{"no acting user", "m1", PlainChange{ShortName: strp("x")}, registry.Principal{Kind: registry.PrincipalAgent, RunID: "r"}},
	}
	// A nil pool: any database call would panic, so a pass proves nothing reached it.
	u := NewPGUpdater(nil)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := u.Update(context.Background(), tt.id, 1, tt.c, tt.by); !errors.Is(err, ErrInvalid) {
				t.Fatalf("Update = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestPGUpdaterMapsPostgresErrors(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		invalid bool
	}{
		{"bad uuid", &pgconn.PgError{Code: "22P02"}, true},
		{"unknown practice area", &pgconn.PgError{Code: "23503"}, true},
		{"check violation", &pgconn.PgError{Code: "23514"}, true},
		{"other", errors.New("boom"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errors.Is(pgErr(tt.err), ErrInvalid); got != tt.invalid {
				t.Fatalf("invalid = %v, want %v", got, tt.invalid)
			}
		})
	}
}

// rule: ARC-I-042
func TestARC_I_042_PGWallsGuards(t *testing.T) {
	w := NewPGWalls(nil)
	if _, err := w.Walled(context.Background(), "", "m1"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Walled without user = %v", err)
	}
	if err := w.AddWall(context.Background(), conflict.Wall{}); !errors.Is(err, ErrWallsGateOwned) {
		t.Fatalf("AddWall = %v, want ErrWallsGateOwned", err)
	}
}
