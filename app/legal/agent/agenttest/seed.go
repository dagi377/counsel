package agenttest

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/app/legal/billing/bill/billfake"
	legaldocs "github.com/dagi377/counsel/app/legal/docs"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/matter/matterfake"
	"github.com/dagi377/counsel/app/legal/party"
	"github.com/dagi377/counsel/app/legal/party/partyfake"
	"github.com/dagi377/counsel/app/legal/roles/rolesfake"
	tk "github.com/dagi377/counsel/app/legal/timekeeping"
	"github.com/dagi377/counsel/app/legal/timekeeping/timekeepingfake"
	"github.com/dagi377/counsel/app/legal/trust/withdraw/withdrawfake"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/deadline/deadlinefake"
	kdocs "github.com/dagi377/counsel/kernel/docs"
	"github.com/dagi377/counsel/kernel/docs/docsfake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/work"
	"github.com/dagi377/counsel/kernel/work/workfake"
	"github.com/jackc/pgx/v5"
)

// SeedDocKind is the document kind seeded versions are stored under.
const SeedDocKind = "legal.synthetic"

// seedNow is the fixed clock of seeded records: a weekday morning in America/Toronto.
var seedNow = time.Date(2026, 10, 5, 9, 0, 0, 0, mustToronto())

func mustToronto() *time.Location {
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		return time.FixedZone("EDT", -4*60*60)
	}
	return loc
}

// Seed holds the module fakes a feature run reads through, filled with synthetic records
// only. Its Deps feed the real tool handlers; a handler whose dependency the Seed lacks falls
// back to a stand-in.
type Seed struct {
	ActingFor   string
	Walls       *rolesfake.Fake
	Contacts    *partyfake.Fake
	Matters     *matterfake.Fake
	Text        *docsfake.Fake
	Work        *workfake.Fake
	Deadlines   *deadlinefake.Fake
	Time        *timekeepingfake.Fake
	Bills       *billfake.Fake
	Withdrawals *withdrawfake.Fake
	Gates       *gatefake.Fake
	Store       *SeedStore

	t testing.TB
}

// NewSeed returns an empty seed for runs acting for actingFor. WalledMatter is walled from
// actingFor; a gate the run opens is stored in Gates under any kind the seed registered.
func NewSeed(t testing.TB, actingFor string, withdrawals *withdrawfake.Fake) *Seed {
	t.Helper()
	contacts := partyfake.New(func() time.Time { return seedNow })
	text := docsfake.New()
	if err := text.RegisterKind(kdocs.Kind{ID: SeedDocKind, Classification: kdocs.Confidential}); err != nil {
		t.Fatalf("seed: doc kind: %v", err)
	}
	walls := rolesfake.New()
	walls.Wall(actingFor, WalledMatter)
	return &Seed{
		ActingFor:   actingFor,
		Walls:       walls,
		Contacts:    contacts,
		Matters:     matterfake.New(func() time.Time { return seedNow }, contacts),
		Text:        text,
		Work:        workfake.New(),
		Deadlines:   deadlinefake.New(),
		Time:        timekeepingfake.New(nil, nil),
		Bills:       billfake.New(func() time.Time { return seedNow }),
		Withdrawals: withdrawals,
		Gates:       gatefake.New(func() time.Time { return seedNow }, gatefake.Caps{}),
		Store:       newSeedStore(),
		t:           t,
	}
}

// Deps returns the module query APIs the real handlers read through, with can as the
// acting user's capability check and plans as the run's plan store.
func (s *Seed) Deps(can agent.CapabilityCheck, plans agenttools.Plans) agent.Deps {
	open := agenttools.GateOpener(func(ctx context.Context, kind string, subject registry.Subject, payload json.RawMessage, preparer registry.Principal) (gate.GateID, error) {
		return s.Gates.Open(ctx, nil, kind, subject, payload, preparer)
	})
	return agent.NewDeps(
		can, s.Walls, plans, open, agenttools.RunScopes{},
		agent.ContactSearch(s.Contacts), agent.ClientMatters(s.Matters), agent.WorkList(s.Work),
		agent.DocumentText(s.Text), s.Store,
	)
}

