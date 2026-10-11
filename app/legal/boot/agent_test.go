package boot

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/billing/batch"
	"github.com/dagi377/counsel/app/legal/billing/draft"
	"github.com/dagi377/counsel/app/legal/billing/payment"
	"github.com/dagi377/counsel/app/legal/comms"
	"github.com/dagi377/counsel/app/legal/conflict"
	"github.com/dagi377/counsel/app/legal/deadlines"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/roles"
	tk "github.com/dagi377/counsel/app/legal/timekeeping"
	"github.com/dagi377/counsel/kernel/agenttools"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/gate"
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
		Entries:         stubEntries{},
		UnbilledWIP:     stubWIP{},
		MatterTime:      stubMatterTime{},
		DraftCalc:       stubCalc{},
		DraftStore:      stubDraftStore{},
		BatchDrafts:     stubBatch{},
		Payments:        stubPayments{},
		SubjectMatters:  stubSubjects{},
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
		name      string
		ruleAdmin string
		wantErr   error
	}{
		{"every job feature binds", "admin-1", nil},
		{"scheduled rule watch needs an acting user", "", trigger.ErrBadBinding},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clk, err := clock.New("America/Toronto")
			if err != nil {
				t.Fatal(err)
			}
			b := trigger.New(stubEnqueuerNone{}, clk, nil)
			err = JobBindings(s, b, tc.ruleAdmin)
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
