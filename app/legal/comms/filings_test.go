package comms

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/db/dbfake"
)

func toronto(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatalf("load zone: %v", err)
	}
	return loc
}

func TestNormalizeFiling(t *testing.T) {
	loc := toronto(t)
	// 2026-03-01 02:30 UTC is still 2026-02-28 in Toronto: the business date wins.
	lateEvening := time.Date(2026, 3, 1, 2, 30, 0, 0, time.UTC)
	tests := []struct {
		name       string
		in         Filing
		wantErr    error
		wantStatus FilingStatus
		wantDate   string
	}{
		{name: "empty status is a draft", in: Filing{MatterID: "m1", Court: " Superior Court "}, wantStatus: FilingDraft},
		{name: "filed with date and user", in: Filing{MatterID: "m1", Court: "Superior Court", Status: FilingFiled, FiledBy: "u1", FiledOn: lateEvening}, wantStatus: FilingFiled, wantDate: "2026-02-28"},
		{name: "rejected", in: Filing{MatterID: "m1", Court: "Small Claims", Status: FilingRejected}, wantStatus: FilingRejected},
		{name: "no matter", in: Filing{Court: "Superior Court"}, wantErr: ErrInvalid},
		{name: "blank court", in: Filing{MatterID: "m1", Court: "  "}, wantErr: ErrInvalid},
		{name: "unknown status", in: Filing{MatterID: "m1", Court: "Superior Court", Status: "proposed"}, wantErr: ErrInvalid},
		{name: "filed without date", in: Filing{MatterID: "m1", Court: "Superior Court", Status: FilingFiled, FiledBy: "u1"}, wantErr: ErrInvalid},
		{name: "filed without user", in: Filing{MatterID: "m1", Court: "Superior Court", Status: FilingFiled, FiledOn: lateEvening}, wantErr: ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeFiling(tc.in, loc)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalize: %v", err)
			}
			if got.Status != tc.wantStatus {
				t.Errorf("status = %q, want %q", got.Status, tc.wantStatus)
			}
			if got.Court != strings.TrimSpace(tc.in.Court) {
				t.Errorf("court = %q, want trimmed %q", got.Court, tc.in.Court)
			}
			gotDate := ""
			if got.FiledOn != nil {
				gotDate = got.FiledOn.String()
			}
			if gotDate != tc.wantDate {
				t.Errorf("filed_on = %q, want %q", gotDate, tc.wantDate)
			}
		})
	}
}

func TestRecordFilingRejectsBeforeTouchingTheDatabase(t *testing.T) {
	ctx := clock.WithClock(context.Background(), clockfake.New(time.Date(2026, 3, 2, 15, 0, 0, 0, time.UTC), toronto(t)))
	tests := []struct {
		name string
		in   Filing
	}{
		{name: "no court", in: Filing{MatterID: "m1"}},
		{name: "filed without date", in: Filing{MatterID: "m1", Court: "Superior Court", Status: FilingFiled, FiledBy: "u1"}},
		{name: "filed with no event emitter", in: Filing{MatterID: "m1", Court: "Superior Court", Status: FilingFiled, FiledBy: "u1",
			FiledOn: time.Date(2026, 3, 2, 15, 0, 0, 0, time.UTC)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pool := dbfake.New()
			_, err := NewFilingStore(pool).RecordFiling(ctx, tc.in)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}
			if pool.Commits+pool.Rollbacks != 0 {
				t.Errorf("transactions = %d, want none", pool.Commits+pool.Rollbacks)
			}
		})
	}
	if _, err := NewFilingStore(dbfake.New()).FilingsFor(ctx, " "); !errors.Is(err, ErrInvalid) {
		t.Errorf("FilingsFor blank matter: err = %v, want ErrInvalid", err)
	}
}

func TestTemplateRender(t *testing.T) {
	s, err := NewTemplateStore(dbfake.New(), []Template{
		{ID: "t1", Name: "One", Subject: "Re {{ matter_number }}", Body: "Hi {{client_name}}, re {{matter_number}}."},
	})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	tests := []struct {
		name        string
		id          string
		vars        map[string]string
		wantSubject string
		wantBody    string
		wantErr     error
	}{
		{name: "all vars", id: "t1", vars: map[string]string{"matter_number": "2026-001", "client_name": "A"}, wantSubject: "Re 2026-001", wantBody: "Hi A, re 2026-001."},
		{name: "missing var", id: "t1", vars: map[string]string{"matter_number": "2026-001"}, wantErr: ErrMissingVar},
		{name: "unknown template", id: "nope", wantErr: ErrNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			subj, body, err := s.Render(context.Background(), tc.id, tc.vars)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if subj != tc.wantSubject || body != tc.wantBody {
				t.Errorf("render = %q / %q, want %q / %q", subj, body, tc.wantSubject, tc.wantBody)
			}
		})
	}
}

