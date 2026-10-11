package work

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/dagi377/counsel/kernel/registry"
)

// DefaultPriority is the priority of an item created with Priority 0.
const DefaultPriority = 3

// MaxDescriptionLen is the longest item description, in characters (work_item.description).
const MaxDescriptionLen = 10000

// subjectKindPattern mirrors work_item_subject_kind_ck (migrations/1000_work_item_subject.sql).
var subjectKindPattern = regexp.MustCompile(`^[a-z][a-z0-9_.]*$`)

// uuidPattern matches the text form of work.work_item.subject_id (uuid).
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// SubjectKinds is the set of subject kinds the app registered at boot. Work items attach
// only to these. The kernel never interprets a kind; it only checks that one was registered.
type SubjectKinds struct {
	ids map[string]bool
}

// NewSubjectKinds returns the subject kinds among the app's recorded Registry calls
// (registry.Recorder.Calls).
func NewSubjectKinds(calls []registry.Call) *SubjectKinds {
	ks := &SubjectKinds{ids: map[string]bool{}}
	for _, c := range calls {
		if c.Method != registry.MethodSubjectKind || len(c.Args) == 0 {
			continue
		}
		if k, ok := c.Args[0].(registry.SubjectKind); ok && k.ID != "" {
			ks.ids[k.ID] = true
		}
	}
	return ks
}

// Registered reports whether kind was registered.
func (ks *SubjectKinds) Registered(kind string) bool {
	return ks != nil && ks.ids[kind]
}

// IDs returns the registered kinds, sorted.
func (ks *SubjectKinds) IDs() []string {
	if ks == nil {
		return nil
	}
	out := make([]string, 0, len(ks.ids))
	for id := range ks.ids {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// CheckSubject validates the subject a work item attaches to. The zero Subject means the
// item has none. Otherwise both halves are set, the kind is registered and matches the
// column check, and the id is a uuid. Failures wrap ErrInvalidItem.
func (ks *SubjectKinds) CheckSubject(s Subject) error {
	switch {
	case s.Kind == "" && s.ID == "":
		return nil
	case s.Kind == "" || s.ID == "":
		return fmt.Errorf("work item subject %q/%q: kind and id go together: %w", s.Kind, s.ID, ErrInvalidItem)
	case !subjectKindPattern.MatchString(s.Kind):
		return fmt.Errorf("work item subject kind %q: malformed: %w", s.Kind, ErrInvalidItem)
	case !ks.Registered(s.Kind):
		return fmt.Errorf("work item subject kind %q: not registered: %w", s.Kind, ErrInvalidItem)
	case !uuidPattern.MatchString(s.ID):
		return fmt.Errorf("work item subject %s id %q: not a uuid: %w", s.Kind, s.ID, ErrInvalidItem)
	}
	return nil
}

// PrepareItem validates it for Create and returns it with defaults applied: Priority 0
// becomes DefaultPriority and the subject id is lower-cased. A missing title, type,
// workflow, status or reporter, a priority outside 1..5, a description longer than
// MaxDescriptionLen, or a bad subject is ErrInvalidItem. A blank description becomes nil.
func (ks *SubjectKinds) PrepareItem(it Item) (Item, error) {
	if it.Priority == 0 {
		it.Priority = DefaultPriority
	}
	switch {
	case strings.TrimSpace(it.Title) == "":
		return Item{}, fmt.Errorf("work item: no title: %w", ErrInvalidItem)
	case it.Type == "":
		return Item{}, fmt.Errorf("work item: no type: %w", ErrInvalidItem)
	case it.WorkflowID == "":
		return Item{}, fmt.Errorf("work item: no workflow: %w", ErrInvalidItem)
	case it.StatusID == "":
		return Item{}, fmt.Errorf("work item: no status: %w", ErrInvalidItem)
	case it.ReporterID == "":
		return Item{}, fmt.Errorf("work item: no reporter: %w", ErrInvalidItem)
	case it.Priority < 1 || it.Priority > 5:
		return Item{}, fmt.Errorf("work item: priority %d outside 1..5: %w", it.Priority, ErrInvalidItem)
	case it.Description != nil && utf8.RuneCountInString(*it.Description) > MaxDescriptionLen:
		return Item{}, fmt.Errorf("work item: description longer than %d: %w", MaxDescriptionLen, ErrInvalidItem)
	}
	// A blank description is stored as none (NULL), never as an empty string.
	if it.Description != nil && strings.TrimSpace(*it.Description) == "" {
		it.Description = nil
	}
	if err := ks.CheckSubject(it.Subject); err != nil {
		return Item{}, err
	}
	it.Subject.ID = strings.ToLower(it.Subject.ID)
	return it, nil
}

// SubjectColumns returns the (subject_kind, subject_id) column values for s: both NULL for
// the zero Subject.
func SubjectColumns(s Subject) (kind, id *string) {
	if s.Kind == "" && s.ID == "" {
		return nil, nil
	}
	k, i := s.Kind, s.ID
	return &k, &i
}

// SubjectFromColumns builds a Subject from nullable (subject_kind, subject_id) columns.
func SubjectFromColumns(kind, id *string) Subject {
	if kind == nil || id == nil {
		return Subject{}
	}
	return Subject{Kind: *kind, ID: *id}
}
