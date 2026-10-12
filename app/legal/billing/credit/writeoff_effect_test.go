package credit

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/money/moneyfake"
	"github.com/dagi377/counsel/kernel/registry"
)

const specGatesFile = "../../../../spec/gates.json"

// memWriteOffs is an in-memory WriteOffStore.
type memWriteOffs struct {
	w        map[string]WriteOff
	awaiting map[string]string // write-off id -> second gate id
}

func (m *memWriteOffs) Lock(_ context.Context, _ pgx.Tx, id string) (WriteOff, error) {
	w, ok := m.w[id]
	if !ok {
		return WriteOff{}, ErrNotFound
	}
	return w, nil
}

func (m *memWriteOffs) MarkAwaitingSecond(_ context.Context, _ pgx.Tx, id, gateID string) error {
	m.awaiting[id] = gateID
	return nil
}

func (m *memWriteOffs) AwaitingSecond(_ context.Context, _ pgx.Tx, id string) (string, error) {
	return m.awaiting[id], nil
}

func (m *memWriteOffs) SaveApproved(_ context.Context, _ pgx.Tx, w WriteOff) error {
	m.w[w.ID] = w
	return nil
}

// memTotals is a WriteOffTotals over a fixed prior total. It records every scope asked.
type memTotals struct {
	prior  int64
	err    error
	scopes []WriteOffScope
}

func (m *memTotals) CumulativeCents(_ context.Context, _ pgx.Tx, s WriteOffScope) (int64, error) {
	m.scopes = append(m.scopes, s)
	return m.prior, m.err
}

type memBills map[string]WriteOffBill

func (m memBills) WriteOffBill(_ context.Context, _ pgx.Tx, id string) (WriteOffBill, error) {
	b, ok := m[id]
	if !ok {
		return WriteOffBill{}, ErrNotFound
	}
	return b, nil
}

// memLedger records every bad-debt post.
type memLedger struct{ posts []BadDebtAdjustment }

func (m *memLedger) PostBadDebt(_ context.Context, _ pgx.Tx, _ string, adj BadDebtAdjustment) error {
	m.posts = append(m.posts, adj)
	return nil
}

var (
	toronto = func() *time.Location {
		loc, err := time.LoadLocation("America/Toronto")
		if err != nil {
			panic(err)
		}
		return loc
	}()
	// 2026-11-16 01:30 UTC is still 2026-11-15 in Toronto.
	effectNow     = time.Date(2026, 11, 16, 1, 30, 0, 0, time.UTC)
	effectBizDate = time.Date(2026, 11, 15, 0, 0, 0, 0, toronto)
)

// writeOffRig is a G-B5 write-off effect over in-memory stores and gatefake. Bill "b1" is
// BIL-X-014: open $590.00 of a bill with tax $130.00 and total $1,180.00; "lawyer" is
// responsible, "clerk" prepares, "owner" and "owner2" hold the owner role.
type writeOffRig struct {
	e      *WriteOffEffect
	store  *memWriteOffs
	ledger *memLedger
	gates  *gatefake.Fake
	totals *memTotals
}

func newWriteOffRig(t *testing.T, amount int64, cfg SecondApprovalConfig) writeOffRig {
	t.Helper()
	clk := clockfake.New(effectNow, toronto)
	caps := gatefake.Caps{
		"lawyer": {"billing.credit"},
		"owner":  {"billing.credit", "billing.writeoff_second_approve"},
		"owner2": {"billing.credit", "billing.writeoff_second_approve"},
		"clerk":  {"billing.credit"},
	}
	r := writeOffRig{
		store: &memWriteOffs{
			w: map[string]WriteOff{"w1": {ID: "w1", BillID: "b1", AmountCents: amount, Reason: "uncollectible",
				Status: StatusDraft, PreparedBy: "clerk", ArmsLength: ArmsLengthYes}},
			awaiting: map[string]string{},
		},
		ledger: &memLedger{},
		gates:  gatefake.New(clk.Now, caps),
		totals: &memTotals{},
	}
	r.e = &WriteOffEffect{
		Calc:   Calc{M: moneyfake.New()},
		Config: cfg,
		Store:  r.store,
		Bills: memBills{"b1": {Bill: Bill{ID: "b1", Delivered: true, OpenCents: 200000, ResponsibleUser: "lawyer"},
			TaxCents: 13000, TotalCents: 118000, ClientID: "client1"}},
		Ledger: r.ledger,
		Gates:  r.gates,
		Clock:  clk,
		Totals: r.totals,
	}
	r.registerKinds(t, map[string]registry.EffectFunc{GateKindCreditNote: r.e.Apply})
	return r
}

