package boot

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/kernel/agenttools"
	agentrun "github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/deadline"
)

// noDB fails every transaction: these tests check what the adapters refuse before any query.
type noDB struct{}

var errNoDB = errors.New("no database in unit tests")

func (noDB) InTx(context.Context, func(context.Context, pgx.Tx) error) error { return errNoDB }
func (noDB) Bind(context.Context, pgx.Tx, db.Session) error                  { return errNoDB }
func (noDB) Ping(context.Context) error                                      { return errNoDB }
func (noDB) Close()                                                          {}

type noteBodies struct{ puts int }

// oneMatter is the matter module's Get over one matter.
type oneMatter struct{ m matter.Matter }

func (o oneMatter) Get(_ context.Context, id matter.ID) (matter.Matter, error) {
	if id != o.m.ID {
		return matter.Matter{}, matter.ErrNotFound
	}
	return o.m, nil
}

func (b *noteBodies) Put(context.Context, string) (string, error) {
	b.puts++
	return "obj-1", nil
}

var matterPortNames = []string{"PublishedPacks", "SubjectMatters", "ProposedTasks", "ConflictParties",
	"MatterContacts", "DocumentDrafts", "EmailDrafts", "NoteDrafts", "Plans", "Provenance"}

func TestWireMatterPorts(t *testing.T) {
	cases := []struct {
		name    string
		ports   *AgentPorts
		db      db.DB
		matters MatterGetter
		bodies  *noteBodies
		err     bool
	}{
		{"fills every matter port", &AgentPorts{}, noDB{}, oneMatter{}, &noteBodies{}, false},
		{"nil ports", nil, noDB{}, oneMatter{}, &noteBodies{}, true},
		{"nil db", &AgentPorts{}, nil, oneMatter{}, &noteBodies{}, true},
		{"nil matter query", &AgentPorts{}, noDB{}, nil, &noteBodies{}, true},
		{"nil bodies", &AgentPorts{}, noDB{}, oneMatter{}, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			if tc.bodies == nil {
				err = WireMatterPorts(tc.ports, tc.db, tc.matters, nil)
			} else {
				err = WireMatterPorts(tc.ports, tc.db, tc.matters, tc.bodies)
			}
			if (err != nil) != tc.err {
				t.Fatalf("err = %v, want error %v", err, tc.err)
			}
			if tc.err {
				return
			}
			for _, m := range tc.ports.Missing() {
				if slices.Contains(matterPortNames, m) {
					t.Errorf("port %s still missing", m)
				}
			}
			if _, ok := tc.ports.Plans.(agenttools.StepIDs); !ok {
				t.Error("Plans does not store step ids")
			}
			if w := AgentWriteDeps(*tc.ports, agent.WriteDeps{}); w.Provenance != tc.ports.Provenance {
				t.Error("write deps do not share the ports' Provenance")
			}
		})
	}
}

// recRunner records runs; each run observes a figure the way a read tool would.
type recRunner struct {
	pv   *agenttools.Provenance
	runs int
}

func (r *recRunner) Run(_ context.Context, _ agentrun.Spec, p agentrun.Principal, _ string, _ ...agentrun.Trigger) (agentrun.Outcome, error) {
	r.runs++
	r.pv.Observe(p.RunID, json.RawMessage(`{"amount_cents":12500}`))
	return agentrun.Outcome{}, nil
}

func TestForgetOnEnd(t *testing.T) {
	ctx := context.Background()
	in := json.RawMessage(`{"amount_cents":12500}`)
	cases := []struct {
		name string
		wrap func(pv *agenttools.Provenance, r agentrun.Runner) agentrun.Runner
	}{
		{"wrapped once", ForgetOnEnd},
		{"wrapped twice stays one wrapper", func(pv *agenttools.Provenance, r agentrun.Runner) agentrun.Runner {
			once := ForgetOnEnd(pv, r)
			twice := ForgetOnEnd(pv, once)
			if twice != once {
				t.Fatal("double wrap")
			}
			return twice
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pv := agenttools.NewProvenance()
			inner := &recRunner{pv: pv}
			r := tc.wrap(pv, inner)
			p := agentrun.Principal{RunID: "run-1"}
			if _, err := r.Run(ctx, agentrun.Spec{}, p, "x"); err != nil {
				t.Fatal(err)
			}
			if inner.runs != 1 {
				t.Fatalf("inner runs = %d", inner.runs)
			}
			if err := pv.Check("run-1", []string{"amount_cents"}, in); err == nil {
				t.Fatal("run values survive the end of the run")
			}
		})
	}
	if ForgetOnEnd(nil, nil) != nil {
		t.Fatal("nil runner wrapped")
	}
}

