// Package featureset lists the app's AI feature definitions. The definitions live in the
// packages that own them (app/legal/deadlines, ...), which import app/legal/agent/feature, so
// the list sits here, above both, instead of inside package feature. Boot and the plugin
// skill export both build from App(); nothing registers from init().
//
// The plugin's checked-in skills are regenerated from this list with
//
//	go generate ./app/legal/agent/featureset
//
// and TestExportSkillsCheckedIn fails while they differ.
package featureset

//go:generate go test -run TestExportSkillsCheckedIn -update-skills .

import (
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/conflict"
	"github.com/dagi377/counsel/app/legal/deadlines"
	legaldocs "github.com/dagi377/counsel/app/legal/docs"
)

// App returns the set of the app's feature definitions. Each feature task adds its Def to
// appDefs.
func App() (*feature.Set, error) {
	s := feature.NewSet()
	for _, d := range appDefs() {
		if err := s.Add(d); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// appDefs lists the app's feature definitions, grouped by area.
func appDefs() []feature.Def {
	return []feature.Def{
		// deadlines
		deadlines.ExtractDeadlinesDef(),
		deadlines.LimitationAssessmentDef(),
		deadlines.RecomputeDependantsDef(),
		deadlines.ComplianceEvidenceMatcherDef(),
		deadlines.RuleWatchDef(),
		deadlines.PostEventFollowUpDef(),
		deadlines.DigestDef(),
		deadlines.ProposeMeetingDef(),
		deadlines.FindTeamTimeDef(),
		deadlines.ExplainStepDef(),
		deadlines.DraftAgendaDef(),
		deadlines.AdjournmentWatcherDef(),
		deadlines.AbsenceCoverageCheckDef(),
		// conflicts
		conflict.PartyExtractionDef(),
		// conflicts
		conflict.SearchRankingDef(),
		conflict.ClearanceMemoDef(),
		// documents
		legaldocs.TemplateCurrencyDef(),
	}
}