// registerKinds registers the two write-off kinds from spec/gates.json with gatefake. A kind
// with no effect in effects gets one that fails, so a test notices it ran.
func (r writeOffRig) registerKinds(t *testing.T, effects map[string]registry.EffectFunc) {
	t.Helper()
	data, err := os.ReadFile(specGatesFile)
	if err != nil {
		t.Fatal(err)
	}
	kinds, err := gates.ParseKinds(data)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, k := range kinds {
		if k.Code != GateKindCreditNote && k.Code != GateKindWriteOffSecond {
			continue
		}
		found++
		eff := effects[k.Code]
		if eff == nil {
			eff = func(context.Context, pgx.Tx, registry.Approved) error { return errors.New("unexpected effect") }
		}
		if err := r.gates.Register(k.GateKind(eff)); err != nil {
			t.Fatal(err)
		}
	}
	if found != 2 {
		t.Fatalf("spec/gates.json has %d of the 2 write-off gate kinds", found)
	}
}

func gb5Approved(amount int64, approver gate.Principal) registry.Approved {
	p, _ := json.Marshal(creditNotePayload{BillID: "b1", AmountCents: amount, Kind: payloadKindWriteOff, Reason: "uncollectible"})
	return registry.Approved{
		GateID: "gb5", Kind: GateKindCreditNote, Version: 1, Subject: gate.Subject{Kind: SubjectWriteOff, ID: "w1"},
		Payload: p, Preparer: gate.Principal{Kind: gate.PrincipalUser, UserID: "clerk"}, Approvers: []gate.Principal{approver},
	}
}

func user(id string) gate.Principal { return gate.Principal{Kind: gate.PrincipalUser, UserID: id} }

// rule: BIL-C-085
func TestBIL_C_085_WriteOffEffectOpensSecondGate(t *testing.T) {
	agent := gate.Principal{Kind: gate.PrincipalAgent, RunID: "run1", ActingFor: "lawyer"}
	tests := []struct {
		name       string
		amount     int64
		cfg        SecondApprovalConfig
		approver   gate.Principal
		payload    func(registry.Approved) registry.Approved
		wantErr    error
		wantSecond bool
		wantTax    int64 // posted adjustment when approved now
	}{
		{"BIL-X-014: $590.00 at default threshold approves now", 59000, SecondApprovalConfig{}, user("lawyer"), nil, nil, false, 6500},
		{"exactly the threshold approves now", 59000, SecondApprovalConfig{WriteOffSecondApprovalCents: 59000}, user("lawyer"), nil, nil, false, 6500},
		{"above the threshold opens the second gate", 59000, SecondApprovalConfig{WriteOffSecondApprovalCents: 50000}, user("lawyer"), nil, nil, true, 0},
		{"above the default threshold opens the second gate", 118000, SecondApprovalConfig{}, user("lawyer"), nil, nil, true, 0},
		{"agent approval above threshold opens nothing", 118000, SecondApprovalConfig{}, agent, nil, ErrAgentApproval, false, 0},
		{"preparer approval above threshold opens nothing", 118000, SecondApprovalConfig{}, user("clerk"), nil, ErrSelfApproval, false, 0},
		{"non-responsible approver above threshold opens nothing", 118000, SecondApprovalConfig{}, user("owner"), nil, ErrNotResponsible, false, 0},
		{"credit-note payload is not a write-off", 59000, SecondApprovalConfig{}, user("lawyer"), func(g registry.Approved) registry.Approved {
			g.Payload = json.RawMessage(`{"bill_id":"b1","amount_cents":59000,"kind":"credit_note","reason":"x"}`)
			return g
		}, ErrInvalidState, false, 0},
		{"payload amount differs from the write-off", 59000, SecondApprovalConfig{}, user("lawyer"), func(g registry.Approved) registry.Approved {
			g.Payload = json.RawMessage(`{"bill_id":"b1","amount_cents":1,"kind":"write_off","reason":"x"}`)
			return g
		}, ErrInvalidState, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newWriteOffRig(t, tt.amount, tt.cfg)
			g := gb5Approved(tt.amount, tt.approver)
			if tt.payload != nil {
				g = tt.payload(g)
			}
			err := r.e.Apply(context.Background(), nil, g)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Apply err = %v, want %v", err, tt.wantErr)
			}
			w := r.store.w["w1"]
			gateID, awaiting := r.store.awaiting["w1"]
			if tt.wantErr != nil || tt.wantSecond {
				// rule: BIL-C-085
				if len(r.ledger.posts) != 0 || w.Status != StatusDraft {
					t.Fatalf("posts = %v, status %s; want no post and a draft write-off", r.ledger.posts, w.Status)
				}
			}
			if !tt.wantSecond {
				if awaiting {
					t.Fatalf("second gate %s opened, want none", gateID)
				}
				if _, err := r.gates.Get(context.Background(), "gate-1"); !errors.Is(err, gate.ErrNotFound) {
					t.Fatalf("a gate was opened: %v", err)
				}
			}
			if tt.wantErr != nil {
				return
			}
			if tt.wantSecond {
				sg, err := r.gates.Get(context.Background(), gate.GateID(gateID))
				if err != nil {
					t.Fatalf("second gate %q: %v", gateID, err)
				}
				if _, err := r.gates.Get(context.Background(), "gate-2"); !errors.Is(err, gate.ErrNotFound) {
					t.Fatalf("more than one gate opened: %v", err)
				}
				if sg.Kind != GateKindWriteOffSecond || sg.Status != gate.StatusPending {
					t.Fatalf("second gate = %s %s, want pending %s", sg.Kind, sg.Status, GateKindWriteOffSecond)
				}
				if sg.Preparer.Kind != gate.PrincipalUser || sg.Preparer.UserID != "lawyer" {
					t.Fatalf("second gate preparer = %+v, want the first approver", sg.Preparer)
				}
				var p SecondApprovalPayload
				if err := json.Unmarshal(sg.Payload, &p); err != nil {
					t.Fatal(err)
				}
				want := SecondApprovalPayload{WriteOffID: "w1", BillID: "b1", AmountCents: tt.amount, FirstApprover: "lawyer"}
				if p != want || sg.Subject != (gate.Subject{Kind: SubjectWriteOff, ID: "w1"}) {
					t.Fatalf("second gate payload %+v subject %+v, want %+v on w1", p, sg.Subject, want)
				}
				return
			}
			// rule: BIL-C-205
			if len(r.ledger.posts) != 1 || r.ledger.posts[0].TaxCents != tt.wantTax || !r.ledger.posts[0].On.Equal(effectBizDate) {
				t.Fatalf("posts = %+v, want one adjustment of %d on %v", r.ledger.posts, tt.wantTax, effectBizDate)
			}
			if w.Status != StatusApproved || w.ApprovedBy != "lawyer" {
				t.Fatalf("write-off = %+v, want approved by lawyer", w)
			}
		})
	}
}

