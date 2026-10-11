package docs_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dagi377/counsel/kernel/docs"
	"github.com/dagi377/counsel/kernel/docs/docsfake"
)

// textReader is the citable-text port PGText and the fake both serve.
type textReader interface {
	Lines(ctx context.Context, v docs.VersionID, page int) ([]docs.Line, error)
	Cite(ctx context.Context, v docs.VersionID, page, l0, l1 int) (docs.Citation, error)
}

// parsed serves the stored text the way PGText does once it has read the row: ParseText.
type parsed map[docs.VersionID]string

func (p parsed) text(v docs.VersionID) (docs.Text, error) {
	b, ok := p[v]
	if !ok {
		return docs.Text{}, docs.ErrNotFound
	}
	return docs.ParseText(v, []byte(b)), nil
}

func (p parsed) Lines(_ context.Context, v docs.VersionID, page int) ([]docs.Line, error) {
	t, err := p.text(v)
	if err != nil {
		return nil, err
	}
	return t.Page(page)
}

func (p parsed) Cite(_ context.Context, v docs.VersionID, page, l0, l1 int) (docs.Citation, error) {
	t, err := p.text(v)
	if err != nil {
		return docs.Citation{}, err
	}
	return t.Cite(page, l0, l1)
}

const contractBody = "Order summary\nThe customer asked for a refund.\fPage two\nSigned"

// TestTextContract runs the citable-text contract against the fake and against the parsing
// PGText applies to a stored version: same lines, same citations, same errors.
//
// rule: ARC-I-037
func TestTextContract(t *testing.T) {
	ctx := context.Background()
	f := docsfake.New()
	if err := f.RegisterKind(docs.Kind{ID: "ticket.attachment", Classification: docs.Confidential, RetentionClass: "ticket.standard"}); err != nil {
		t.Fatal(err)
	}
	fv, err := f.Put(ctx, nil, "ticket.attachment", docs.Subject{Kind: "ticket", ID: "t1"}, strings.NewReader(contractBody))
	if err != nil {
		t.Fatal(err)
	}
	impls := []struct {
		name string
		text textReader
		v    docs.VersionID
	}{
		{"fake", f, fv},
		{"parsed", parsed{"v1": contractBody}, "v1"},
	}
	tests := []struct {
		name           string
		page, l0, l1   int
		wantLines      int
		wantErr        error
		wantQuoteMatch string
	}{
		{"first page", 1, 1, 2, 2, nil, "Order summary\nThe customer asked for a refund."},
		{"second page", 2, 2, 2, 2, nil, "Signed"},
		{"page out of range", 3, 1, 1, 0, docs.ErrBadRange, ""},
		{"line out of range", 1, 2, 5, 2, docs.ErrBadRange, ""},
	}
	for _, im := range impls {
		for _, tc := range tests {
			t.Run(im.name+"/"+tc.name, func(t *testing.T) {
				lines, err := im.text.Lines(ctx, im.v, tc.page)
				if tc.wantLines == 0 {
					if !errors.Is(err, tc.wantErr) {
						t.Fatalf("lines err = %v, want %v", err, tc.wantErr)
					}
					return
				}
				if err != nil || len(lines) != tc.wantLines {
					t.Fatalf("lines = %v, %v", lines, err)
				}
				c, err := im.text.Cite(ctx, im.v, tc.page, tc.l0, tc.l1)
				if tc.wantErr != nil {
					if !errors.Is(err, tc.wantErr) {
						t.Fatalf("cite err = %v, want %v", err, tc.wantErr)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if c.QuoteHash != docs.QuoteHash(tc.wantQuoteMatch) || c.Page != tc.page || c.FromLine != tc.l0 || c.ToLine != tc.l1 {
					t.Fatalf("citation = %+v", c)
				}
			})
		}
		t.Run(im.name+"/unknown version", func(t *testing.T) {
			if _, err := im.text.Lines(ctx, "nope", 1); !errors.Is(err, docs.ErrNotFound) {
				t.Fatalf("err = %v, want ErrNotFound", err)
			}
		})
	}
}

// TestPGTextNoDatabase: PGText without a pool finds no version instead of panicking.
func TestPGTextNoDatabase(t *testing.T) {
	var p docs.PGText
	if _, err := p.Lines(context.Background(), "v1", 1); !errors.Is(err, docs.ErrNotFound) {
		t.Fatalf("lines err = %v", err)
	}
	if _, err := p.Cite(context.Background(), "v1", 1, 1, 1); !errors.Is(err, docs.ErrNotFound) {
		t.Fatalf("cite err = %v", err)
	}
}