// GateKind registers a gate kind in Gates so the run's write tools can open it.
func (s *Seed) GateKind(slug string) {
	s.t.Helper()
	err := s.Gates.Register(gate.GateKind{
		Slug: slug, Approver: slug + ".approve", Risk: gate.RiskMedium, MinApprovers: 1, TTL: 48 * time.Hour,
		Effect: func(context.Context, pgx.Tx, gate.Approved) error { return nil },
	})
	if err != nil {
		s.t.Fatalf("seed: gate kind %s: %v", slug, err)
	}
}

// Contact creates a synthetic client contact.
func (s *Seed) Contact(name string) party.Contact {
	s.t.Helper()
	c, err := s.Contacts.Create(context.Background(), party.NewContact{
		Kind: party.Organization, LegalName: name, DisplayName: name, IsClient: true, CreatedBy: s.ActingFor,
	})
	if err != nil {
		s.t.Fatalf("seed: contact: %v", err)
	}
	return c
}

// Matter creates a synthetic matter for a new client contact, responsible lawyer ActingFor.
func (s *Seed) Matter(number, shortName string) matter.Matter {
	s.t.Helper()
	client := s.Contact(shortName + " Client Inc.")
	m, err := s.Matters.Create(context.Background(), matter.NewMatter{
		Number: number, ShortName: shortName, ClientID: client.ID, PracticeAreaID: "litigation",
		ResponsibleLawyerID: s.ActingFor, CreatedBy: s.ActingFor,
	})
	if err != nil {
		s.t.Fatalf("seed: matter: %v", err)
	}
	s.Store.addContact(string(m.ID), string(client.ID))
	return m
}

// Wall hides matterID from the acting user.
func (s *Seed) Wall(matterID string) {
	s.Walls.Wall(s.ActingFor, matterID)
}

// Document files a synthetic document on matterID whose current version holds pages, each a
// list of lines. It returns the document id and version id.
func (s *Seed) Document(matterID string, pages ...[]string) (string, kdocs.VersionID) {
	s.t.Helper()
	body := make([]string, 0, len(pages))
	for _, p := range pages {
		body = append(body, strings.Join(p, "\n"))
	}
	v, err := s.Text.Put(context.Background(), nil, SeedDocKind, kdocs.Subject{Kind: "matter", ID: matterID}, strings.NewReader(strings.Join(body, "\f")))
	if err != nil {
		s.t.Fatalf("seed: document: %v", err)
	}
	id := s.Store.addDocument(matterID, SeedDocKind, v)
	return id, v
}

// Deadline saves a proposed synthetic deadline on matterID.
func (s *Seed) Deadline(matterID, rule string, due deadline.Date) deadline.DeadlineID {
	s.t.Helper()
	id, err := s.Deadlines.Save(context.Background(), nil, deadline.Proposed{
		PackID: "synthetic", PackVersion: "1", RuleID: rule, Trigger: due, Due: due,
		Subject: deadline.Subject{Kind: "matter", ID: matterID},
	})
	if err != nil {
		s.t.Fatalf("seed: deadline: %v", err)
	}
	return id
}

// WorkItem creates an open synthetic task on matterID.
func (s *Seed) WorkItem(matterID, title string) work.ItemID {
	s.t.Helper()
	id, err := s.Work.Create(context.Background(), nil, work.Item{
		Type: "task", Title: title, WorkflowID: "default", StatusID: "open", ReporterID: s.ActingFor,
		AssigneeUserID: s.ActingFor, Subject: work.Subject{Kind: agent.WorkSubjectMatter, ID: matterID},
	})
	if err != nil {
		s.t.Fatalf("seed: work item: %v", err)
	}
	return id
}

// UnbilledTime records an approved, unbilled synthetic time entry on matterID.
func (s *Seed) UnbilledTime(matterID string, minutes int, narrative string) tk.EntryID {
	s.t.Helper()
	id, err := s.Time.Record(context.Background(), nil, tk.TimeEntry{
		MatterID: matterID, UserID: s.ActingFor, WorkDate: seedNow, Minutes: minutes, BilledMinutes: minutes,
		ActivityCode: "A101", Narrative: narrative, Billable: true,
	}, tk.Actor{UserID: s.ActingFor})
	if err != nil {
		s.t.Fatalf("seed: time: %v", err)
	}
	return id
}

