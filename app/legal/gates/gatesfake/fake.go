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
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/registry"
)

var _ gates.Gates = (*Fake)(nil)

var specGateID = regexp.MustCompile(`^G-?[A-Z]*\d+$`)

// Fake holds gate kinds in memory.
type Fake struct {
	mu    sync.Mutex
	kinds map[string]gates.Kind
	specs map[string]gates.SpecKind // risk and sole-owner delay, set by NewSpec
}

// New returns a Fake with the given kinds. A kind whose Code looks like a spec id (G-B2)
// is rejected with gates.ErrSpecCodeAsGateCode.
func New(kinds ...gates.Kind) (*Fake, error) {
	f := &Fake{kinds: map[string]gates.Kind{}, specs: map[string]gates.SpecKind{}}
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

// NewSpec returns a Fake whose kinds also carry risk and sole-owner delay, so SelfApproval
// applies the same policy as gates.Catalog.
func NewSpec(kinds ...gates.SpecKind) (*Fake, error) {
	plain := make([]gates.Kind, len(kinds))
	for i, k := range kinds {
		plain[i] = k.Kind
	}
	f, err := New(plain...)
	if err != nil {
		return nil, err
	}
	for _, k := range kinds {
		f.specs[k.Code] = k
	}
	return f, nil
}

// SelfApproval allows a human with the approver capability when there is exactly one
// active licensee and the setting is on. Exception is true only when the decider prepared
// the gate (or an agent prepared it for them). A kind with a sole-owner delay waits for it,
// and a two-approver or irreversible kind waits at least gates.SoleSecondConfirmDelay, timed
// by the clock in ctx.
func (f *Fake) SelfApproval(ctx context.Context, in gates.SelfApprovalInput) (gates.SelfApprovalResult, error) {
	k, err := f.Kind(ctx, in.Gate.Code)
	if err != nil {
		return gates.SelfApprovalResult{}, err
	}
	f.mu.Lock()
	spec := f.specs[in.Gate.Code]
	f.mu.Unlock()
	switch {
	case in.By.Agent:
		return gates.SelfApprovalResult{Reason: "agent"}, nil
	case in.By.UserID == "":
		return gates.SelfApprovalResult{Reason: "no user"}, nil
	case !in.SettingEnabled:
		return gates.SelfApprovalResult{Reason: "setting off"}, nil
	case in.ActiveLicensees != 1:
		return gates.SelfApprovalResult{Reason: "not a sole practitioner"}, nil
	case !slices.Contains(in.By.Capabilities, k.Approver):
		return gates.SelfApprovalResult{Reason: "missing capability"}, nil
	}
	p := in.Gate.Preparer
	self := in.By.UserID == p.UserID || (p.Agent && in.By.UserID == p.ActingFor)
	delay := spec.SoleOwnerDelay
	if spec.MinApprovers >= 2 || (self && spec.Risk == registry.RiskIrreversible) {
		delay = max(delay, gates.SoleSecondConfirmDelay)
	}
	if delay > 0 {
		clk, err := clock.From(ctx)
		if err != nil {
			return gates.SelfApprovalResult{}, fmt.Errorf("gatesfake: %w", err)
		}
		if in.Gate.OpenedAt.IsZero() || clk.Now().Before(in.Gate.OpenedAt.Add(delay)) {
			return gates.SelfApprovalResult{Reason: "sole-owner delay"}, nil
		}
	}
	return gates.SelfApprovalResult{Allowed: true, Reason: "sole practitioner", Exception: self}, nil
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
