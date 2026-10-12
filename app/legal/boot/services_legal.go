package boot

import (
	"fmt"

	"github.com/dagi377/counsel/app/legal/limitation"
	"github.com/dagi377/counsel/app/legal/rulepacks/ontario"
	"github.com/dagi377/counsel/app/legal/trust/signatory"
	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/identity"
)

// LimitationStatute builds the Limitations Act calculator with the Legislation Act, 2006
// s.87 holiday calendar attached, so the s.88 roll applies: a period that ends on a holiday
// (a Sunday or a dated s.87 holiday) runs to the next day that is not a holiday. Saturday is
// not a holiday, so a Saturday last day stands. The r. 1.03 court calendar is not used: its
// Saturday-substitute Mondays would make limitation dates one day late. schedule may be nil.
func LimitationStatute(schedule limitation.Schedule) (*limitation.Statute, error) {
	s, err := limitation.NewStatute(schedule)
	if err != nil {
		return nil, fmt.Errorf("limitation statute: %w", err)
	}
	// rule: Legislation Act, 2006 s.87, s.88 (verify)
	holidays, from, to, err := ontario.LegislationActHolidays()
	if err != nil {
		return nil, fmt.Errorf("limitation statute: %w", err)
	}
	return s.WithHolidays(holidays, from, to), nil
}

// LimitationAssessor builds the limitation Assessor on the LimitationStatute calculator, so
// every assessment the app records gets the s.88 holiday roll. store and deadlines are
// required; a missing one is ErrJobDependency naming it.
func LimitationAssessor(schedule limitation.Schedule, store limitation.Store, deadlines deadline.Engine) (*limitation.Assessor, error) {
	if err := requireDeps("limitation assessor", []dep{
		{"limitation Store", store == nil},
		{"deadline Engine", deadlines == nil},
	}); err != nil {
		return nil, err
	}
	calc, err := LimitationStatute(schedule)
	if err != nil {
		return nil, err
	}
	return &limitation.Assessor{Calc: calc, Store: store, Deadlines: deadlines}, nil
}

// CoreGateEffects are the effects the legal app supplies for core.* gate kinds. The kernel
// owns those kinds (kernel/identity admin gates); the app supplies only the handlers, which
// kernel boot passes to identity.NewAdminGates. svc applies signatory changes.
func CoreGateEffects(svc signatory.Service) []identity.AdminOption {
	return []identity.AdminOption{
		identity.WithCoreEffect(signatory.KindChange, signatoryEffects(svc)[signatory.KindChange]),
	}
}
