package boot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/featureset"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/billing/batch"
	"github.com/dagi377/counsel/app/legal/billing/draft"
	"github.com/dagi377/counsel/app/legal/billing/payment"
	"github.com/dagi377/counsel/app/legal/comms"
	"github.com/dagi377/counsel/app/legal/conflict"
	"github.com/dagi377/counsel/app/legal/deadlines"
	legaldocs "github.com/dagi377/counsel/app/legal/docs"
	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/party"
	"github.com/dagi377/counsel/app/legal/roles"
	tk "github.com/dagi377/counsel/app/legal/timekeeping"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	"github.com/dagi377/counsel/kernel/agenttools"
	agentrun "github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/notify"
	"github.com/dagi377/counsel/kernel/registry"
)

// AgentPorts are the module query APIs the agent tool handlers read through, one field per
// agent.Deps port. Boot fills them from the module services; a nil port fails the handler
// that reads it with agent.ErrMissingDep, so a missing wire stops boot instead of serving a
// tool that cannot answer.
type AgentPorts struct {
	Can      agent.CapabilityCheck
	Walls    roles.Walls
	Plans    agenttools.Plans // nil: a Planner over PlanStore
	OpenGate agenttools.GateOpener

	// Provenance checks that sourced write inputs come from the run's read results. Nil
	// builds a fresh one; the agent runner forgets a run's values when the run ends.
	Provenance *agenttools.Provenance

	Contacts      agent.ContactSearch
	ClientMatters agent.ClientMatters
	Matters       matter.Query
	DocLookup     agent.DocumentLookup
	Work          agent.WorkList
	Text          agent.DocumentText
	Documents     agent.MatterDocuments
	Folders       agent.MatterFolders

	Bills          agent.BillReader
	UnbilledWIP    agent.UnbilledWIP
	WriteTools     *agent.WriteTools
	BillBalances   agent.BillBalances
	Trust          agent.TrustAvailable
	TrustAccounts  agent.MatterTrustAccounts
	Entries        tk.Entries
	MatterTime     agent.MatterTime
	DraftCalc      draft.Calc
	DraftStore     draft.Store
	BatchDrafts    batch.Drafts
	Payments       payment.Service
	SubjectMatters agent.SubjectMatters

	Deadlines       deadline.Lister
	DeadlineCompute agent.DeadlineCompute
	DeadlineSaver   agent.DeadlineSaver
	DeadlineGetter  agent.DeadlineGetter
	PublishedPacks  agent.PublishedPacks

	Tasks           agent.TaskCreator
	ProposedTasks   agent.ProposedTasks
	Conflicts       conflict.Checker
	ConflictParties agent.ConflictCheckParties

	MatterContacts agent.MatterContacts
	DocumentDrafts agent.DocumentDrafts
	EmailDrafts    agent.EmailDrafts
	NoteDrafts     agent.NoteDrafts
}

// port is one named AgentPorts value.
type port struct {
	name  string
	value any
	set   bool
}

