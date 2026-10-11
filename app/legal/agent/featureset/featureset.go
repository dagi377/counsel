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
	"github.com/dagi377/counsel/app/legal/billing"
	"github.com/dagi377/counsel/app/legal/conflict"
	"github.com/dagi377/counsel/app/legal/deadlines"
	legaldocs "github.com/dagi377/counsel/app/legal/docs"
	"github.com/dagi377/counsel/app/legal/intake"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/timekeeping"
	tk "github.com/dagi377/counsel/app/legal/timekeeping"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/app/legal/work"
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
		conflict.SearchRankingDef(),
		conflict.ClearanceMemoDef(),
		conflict.ConsentDraftingDef(),
		conflict.ReverseSweepDef(),
		conflict.IDObligationWatcherDef(),
		conflict.IDEvidenceCheckDef(),
		conflict.BeneficialOwnershipDef(),
		conflict.WallMonitorDef(),
		conflict.ExpiryRetentionDef(),
		conflict.ProposeWallDef(),
		conflict.LateralCheckDef(),
		conflict.AuditAnomalyDigestDef(),
		conflict.DuplicateFinderDef(),
		conflict.DetermineObligationDef(),
		conflict.KYCSummaryDef(),
		conflict.EstimateImpactDef(),
		// documents
		legaldocs.TemplateCurrencyDef(),
		legaldocs.AutoFileDef(),
		legaldocs.EngagementLetterDef(),
		legaldocs.IngestDocumentDef(),
		legaldocs.DraftWithCitationsDef(),
		legaldocs.PrecheckOutgoingDef(),
		legaldocs.PrecedentCandidatesDef(),
		legaldocs.EmailFilerDef(),
		legaldocs.CourtFormCurrencyDef(),
		legaldocs.AssemblyOnStageDef(),
		legaldocs.TrackedRevisionDef(),
		legaldocs.ProposeFieldsDef(),
		legaldocs.AskClientForMissingAnswersDef(),
		legaldocs.AnonymizeDef(),
		legaldocs.RequestDocumentsDef(),
		legaldocs.CitationStalenessDef(),
		legaldocs.PlaceSigningFieldsDef(),
		legaldocs.EmailAutoFileDef(),
		legaldocs.DetectMatterContentDef(),
		legaldocs.AnswerWithCitationsDef(),
		legaldocs.AnalyzeDef(),
		legaldocs.FollowUpQuestionDef(),
		legaldocs.SuggestOrderAndTabsDef(),
		// matters
		matter.BriefRefreshDef(),
		matter.CallSummaryDef(),
		matter.ChronologyDef(),
		matter.StageAdvanceDef(),
		matter.PartyExtractionDef(),
		matter.ContactDedupeDef(),
		matter.OpeningPrefillDef(),
		matter.RetainerWatchDef(),
		matter.ConfigHygieneDef(),
		matter.TemplateFitDef(),
		matter.RerunConflictDef(),
		matter.OpeningPlanDef(),
		matter.ProposeRelationshipDef(),
		matter.RequestIDDef(),
		matter.MatterDigestDef(),
		matter.RelatedMattersDef(),
		matter.AccessHygieneDef(),
		matter.MatterQuestionAnsweringDef(),
		matter.AutoMapColumnsDef(),
		matter.ProposeNextActionsDef(),
		matter.SuggestFromPastMattersDef(),
		matter.SuggestCleanupDef(),
		matter.SummarizeThisWeekDef(),
		// trust
		trust.AnnualReportPrepDef(),
		trust.RetainerCoverageScanDef(),
		trust.SpotAuditPackDef(),
		trust.BankFeedSyncDef(),
		trust.MonthEndReconciliationDef(),
		trust.RetainerReplenishmentDef(),
		trust.DraftTransferDef(),
		trust.ProposeDispositionDef(),
		trust.PayeeFraudCheckDef(),
		trust.WithdrawalDrafterDef(),
		// billing
		billing.TrustApplicationDef(),
		billing.DraftCreditNoteDef(),
		billing.MonthlyDigestDef(),
		// intake
		intake.TriageNewLeadDef(),
		intake.MailboxWatcherDef(),
		intake.MissingInfoChaserDef(),
		intake.DeclineReferLetterDef(),
		intake.ConsultNoteDef(),
		intake.NonEngagementLetterDef(),
		intake.ConversionPrefillDef(),
		intake.AIReceptionistDef(),
		intake.DraftFormFromDescriptionDef(),
		intake.EngagementLetterDraftDef(),
		intake.DraftSequenceDef(),
		intake.ReceptionistCriteriaDef(),
		intake.ThankReferrerDef(),
		intake.PreConsultBriefDef(),
		intake.PipelineFollowUpsDef(),
		intake.WeeklyIntakeDigestDef(),
		intake.MapAnswersToLeadDef(),
		intake.GuidedChatIntakeDef(),
		intake.ProposeSlotsDef(),
		intake.SuggestScoringWeightsDef(),
		intake.DryRunLastLeadsDef(),
		// time
		tk.RebuildDayDef(),
		timekeeping.ReceiptCaptureDef(),
		timekeeping.PreBillCheckDef(),
		timekeeping.TimesheetPreReviewDef(),
		timekeeping.EngagementTermsDef(),
		timekeeping.RateChangeImpactDef(),
		timekeeping.RewriteNarrativeDef(),
		timekeeping.FindInconsistenciesDef(),
		timekeeping.FlagOutOfScopeDef(),
		timekeeping.ExplainTrendDef(),
		timekeeping.SuggestCategoriesDef(),
		timekeeping.ExplainShortfallDef(),
		// work
		work.PickUpAgentReadyDef(),
		work.ProposeMatterPlanDef(),
		work.DateShiftDef(),
		work.WorkflowLintDef(),
		work.RuleSuggesterDef(),
		work.AbsenceCoverPlannerDef(),
		work.CreateFromNoteDef(),
		work.DraftTemplateDef(),
		work.DailyDigestDef(),
		work.ExplainAutomationRunsDef(),
		work.ExplainFailedRunDef(),
		work.SuggestCategoriesDef(),
		work.ExplainNumberDef(),
	}
}