func TestMissingVarErrorNamesNoValue(t *testing.T) {
	s, err := NewTemplateStore(dbfake.New(), []Template{{ID: "t", Subject: "s", Body: "{{amount}} for {{client_name}}"}})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	_, _, err = s.Render(context.Background(), "t", map[string]string{"amount": "secret-amount"})
	if !errors.Is(err, ErrMissingVar) || strings.Contains(err.Error(), "secret-amount") || !strings.Contains(err.Error(), "client_name") {
		t.Errorf("err = %v, want ErrMissingVar naming client_name without values", err)
	}
}

func TestPutTemplateValidates(t *testing.T) {
	tests := []struct {
		name    string
		in      Template
		wantErr error
	}{
		{name: "ok", in: Template{ID: "t", Subject: "s {{a}}", Body: "b"}},
		{name: "no id", in: Template{Subject: "s", Body: "b"}, wantErr: ErrInvalid},
		{name: "no body", in: Template{ID: "t", Subject: "s"}, wantErr: ErrInvalid},
		{name: "unclosed placeholder", in: Template{ID: "t", Subject: "s", Body: "Hi {{client_name"}, wantErr: ErrInvalid},
		{name: "bad placeholder name", in: Template{ID: "t", Subject: "s", Body: "Hi {{client name}}"}, wantErr: ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, err := NewTemplateStore(dbfake.New(), nil)
			if err != nil {
				t.Fatalf("new store: %v", err)
			}
			err = s.PutTemplate(context.Background(), tc.in)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestLegalNoticeTemplatesRender(t *testing.T) {
	ts := LegalNoticeTemplates()
	s, err := NewTemplateStore(dbfake.New(), ts)
	if err != nil {
		t.Fatalf("seeded templates invalid: %v", err)
	}
	seen := map[string]bool{}
	for _, tpl := range ts {
		t.Run(tpl.ID, func(t *testing.T) {
			if seen[tpl.ID] {
				t.Fatalf("duplicate template id %s", tpl.ID)
			}
			seen[tpl.ID] = true
			vars := map[string]string{}
			for _, n := range Placeholders(tpl.Subject + tpl.Body) {
				vars[n] = "<" + n + ">"
			}
			subj, body, err := s.Render(context.Background(), tpl.ID, vars)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if strings.Contains(subj+body, "{{") {
				t.Errorf("unfilled placeholder in %q / %q", subj, body)
			}
		})
	}
}

func TestNextVersion(t *testing.T) {
	a := Template{ID: "t", Subject: "s", Body: "b"}
	b := Template{ID: "t", Subject: "s", Body: "b2"}
	if bodyRef(a) == bodyRef(b) {
		t.Fatal("changed body keeps the same body_ref")
	}
	if bodyRef(a) != bodyRef(Template{ID: "t", Name: "renamed", Subject: "s", Body: "b"}) {
		t.Error("body_ref depends on the display name")
	}
	tests := []struct {
		name        string
		found       bool
		latest      int
		latestRef   string
		ref         string
		wantVersion int
		wantWrite   bool
	}{
		{name: "first seed", ref: bodyRef(a), wantVersion: 1, wantWrite: true},
		{name: "unchanged", found: true, latest: 3, latestRef: bodyRef(a), ref: bodyRef(a), wantVersion: 3},
		{name: "changed text", found: true, latest: 3, latestRef: bodyRef(a), ref: bodyRef(b), wantVersion: 4, wantWrite: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v, w := nextVersion(tc.found, tc.latest, tc.latestRef, tc.ref)
			if v != tc.wantVersion || w != tc.wantWrite {
				t.Errorf("nextVersion = (%d, %v), want (%d, %v)", v, w, tc.wantVersion, tc.wantWrite)
			}
		})
	}
}

// A filed filing is the e-filing receipt F-DL-04 matches; drafts and rejections are not.
func TestFilingEvent(t *testing.T) {
	day := clock.Date{Year: 2026, Month: time.March, Day: 2}
	tests := []struct {
		name    string
		f       Filing
		filedOn *clock.Date
		want    FilingReceipt
		wantOK  bool
	}{
		{name: "filed", f: Filing{ID: "f1", MatterID: "m1", DocumentID: "d1", Status: FilingFiled}, filedOn: &day,
			want: FilingReceipt{FilingID: "f1", MatterID: "m1", DocumentID: "d1", FiledOn: "2026-03-02"}, wantOK: true},
		{name: "draft", f: Filing{ID: "f2", MatterID: "m1", Status: FilingDraft}},
		{name: "rejected", f: Filing{ID: "f3", MatterID: "m1", Status: FilingRejected}, filedOn: &day},
		{name: "filed without a date", f: Filing{ID: "f4", MatterID: "m1", Status: FilingFiled}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := filingEvent(tt.f, tt.filedOn)
			if ok != tt.wantOK || got != tt.want {
				t.Fatalf("filingEvent = %+v, %v; want %+v, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
