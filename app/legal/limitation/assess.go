package limitation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dagi377/counsel/app/legal/legalflags"
	"github.com/dagi377/counsel/app/legal/matter/matterkind"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/jackc/pgx/v5"
)

// ErrOff means the deadline.ontario_pack flag is off, so no limitation date is computed.
var ErrOff = errors.New("limitation: deadline.ontario_pack is off")

// PackID names the limitation periods in the deadline engine. Its rules are the period kinds
// ("basic", "ultimate" or a notice id) the Calculator returns.
const PackID = "ontario_limitations"

// PackVersion is the version of the limitation arithmetic recorded on each proposal.
const PackVersion = "1"

// zone is the business time zone the limitation dates are read in.
const zone = "America/Toronto"

// Assessor records limitation assessments and links each to its computed deadlines. Every
// linked deadline is a proposal until a person other than the preparer confirms it.
type Assessor struct {
	Calc      Calculator
	Store     Store
	Deadlines deadline.Engine
}

// preparer is the user a principal prepares for: an agent's acting-for user, else the user.
func preparer(by registry.Principal) string {
	if by.Kind == registry.PrincipalAgent {
		return by.ActingFor
	}
	return by.UserID
}

// Assess computes a's limitation periods, saves each as a proposed deadline on the matter and
// stores the assessment with the deadline ids. It works only while deadline.ontario_pack is
// on and only for matter assessments: a lead has no deadline subject yet.
func (s *Assessor) Assess(ctx context.Context, tx pgx.Tx, a Assessment, by registry.Principal) (Assessment, error) {
	// The pack is unverified law: fail closed while its flag is off.
	if !legalflags.Enabled(ctx, legalflags.DeadlineOntarioPack) {
		return Assessment{}, fmt.Errorf("assess: %w", ErrOff)
	}
	if a.MatterID == "" || a.LeadID != "" {
		return Assessment{}, fmt.Errorf("assess: %w", ErrOwner)
	}
	a.PreparedBy, a.ConfirmedBy = preparer(by), ""
	if a.PreparedBy == "" || by.Kind == registry.PrincipalGuest {
		return Assessment{}, fmt.Errorf("assess: no preparing user: %w", ErrSelfConfirm)
	}
	periods, err := s.Calc.Compute(ctx, a)
	if err != nil {
		return Assessment{}, fmt.Errorf("assess: %w", err)
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return Assessment{}, fmt.Errorf("assess: zone: %w", err)
	}
	a.BasicDeadlineID, a.UltimateDeadlineID, a.NoticeDeadlineIDs = "", "", nil
	for _, p := range periods {
		trigger := a.DiscoveryDate
		if p.Kind == "ultimate" || trigger == nil {
			trigger = a.ActDate
		}
		if trigger == nil {
			return Assessment{}, fmt.Errorf("assess %s: %w", p.Kind, ErrDates)
		}
		id, err := s.Deadlines.Save(ctx, tx, deadline.Proposed{
			PackID: PackID, PackVersion: PackVersion, RuleID: p.Kind,
			Trigger: clock.DateOf(*trigger, loc), Due: clock.DateOf(p.Due, loc),
			Subject: deadline.Subject{Kind: matterkind.Subject, ID: a.MatterID},
		})
		if err != nil {
			return Assessment{}, fmt.Errorf("assess: save %s deadline: %w", p.Kind, err)
		}
		switch p.Kind {
		case "basic":
			a.BasicDeadlineID = string(id)
		case "ultimate":
			a.UltimateDeadlineID = string(id)
		default:
			a.NoticeDeadlineIDs = append(a.NoticeDeadlineIDs, string(id))
		}
	}
	out, err := s.Store.Create(ctx, a)
	if err != nil {
		return Assessment{}, fmt.Errorf("assess: %w", err)
	}
	return out, nil
}

// Confirm records by's confirmation of assessment id and of every deadline linked to it. Only
// a person who did not prepare the assessment confirms; an agent never does.
func (s *Assessor) Confirm(ctx context.Context, tx pgx.Tx, id string, by registry.Principal) (Assessment, error) {
	if !legalflags.Enabled(ctx, legalflags.DeadlineOntarioPack) {
		return Assessment{}, fmt.Errorf("confirm: %w", ErrOff)
	}
	if by.Kind != registry.PrincipalUser || by.UserID == "" {
		return Assessment{}, fmt.Errorf("confirm %s: %w", id, ErrSelfConfirm)
	}
	a, err := s.Store.Get(ctx, id)
	if err != nil {
		return Assessment{}, fmt.Errorf("confirm: %w", err)
	}
	if a.ConfirmedBy != "" {
		return Assessment{}, fmt.Errorf("confirm %s: %w", id, ErrConfirmed)
	}
	if by.UserID == a.PreparedBy {
		return Assessment{}, fmt.Errorf("confirm %s: %w", id, ErrSelfConfirm)
	}
	ids := append([]string{a.BasicDeadlineID, a.UltimateDeadlineID}, a.NoticeDeadlineIDs...)
	for _, d := range ids {
		if d == "" {
			continue
		}
		if err := s.Deadlines.Confirm(ctx, tx, deadline.DeadlineID(d), by); err != nil && !errors.Is(err, deadline.ErrAlreadyConfirmed) {
			return Assessment{}, fmt.Errorf("confirm %s: deadline: %w", id, err)
		}
	}
	out, err := s.Store.Confirm(ctx, id, by.UserID)
	if err != nil {
		return Assessment{}, fmt.Errorf("confirm: %w", err)
	}
	return out, nil
}
