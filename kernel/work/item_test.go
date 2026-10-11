package work_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/work"
)

const subjectID = "0190A1B2-0000-7000-8000-00000000000A"

// appKinds stands for an app registering its subject kinds at boot.
func appKinds() *work.SubjectKinds {
	r := &registry.Recorder{}
	r.SubjectKind(registry.SubjectKind{ID: "project", Description: "a project"}, nil)
	r.SubjectKind(registry.SubjectKind{ID: "intake.request"}, nil)
	r.Capability(registry.Capability{ID: "work.read"})
	return work.NewSubjectKinds(r.Calls())
}

func validItem() work.Item {
	return work.Item{
		Type:       "task",
		Title:      "Draft the letter",
		WorkflowID: "wf-1",
		StatusID:   "st-todo",
		ReporterID: "u-1",
	}
}

func TestNewSubjectKindsReadsRegisteredKinds(t *testing.T) {
	tests := []struct {
		name  string
		kinds *work.SubjectKinds
		want  []string
	}{
		{"app registrations", appKinds(), []string{"intake.request", "project"}},
		{"no registrations", work.NewSubjectKinds(nil), []string{}},
		{"nil set", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.kinds.IDs(); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("IDs() = %v, want %v", got, tt.want)
			}
			if tt.kinds.Registered("work.read") {
				t.Error("a capability counted as a subject kind")
			}
		})
	}
}

func TestPrepareItemSubject(t *testing.T) {
	tests := []struct {
		name    string
		subject work.Subject
		kinds   *work.SubjectKinds
		want    work.Subject
		wantErr bool
	}{
		{"no subject", work.Subject{}, appKinds(), work.Subject{}, false},
		{"registered kind", work.Subject{Kind: "project", ID: subjectID},
			appKinds(), work.Subject{Kind: "project", ID: "0190a1b2-0000-7000-8000-00000000000a"}, false},
		{"second registered kind", work.Subject{Kind: "intake.request", ID: subjectID},
			appKinds(), work.Subject{Kind: "intake.request", ID: "0190a1b2-0000-7000-8000-00000000000a"}, false},
		{"unregistered kind", work.Subject{Kind: "invoice", ID: subjectID}, appKinds(), work.Subject{}, true},
		{"no kinds registered", work.Subject{Kind: "project", ID: subjectID}, work.NewSubjectKinds(nil), work.Subject{}, true},
		{"nil kinds", work.Subject{Kind: "project", ID: subjectID}, nil, work.Subject{}, true},
		{"kind without id", work.Subject{Kind: "project"}, appKinds(), work.Subject{}, true},
		{"id without kind", work.Subject{ID: subjectID}, appKinds(), work.Subject{}, true},
		{"malformed kind", work.Subject{Kind: "Project", ID: subjectID}, appKinds(), work.Subject{}, true},
		{"id not a uuid", work.Subject{Kind: "project", ID: "P-2026-001"}, appKinds(), work.Subject{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			it := validItem()
			it.Subject = tt.subject
			got, err := tt.kinds.PrepareItem(it)
			if tt.wantErr {
				if !errors.Is(err, work.ErrInvalidItem) {
					t.Fatalf("err = %v, want ErrInvalidItem", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("PrepareItem: %v", err)
			}
			if got.Subject != tt.want {
				t.Errorf("subject = %+v, want %+v", got.Subject, tt.want)
			}
		})
	}
}

func TestPrepareItemFields(t *testing.T) {
	tests := []struct {
		name         string
		edit         func(*work.Item)
		wantPriority int
		wantErr      bool
	}{
		{"default priority", func(*work.Item) {}, work.DefaultPriority, false},
		{"explicit priority", func(it *work.Item) { it.Priority = 1 }, 1, false},
		{"lowest priority", func(it *work.Item) { it.Priority = 5 }, 5, false},
		{"priority too high", func(it *work.Item) { it.Priority = 6 }, 0, true},
		{"negative priority", func(it *work.Item) { it.Priority = -1 }, 0, true},
		{"no title", func(it *work.Item) { it.Title = "  " }, 0, true},
		{"no type", func(it *work.Item) { it.Type = "" }, 0, true},
		{"no workflow", func(it *work.Item) { it.WorkflowID = "" }, 0, true},
		{"no status", func(it *work.Item) { it.StatusID = "" }, 0, true},
		{"no reporter", func(it *work.Item) { it.ReporterID = "" }, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			it := validItem()
			tt.edit(&it)
			got, err := appKinds().PrepareItem(it)
			if tt.wantErr {
				if !errors.Is(err, work.ErrInvalidItem) {
					t.Fatalf("err = %v, want ErrInvalidItem", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("PrepareItem: %v", err)
			}
			if got.Priority != tt.wantPriority {
				t.Errorf("priority = %d, want %d", got.Priority, tt.wantPriority)
			}
		})
	}
}

func TestSubjectColumnsRoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		subject  work.Subject
		wantNull bool
	}{
		{"none", work.Subject{}, true},
		{"project", work.Subject{Kind: "project", ID: subjectID}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, id := work.SubjectColumns(tt.subject)
			if (kind == nil) != tt.wantNull || (id == nil) != tt.wantNull {
				t.Fatalf("columns null = %v/%v, want %v", kind == nil, id == nil, tt.wantNull)
			}
			if got := work.SubjectFromColumns(kind, id); got != tt.subject {
				t.Errorf("round trip = %+v, want %+v", got, tt.subject)
			}
		})
	}
}