// ports lists p's values by field name. RunScope has no field: the stateless
// agenttools.RunScopes reads the Scope the Runner puts in ctx (Q-INTEG-12).
func (p AgentPorts) ports() []port {
	return []port{
		{"Can", p.Can, p.Can != nil},
		{"Walls", p.Walls, p.Walls != nil},
		{"Plans", p.Plans, p.Plans != nil},
		{"OpenGate", p.OpenGate, p.OpenGate != nil},
		{"Provenance", p.Provenance, p.Provenance != nil},
		{"Contacts", p.Contacts, p.Contacts != nil},
		{"ClientMatters", p.ClientMatters, p.ClientMatters != nil},
		{"Matters", p.Matters, p.Matters != nil},
		{"DocLookup", p.DocLookup, p.DocLookup != nil},
		{"Work", p.Work, p.Work != nil},
		{"Text", p.Text, p.Text != nil},
		{"Documents", p.Documents, p.Documents != nil},
		{"Folders", p.Folders, p.Folders != nil},
		{"Bills", p.Bills, p.Bills != nil},
		{"UnbilledWIP", p.UnbilledWIP, p.UnbilledWIP != nil},
		{"WriteTools", p.WriteTools, p.WriteTools != nil},
		{"BillBalances", p.BillBalances, p.BillBalances != nil},
		{"Trust", p.Trust, p.Trust != nil},
		{"TrustAccounts", p.TrustAccounts, p.TrustAccounts != nil},
		{"Entries", p.Entries, p.Entries != nil},
		{"MatterTime", p.MatterTime, p.MatterTime != nil},
		{"DraftCalc", p.DraftCalc, p.DraftCalc != nil},
		{"DraftStore", p.DraftStore, p.DraftStore != nil},
		{"BatchDrafts", p.BatchDrafts, p.BatchDrafts != nil},
		{"Payments", p.Payments, p.Payments != nil},
		{"SubjectMatters", p.SubjectMatters, p.SubjectMatters != nil},
		{"Deadlines", p.Deadlines, p.Deadlines != nil},
		{"DeadlineCompute", p.DeadlineCompute, p.DeadlineCompute != nil},
		{"DeadlineSaver", p.DeadlineSaver, p.DeadlineSaver != nil},
		{"DeadlineGetter", p.DeadlineGetter, p.DeadlineGetter != nil},
		{"PublishedPacks", p.PublishedPacks, p.PublishedPacks != nil},
		{"Tasks", p.Tasks, p.Tasks != nil},
		{"ProposedTasks", p.ProposedTasks, p.ProposedTasks != nil},
		{"Conflicts", p.Conflicts, p.Conflicts != nil},
		{"ConflictParties", p.ConflictParties, p.ConflictParties != nil},
		{"MatterContacts", p.MatterContacts, p.MatterContacts != nil},
		{"DocumentDrafts", p.DocumentDrafts, p.DocumentDrafts != nil},
		{"EmailDrafts", p.EmailDrafts, p.EmailDrafts != nil},
		{"NoteDrafts", p.NoteDrafts, p.NoteDrafts != nil},
	}
}

// Missing names the ports boot has not wired, in field order.
func (p AgentPorts) Missing() []string {
	var out []string
	for _, v := range p.ports() {
		if !v.set {
			out = append(out, v.name)
		}
	}
	return out
}

// Deps returns the agent.Deps the handlers build from. Only set ports go in, so DepOf
// never returns a typed nil.
func (p AgentPorts) Deps() agent.Deps {
	values := []any{agenttools.RunScopes{}}
	for _, v := range p.ports() {
		if v.set {
			values = append(values, v.value)
		}
	}
	return agent.NewDeps(values...)
}

