package boot

import (
	"github.com/dagi377/counsel/app/legal/trust/withdraw/withdrawfake"

	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/billing"
	"github.com/dagi377/counsel/app/legal/billing/batch"
	"github.com/dagi377/counsel/app/legal/billing/draft"
	"github.com/dagi377/counsel/app/legal/billing/payment"
	"github.com/dagi377/counsel/app/legal/comms"
	"github.com/dagi377/counsel/app/legal/conflict"
	"github.com/dagi377/counsel/app/legal/deadlines"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/platform"
	"github.com/dagi377/counsel/app/legal/roles"
	tk "github.com/dagi377/counsel/app/legal/timekeeping"
	"github.com/dagi377/counsel/kernel/agenttools"
	agentrun "github.com/dagi377/counsel/kernel/agenttools/run"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/db/dbfake"
	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/outbox"
	"github.com/dagi377/counsel/kernel/registry"
)

// Port stand-ins: building a handler reads its ports but never calls them, so an embedded
// nil interface is enough to prove the wire exists.
type (
	stubWalls        struct{ roles.Walls }
	stubPlans        struct{ agenttools.Plans }
	stubContacts     struct{ agent.ContactSearch }
	stubMatters      struct{ agent.ClientMatters }
	stubWork         struct{ agent.WorkList }
	stubText         struct{ agent.DocumentText }
	stubDocs         struct{ docs agent.MatterDocuments }
	stubFolders      struct{ agent.MatterFolders }
	stubBills        struct{ agent.BillReader }
	stubWIP          struct{ agent.UnbilledWIP }
	stubMatterQuery  struct{ matter.Query }
	stubDocLookup    struct{ agent.DocumentLookup }
	stubBalances     struct{ agent.BillBalances }
	stubTrust        struct{ agent.TrustAvailable }
	stubTrustAccts   struct{ agent.MatterTrustAccounts }
	stubEntries      struct{ tk.Entries }
	stubMatterTime   struct{ mt agent.MatterTime }
	stubCalc         struct{ draft.Calc }
	stubDraftStore   struct{ draft.Store }
	stubBatch        struct{ batch.Drafts }
	stubPayments     struct{ payment.Service }
	stubSubjects     struct{ agent.SubjectMatters }
	stubLister       struct{ deadline.Lister }
	stubCompute      struct{ agent.DeadlineCompute }
	stubSaver        struct{ agent.DeadlineSaver }
	stubGetter       struct{ agent.DeadlineGetter }
	stubPacks        struct{ packs agent.PublishedPacks }
	stubTasks        struct{ agent.TaskCreator }
	stubProposed     struct{ agent.ProposedTasks }
	stubConflicts    struct{ conflict.Checker }
	stubParties      struct{ agent.ConflictCheckParties }
	stubContactIDs   struct{ agent.MatterContacts }
	stubDocDrafts    struct{ agent.DocumentDrafts }
	stubEmailDrafts  struct{ agent.EmailDrafts }
	stubNoteDrafts   struct{ agent.NoteDrafts }
	stubEnqueuerNone struct{ trigger.Enqueuer }
)

