package agent

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/notify/notifyfake"
)

// memFlags is an in-memory FlagStore with the open-flag uniqueness of agent_flag_open_uq.
type memFlags struct {
	rows      []Flag
	dismissed map[string]bool
}

func (m *memFlags) Insert(_ context.Context, _ pgx.Tx, f Flag) (string, bool, error) {
	for _, r := range m.rows {
		if !m.dismissed[r.ID] && r.FeatureID == f.FeatureID && r.SubjectKind == f.SubjectKind && r.SubjectID == f.SubjectID && r.Code == f.Code {
			return r.ID, false, nil
		}
	}
	f.ID = "fl-" + strconv.Itoa(len(m.rows)+1)
	m.rows = append(m.rows, f)
	return f.ID, true, nil
}

func (m *memFlags) Dismiss(_ context.Context, _ pgx.Tx, id, _ string) error {
	for _, r := range m.rows {
		if r.ID == id && !m.dismissed[id] {
			m.dismissed[id] = true
			return nil
		}
	}
	return ErrFlagNotFound
}

func (m *memFlags) OpenBlocking(_ context.Context, _ pgx.Tx, gate, kind, id string) ([]Flag, error) {
	var out []Flag
	for _, r := range m.rows {
		if !m.dismissed[r.ID] && r.BlocksGate == gate && r.SubjectKind == kind && r.SubjectID == id {
			out = append(out, r)
		}
	}
	return out, nil
}

func newSink(t *testing.T) (*FlagSink, *memFlags, *notifyfake.Fake) {
	t.Helper()
	n := notifyfake.New()
	if err := n.RegisterTemplate(FlagDigestTemplate); err != nil {
		t.Fatal(err)
	}
	st := &memFlags{dismissed: map[string]bool{}}
	return &FlagSink{Store: st, Notify: n, BlocksGate: map[string][]string{"F-DUT-01": {"matter.opening"}}}, st, n
}

func flag(feature, code string, sev Severity) Flag {
	return Flag{
		FeatureID: feature, RunID: "run-1", SubjectKind: "matter", SubjectID: "m-1", Code: code, Severity: sev,
		Citations: []agenttools.Citation{{VersionID: "v-1", Page: 1, FromLine: 2, ToLine: 3}}, ActingFor: "u-1",
	}
}

func TestFlagRecord(t *testing.T) {
	blocking := flag("F-DUT-01", "duty.conflict", SeverityCritical)
	blocking.BlocksGate = "matter.opening"
	undeclared := flag("F-DL-02", "limitation.near", SeverityWarning)
	undeclared.BlocksGate = "matter.opening"
	sentence := flag("F-DL-02", "the client told us the limitation is near", SeverityWarning)
	badCite := flag("F-DL-02", "limitation.near", SeverityWarning)
	badCite.Citations = []agenttools.Citation{{VersionID: "v-1"}}
	cases := []struct {
		name       string
		flags      [][]Flag // one Record call per entry
		wantNew    int
		wantErr    error
		wantDigest []map[string]string
	}{
		{"new flags and one digest per feature", [][]Flag{{
			flag("F-DL-02", "limitation.near", SeverityWarning),
			flag("F-DL-02", "service.missing", SeverityCritical),
		}}, 2, nil, []map[string]string{{"feature_id": "F-DL-02", "count": "2", "severity": "critical"}}},
		{"rerun keeps the open flag", [][]Flag{
			{flag("F-DL-02", "limitation.near", SeverityWarning)},
			{flag("F-DL-02", "limitation.near", SeverityWarning)},
		}, 0, nil, []map[string]string{{"feature_id": "F-DL-02", "count": "1", "severity": "warning"}}},
		{"declared blocking flag", [][]Flag{{blocking}}, 1, nil,
			[]map[string]string{{"feature_id": "F-DUT-01", "count": "1", "severity": "critical"}}},
		// rule: AI-R-003
		{"undeclared blocking refused", [][]Flag{{undeclared}}, 0, ErrBadFlag, nil},
		// rule: AI-R-005
		{"free text in code refused", [][]Flag{{sentence}}, 0, ErrBadFlag, nil},
		{"bad citation refused", [][]Flag{{badCite}}, 0, ErrBadFlag, nil},
		{"unknown severity refused", [][]Flag{{flag("F-DL-02", "limitation.near", "urgent")}}, 0, ErrBadFlag, nil},
		{"missing acting user refused", [][]Flag{{func() Flag { f := flag("F-DL-02", "x", SeverityInfo); f.ActingFor = ""; return f }()}}, 0, ErrBadFlag, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, st, n := newSink(t)
			var got int
			var err error
			for _, batch := range c.flags {
				got, err = s.Record(context.Background(), nil, batch)
				if err != nil {
					break
				}
			}
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("err = %v, want %v", err, c.wantErr)
			}
			if err != nil {
				if len(st.rows) != 0 || len(n.Sent()) != 0 {
					t.Fatalf("refused batch stored %d flags and sent %d digests", len(st.rows), len(n.Sent()))
				}
				return
			}
			if got != c.wantNew {
				t.Fatalf("new = %d, want %d", got, c.wantNew)
			}
			var digests []map[string]string
			for _, m := range n.Sent() {
				if m.To != "u-1" || m.Template != FlagDigestTemplate.ID {
					t.Fatalf("digest = %+v", m)
				}
				digests = append(digests, m.Data)
			}
			if !reflect.DeepEqual(digests, c.wantDigest) {
				t.Fatalf("digests = %v, want %v", digests, c.wantDigest)
			}
		})
	}
}