func TestWriteOffEffectsSlugsFromSpec(t *testing.T) {
	data, err := os.ReadFile(specGatesFile)
	if err != nil {
		t.Fatal(err)
	}
	kinds, err := gates.ParseKinds(data)
	if err != nil {
		t.Fatal(err)
	}
	spec := map[string]bool{}
	for _, k := range kinds {
		spec[k.Code] = true
	}
	r := newWriteOffRig(t, 59000, SecondApprovalConfig{})
	effects := Effects(r.e, SecondApproval{})
	tests := []struct{ slug string }{{GateKindCreditNote}, {GateKindWriteOffSecond}}
	if len(effects) != len(tests) {
		t.Fatalf("Effects = %d slugs, want %d", len(effects), len(tests))
	}
	for _, tt := range tests {
		t.Run(tt.slug, func(t *testing.T) {
			if !spec[tt.slug] {
				t.Fatalf("%s is not a gate kind in spec/gates.json", tt.slug)
			}
			if effects[tt.slug] == nil {
				t.Fatalf("Effects has no func for %s", tt.slug)
			}
		})
	}
}

func TestWriteOffEffectApproverIsFirstApprover(t *testing.T) {
	agent := gate.Principal{Kind: gate.PrincipalAgent, RunID: "run1", ActingFor: "lawyer"}
	tests := []struct {
		name      string
		approvers []gate.Principal
		wantErr   []error
		wantBy    string
	}{
		{"responsible lawyer approves", []gate.Principal{user("lawyer")}, nil, "lawyer"},
		{"first approver counts, not the last", []gate.Principal{user("lawyer"), agent}, nil, "lawyer"},
		{"agent approver fails the effect", []gate.Principal{agent}, []error{gate.ErrEffectFailed, ErrAgentApproval}, ""},
		{"agent first fails even with a lawyer after", []gate.Principal{agent, user("lawyer")}, []error{gate.ErrEffectFailed, ErrAgentApproval}, ""},
		{"preparer approver fails the effect", []gate.Principal{user("clerk")}, []error{gate.ErrEffectFailed, ErrSelfApproval}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newWriteOffRig(t, 59000, SecondApprovalConfig{})
			g := gb5Approved(59000, user("lawyer"))
			g.Approvers = tt.approvers
			err := Effects(r.e, SecondApproval{})[GateKindCreditNote](context.Background(), nil, g)
			for _, want := range tt.wantErr {
				if !errors.Is(err, want) {
					t.Fatalf("err = %v, want %v", err, want)
				}
			}
			w := r.store.w["w1"]
			if tt.wantErr != nil {
				if len(r.ledger.posts) != 0 || w.Status != StatusDraft {
					t.Fatalf("posts = %v, status %s; want nothing applied", r.ledger.posts, w.Status)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if w.Status != StatusApproved || w.ApprovedBy != tt.wantBy {
				t.Fatalf("write-off = %+v, want approved by %s", w, tt.wantBy)
			}
		})
	}
}

func TestWriteOffEffectReplayChangesNothing(t *testing.T) {
	tests := []struct {
		name       string
		cfg        SecondApprovalConfig
		wantPosts  int
		wantSecond bool
	}{
		{"approved at or below the threshold", SecondApprovalConfig{}, 1, false},
		{"second gate opened above the threshold", SecondApprovalConfig{WriteOffSecondApprovalCents: 50000}, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newWriteOffRig(t, 59000, tt.cfg)
			apply := Effects(r.e, SecondApproval{})[GateKindCreditNote]
			g := gb5Approved(59000, user("lawyer"))
			for i := 0; i < 2; i++ {
				if err := apply(context.Background(), nil, g); err != nil {
					t.Fatalf("run %d: %v", i+1, err)
				}
			}
			if len(r.ledger.posts) != tt.wantPosts {
				t.Fatalf("posts = %d, want %d", len(r.ledger.posts), tt.wantPosts)
			}
			_, err := r.gates.Get(context.Background(), "gate-1")
			if opened := err == nil; opened != tt.wantSecond {
				t.Fatalf("second gate opened = %v, want %v", opened, tt.wantSecond)
			}
			if _, err := r.gates.Get(context.Background(), "gate-2"); !errors.Is(err, gate.ErrNotFound) {
				t.Fatalf("replay opened another gate: %v", err)
			}
		})
	}
}