// Bill puts a draft synthetic bill on matterID for feesCents of fees, with no tax.
func (s *Seed) Bill(id bill.ID, matterID string, feesCents int64) bill.Bill {
	s.t.Helper()
	b := bill.Bill{
		ID: id, MatterID: matterID, Status: bill.StatusDraft, PreparedBy: s.ActingFor,
		FeesCents: feesCents, TotalCents: feesCents, BalanceCents: feesCents, CreatedAt: seedNow,
	}
	if err := s.Bills.Put(b); err != nil {
		s.t.Fatalf("seed: bill: %v", err)
	}
	return b
}

// TrustBalance sets matterID's cleared balance in TrustAccount.
func (s *Seed) TrustBalance(matterID string, cents int64) {
	s.Withdrawals.SetBalance(TrustAccount, matterID, cents, 0)
}

// SeedStore is the in-memory module side the seed owns: the documents filed per matter,
// matter contacts and the drafts the run's write tools made.
type SeedStore struct {
	mu       sync.Mutex
	seq      int
	docs     map[string][]agent.ListedDocument // matter id -> filed documents
	contacts map[string][]string               // matter id -> contact ids
	Drafts   []SeedDraft
}

// SeedDraft is one draft a real draft tool created through the seed.
type SeedDraft struct {
	ID         string
	Kind       string // "document", "email" or "note"
	MatterID   string
	PreparedBy string
	RunID      string
}

func newSeedStore() *SeedStore {
	return &SeedStore{docs: map[string][]agent.ListedDocument{}, contacts: map[string][]string{}}
}

func (s *SeedStore) next(prefix string) string {
	s.seq++
	return fmt.Sprintf("%s-%d", prefix, s.seq)
}

func (s *SeedStore) addContact(matterID, contactID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.contacts[matterID] = append(s.contacts[matterID], contactID)
}

func (s *SeedStore) addDocument(matterID, kind string, v kdocs.VersionID) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.next("doc")
	s.docs[matterID] = append(s.docs[matterID], agent.ListedDocument{
		Document:  legaldocs.Document{ID: id, MatterID: matterID, Kind: kind, FolderID: legaldocs.FolderID("folder-" + matterID)},
		VersionID: v,
	})
	return id
}

// MatterDocuments implements agent.MatterDocuments.
func (s *SeedStore) MatterDocuments(_ context.Context, matterID string) ([]agent.ListedDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.docs[matterID]), nil
}

// Folders implements agent.MatterFolders: one folder per matter that has documents.
func (s *SeedStore) Folders(_ context.Context, matterID string) ([]legaldocs.Folder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.docs[matterID]) == 0 {
		return nil, nil
	}
	return []legaldocs.Folder{{ID: legaldocs.FolderID("folder-" + matterID), MatterID: matterID, Name: "Documents"}}, nil
}

// MatterContactIDs implements agent.MatterContacts.
func (s *SeedStore) MatterContactIDs(_ context.Context, matterID string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.contacts[matterID]), nil
}

func (s *SeedStore) draft(kind, matterID, by, runID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.next("draft-" + kind)
	s.Drafts = append(s.Drafts, SeedDraft{ID: id, Kind: kind, MatterID: matterID, PreparedBy: by, RunID: runID})
	return id
}

// CreateDraft implements agent.DocumentDrafts.
func (s *SeedStore) CreateDraft(_ context.Context, d agent.NewDraftDocument) (string, error) {
	return s.draft("document", d.MatterID, d.PreparedBy, d.RunID), nil
}

// CreateEmailDraft implements agent.EmailDrafts.
func (s *SeedStore) CreateEmailDraft(_ context.Context, d agent.NewDraftEmail) (string, error) {
	return s.draft("email", d.MatterID, d.PreparedBy, d.RunID), nil
}

// CreateNoteDraft implements agent.NoteDrafts.
func (s *SeedStore) CreateNoteDraft(_ context.Context, n agent.NewDraftNote) (string, error) {
	return s.draft("note", n.MatterID, n.PreparedBy, n.RunID), nil
}
