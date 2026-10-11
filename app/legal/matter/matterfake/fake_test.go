package matterfake

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/party"
	"github.com/dagi377/counsel/app/legal/party/partyfake"
	"github.com/dagi377/counsel/kernel/gate"
)

var t0 = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

func setup(t *testing.T) (*Fake, party.ContactID) {
	t.Helper()
	pf := partyfake.New(func() time.Time { return t0 })
	c, err := pf.Create(context.Background(), party.NewContact{Kind: party.Person, LegalName: "A", DisplayName: "A", IsClient: true})
	if err != nil {
		t.Fatal(err)
	}
	return New(func() time.Time { return t0 }, pf), c.ID
}

func newMatter(client party.ContactID, number string) matter.NewMatter {
	return matter.NewMatter{Number: number, ShortName: "S", ClientID: client, PracticeAreaID: "pa1", ResponsibleLawyerID: "u1", CreatedBy: "u2"}
}

func TestCreate(t *testing.T) {
	tests := []struct {
		name    string
		edit    func(m *matter.NewMatter)
		wantErr error
	}{
		{"ok", func(*matter.NewMatter) {}, nil},
		{"duplicate number", func(m *matter.NewMatter) { m.Number = "2026-001" }, matter.ErrDuplicate},
		{"unknown client", func(m *matter.NewMatter) { m.ClientID = "ghost" }, matter.ErrUnknownClient},
		{"missing lawyer", func(m *matter.NewMatter) { m.ResponsibleLawyerID = "" }, matter.ErrInvalid},
		{"bad visibility", func(m *matter.NewMatter) { m.Visibility = "public" }, matter.ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			f, client := setup(t)
			if _, err := f.Create(ctx, newMatter(client, "2026-001")); err != nil {
				t.Fatal(err)
			}
			in := newMatter(client, "2026-002")
			tt.edit(&in)
			m, err := f.Create(ctx, in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			got, err := f.Get(ctx, m.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != matter.Pending || got.Visibility != matter.FirmOpen || got.RetentionClass != matter.DefaultRetentionClass || got.RowVersion != 1 {
				t.Fatalf("Get = %+v", got)
			}
		})
	}
}

func TestAddMemberAndList(t *testing.T) {
	ctx := context.Background()
	f, client := setup(t)
	m1, _ := f.Create(ctx, newMatter(client, "2026-002"))
	m2, _ := f.Create(ctx, newMatter(client, "2026-001"))
	tests := []struct {
		name    string
		in      matter.NewMember
		wantErr error
	}{
		{"ok", matter.NewMember{MatterID: m1.ID, UserID: "u3", GrantedBy: "u1"}, nil},
		{"self grant", matter.NewMember{MatterID: m1.ID, UserID: "u1", GrantedBy: "u1"}, matter.ErrSelfGrant},
		{"unknown matter", matter.NewMember{MatterID: "ghost", UserID: "u3", GrantedBy: "u1"}, matter.ErrNotFound},
		{"bad role", matter.NewMember{MatterID: m1.ID, UserID: "u4", GrantedBy: "u1", Role: "boss"}, matter.ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := f.AddMember(ctx, tt.in); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
	lists := []struct {
		name string
		flt  matter.Filter
		want []matter.ID
	}{
		{"all by number", matter.Filter{}, []matter.ID{m2.ID, m1.ID}},
		{"by member", matter.Filter{MemberID: "u3"}, []matter.ID{m1.ID}},
		{"by status", matter.Filter{Status: matter.Open}, nil},
		{"by client", matter.Filter{ClientID: client}, []matter.ID{m2.ID, m1.ID}},
	}
	for _, tt := range lists {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.List(ctx, tt.flt)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("List = %d matters, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i].ID != tt.want[i] {
					t.Fatalf("List[%d] = %s, want %s", i, got[i].ID, tt.want[i])
				}
			}
		})
	}
	mb, err := f.Members(ctx, m1.ID)
	if err != nil || len(mb) != 1 || mb[0].Role != matter.RoleMember {
		t.Fatalf("Members = %+v, %v", mb, err)
	}
}

// recordTB records Errorf instead of failing the real test.
type recordTB struct {
	testing.TB
	failed bool
}

func (r *recordTB) Helper()               {}
func (r *recordTB) Errorf(string, ...any) { r.failed = true }

func TestARC_I_012_EffectOnlyMethodsNeedEffectContext(t *testing.T) {
	tests := []struct {
		name     string
		ctx      context.Context
		wantFail bool
	}{
		{"route context", context.Background(), true},
		{"effect context", gate.WithEffect(context.Background(), "g-1"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, client := setup(t)
			rec := &recordTB{TB: t}
			f.GuardEffects(rec)
			_, err := f.Create(tc.ctx, matter.NewMatter{Number: "M-1", ShortName: "x", ClientID: client,
				PracticeAreaID: "pa", ResponsibleLawyerID: "u1"})
			if rec.failed != tc.wantFail || (err != nil) != tc.wantFail {
				t.Errorf("Create: failed=%v err=%v, want fail=%v", rec.failed, err, tc.wantFail)
			}
			if tc.wantFail && !errors.Is(err, gate.ErrOutsideEffect) {
				t.Errorf("Create err = %v, want ErrOutsideEffect", err)
			}
			if f.EffectCalls() != 1 {
				t.Errorf("EffectCalls = %d, want 1", f.EffectCalls())
			}
		})
	}
}
