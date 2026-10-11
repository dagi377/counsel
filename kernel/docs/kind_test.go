package docs_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/dagi377/counsel/kernel/docs"
)

func TestRegisterKind(t *testing.T) {
	ok := docs.Kind{ID: "app.form", Templates: []string{"form-a"}, Classification: docs.Confidential, RetentionClass: "long_record"}
	tests := []struct {
		name    string
		pre     []docs.Kind
		kind    docs.Kind
		wantErr bool
		is      error
	}{
		{"valid", nil, ok, false, nil},
		{"no module prefix", nil, docs.Kind{ID: "plain_form", Classification: docs.Internal, RetentionClass: "r"}, true, nil},
		{"empty kind name", nil, docs.Kind{ID: "app.", Classification: docs.Internal, RetentionClass: "r"}, true, nil},
		{"bad classification", nil, docs.Kind{ID: "app.x", Classification: "secret", RetentionClass: "r"}, true, nil},
		{"no retention class", nil, docs.Kind{ID: "app.x", Classification: docs.Public}, true, nil},
		{"duplicate", []docs.Kind{ok}, ok, true, docs.ErrDuplicateKind},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := docs.NewRegistry()
			for _, k := range tt.pre {
				if err := g.RegisterKind(k); err != nil {
					t.Fatalf("pre-register: %v", err)
				}
			}
			err := g.RegisterKind(tt.kind)
			if (err != nil) != tt.wantErr || (tt.is != nil && !errors.Is(err, tt.is)) {
				t.Fatalf("RegisterKind() = %v, wantErr %v is %v", err, tt.wantErr, tt.is)
			}
			if tt.wantErr {
				return
			}
			got, err := g.Kind(tt.kind.ID)
			if err != nil || got.ID != tt.kind.ID || got.RetentionClass != tt.kind.RetentionClass ||
				got.Classification != tt.kind.Classification || len(got.Templates) != len(tt.kind.Templates) {
				t.Fatalf("Kind() = %+v, %v; want %+v", got, err, tt.kind)
			}
		})
	}
}

func TestKindTemplatesCopied(t *testing.T) {
	g := docs.NewRegistry()
	tpl := []string{"a"}
	if err := g.RegisterKind(docs.Kind{ID: "app.x", Templates: tpl, Classification: docs.Internal, RetentionClass: "r"}); err != nil {
		t.Fatal(err)
	}
	tpl[0] = "changed"
	got, _ := g.Kind("app.x")
	got.Templates[0] = "mutated"
	again, _ := g.Kind("app.x")
	if again.Templates[0] != "a" {
		t.Fatalf("Templates = %v, want [a]", again.Templates)
	}
}

func TestCheckUpload(t *testing.T) {
	g := docs.NewRegistry()
	if err := g.RegisterKind(docs.Kind{ID: "app.letter", Classification: docs.Confidential, RetentionClass: "long_record"}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		kind       string
		wantStatus int // 0 = accepted
	}{
		{"registered kind", "app.letter", 0},
		{"unregistered kind", "app.unknown", http.StatusUnprocessableEntity},
		{"empty kind", "", http.StatusUnprocessableEntity},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, err := docs.CheckUpload(g, tt.kind)
			if tt.wantStatus == 0 {
				if err != nil || k.ID != tt.kind {
					t.Fatalf("CheckUpload() = %+v, %v", k, err)
				}
				return
			}
			if !errors.Is(err, docs.ErrUnknownKind) {
				t.Fatalf("CheckUpload() err = %v, want ErrUnknownKind", err)
			}
			p := docs.UploadProblem(err)
			if p == nil || p.Status != tt.wantStatus {
				t.Fatalf("UploadProblem() = %+v, want status %d", p, tt.wantStatus)
			}
		})
	}
}

func TestUploadProblemOther(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"not found", docs.ErrNotFound, http.StatusNotFound},
		{"unrelated", errors.New("boom"), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := docs.UploadProblem(tt.err)
			got := 0
			if p != nil {
				got = p.Status
			}
			if got != tt.want {
				t.Fatalf("status = %d, want %d", got, tt.want)
			}
		})
	}
}