// AgentTools builds every tool in agent.Handlers() over p. A port the set needs and p lacks
// fails with agent.ErrMissingDep naming the tool; nothing is half-built.
func AgentTools(p AgentPorts) ([]registry.Tool, error) {
	if p.Provenance == nil {
		p.Provenance = agenttools.NewProvenance()
	}
	set := agent.Handlers()
	deps := p.Deps()
	var (
		tools []registry.Tool
		errs  []error
	)
	for _, name := range set.Names() {
		h, _ := set.For(name)
		t, err := h.Build(deps)
		if err != nil {
			errs = append(errs, fmt.Errorf("agent tool %s: %w", name, err))
			continue
		}
		tools = append(tools, t)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return tools, nil
}

// Features returns the app's feature set, each feature checked against the tools boot can
// serve and the gate kinds the gate catalog defines.
func Features() (*feature.Set, error) {
	s, err := featureset.App()
	if err != nil {
		return nil, fmt.Errorf("feature set: %w", err)
	}
	tools, err := ServedToolNames()
	if err != nil {
		return nil, err
	}
	kinds, err := gateKindCodes()
	if err != nil {
		return nil, err
	}
	var errs []error
	for _, d := range s.All() {
		for _, t := range d.Tools {
			if !slices.Contains(tools, t) {
				errs = append(errs, fmt.Errorf("feature %s: tool %s has no handler: %w", d.ID, t, agent.ErrMissingDep))
			}
		}
		for _, g := range d.GateKinds {
			if !slices.Contains(kinds, g) {
				errs = append(errs, fmt.Errorf("feature %s: gate kind %s is not in the gate catalog: %w", d.ID, g, agent.ErrMissingDep))
			}
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return s, nil
}

// ServedToolNames lists every tool an agent run can call: the handler set plus the read and
// write tool sets, sorted.
func ServedToolNames() ([]string, error) {
	names := agent.Handlers().Names()
	rt, err := agent.NewReadTools(agent.ReadDeps{})
	if err != nil {
		return nil, fmt.Errorf("read tools: %w", err)
	}
	wt, err := agent.NewWriteTools(agent.WriteDeps{})
	if err != nil {
		return nil, fmt.Errorf("write tools: %w", err)
	}
	for _, s := range append(rt.Specs(), wt.Specs()...) {
		if !slices.Contains(names, s.Name) {
			names = append(names, s.Name)
		}
	}
	slices.Sort(names)
	return names, nil
}

func gateKindCodes() ([]string, error) {
	kinds, err := gates.ParseKinds(gates.SpecData())
	if err != nil {
		return nil, fmt.Errorf("gate kinds: %w", err)
	}
	out := make([]string, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, k.Code)
	}
	return out, nil
}

// JobBindings binds every job feature of s to the events and schedule that start it.
// Every scheduled run (F-DL-05, F-DL-09, F-CON-05, F-CON-09 to 12, F-DOC-06, F-DOC-09 and the
// F-DL-06 rule watch) acts for its feature's designated lawyer in firm settings, read at each
// tick (JOB-ACTOR). A job feature with no binding listed here fails, so a new job feature
// cannot ship unbound.
func JobBindings(s *feature.Set, b *trigger.Bindings) error {
	// rule: ARC-I-040
	// A scheduled run acts for the feature's designated lawyer in firm settings, never for
	// an administrator; with none named the feature does not run (JOB-ACTOR).
	scheduled := func(d feature.Def, events ...string) func() (trigger.Binding, error) {
		return func() (trigger.Binding, error) { return trigger.BindDesignated(d, events...) }
	}
	byID := map[string]func() (trigger.Binding, error){
		deadlines.FeatureExtractDeadlines: func() (trigger.Binding, error) {
			// F-DL-01 also starts on an email filed to a matter (Q-DRAFT-FIX).
			d, _ := s.ByID(deadlines.FeatureExtractDeadlines)
			return trigger.Bind(d, "", deadlines.EventDocumentUploaded, comms.EventEmailFiled)
		},
		deadlines.FeatureLimitationAssessment:       deadlines.LimitationAssessmentBinding,
		deadlines.RecomputeDependantsDef().ID:       deadlines.RecomputeDependantsBinding,
		deadlines.ComplianceEvidenceMatcherDef().ID: deadlines.ComplianceEvidenceMatcherBinding,
		deadlines.RuleWatchDef().ID:                 deadlines.RuleWatchBinding,
		deadlines.DigestDef().ID:                    deadlines.DigestBinding,
		deadlines.PostEventFollowUpDef().ID:         deadlines.PostEventFollowUpBinding,
		deadlines.AdjournmentWatcherDef().ID:        deadlines.AdjournmentWatcherBinding,
		deadlines.AbsenceCoverageCheckDef().ID:      scheduled(deadlines.AbsenceCoverageCheckDef(), deadlines.EventAbsenceApproved),
		conflict.FeaturePartyExtraction:             conflict.PartyExtractionBinding,
		conflict.SearchRankingDef().ID:              conflict.SearchRankingBinding,
		conflict.ClearanceMemoDef().ID:              conflict.ClearanceMemoBinding,
		conflict.ConsentDraftingDef().ID:            conflict.ConsentDraftingBinding,
		conflict.ReverseSweepDef().ID:               conflict.ReverseSweepBinding,
		conflict.IDObligationWatcherDef().ID:        conflict.IDObligationWatcherBinding,
		conflict.IDEvidenceCheckDef().ID:            conflict.IDEvidenceCheckBinding,
		conflict.BeneficialOwnershipDef().ID:        conflict.BeneficialOwnershipBinding,
		conflict.WallMonitorDef().ID:                scheduled(conflict.WallMonitorDef(), conflict.EventWalledMatterAccessed),
		conflict.ExpiryRetentionDef().ID:            conflict.ExpiryRetentionBinding,
		conflict.AuditAnomalyDigestDef().ID:         scheduled(conflict.AuditAnomalyDigestDef()),
		conflict.DuplicateFinderDef().ID:            scheduled(conflict.DuplicateFinderDef()),
		legaldocs.IngestDocumentDef().ID:            legaldocs.IngestDocumentBinding,
		legaldocs.EngagementLetterDef().ID:          legaldocs.EngagementLetterBinding,
		legaldocs.DraftWithCitationsDef().ID:        legaldocs.DraftWithCitationsBinding,
		legaldocs.FeatureTemplateCurrency:           legaldocs.TemplateCurrencyBinding,
		legaldocs.PrecheckOutgoingDef().ID:          legaldocs.PrecheckOutgoingBinding,
		legaldocs.CitationStalenessDef().ID:         legaldocs.CitationStalenessBinding,
		legaldocs.PrecedentCandidatesDef().ID:       legaldocs.PrecedentCandidatesBinding,
		legaldocs.EmailFilerDef().ID:                legaldocs.EmailFilerBinding,
		legaldocs.AssemblyOnStageDef().ID:           legaldocs.AssemblyOnStageBinding,
		legaldocs.TrackedRevisionDef().ID:           legaldocs.TrackedRevisionBinding,
		legaldocs.CourtFormCurrencyDef().ID:         scheduled(legaldocs.CourtFormCurrencyDef()),
		matter.BriefRefreshDef().ID:                 matter.BriefRefreshBinding,
		matter.StageAdvanceDef().ID:                 matter.StageAdvanceBinding,
		matter.PartyExtractionDef().ID:              matter.PartyExtractionBinding,
		matter.ContactDedupeDef().ID:                matter.ContactDedupeBinding,
		matter.OpeningPrefillDef().ID:               matter.OpeningPrefillBinding,
		matter.CallSummaryDef().ID:                  matter.CallSummaryBinding,
		matter.RetainerWatchDef().ID:                matter.RetainerWatchBinding,
		matter.ConfigHygieneDef().ID:                matter.ConfigHygieneBinding,
		matter.TemplateFitDef().ID:                  matter.TemplateFitBinding,
		matter.MatterDigestDef().ID:                 matter.MatterDigestBinding,
		matter.RelatedMattersDef().ID:               matter.RelatedMattersBinding,
		matter.AccessHygieneDef().ID:                matter.AccessHygieneBinding,
		matter.FeatureChronology:                    matter.ChronologyBinding,
	}
	// onRequest lists the job features that only their screen actions start (trigger.Actions
	// serves them); they have no event or schedule to bind (Q-INTEG-16-SCREENS).
	onRequest := map[string]bool{
		matter.FeatureMatterQuestionAnswering: true,
	}
	for _, d := range s.All() {
		if d.Source != feature.SourceJob {
			continue
		}
		if onRequest[d.ID] {
			if len(d.Actions) == 0 {
				return fmt.Errorf("job feature %s: on request with no screen action: %w", d.ID, trigger.ErrBadBinding)
			}
			continue
		}
		bind, ok := byID[d.ID]
		if !ok {
			return fmt.Errorf("job feature %s: no binding: %w", d.ID, trigger.ErrBadBinding)
		}
		bd, err := bind()
		if err != nil {
			return fmt.Errorf("job feature %s: %w", d.ID, err)
		}
		if err := b.Add(bd); err != nil {
			return err
		}
	}
	return nil
}

// AddTickJob adds the TickWorker and its periodic job, which enqueue the scheduled runs.
func AddTickJob(j *kboot.Jobs, b *trigger.Bindings) error {
	if err := kboot.AddWorker(j, &trigger.TickWorker{Bindings: b}); err != nil {
		return fmt.Errorf("agent tick job: %w", err)
	}
	j.AddPeriodic(b.PeriodicJob(), trigger.JobKindTick)
	return nil
}

// FirmSubject is the subject kind of a firm-level screen's records: settings and the audit
// log belong to the firm, not to one matter.
const FirmSubject = "firm"

// actionScreens maps each screen that offers a feature action to the records it selects and
// the capability the acting user needs on each. Every screen named in a feature's Actions
// must be listed (TestActionScreensCoverFeatures).
var actionScreens = map[string]trigger.Screen{
	"matter-deadlines":      {SubjectKind: "matter", Capability: "deadlines.view"},
	"limitation-assessment": {SubjectKind: "matter", Capability: "deadlines.view"},
	// A new conflict check selects the matter it is for; results and clearance select the
	// check run (EVENTS-WIRE, Q-INTEG-15A-SCREENS).
	"conflict-check-new": {SubjectKind: "matter", Capability: "conflicts.run"},
	"conflict-results":   {SubjectKind: conflict.SubjectRun, Capability: "conflicts.run"},
	"conflict-clearance": {SubjectKind: conflict.SubjectRun, Capability: "conflicts.run"},
	// Calendar screens act on the matter the event or deadline belongs to.
	"event-detail":        {SubjectKind: "matter", Capability: "deadlines.view"},
	"firm-calendar":       {SubjectKind: "matter", Capability: "deadlines.view"},
	"rule-trigger-wizard": {SubjectKind: "matter", Capability: "deadlines.view"},
	// Conflicts and client identification (pass 15b features; Q-INTEG-15A-SCREENS).
	"consents-waivers":            {SubjectKind: conflict.SubjectRun, Capability: "conflicts.run"},
	"identity-portal":             {SubjectKind: party.SubjectKind, Capability: "conflicts.identify"},
	"organization-identification": {SubjectKind: party.SubjectKind, Capability: "conflicts.identify"},
	"client-identification":       {SubjectKind: "matter", Capability: "conflicts.identify"},
	"party-records-hygiene":       {SubjectKind: party.SubjectKind, Capability: "contacts.edit"},
	"lateral-hire-intake":         {SubjectKind: party.SubjectKind, Capability: "conflicts.run"},
	"ethical-walls":               {SubjectKind: "matter", Capability: "conflicts.walls"},
	// Firm-level screens act on the firm, not on a matter.
	"conflict-search-settings": {SubjectKind: FirmSubject, Capability: "conflicts.run"},
	"compliance-audit-log":     {SubjectKind: FirmSubject, Capability: "admin.audit_log"},
	// Documents.
	"engagement-letter-composer": {SubjectKind: "matter", Capability: "documents.edit"},
	"ai-draft-workspace":         {SubjectKind: "matter", Capability: "documents.edit"},
	// Document and matter features of pass 16 (Q-INTEG-16-SCREENS): a screen on a matter's
	// documents selects the matter; a library screen selects the documents themselves.
	"document-assembly":       {SubjectKind: "matter", Capability: "documents.edit"},
	"client-sharing":          {SubjectKind: "matter", Capability: "documents.edit"},
	"email-filing":            {SubjectKind: "matter", Capability: "documents.view"},
	"bundle-builder":          {SubjectKind: "matter", Capability: "documents.view"},
	"multi-document-analysis": {SubjectKind: "matter", Capability: "documents.view"},
	"template-library":        {SubjectKind: legaldocs.SubjectKind, Capability: "documents.edit"},
	"precedent-library":       {SubjectKind: legaldocs.SubjectKind, Capability: "documents.view"},
	"firm-library":            {SubjectKind: legaldocs.SubjectKind, Capability: "documents.view"},
	// Matters and contacts.
	"matter-overview":          {SubjectKind: "matter", Capability: "matters.view"},
	"matter-chronology":        {SubjectKind: "matter", Capability: "matters.view"},
	"matter-stage-board":       {SubjectKind: "matter", Capability: "matters.view"},
	"matter-financial-summary": {SubjectKind: "matter", Capability: "matters.view"},
	"matter-open-wizard":       {SubjectKind: "matter", Capability: "matters.view"},
	"matter-parties":           {SubjectKind: "matter", Capability: "matters.view"},
	"matter-notes-activity":    {SubjectKind: "matter", Capability: "matters.edit"},
	"practice-area-settings":   {SubjectKind: "matter", Capability: "matters.view"},
	"contact-list":             {SubjectKind: party.SubjectKind, Capability: "contacts.view"},
	"firm-home":                {SubjectKind: "matter", Capability: "matters.view"},
	"matter-access":            {SubjectKind: "matter", Capability: "matters.view"},
	"matter-access-report":     {SubjectKind: "matter", Capability: "matters.view"},
	"matter-ask":               {SubjectKind: "matter", Capability: "matters.view"},
	"global-search":            {SubjectKind: "matter", Capability: "matters.view"},
	"contact-import":           {SubjectKind: party.SubjectKind, Capability: "contacts.view"},
	"matter-list":              {SubjectKind: "matter", Capability: "matters.view"},
	"matter-templates":         {SubjectKind: "matter", Capability: "matters.view"},
	"contact-field-settings":   {SubjectKind: party.SubjectKind, Capability: "contacts.view"},
	"firm-feed":                {SubjectKind: "matter", Capability: "matters.view"},
	"contact-detail":           {SubjectKind: party.SubjectKind, Capability: "contacts.view"},
	"document-review":          {SubjectKind: "matter", Capability: "documents.view"},
	"signature-request":        {SubjectKind: "matter", Capability: "documents.edit"},
	"matter-documents":         {SubjectKind: "matter", Capability: "documents.view"},
	"document-search":          {SubjectKind: "matter", Capability: "documents.view"},
}

// ActionScreens returns a copy of the screen-to-subject map of the action route.
func ActionScreens() map[string]trigger.Screen {
	out := make(map[string]trigger.Screen, len(actionScreens))
	for k, v := range actionScreens {
		out[k] = v
	}
	return out
}

// screenOf is the screen id of one feature action, "matter-deadlines > Extract".
func screenOf(action string) string {
	s, _, _ := strings.Cut(action, ">")
	return strings.TrimSpace(s)
}

// RiverActionQueue queues a requested write feature's run as an agent_feature_run job.
type RiverActionQueue struct {
	Q        trigger.Enqueuer
	NewRunID func() string
}

// EnqueueAction implements trigger.ActionQueue. The run id stands in as the job's event id,
// so each request is its own job; the job holds ids only.
func (q RiverActionQueue) EnqueueAction(ctx context.Context, featureID string, subjects []trigger.SubjectRef, actingFor string) (string, error) {
	if q.Q == nil || q.NewRunID == nil {
		return "", fmt.Errorf("action queue: %w", agent.ErrMissingDep)
	}
	runID := q.NewRunID()
	// rule: AI-R-005
	args := trigger.RunArgs{FeatureID: featureID, Subjects: subjects, ActingFor: actingFor, EventID: runID}
	opts := args.InsertOpts()
	res, err := q.Q.Insert(ctx, args, &opts)
	if err != nil {
		return "", fmt.Errorf("action queue %s: %w", featureID, err)
	}
	// The worker records the run under the job's run id, so the caller gets that one.
	if res != nil && res.Job != nil {
		return JobRunID(res.Job.ID), nil
	}
	return runID, nil
}

// flagBlocks maps a feature id to the gate kinds its flags block. No listed feature's flags
// block a gate; a blocking feature (the GA1 duty scan) adds its entry here.
var flagBlocks = map[string][]string{}

// NewFlagSink returns the sink read-only job features record flags in.
func NewFlagSink(store agent.FlagStore, notify agent.Sender) *agent.FlagSink {
	blocks := make(map[string][]string, len(flagBlocks))
	for k, v := range flagBlocks {
		blocks[k] = slices.Clone(v)
	}
	return &agent.FlagSink{Store: store, Notify: notify, BlocksGate: blocks}
}

// ForgetRunner drops a run's provenance values when the run ends, whatever its outcome.
type ForgetRunner struct {
	agentrun.Runner
	Provenance *agenttools.Provenance
}

// Run implements the run Runner.
func (r ForgetRunner) Run(ctx context.Context, spec agentrun.Spec, p agentrun.Principal, prompt string, triggers ...agentrun.Trigger) (agentrun.Outcome, error) {
	defer r.Provenance.Forget(p.RunID)
	return r.Runner.Run(ctx, spec, p, prompt, triggers...)
}

// AgentWiring is what the agent layer needs at boot beyond its ports.
type AgentWiring struct {
	Ports AgentPorts
	// Write is what prepare_trust_withdrawal proposes through when Ports.WriteTools is nil:
	// Withdrawals (the trust withdrawal module), and FirmAccount and Bills
	// (WireBillingAgentPorts). Boot fills Can, Walls, Provenance, Contacts and TrustAccount
	// from Ports (AgentWriteTools).
	Write agent.WriteDeps
	// Designated reads, per scheduled feature, the lawyer its runs act for. Nil uses the
	// Postgres firm setting on the kernel DB (agent.designated_lawyer).
	Designated trigger.DesignatedLawyers
	// Bindings receives the job-feature bindings; nil skips triggers and the tick job.
	Bindings *trigger.Bindings
	// Actions, when set, is mounted as the screen-action route. Boot sets its Features,
	// Screens and Queue.
	Actions *trigger.Actions
	// Queue enqueues requested write features (Actions.Queue).
	Queue trigger.ActionQueue
	// Flags is the flag sink; boot sets its BlocksGate map.
	Flags *agent.FlagSink
	// Runners are every other agent runner boot hands out (the job-run worker's among them).
	// Boot wraps each in place so it forgets a run's provenance values when the run ends.
	Runners []*agentrun.Runner
	// Unready, when set, serves a tool whose port has no implementation in this process as not
	// served and reports its features not ready, instead of failing boot (Production).
	Unready *AgentReadiness
	// RunModel, RunTools and RunAccess drive the agent_feature_run worker (FeatureRunWorker):
	// the model, the tool registry the action route uses, and the builder of agent
	// principals (nil: Actions.Access). With all three set and a job set, boot adds the
	// worker. RunRecords stores its runs; nil is run.PGRecords on the kernel DB.
	RunModel   ai.ModelClient
	RunTools   agenttools.Tools
	RunAccess  RunPrincipals
	RunRecords agent.RunRecords
	// RunWorker is the agent_feature_run worker boot added; nil when it added none.
	RunWorker *FeatureRunWorker
}

// registerAgent registers the agent tools and features: every handler tool, the job
// bindings and tick job, the action route, the flag digest template and the flag sink's
// gate blocks. Nil w skips the agent layer.
func registerAgent(w *AgentWiring) hook {
	return func(ctx context.Context, k *kboot.Kernel) error {
		if w == nil {
			return nil
		}
		if k.Registry == nil {
			return ErrNoRegistry
		}
		features, err := Features()
		if err != nil {
			return err
		}
		// One Provenance for every tool, write dep and runner: a run's values are recorded,
		// checked and forgotten in the same place.
		if w.Ports.Provenance == nil {
			w.Ports.Provenance = agenttools.NewProvenance()
		}
		// The ports WireMatterPorts built publish through the kernel emitter (matter.party_added
		// from PGConflictParties); without one they refuse rather than drop the event.
		if k.Emitter != nil {
			SetMatterPortEvents(&w.Ports, k.Emitter)
		}
		if w.Ports.WriteTools == nil {
			wt, err := AgentWriteTools(w.Ports, w.Write)
			if err != nil {
				return err
			}
			w.Ports.WriteTools = wt
		}
		tools, err := AgentTools(w.Ports)
		if w.Unready != nil {
			tools, err = servedTools(w.Ports, w.Unready, features)
		}
		if err != nil {
			return err
		}
		for _, t := range tools {
			k.Registry.Tool(t)
		}
		if w.Bindings != nil {
			if d := designatedOrPG(k, w.Designated); d != nil {
				w.Bindings.WithDesignated(d)
			}
			if err := JobBindings(features, w.Bindings); err != nil {
				return err
			}
			if k.Jobs != nil {
				if err := AddTickJob(k.Jobs, w.Bindings); err != nil {
					return err
				}
			}
		}
		if err := addFeatureRunWorker(k, w, features); err != nil {
			return err
		}
		if w.Actions != nil {
			w.Actions.Features = features
			w.Actions.Screens = ActionScreens()
			if w.Queue != nil {
				w.Actions.Queue = w.Queue
			}
			w.Actions.Runner = ForgetOnEnd(w.Ports.Provenance, w.Actions.Runner)
			k.Registry.Routes(w.Actions.Routes)
		}
		for _, r := range w.Runners {
			if r != nil {
				*r = ForgetOnEnd(w.Ports.Provenance, *r)
			}
		}
		if w.Flags != nil {
			fresh := NewFlagSink(w.Flags.Store, w.Flags.Notify)
			w.Flags.BlocksGate = fresh.BlocksGate
		}
		if k.Notify != nil {
			if err := registerTemplates(ctx, k.Notify, []notify.Template{agent.FlagDigestTemplate}); err != nil {
				return err
			}
		}
		return nil
	}
}

// AgentWriteTools builds the write tools prepare_trust_withdrawal runs on: base's
// Withdrawals, FirmAccount and Bills, with Can, Walls, Provenance, Contacts (MatterContacts)
// and TrustAccount (TrustAccounts) from p. It fails closed with agent.ErrMissingDep naming
// every port that is nil, so boot never serves a withdrawal tool that skips a check.
func AgentWriteTools(p AgentPorts, base agent.WriteDeps) (*agent.WriteTools, error) {
	var missing []string
	for _, v := range []struct {
		name string
		nil  bool
	}{
		{"Can", p.Can == nil},
		{"Walls", p.Walls == nil},
		{"Provenance", p.Provenance == nil},
		{"MatterContacts", p.MatterContacts == nil},
		{"TrustAccounts", p.TrustAccounts == nil},
		{"Withdrawals", base.Withdrawals == nil},
		{"FirmAccount", base.FirmAccount == nil},
		{"Bills", base.Bills == nil},
	} {
		if v.nil {
			missing = append(missing, v.name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("agent write tools: %s: %w", strings.Join(missing, ", "), agent.ErrMissingDep)
	}
	d := AgentWriteDeps(p, base)
	d.Can = p.Can
	d.Walls = p.Walls
	d.Contacts = p.MatterContacts
	d.TrustAccount = soleTrustAccount(p.TrustAccounts)
	// rule: ARC-U-027
	// The agent reaches the trust module through Request and Available only: the wrapper
	// hides the concrete service, so no agent code can assert its way to Authorize or
	// RecordExecution.
	d.Withdrawals = requestOnly{w: base.Withdrawals}
	wt, err := agent.NewWriteTools(d)
	if err != nil {
		return nil, fmt.Errorf("agent write tools: %w", err)
	}
	return wt, nil
}

// requestOnly exposes only the request half of the trust withdrawal module.
type requestOnly struct{ w agent.Withdrawals }

// Request implements agent.Withdrawals.
func (r requestOnly) Request(ctx context.Context, req withdraw.Request) (withdraw.Withdrawal, error) {
	return r.w.Request(ctx, req)
}

// Available implements agent.Withdrawals.
func (r requestOnly) Available(ctx context.Context, trustAccountID, matterID string) (int64, error) {
	return r.w.Available(ctx, trustAccountID, matterID)
}

// soleTrustAccount returns the one trust account that holds a matter's ledger. A matter with
// no ledger, or with ledgers in several accounts, fails closed: the agent never picks the
// account a withdrawal leaves from (By-Law 9 s.9(2): money leaves only the client's own ledger).
func soleTrustAccount(src agent.MatterTrustAccounts) func(ctx context.Context, matterID string) (string, error) {
	return func(ctx context.Context, matterID string) (string, error) {
		accts, err := src.TrustAccountsOf(ctx, matterID)
		if err != nil {
			return "", fmt.Errorf("trust accounts of %s: %w", matterID, err)
		}
		// rule: BIL-L-036
		if len(accts) != 1 {
			return "", fmt.Errorf("trust accounts of %s: %d ledgers: %w", matterID, len(accts), agent.ErrInvalidInput)
		}
		return accts[0], nil
	}
}

// registerAgent registers Options.Agent as it stands when the step runs, so the wiring
// bindProduction built is the one registered.
func (w *wiring) registerAgent(ctx context.Context, k *kboot.Kernel) error {
	return registerAgent(w.opts.Agent)(ctx, k)
}

// addFeatureRunWorker adds the agent_feature_run worker to k's job set when w carries a model,
// a tool registry and a principal builder. The run records default to agent.run_record on
// the kernel DB; with neither, the worker is not added and the queued features stay queued.
func addFeatureRunWorker(k *kboot.Kernel, w *AgentWiring, features *feature.Set) error {
	if k.Jobs == nil || w.RunModel == nil || w.RunTools == nil {
		return nil
	}
	access := w.RunAccess
	if access == nil && w.Actions != nil && w.Actions.Access != nil {
		access = w.Actions.Access
	}
	records := w.RunRecords
	if records == nil && k.DB != nil {
		records = agentrun.PGRecords{DB: k.DB}
	}
	if access == nil || records == nil {
		slog.Warn("agent feature run worker not added: no principal builder or run store")
		return nil
	}
	wk := &FeatureRunWorker{Features: features, Access: access, Model: w.RunModel, Tools: w.RunTools,
		Provenance: w.Ports.Provenance, Records: records, Designated: designatedOrPG(k, w.Designated)}
	if k.Clock != nil {
		wk.Now = k.Clock.Now
	}
	if err := kboot.AddWorker(k.Jobs, wk); err != nil {
		return fmt.Errorf("agent feature run worker: %w", err)
	}
	w.RunWorker = wk
	return nil
}
