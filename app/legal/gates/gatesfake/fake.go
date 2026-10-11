// Package gatesfake is an in-memory gates.Gates for tests in other packages.
package gatesfake

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/dagi377/counsel/app/legal/gates"
)

var _ gates.Gates = (*Fake)(nil)

var specGateID = regexp.MustCompile(`^G-?[A-Z]*\d+$`)

// Fake holds gate kinds in memory.
type Fake struct {
	mu    sync.Mutex
	kinds map[string]gates.Kind
}

// New returns a Fake with the given kinds. A kind whose Code looks like a spec id (G-B2)
// is rejected with gates.ErrSpecCodeAsGateCode.
func New(kinds ...gates.Kind) (*Fake, error) {
	f := &Fake{kinds: map[string]gates.Kind{}}
	for _, k := range kinds {
		if specGateID.MatchString(k.Code) {
			return nil, fmt.Errorf("gatesfake: kind %q: %w", k.Code, gates.ErrSpecCodeAsGateCode)
		}
		f.kinds[k.Code] = k
	}
	return f, nil
}

// Kinds lists the kinds ordered by Code.
func (f *Fake) Kinds(_ context.Context) ([]gates.Kind, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]gates.Kind, 0, len(f.kinds))
	for _, k := range f.kinds {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out, nil
}

// Kind returns one kind or gates.ErrUnknownKind.
func (f *Fake) Kind(_ context.Context, code string) (gates.Kind, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k, ok := f.kinds[code]
	if !ok {
		return gates.Kind{}, fmt.Errorf("gatesfake: kind %q: %w", code, gates.ErrUnknownKind)
	}
	return k, nil
}

// SelfApproval allows only a human with exactly one active licensee and the setting on.
func (f *Fake) SelfApproval(_ context.Context, in gates.SelfApprovalInput) (gates.SelfApprovalResult, error) {
	switch {
	case in.By.Agent:
		return gates.SelfApprovalResult{Reason: "agent"}, nil
	case !in.SettingEnabled:
		return gates.SelfApprovalResult{Reason: "setting off"}, nil
	case in.ActiveLicensees != 1:
		return gates.SelfApprovalResult{Reason: "not a sole practitioner"}, nil
	}
	return gates.SelfApprovalResult{Allowed: true, Reason: "sole practitioner", Exception: true}, nil
}

// CheckDecision rejects agents, the preparer (or the user an agent prepared for), deciders
// without the approver capability and decisions inside the cooling-off period.
func (f *Fake) CheckDecision(ctx context.Context, g gates.Gate, by gates.Principal, now time.Time) error {
	k, err := f.Kind(ctx, g.Code)
	if err != nil {
		return err
	}
	if by.Agent {
		return gates.ErrAgentApproves
	}
	if by.UserID == g.Preparer.UserID || (g.Preparer.Agent && by.UserID == g.Preparer.ActingFor) {
		return gates.ErrPreparerApproves
	}
	if k.Approver != "" && !slices.Contains(by.Capabilities, k.Approver) {
		return gates.ErrMissingCapability
	}
	if now.Before(g.OpenedAt.Add(k.CoolingOff)) {
		return gates.ErrCoolingOff
	}
	return nil
}
