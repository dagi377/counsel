package boot

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/conflict"
	"github.com/dagi377/counsel/app/legal/deadlines"
	"github.com/dagi377/counsel/app/legal/docs"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/matter/matterkind"
	kdocs "github.com/dagi377/counsel/kernel/docs"
	"github.com/dagi377/counsel/kernel/registry"
)

// legalEffects are the dedicated approval effects of the document, matter, conflict,
// deadline and calendar gate kinds, one line per kind. They replace ApprovalEffect for the
// kind. open (the kernel gate engine) opens the cal.change proposals of an approved rule
// pack; while nil, a rule pack approval that moves a deadline fails closed. ID destruction
// purges from the cell bucket (docsBucket); without one, a destruction with a stored copy
// fails closed.
func legalEffects(open deadlines.ChangeOpener) map[string]registry.EffectFunc {
	// ID destruction purges stored copies through the docs storage port: the cell bucket.
	var files kdocs.ObjectDeleter
	if b, ok := docsBucket().(kdocs.ObjectDeleter); ok {
		files = b
	}
	d, c, dl, m := docs.GateEffects(), conflict.GateEffects(files), deadlines.GateEffects(open), matter.GateEffects()
	return map[string]registry.EffectFunc{
		docs.GateEngagementLetter:    draftOr(d[docs.GateEngagementLetter]),
		docs.GateOutgoing:            draftOr(d[docs.GateOutgoing]),
		docs.GateFilingPrivilege:     draftOr(d[docs.GateFilingPrivilege]),
		docs.GateTemplatePublish:     draftOr(d[docs.GateTemplatePublish]),
		docs.GatePrecedent:           draftOr(d[docs.GatePrecedent]),
		conflict.GateClearance:       draftOr(c[conflict.GateClearance]),
		conflict.GateConsentText:     draftOr(c[conflict.GateConsentText]),
		conflict.GateWall:            draftOr(c[conflict.GateWall]),
		conflict.GateLateral:         draftOr(c[conflict.GateLateral]),
		conflict.GateClientID:        draftOr(c[conflict.GateClientID]),
		conflict.GateIDDestruction:   draftOr(c[conflict.GateIDDestruction]),
		deadlines.GateCalConfirm:     draftOr(dl[deadlines.GateCalConfirm]),
		deadlines.GateCalSecondCheck: draftOr(dl[deadlines.GateCalSecondCheck]),
		deadlines.GateCalChange:      draftOr(dl[deadlines.GateCalChange]),
		deadlines.GateCalMarkMet:     draftOr(dl[deadlines.GateCalMarkMet]),
		deadlines.GateCalRelease:     draftOr(dl[deadlines.GateCalRelease]),
		deadlines.GateCalRulePack:    draftOr(dl[deadlines.GateCalRulePack]),
		deadlines.GateCalDelegation:  draftOr(dl[deadlines.GateCalDelegation]),
		matterkind.GatePartyConflict: draftOr(m[matterkind.GatePartyConflict]),
		matter.GateKeyFact:           draftOr(m[matter.GateKeyFact]),
		matter.GateContactMerge:      draftOr(m[matter.GateContactMerge]),
		matter.GatePracticeConfig:    draftOr(m[matter.GatePracticeConfig]),
		matter.GateBulkChange:        draftOr(m[matter.GateBulkChange]),
		matter.GateRenumber:          draftOr(m[matter.GateRenumber]),
		matter.GateWidenAccess:       draftOr(m[matter.GateWidenAccess]),
		matter.GateApplyTemplate:     draftOr(m[matter.GateApplyTemplate]),
	}
}

// draftOr runs ApprovalEffect for a gate an agent opened on its own draft (a tool-gap
// submit_for_approval, whose payload names the draft rather than the change) and the kind's
// dedicated effect for every other gate.
func draftOr(fx registry.EffectFunc) registry.EffectFunc {
	return func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
		switch g.Subject.Kind {
		case SubjectDocumentDraft, matter.SubjectAgentDraft, agent.SubjectAgentNote, agent.SubjectEmailDraft:
			return ApprovalEffect(ctx, tx, g)
		}
		return fx(ctx, tx, g)
	}
}

// withLegalEffects adds legalEffects to fx; a kind fx already handles is an error.
func withLegalEffects(open deadlines.ChangeOpener, fx map[string]registry.EffectFunc) (map[string]registry.EffectFunc, error) {
	return mergeEffects([]effectSource{{"gates", fx}, {"legal", legalEffects(open)}})
}