func TestSubjectMatterFailsClosed(t *testing.T) {
	const known = "0192f000-0000-7000-8000-0000000000aa"
	s := PGSubjectMatters{DB: noDB{}, Matters: oneMatter{matter.Matter{ID: known}}}
	cases := []struct {
		name, kind, id string
		want           error
	}{
		{"matter read through the matter module", agent.WorkSubjectMatter, known, nil},
		{"unknown matter", agent.WorkSubjectMatter, "0192f000-0000-7000-8000-0000000000bb", agenttools.ErrNotFound},
		{"unknown kind", "billing.payment", "0192f000-0000-7000-8000-000000000001", ErrSubjectKind},
		{"empty kind", "", "0192f000-0000-7000-8000-000000000001", agenttools.ErrNotFound},
		{"bad id", agent.SubjectAgentNote, "note-1", agenttools.ErrNotFound},
		{"known kind reaches the db", agent.SubjectAgentNote, "0192f000-0000-7000-8000-000000000001", errNoDB},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.SubjectMatter(context.Background(), tc.kind, tc.id)
			if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if tc.want == nil && got != tc.id {
				t.Fatalf("matter = %q, want %q", got, tc.id)
			}
		})
	}
}

func TestDraftsRefuseBeforeStoring(t *testing.T) {
	const m, u = "0192f000-0000-7000-8000-000000000001", "0192f000-0000-7000-8000-000000000002"
	ctx := context.Background()
	cases := []struct {
		name      string
		matter    string
		preparer  string
		run       string
		want      error
		wantStore bool
	}{
		{"agent as preparer", m, "agent", "", agent.ErrInvalidInput, false},
		{"bad matter", "m-1", u, "", agenttools.ErrNotFound, false},
		{"bad run id", m, u, "run-1", agent.ErrInvalidInput, false},
		{"valid draft reaches the store", m, u, "", errNoDB, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := &noteBodies{}
			calls := []func() error{
				func() error {
					_, err := PGNoteDrafts{DB: noDB{}, Bodies: b}.CreateNoteDraft(ctx, agent.NewDraftNote{MatterID: tc.matter, PreparedBy: tc.preparer, RunID: tc.run})
					return err
				},
				func() error {
					_, err := PGDocumentDrafts{DB: noDB{}, Bodies: b}.CreateDraft(ctx, agent.NewDraftDocument{MatterID: tc.matter, PreparedBy: tc.preparer, RunID: tc.run})
					return err
				},
				func() error {
					_, err := PGEmailDrafts{DB: noDB{}, Bodies: b}.CreateEmailDraft(ctx, agent.NewDraftEmail{MatterID: tc.matter, PreparedBy: tc.preparer, RunID: tc.run})
					return err
				},
			}
			for _, call := range calls {
				if err := call(); !errors.Is(err, tc.want) {
					t.Fatalf("err = %v, want %v", err, tc.want)
				}
			}
			if (b.puts > 0) != tc.wantStore {
				t.Fatalf("bodies stored = %d, want stored %v", b.puts, tc.wantStore)
			}
		})
	}
}

func TestPartyKind(t *testing.T) {
	cases := []struct {
		in, want string
		err      bool
	}{
		{"", "person", false},
		{"person", "person", false},
		{"org", "organization", false},
		{"organization", "organization", false},
		{"trust", "", true},
	}
	for _, tc := range cases {
		got, err := partyKind(tc.in)
		if got != tc.want || (err != nil) != tc.err {
			t.Errorf("partyKind(%q) = %q, %v", tc.in, got, err)
		}
	}
}

func TestPackOf(t *testing.T) {
	day := time.Date(2026, 12, 25, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		rules string
		want  []deadline.Rule
		err   bool
	}{
		{"rules decoded, empty roll is none", `[{"id":"r1","trigger":"served","mode":"calendar","days":20}]`,
			[]deadline.Rule{{ID: "r1", Trigger: "served", Mode: deadline.Calendar, Days: 20, Roll: deadline.RollNone}}, false},
		{"months and roll", `[{"id":"r2","mode":"clear","months_after":2,"roll":"forward"}]`,
			[]deadline.Rule{{ID: "r2", Mode: deadline.Clear, MonthsAfter: 2, Roll: deadline.RollForward}}, false},
		{"not a rule list", `{"id":"r1"}`, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := packOf("ON-RCP", "2026.1", []byte(tc.rules), []time.Time{day})
			if (err != nil) != tc.err {
				t.Fatalf("err = %v", err)
			}
			if tc.err {
				if !errors.Is(err, deadline.ErrInvalidPack) {
					t.Fatalf("err = %v, want ErrInvalidPack", err)
				}
				return
			}
			if p.ID != "ON-RCP" || p.Version != "2026.1" || !slices.Equal(p.Rules, tc.want) || len(p.Holidays) != 1 || p.Holidays[0].String() != "2026-12-25" {
				t.Fatalf("pack = %+v", p)
			}
		})
	}
}
