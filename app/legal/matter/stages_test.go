package matter_test

import (
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/matter"
)

var stageAt = time.Date(2026, 3, 2, 15, 0, 0, 0, time.UTC)

func stageList() matter.StageList {
	return matter.NewStageList("pa-lit", []matter.Stage{
		{ID: "s-plead", PracticeAreaID: "pa-lit", Name: "Pleadings", Ordinal: 1},
		{ID: "s-intake", PracticeAreaID: "pa-lit", Name: "Intake", Ordinal: 0},
		{ID: "s-old", PracticeAreaID: "pa-lit", Name: "Old", Ordinal: 2, Archived: true},
		{ID: "s-disc", PracticeAreaID: "pa-lit", Name: "Discovery", Ordinal: 3},
		{ID: "s-trial", PracticeAreaID: "pa-lit", Name: "Trial", Ordinal: 4},
		{ID: "s-other", PracticeAreaID: "pa-re", Name: "Closing", Ordinal: 0},
	})
}

func TestStageListOrdersActiveStages(t *testing.T) {
	got := stageList()
	want := []string{"s-intake", "s-plead", "s-disc", "s-trial"}
	if len(got) != len(want) {
		t.Fatalf("stages = %+v", got)
	}
	for i, s := range got {
		if s.ID != want[i] {
			t.Fatalf("stage %d = %s, want %s", i, s.ID, want[i])
		}
	}
}

func TestStageMovesFollowStageList(t *testing.T) {
	tests := []struct {
		name     string
		from, to string
		wantErr  error
	}{
		{"new matter enters first stage", "", "s-intake", nil},
		{"new matter skips first stage", "", "s-plead", matter.ErrStageSkip},
		{"forward one stage", "s-intake", "s-plead", nil},
		{"forward over archived stage", "s-plead", "s-disc", nil},
		{"forward skips a stage", "s-intake", "s-disc", matter.ErrStageSkip},
		{"back to earlier stage", "s-trial", "s-plead", nil},
		{"same stage", "s-disc", "s-disc", matter.ErrSameStage},
		{"archived target", "s-plead", "s-old", matter.ErrUnknownStage},
		{"other practice area", "s-intake", "s-other", matter.ErrUnknownStage},
		{"unknown from", "s-gone", "s-plead", matter.ErrUnknownStage},
	}
	l := stageList()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := l.Move("m1", tc.from, tc.to, "u-law", stageAt)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			want := matter.StageChange{MatterID: "m1", FromStageID: tc.from, ToStageID: tc.to, ApprovedBy: "u-law", At: stageAt}
			if got != want {
				t.Fatalf("change = %+v, want %+v", got, want)
			}
		})
	}
}

func TestRemovingLastResponsibleLawyerConflicts(t *testing.T) {
	one := []matter.Member{
		{ID: "mm1", MatterID: "m1", UserID: "u-law", Role: matter.RoleResponsible},
		{ID: "mm2", MatterID: "m1", UserID: "u-clerk", Role: matter.RoleStaff},
	}
	two := append([]matter.Member{{ID: "mm3", MatterID: "m1", UserID: "u-law2", Role: matter.RoleResponsible}}, one...)
	tests := []struct {
		name    string
		members []matter.Member
		remove  matter.MemberID
		wantErr error
	}{
		{"last responsible lawyer", one, "mm1", matter.ErrLastResponsible},
		{"one of two responsible lawyers", two, "mm1", nil},
		{"staff member", one, "mm2", nil},
		{"not on team", one, "mm9", matter.ErrNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := matter.CanRemoveMember(tc.members, tc.remove); !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestChangingLastResponsibleRoleConflicts(t *testing.T) {
	members := []matter.Member{
		{ID: "mm1", MatterID: "m1", UserID: "u-law", Role: matter.RoleResponsible},
		{ID: "mm2", MatterID: "m1", UserID: "u-law2", Role: matter.RoleMember},
	}
	tests := []struct {
		name    string
		id      matter.MemberID
		to      matter.MemberRole
		wantErr error
	}{
		{"demote last responsible", "mm1", matter.RoleMember, matter.ErrLastResponsible},
		{"promote member", "mm2", matter.RoleResponsible, nil},
		{"promote unknown", "mm9", matter.RoleResponsible, matter.ErrNotFound},
		{"change member role", "mm2", matter.RoleReviewer, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := matter.CanChangeRole(members, tc.id, tc.to); !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
