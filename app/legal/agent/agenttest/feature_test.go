package agenttest_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/app/legal/matter"
	tk "github.com/dagi377/counsel/app/legal/timekeeping"
	"github.com/dagi377/counsel/kernel/ai/aifake"
	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/work"
)

// tstDef is a synthetic feature: report_plan and search_documents have real handlers,
// get_bill has none and runs as a stand-in.
func tstDef() feature.Def {
	return feature.Def{
		ID: "F-TST-01", Area: "test", Name: "Synthetic search", Source: feature.SourceScreenAction,
		Tools:      []string{agent.ToolReportPlan, agent.ToolSearchDocuments, "get_bill"},
		Grant:      []string{"agent.ask", "documents.view", "billing.view"},
		Prompt:     fstest.MapFS{"prompt.md": {Data: []byte("Search the matter's documents and cite every line you use.\n")}},
		PromptPath: "prompt.md",
	}
}

// tstFixture scripts one run: plan, search matterID for "limitation", read a bill, end.
func tstFixture(matterID string) fstest.MapFS {
	y := `synthetic: true
feature: F-TST-01
match:
  last_user_regex: 'search'
turns:
  - reply:
      tool_calls:
        - name: report_plan
          input: {steps: [{step_id: search, title: "Search the documents"}]}
      stop_reason: tool_use
  - reply:
      tool_calls:
        - name: search_documents
          input: {matter_id: "` + matterID + `", query: "limitation"}
      stop_reason: tool_use
  - reply:
      tool_calls:
        - name: get_bill
          input: {bill_id: "00000000-0000-4000-8000-0000000000c3"}
      stop_reason: tool_use
  - reply:
      text: "Found the limitation line."
      stop_reason: end_turn
`
	return fstest.MapFS{"f-tst-01/search/ok.yaml": {Data: []byte(y)}}
}

// rule: ARC-I-035
// rule: ARC-I-038
// rule: ARC-I-039
func TestARC_I_035_FromFeature(t *testing.T) {
	cases := []struct {
		name         string
		handlers     func(t *testing.T) agent.HandlerSet
		walled       bool
		wantReal     []string
		wantStandIns []string
		wantRefused  error
	}{
		{"real handlers from the app set", func(*testing.T) agent.HandlerSet { return agent.Handlers() }, false,
			[]string{agent.ToolReportPlan, agent.ToolSearchDocuments}, []string{"get_bill"}, nil},
		{"real handler applies walls", func(*testing.T) agent.HandlerSet { return agent.Handlers() }, true,
			[]string{agent.ToolReportPlan, agent.ToolSearchDocuments}, []string{"get_bill"}, agent.ErrNotVisible},
		{"empty set keeps the stand-in path", func(t *testing.T) agent.HandlerSet {
			hs, err := agent.NewHandlerSet()
			if err != nil {
				t.Fatal(err)
			}
			return hs
		}, false, nil, []string{"get_bill", agent.ToolReportPlan, agent.ToolSearchDocuments}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fd := agenttest.FromFeatureWith(t, tstDef(), tc.handlers(t))
			m := fd.Seed.Matter("2026-0001", "Synth v Example")
			fd.Seed.Document(string(m.ID), []string{"Statement of claim", "The limitation period ends 2026-11-02."})
			if tc.walled {
				fd.Seed.Wall(string(m.ID))
			}
			real := slices.Clone(fd.Real)
			slices.Sort(tc.wantReal)
			if !slices.Equal(real, tc.wantReal) {
				t.Fatalf("real = %v, want %v", real, tc.wantReal)
			}
			if !slices.Equal(fd.StandIns, tc.wantStandIns) {
				t.Fatalf("stand-ins = %v, want %v", fd.StandIns, tc.wantStandIns)
			}
			model := aifake.New(tstFixture(string(m.ID)))
			run := fd.Fire(context.Background(), model, "run-tst-1", "search the claim")
			if run.Err != nil {
				t.Fatalf("run: %v", run.Err)
			}
			var wantRun []string
			for _, name := range []string{agent.ToolReportPlan, agent.ToolSearchDocuments, "get_bill"} {
				if slices.Contains(tc.wantStandIns, name) {
					wantRun = append(wantRun, name)
				}
			}
			if !slices.Equal(run.StandIns, wantRun) {
				t.Fatalf("run stand-ins = %v, want %v", run.StandIns, wantRun)
			}
			// The Def's prompt is sent with its feature preamble.
			if req := run.Requests[0]; req.FeatureID != "F-TST-01" || !strings.HasPrefix(req.System, feature.Preamble("F-TST-01")) || len(req.Tools) != 3 {
				t.Fatalf("request = %q %q %d tools", req.FeatureID, req.System, len(req.Tools))
			}
			if tc.wantRefused != nil {
				if len(run.Refused) != 1 || !errors.Is(run.Refused[0].Err, tc.wantRefused) {
					t.Fatalf("refused = %v, want %v", run.Refused, tc.wantRefused)
				}
				return
			}
			if len(run.Refused) != 0 {
				t.Fatalf("refused = %v", run.Refused)
			}
			if run.PlanAt != 0 {
				t.Fatalf("plan at %d, want 0", run.PlanAt)
			}
			// The real search result goes back as quoted untrusted data (ARC-I-038).
			if slices.Contains(fd.Real, agent.ToolSearchDocuments) && !strings.Contains(run.ToolResults[1].Text, "limitation period") {
				t.Fatalf("search result = %s", run.ToolResults[1].Text)
			}
		})
	}
}

