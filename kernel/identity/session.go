package identity

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/registry"
)

var _ Session = (*TxSession)(nil)

// TxSession is the kernel Session: it binds a principal to one transaction through the
// database's SET LOCAL session variables, so row-level security sees the request's principal
// and a pooled connection carries nothing into the next request (ARC-U-011).
type TxSession struct {
	db db.DB
}

// NewTxSession returns a Session that binds through d.
func NewTxSession(d db.DB) *TxSession {
	return &TxSession{db: d}
}

// Bind implements Session. An agent principal binds its run and acting-for user; the user id
// the row-level policies read is the acting-for user, so the agent never sees more rows than
// that user.
func (s *TxSession) Bind(ctx context.Context, tx pgx.Tx, p Principal) error {
	vars, err := sessionOf(p)
	if err != nil {
		return fmt.Errorf("identity bind: %w", err)
	}
	if err := s.db.Bind(ctx, tx, vars); err != nil {
		return fmt.Errorf("identity bind: %w", err)
	}
	return nil
}

// sessionOf maps a principal to the database session variables.
func sessionOf(p Principal) (db.Session, error) {
	switch p.Kind {
	case registry.PrincipalUser, registry.PrincipalGuest:
		if p.UserID == "" {
			return db.Session{}, ErrNoPrincipal
		}
		return db.Session{UserID: p.UserID, PrincipalKind: string(p.Kind)}, nil
	case registry.PrincipalAgent:
		if p.RunID == "" || p.ActingFor == "" {
			return db.Session{}, ErrNoPrincipal
		}
		return db.Session{UserID: p.ActingFor, PrincipalKind: string(p.Kind), RunID: p.RunID, ActingFor: p.ActingFor}, nil
	default:
		return db.Session{}, ErrNoPrincipal
	}
}
