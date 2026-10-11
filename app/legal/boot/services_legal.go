package boot

import (
	"fmt"

	"github.com/dagi377/counsel/app/legal/limitation"
	"github.com/dagi377/counsel/app/legal/rulepacks/ontario"
	"github.com/dagi377/counsel/app/legal/trust/signatory"
	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/identity"
)

// LimitationStatute builds the Limitations Act calculator with the Ontario holiday calendar
// attached, so the Legislation Act, 2006 s.88 roll applies: a period that ends on a holiday
// or a weekend runs to the next business day. schedule may be nil.
func LimitationStatute(schedule limitation.Schedule) (*limitation.Statute, error) {
	s, err := limitation.NewStatute(schedule)
	if err != nil {
		return nil, fmt.Errorf("limitation statute: %w", err)
	}
	holidays, err := ontario.Holidays()
	if err != nil {
		return nil, fmt.Errorf("limitation statute: %w", err)
	}
	return s.WithHolidays(holidays), nil
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
		identity.WithCoreEffect(signatory.KindChange, signatory.Effects(svc)[signatory.KindChange]),
	}
}