func fullPorts() AgentPorts {
	return AgentPorts{
		Can:             func(context.Context, string, string) (bool, error) { return false, nil },
		Walls:           stubWalls{},
		Plans:           stubPlans{},
		Provenance:      agenttools.NewProvenance(),
		Contacts:        stubContacts{},
		ClientMatters:   stubMatters{},
		Work:            stubWork{},
		Text:            stubText{},
		Documents:       stubDocs{},
		Folders:         stubFolders{},
		Bills:           stubBills{},
		Matters:         stubMatterQuery{},
		DocLookup:       stubDocLookup{},
		WriteTools:      mustWriteTools(),
		BillBalances:    stubBalances{},
		Trust:           stubTrust{},
		TrustAccounts:   stubTrustAccts{},
		TrustTransfers:  withdrawfake.NewTransfers().Requests(nil),
		Entries:         stubEntries{},
		UnbilledWIP:     stubWIP{},
		MatterTime:      stubMatterTime{},
		DraftCalc:       stubCalc{},
		DraftStore:      stubDraftStore{},
		BatchDrafts:     stubBatch{},
		Payments:        stubPayments{},
		SubjectMatters:  stubSubjects{},
		PayloadBuilders: agenttools.NewPayloadBuilders(),
		TrustBalances:   signedBalance(0),
		Deadlines:       stubLister{},
		DeadlineCompute: stubCompute{},
		DeadlineSaver:   stubSaver{},
		DeadlineGetter:  stubGetter{},
		PublishedPacks:  stubPacks{},
		Tasks:           stubTasks{},
		ProposedTasks:   stubProposed{},
		Conflicts:       stubConflicts{},
		ConflictParties: stubParties{},
		MatterContacts:  stubContactIDs{},
		DocumentDrafts:  stubDocDrafts{},
		EmailDrafts:     stubEmailDrafts{},
		NoteDrafts:      stubNoteDrafts{},
	}
}

func withGate(p AgentPorts) AgentPorts {
	p.OpenGate = agenttools.GateOpener(func(context.Context, string, registry.Subject, json.RawMessage, registry.Principal) (gate.GateID, error) {
		return "", nil
	})
	return p
}

func TestAgentToolsBuild(t *testing.T) {
	full := withGate(fullPorts())
	noTrust := full
	noTrust.TrustAccounts = nil
	tests := []struct {
		name    string
		ports   AgentPorts
		wantErr error
	}{
		{"every port wired builds the full handler set", full, nil},
		{"a missing port fails closed", noTrust, agent.ErrMissingDep},
		{"no ports fails closed", AgentPorts{}, agent.ErrMissingDep},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tools, err := AgentTools(tc.ports)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}
			if len(tc.ports.Missing()) != 0 {
				t.Fatalf("missing ports %v", tc.ports.Missing())
			}
			var names []string
			for _, tl := range tools {
				names = append(names, tl.Name)
			}
			if want := agent.Handlers().Names(); !slices.Equal(names, want) {
				t.Fatalf("tools = %v, want %v", names, want)
			}
		})
	}
}

func TestFeaturesResolveToolsAndGateKinds(t *testing.T) {
	s, err := Features()
	if err != nil {
		t.Fatalf("Features: %v", err)
	}
	tools, err := ServedToolNames()
	if err != nil {
		t.Fatal(err)
	}
	kinds, err := gateKindCodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.All()) == 0 {
		t.Fatal("empty feature set")
	}
	for _, d := range s.All() {
		for _, tl := range d.Tools {
			if !slices.Contains(tools, tl) {
				t.Errorf("%s: tool %s unresolved", d.ID, tl)
			}
		}
		for _, g := range d.GateKinds {
			if !slices.Contains(kinds, g) {
				t.Errorf("%s: gate kind %s unresolved", d.ID, g)
			}
		}
	}
}

