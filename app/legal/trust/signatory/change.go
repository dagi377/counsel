package signatory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/registry"
)

// Gate kinds for a signatory change. KindChange (GA6) is canonical (CF-07). Its kernel
// prefix is the one allowlisted exception (Q-K1). KindSigningAuthority (spec G-TSIG) is an
// alias: requests made under it open a KindChange gate, and its own effect never applies.
const (
	KindChange           = "core.trust_signatory"
	KindSigningAuthority = "trust.signing_authority"
)

// SubjectAccount is the gate subject kind for a trust account.
const SubjectAccount = "trust.account"

// Payload change values.
const (
	PayloadAdd    = "add"
	PayloadRemove = "remove"
)

var (
	// ErrAliasKind is returned when a gate approved under KindSigningAuthority reaches its
	// effect: signatory changes apply only through KindChange.
	ErrAliasKind = errors.New("signatory: change applies only via " + KindChange)
	// ErrInvalidPayload is returned for a payload missing a field or with an unknown change.
	ErrInvalidPayload = errors.New("signatory: invalid change payload")
)

// Payload is the gate payload for KindChange (spec/gates.json GA6).
type Payload struct {
	TrustAccountID string `json:"trust_account_id"`
	UserID         string `json:"user_id"`
	Change         string `json:"change"` // PayloadAdd or PayloadRemove
	// Kind and BondEvidence describe an added signatory; Kind defaults to Licensee.
	Kind         Kind   `json:"kind,omitempty"`
	BondEvidence string `json:"bond_evidence,omitempty"`
}

func (p Payload) validate() error {
	if p.TrustAccountID == "" || p.UserID == "" || (p.Change != PayloadAdd && p.Change != PayloadRemove) {
		return ErrInvalidPayload
	}
	return nil
}

// Route returns the gate kind a signatory change is opened under: the alias and the
// canonical kind both route to KindChange. Any other kind is not a signatory change.
func Route(kind string) (string, error) {
	switch kind {
	case KindChange, KindSigningAuthority:
		return KindChange, nil
	}
	return "", fmt.Errorf("route %q: %w", kind, gate.ErrUnknownKind)
}

// OpenChange opens a signatory change gate. kind may be KindChange or its alias; the gate
// is always opened under KindChange.
func OpenChange(ctx context.Context, tx pgx.Tx, eng gate.Engine, kind string, p Payload, preparer gate.Principal) (gate.GateID, error) {
	k, err := Route(kind)
	if err != nil {
		return "", fmt.Errorf("open signatory change: %w", err)
	}
	if err := p.validate(); err != nil {
		return "", fmt.Errorf("open signatory change: %w", err)
	}
	body, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("open signatory change: %w", err)
	}
	id, err := eng.Open(ctx, tx, k, gate.Subject{Kind: SubjectAccount, ID: p.TrustAccountID}, body, preparer)
	if err != nil {
		return "", fmt.Errorf("open signatory change: %w", err)
	}
	return id, nil
}

// Effects returns the effect handlers for both kinds, for gates.RegisterKinds. The
// KindChange effect applies the change through svc; the alias effect always fails.
func Effects(svc Service) map[string]registry.EffectFunc {
	return map[string]registry.EffectFunc{
		KindChange:           changeEffect(svc),
		KindSigningAuthority: aliasEffect,
	}
}

func aliasEffect(_ context.Context, _ pgx.Tx, g registry.Approved) error {
	return fmt.Errorf("gate %s: %w", g.GateID, ErrAliasKind)
}

// changeEffect proposes and approves the change in svc. The preparer is the gate preparer
// (an agent's acting-for user) and the approver is the first gate approver other than the
// preparer; svc re-checks eligibility and the last-signatory rule.
func changeEffect(svc Service) registry.EffectFunc {
	return func(ctx context.Context, _ pgx.Tx, g registry.Approved) error {
		if g.Kind != KindChange {
			return fmt.Errorf("gate %s kind %q: %w", g.GateID, g.Kind, ErrAliasKind)
		}
		var p Payload
		dec := json.NewDecoder(bytes.NewReader(g.Payload))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&p); err != nil {
			return fmt.Errorf("gate %s: %w: %w", g.GateID, ErrInvalidPayload, err)
		}
		if err := p.validate(); err != nil {
			return fmt.Errorf("gate %s: %w", g.GateID, err)
		}
		preparer := g.Preparer.UserID
		if g.Preparer.Kind == registry.PrincipalAgent {
			preparer = g.Preparer.ActingFor
		}
		var by Approver
		for _, a := range g.Approvers {
			if a.UserID != "" && a.UserID != preparer {
				by = Approver{UserID: a.UserID, IsAgent: a.Kind == registry.PrincipalAgent}
				break
			}
		}
		if by.UserID == "" {
			return fmt.Errorf("gate %s: %w", g.GateID, ErrSelfApproval)
		}
		c := Change{AccountID: p.TrustAccountID, PreparedBy: preparer}
		if p.Change == PayloadAdd {
			kind := p.Kind
			if kind == "" {
				kind = Licensee
			}
			c.Add = []Signatory{{AccountID: p.TrustAccountID, UserID: p.UserID, Kind: kind, BondEvidence: p.BondEvidence}}
		} else {
			c.Remove = []string{p.UserID}
		}
		pending, err := svc.Propose(ctx, c)
		if err != nil {
			return fmt.Errorf("gate %s: %w", g.GateID, err)
		}
		if _, err := svc.Approve(ctx, pending.ID, by); err != nil {
			return fmt.Errorf("gate %s: %w", g.GateID, err)
		}
		return nil
	}
}