func TestFromFeatureRejectsBadDef(t *testing.T) {
	ft := &fatalT{TB: t}
	def := tstDef()
	def.Prompt = nil
	func() {
		defer func() { _ = recover() }()
		agenttest.FromFeature(ft, def)
	}()
	if !ft.failed {
		t.Fatal("FromFeature accepted a def without a prompt")
	}
}

// fatalT records Fatalf and stops the builder with a panic.
type fatalT struct {
	testing.TB
	failed bool
}

func (f *fatalT) Helper() {}
func (f *fatalT) Fatalf(string, ...any) {
	f.failed = true
	panic("fatal")
}

func TestSeedCreatesSyntheticRecords(t *testing.T) {
	fd := agenttest.FromFeature(t, tstDef())
	s := fd.Seed
	ctx := context.Background()
	m := s.Matter("2026-0002", "Synth Holdings")
	mid := string(m.ID)
	_, v := s.Document(mid, []string{"line one", "line two"}, []string{"page two"})
	dl := s.Deadline(mid, "defence", deadline.Date{Year: 2026, Month: 11, Day: 2})
	wi := s.WorkItem(mid, "Review the claim")
	te := s.UnbilledTime(mid, 90, "Drafted the defence")
	s.Bill(bill.ID("bill-synth-1"), mid, 150_000)
	s.TrustBalance(mid, 250_000)

	checks := []struct {
		name string
		ok   func() (bool, error)
	}{
		{"matter", func() (bool, error) {
			got, err := s.Matters.Get(ctx, matter.ID(mid))
			return got.ShortName == "Synth Holdings", err
		}},
		{"matter contact", func() (bool, error) {
			ids, err := s.Store.MatterContactIDs(ctx, mid)
			return len(ids) == 1 && ids[0] == string(m.ClientID), err
		}},
		{"document lines", func() (bool, error) {
			lines, err := s.Text.Lines(ctx, v, 1)
			return len(lines) == 2, err
		}},
		{"document listed", func() (bool, error) {
			docs, err := s.Store.MatterDocuments(ctx, mid)
			return len(docs) == 1 && docs[0].VersionID == v, err
		}},
		{"deadline", func() (bool, error) {
			d, err := s.Deadlines.Get(ctx, dl)
			return d.Proposed.Subject.ID == mid, err
		}},
		{"work item", func() (bool, error) {
			it, err := s.Work.Get(ctx, wi)
			return it.Subject == work.Subject{Kind: agent.WorkSubjectMatter, ID: mid}, err
		}},
		{"unbilled time", func() (bool, error) {
			e, err := s.Time.Get(ctx, te)
			return e.Minutes == 90 && e.BillID == "" && e.State != tk.StateBilled, err
		}},
		{"bill", func() (bool, error) {
			b, err := s.Bills.Get(ctx, bill.ID("bill-synth-1"))
			return b.TotalCents == 150_000 && b.MatterID == mid, err
		}},
		{"trust balance", func() (bool, error) {
			n, err := s.Withdrawals.Available(ctx, agenttest.TrustAccount, mid)
			return n == 250_000, err
		}},
	}
	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			ok, err := c.ok()
			if err != nil || !ok {
				t.Fatalf("seeded %s: ok=%v err=%v", c.name, ok, err)
			}
		})
	}
}
