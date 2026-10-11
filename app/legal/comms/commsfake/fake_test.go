package commsfake

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/dagi377/counsel/app/legal/comms"
)

func TestFakePortal(t *testing.T) {
	ctx := context.Background()
	f := New()
	for _, it := range []comms.SharedItem{
		{ID: "d1", MatterID: "m1", Kind: comms.ItemDocument, Title: "Retainer"},
		{ID: "b1", MatterID: "m1", Kind: comms.ItemBill, Title: "Bill 2026-0001"},
		{ID: "d2", MatterID: "m2", Kind: comms.ItemDocument, Title: "Other client"},
	} {
		if err := f.Share(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	guest := comms.Guest{ContactID: "c1", MatterIDs: []string{"m1", "m3"}}
	tests := []struct {
		name    string
		matter  string
		want    []string
		wantErr error
	}{
		{"own matter", "m1", []string{"d1", "b1"}, nil},
		{"own matter nothing shared", "m3", nil, nil},
		{"other client's matter", "m2", nil, comms.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, err := f.Items(ctx, guest, tt.matter)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			var ids []string
			for _, it := range items {
				ids = append(ids, it.ID)
			}
			if !slices.Equal(ids, tt.want) {
				t.Fatalf("items = %v, want %v", ids, tt.want)
			}
		})
	}
	if ms, _ := f.Matters(ctx, guest); !slices.Equal(ms, []string{"m1"}) {
		t.Fatalf("Matters = %v", ms)
	}
	if err := f.Share(ctx, comms.SharedItem{ID: "x", MatterID: "m1", Kind: "note"}); !errors.Is(err, comms.ErrInvalid) {
		t.Fatalf("bad kind: %v", err)
	}
}

func TestFakeFilings(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		filing  comms.Filing
		wantErr error
	}{
		{"filed", comms.Filing{MatterID: "m1", Court: "Superior Court", Status: comms.FilingFiled, Reference: "CV-26-1"}, nil},
		{"missing court", comms.Filing{MatterID: "m1", Status: comms.FilingDraft}, comms.ErrInvalid},
	}
	f := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.RecordFiling(ctx, tt.filing)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err == nil && (got.ID == "" || got.RecordedAt.IsZero()) {
				t.Fatalf("filing not stamped: %+v", got)
			}
		})
	}
	if fs, _ := f.FilingsFor(ctx, "m1"); len(fs) != 1 {
		t.Fatalf("FilingsFor = %+v", fs)
	}
}

func TestFakeRender(t *testing.T) {
	ctx := context.Background()
	f := New()
	if err := f.PutTemplate(ctx, comms.Template{ID: "notice", Subject: "Notice re {{ref}}", Body: "Dear {{name}}, see {{ref}}."}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		id      string
		vars    map[string]string
		subject string
		body    string
		wantErr error
	}{
		{"all vars", "notice", map[string]string{"ref": "CV-26-1", "name": "Ms. Roe"}, "Notice re CV-26-1", "Dear Ms. Roe, see CV-26-1.", nil},
		{"missing var", "notice", map[string]string{"ref": "CV-26-1"}, "", "", comms.ErrMissingVar},
		{"unknown template", "nope", nil, "", "", comms.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, b, err := f.Render(ctx, tt.id, tt.vars)
			if !errors.Is(err, tt.wantErr) || s != tt.subject || b != tt.body {
				t.Fatalf("Render = %q, %q, %v", s, b, err)
			}
		})
	}
}