func TestFlagBlocksOnlyDeclaredGate(t *testing.T) {
	s, st, _ := newSink(t)
	ctx := context.Background()
	duty := flag("F-DUT-01", "duty.conflict", SeverityCritical)
	duty.BlocksGate = "matter.opening"
	if _, err := s.Record(ctx, nil, []Flag{duty, flag("F-DL-02", "limitation.near", SeverityCritical)}); err != nil {
		t.Fatal(err)
	}
	// A row written by another path with an undeclared gate still does not block.
	st.rows = append(st.rows, Flag{ID: "fl-x", FeatureID: "F-DL-02", SubjectKind: "matter", SubjectID: "m-1", Code: "x", BlocksGate: "matter.opening"})
	cases := []struct {
		name    string
		gate    string
		dismiss string
		want    []string
	}{
		{"declared feature blocks its gate", "matter.opening", "", []string{"F-DUT-01"}},
		{"other gate not blocked", "billing.approve", "", nil},
		{"dismissed flag no longer blocks", "matter.opening", "fl-1", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.dismiss != "" {
				if err := s.Dismiss(ctx, nil, c.dismiss, "u-2"); err != nil {
					t.Fatal(err)
				}
			}
			got, err := s.Blocking(ctx, nil, c.gate, "matter", "m-1")
			if err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, f := range got {
				ids = append(ids, f.FeatureID)
			}
			if !reflect.DeepEqual(ids, c.want) {
				t.Fatalf("blocking = %v, want %v", ids, c.want)
			}
		})
	}
	if err := s.Dismiss(ctx, nil, "fl-1", "u-2"); !errors.Is(err, ErrFlagNotFound) {
		t.Fatalf("second dismiss err = %v", err)
	}
}

// rule: AI-R-005
// The flag table has no free-text column: every text column is a checked machine code or id.
func TestAgentFlagMigrationHasNoFreeText(t *testing.T) {
	sql, err := os.ReadFile("../../../migrations/6200_agent_flag.sql")
	if err != nil {
		t.Fatal(err)
	}
	body := string(sql)
	start := strings.Index(body, "CREATE TABLE agent.agent_flag (")
	end := strings.Index(body[start:], "\n);")
	if start < 0 || end < 0 {
		t.Fatal("agent_flag table not found")
	}
	for _, line := range strings.Split(body[start:start+end], "\n")[1:] {
		f := strings.Fields(line)
		if len(f) < 2 || f[1] != "text" {
			continue
		}
		if !strings.Contains(line, "CHECK") {
			t.Errorf("text column %s has no CHECK", f[0])
		}
		for _, banned := range []string{"summary", "message", "note", "body", "text_ref", "detail"} {
			if strings.Contains(f[0], banned) {
				t.Errorf("column %s looks like free text", f[0])
			}
		}
	}
}
