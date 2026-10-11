package docsfake

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dagi377/counsel/kernel/docs"
)

func setup(t *testing.T) (*Fake, docs.VersionID) {
	t.Helper()
	f := New()
	if err := f.RegisterKind(docs.Kind{ID: "ticket.attachment", Classification: docs.Confidential, RetentionClass: "ticket.standard"}); err != nil {
		t.Fatal(err)
	}
	v, err := f.Put(context.Background(), nil, "ticket.attachment", docs.Subject{Kind: "ticket", ID: "t1"},
		strings.NewReader("one\ntwo\nthree\n\fpage two\n"))
	if err != nil {
		t.Fatal(err)
	}
	return f, v
}

func TestKinds(t *testing.T) {
	f, _ := setup(t)
	tests := []struct {
		name    string
		run     func() error
		wantErr error
	}{
		{"duplicate kind", func() error { return f.RegisterKind(docs.Kind{ID: "ticket.attachment"}) }, docs.ErrDuplicateKind},
		{"unknown kind lookup", func() error { _, err := f.Kind("ticket.nope"); return err }, docs.ErrUnknownKind},
		{"upload of unregistered kind", func() error {
			_, err := f.Put(context.Background(), nil, "ticket.nope", docs.Subject{}, strings.NewReader("x"))
			return err
		}, docs.ErrUnknownKind},
		{"registered kind lookup", func() error { _, err := f.Kind("ticket.attachment"); return err }, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestLines(t *testing.T) {
	f, v := setup(t)
	tests := []struct {
		name    string
		v       docs.VersionID
		page    int
		want    []string
		wantErr error
	}{
		{"page one", v, 1, []string{"one", "two", "three"}, nil},
		{"page two", v, 2, []string{"page two"}, nil},
		{"no page three", v, 3, nil, docs.ErrBadRange},
		{"unknown version", "nope", 1, nil, docs.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.Lines(context.Background(), tt.v, tt.page)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("Lines = %v, want %v", got, tt.want)
			}
			for i, l := range got {
				if l.Text != tt.want[i] || l.No != i+1 || l.Page != tt.page {
					t.Fatalf("Lines[%d] = %+v", i, l)
				}
			}
		})
	}
}

func TestCiteAndVerify(t *testing.T) {
	tests := []struct {
		name      string
		l0, l1    int
		change    bool
		citeErr   error
		verifyErr error
	}{
		{"unchanged", 1, 2, false, nil, nil},
		{"changed cited line", 1, 2, true, nil, docs.ErrCitationMismatch},
		{"change outside range", 3, 3, true, nil, nil},
		{"reversed range", 2, 1, false, docs.ErrBadRange, nil},
		{"past last line", 1, 4, false, docs.ErrBadRange, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			f, v := setup(t)
			c, err := f.Cite(ctx, v, 1, tt.l0, tt.l1)
			if !errors.Is(err, tt.citeErr) {
				t.Fatalf("Cite err = %v, want %v", err, tt.citeErr)
			}
			if err != nil {
				return
			}
			if len(c.QuoteHash) != 64 {
				t.Fatalf("QuoteHash = %q", c.QuoteHash)
			}
			if tt.change {
				f.Replace(v, 1, 1, "ONE")
			}
			if err := f.Verify(ctx, c); !errors.Is(err, tt.verifyErr) {
				t.Fatalf("Verify err = %v, want %v", err, tt.verifyErr)
			}
		})
	}
}

func TestErrorsCarryNoText(t *testing.T) {
	f, v := setup(t)
	c, _ := f.Cite(context.Background(), v, 1, 1, 1)
	f.Replace(v, 1, 1, "secret words")
	err := f.Verify(context.Background(), c)
	if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "one") {
		t.Fatalf("err = %v", err)
	}
}