// rule: BIL-C-085
// Splitting a write-off never avoids the managing lawyer: earlier write-offs on the bill and
// on the client's bills in the last 30 days count toward the threshold.
func TestBIL_C_085_SplitWriteOffsAggregate(t *testing.T) {
	tests := []struct {
		name       string
		amount     int64
		prior      int64
		totalsErr  error
		noTotals   bool
		wantErr    error
		wantSecond bool
	}{
		{"three $1,000 write-offs: the third opens the second gate", 100000, 200000, nil, false, nil, true},
		{"second $1,000 write-off: total $2,000 opens the second gate", 100000, 100000, nil, false, nil, true},
		{"earlier $0.01 tips $1,000 over the threshold", 100000, 1, nil, false, nil, true},
		{"cumulative exactly the threshold approves now", 59000, 41000, nil, false, nil, false},
		{"first write-off alone below the threshold approves now", 59000, 0, nil, false, nil, false},
		{"totals error fails closed", 59000, 0, errors.New("db down"), false, errDBDown, false},
		{"no totals configured fails closed", 59000, 0, nil, true, ErrInvalidState, false},
		{"negative prior total fails closed", 59000, -1, nil, false, ErrInvalidState, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newWriteOffRig(t, tt.amount, SecondApprovalConfig{})
			r.totals.prior = tt.prior
			r.totals.err = tt.totalsErr
			if tt.noTotals {
				r.e.Totals = nil
			}
			err := r.e.Apply(context.Background(), nil, gb5Approved(tt.amount, user("lawyer")))
			switch {
			case tt.wantErr == errDBDown:
				if err == nil {
					t.Fatal("Apply err = nil, want the totals error")
				}
			case !errors.Is(err, tt.wantErr):
				t.Fatalf("Apply err = %v, want %v", err, tt.wantErr)
			}
			_, awaiting := r.store.awaiting["w1"]
			if awaiting != tt.wantSecond {
				t.Fatalf("second gate opened = %v, want %v", awaiting, tt.wantSecond)
			}
			posted := len(r.ledger.posts) == 1
			if wantPost := err == nil && !tt.wantSecond; posted != wantPost {
				t.Fatalf("bad-debt posted = %v, want %v", posted, wantPost)
			}
			if tt.noTotals {
				return
			}
			want := WriteOffScope{BillID: "b1", ClientID: "client1", Since: effectBizDate.Add(-WriteOffClientWindow), ExcludeID: "w1"}
			if len(r.totals.scopes) != 1 || r.totals.scopes[0] != want {
				t.Fatalf("scopes = %+v, want [%+v]", r.totals.scopes, want)
			}
		})
	}
}

// errDBDown marks a case that expects the store's own error, passed through.
var errDBDown = errors.New("want the totals error")