func TestJobBindings(t *testing.T) {
	s, err := Features()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		wantErr error
	}{
		{"every job feature binds, scheduled ones to their designated lawyer", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clk, err := clock.New("America/Toronto")
			if err != nil {
				t.Fatal(err)
			}
			b := trigger.New(stubEnqueuerNone{}, clk, nil)
			// rule: ARC-I-040
			err = JobBindings(s, b)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}
			if len(b.EventTypes()) == 0 {
				t.Fatal("no event types bound")
			}
			// F-DL-01 starts on a document or an email filed to a matter (Q-DRAFT-FIX).
			for _, ev := range []string{deadlines.EventDocumentUploaded, comms.EventEmailFiled} {
				if !slices.Contains(b.EventTypes(), ev) {
					t.Fatalf("event types %v lack %s", b.EventTypes(), ev)
				}
			}
			j := kboot.NewJobs()
			if err := AddTickJob(j, b); err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(j.Kinds(), trigger.JobKindTick) {
				t.Fatalf("kinds %v lack %s", j.Kinds(), trigger.JobKindTick)
			}
			if err := j.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// recordingEnqueuer keeps the feature id of every run a binding enqueues.
type recordingEnqueuer struct{ features []string }

func (r *recordingEnqueuer) Insert(_ context.Context, args river.JobArgs, _ *river.InsertOpts) (*rivertype.JobInsertResult, error) {
	if a, ok := args.(trigger.RunArgs); ok {
		r.features = append(r.features, a.FeatureID)
	}
	return &rivertype.JobInsertResult{}, nil
}

// A consented call transcript starts F-MAT-07 only: F-PLT-04 summarizes a call on request
// from the comms-log, so one transcript never starts two summaries (Q-FEAT-PLT-06).
func TestCallTranscriptStartsOneSummary(t *testing.T) {
	s, err := Features()
	if err != nil {
		t.Fatal(err)
	}
	clk, err := clock.New("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	q := &recordingEnqueuer{}
	b := trigger.New(q, clk, nil)
	if err := JobBindings(s, b); err != nil {
		t.Fatal(err)
	}
	n, err := b.OnEvent(context.Background(), outbox.Message{
		ID: "evt-transcript-synthetic", Type: matter.EventCallTranscriptFiled,
		Subject: outbox.Subject{Kind: "matter", ID: "matter-synthetic"}, Payload: json.RawMessage(`{"user_id":"user-synthetic-lawyer"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || !slices.Equal(q.features, []string{matter.FeatureCallSummary}) {
		t.Fatalf("transcript started %d runs %v, want only %s", n, q.features, matter.FeatureCallSummary)
	}
	d, ok := s.ByID(platform.FeatureCallSummarizer)
	if !ok {
		t.Fatalf("%s not in the feature set", platform.FeatureCallSummarizer)
	}
	if !slices.Equal(d.Actions, []string{platform.ActionSummarizeCall}) {
		t.Fatalf("%s actions = %v, want %s", d.ID, d.Actions, platform.ActionSummarizeCall)
	}
	if _, ok := ActionScreens()[screenOf(platform.ActionSummarizeCall)]; !ok {
		t.Fatalf("screen of %q not mapped", platform.ActionSummarizeCall)
	}
}

func TestActionScreensCoverFeatures(t *testing.T) {
	s, err := Features()
	if err != nil {
		t.Fatal(err)
	}
	screens := ActionScreens()
	for _, d := range s.All() {
		for _, a := range d.Actions {
			if _, ok := screens[screenOf(a)]; !ok {
				t.Errorf("%s: screen %q of action %q has no subject map entry", d.ID, screenOf(a), a)
			}
		}
	}
}

func TestFlagSinkBlocks(t *testing.T) {
	sink := NewFlagSink(nil, nil)
	if sink.BlocksGate == nil {
		t.Fatal("BlocksGate not set")
	}
	for id := range sink.BlocksGate {
		if _, ok := (func() (feature.Def, bool) { s, _ := Features(); return s.ByID(id) })(); !ok {
			t.Errorf("flag block for unknown feature %s", id)
		}
	}
}

func (s stubDocs) MatterDocuments(ctx context.Context, m string) ([]agent.ListedDocument, error) {
	return s.docs.MatterDocuments(ctx, m)
}

func (s stubMatterTime) MatterTime(ctx context.Context, m string) ([]agent.PricedEntry, error) {
	return s.mt.MatterTime(ctx, m)
}

func (s stubPacks) PublishedPacks(ctx context.Context) ([]deadline.Pack, error) {
	return s.packs.PublishedPacks(ctx)
}

func mustWriteTools() *agent.WriteTools {
	w, err := agent.NewWriteTools(agent.WriteDeps{})
	if err != nil {
		panic(err)
	}
	return w
}

type stubUsers struct{}

func (stubUsers) ActiveUsers(context.Context, string) ([]string, error) { return []string{"u-1"}, nil }

// TestTimekeepersSource: boot hands the per-user features the wired user source, else the
// database's active timekeepers, else nothing (the features stay unready).
func TestTimekeepersSource(t *testing.T) {
	pool := dbfake.New()
	cases := []struct {
		name  string
		wired trigger.ActiveUsers
		pool  db.DB
		want  trigger.ActiveUsers
	}{
		{"wired source wins", stubUsers{}, pool, stubUsers{}},
		{"database timekeepers", nil, pool, tk.PGTimekeepers{DB: pool}},
		{"no source", nil, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := timekeepers(tc.wired, tc.pool); got != tc.want {
				t.Fatalf("source = %#v, want %#v", got, tc.want)
			}
		})
	}
}

// TestTrustDraftsWrapped is the guard: every trust feature (F-TRU-*) whose tools include
// draft_document or draft_email must have that tool wrapped in the production handler set, as
// must F-BIL-08's draft_document (a credit note changes what a client owes); any other
// feature's drafts must not be.
//
// rule: ARC-U-027
func TestTrustDraftsWrapped(t *testing.T) {
	set, err := AgentHandlers()
	if err != nil {
		t.Fatal(err)
	}
	features, err := Features()
	if err != nil {
		t.Fatal(err)
	}
	var trustDrafts, creditNotes int
	for _, d := range features.All() {
		isTrust := strings.HasPrefix(d.ID, "F-TRU-") || d.Area == "trust"
		for _, tool := range d.Tools {
			if tool != agent.ToolDraftDocument && tool != agent.ToolDraftEmail {
				continue
			}
			isCreditNote := d.ID == billing.FeatureDraftCreditNote && tool == agent.ToolDraftDocument
			want := isTrust || isCreditNote
			if isTrust {
				trustDrafts++
			}
			if isCreditNote {
				creditNotes++
			}
			if got := set.Wrapped(d.ID, tool); got != want {
				t.Errorf("feature %s tool %s: wrapped = %v, want %v (trust drafts go through trust.SourcedCents, F-BIL-08 through billing.CheckCreditNote)", d.ID, tool, got, want)
			}
		}
	}
	if trustDrafts == 0 || creditNotes != 1 {
		t.Fatalf("trust drafts %d, credit-note drafts %d: the guard checks nothing", trustDrafts, creditNotes)
	}
}

// TestForgetRunnerScopesFeature checks that the production runner wrapper puts the run's
// feature in ctx, so the per-feature wraps apply on the production run path.
//
// rule: ARC-U-027
func TestForgetRunnerScopesFeature(t *testing.T) {
	var got string
	r := ForgetOnEnd(agenttools.NewProvenance(), runnerFunc(func(ctx context.Context) {
		got = agent.FeatureOf(ctx)
	}))
	if _, err := r.Run(context.Background(), agentrun.Spec{FeatureID: "F-TRU-06"}, agentrun.Principal{RunID: "run-1"}, "go"); err != nil {
		t.Fatal(err)
	}
	if got != "F-TRU-06" {
		t.Fatalf("feature in ctx = %q, want F-TRU-06", got)
	}
}

// runnerFunc is a run Runner that only observes its ctx.
type runnerFunc func(ctx context.Context)

// Run implements the run Runner.
func (f runnerFunc) Run(ctx context.Context, _ agentrun.Spec, _ agentrun.Principal, _ string, _ ...agentrun.Trigger) (agentrun.Outcome, error) {
	f(ctx)
	return agentrun.Outcome{}, nil
}

// TestStrictScreenCapabilities pins the screens whose capability a merge once loosened
// (Q-INTEG-20): their actions start money or client-facing work.
func TestStrictScreenCapabilities(t *testing.T) {
	tests := []struct {
		screen, capability string
	}{
		{"matter-closing", "matters.close"},
		{"client-portal", "documents.edit"},
	}
	screens := ActionScreens()
	for _, tt := range tests {
		t.Run(tt.screen, func(t *testing.T) {
			got, ok := screens[tt.screen]
			if !ok {
				t.Fatalf("screen %q not mapped", tt.screen)
			}
			if got.Capability != tt.capability {
				t.Fatalf("screen %q capability = %q, want %q", tt.screen, got.Capability, tt.capability)
			}
		})
	}
}
