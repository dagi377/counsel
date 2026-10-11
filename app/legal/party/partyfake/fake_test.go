package partyfake

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/party"
)

var t0 = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

func TestCreateAndGet(t *testing.T) {
	tests := []struct {
		name     string
		in       party.NewContact
		wantErr  error
		wantLang string
	}{
		{"person defaults to en", party.NewContact{Kind: party.Person, LegalName: "A B", DisplayName: "A"}, nil, "en"},
		{"organization in fr", party.NewContact{Kind: party.Organization, LegalName: "Co", DisplayName: "Co", PreferredLanguage: "fr", IsClient: true}, nil, "fr"},
		{"unknown kind", party.NewContact{Kind: "robot", LegalName: "x", DisplayName: "x"}, party.ErrInvalid, ""},
		{"missing name", party.NewContact{Kind: party.Person}, party.ErrInvalid, ""},
		{"bad language", party.NewContact{Kind: party.Person, LegalName: "x", DisplayName: "x", PreferredLanguage: "de"}, party.ErrInvalid, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			f := New(func() time.Time { return t0 })
			c, err := f.Create(ctx, tt.in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			got, err := f.Get(ctx, c.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got != c || got.State != party.Proposed || got.PreferredLanguage != tt.wantLang || got.RowVersion != 1 || !got.CreatedAt.Equal(t0) {
				t.Fatalf("Get = %+v, created %+v", got, c)
			}
		})
	}
}

func TestGetMissing(t *testing.T) {
	_, err := New(time.Now).Get(context.Background(), "nope")
	if !errors.Is(err, party.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestSearch(t *testing.T) {
	ctx := context.Background()
	f := New(func() time.Time { return t0 })
	for _, n := range []string{"Beta Synthetic", "alpha synthetic", "Gamma Other"} {
		if _, err := f.Create(ctx, party.NewContact{Kind: party.Person, LegalName: n, DisplayName: n}); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		query string
		want  []string
	}{
		{"SYNTHETIC", []string{"alpha synthetic", "Beta Synthetic"}},
		{"", []string{"alpha synthetic", "Beta Synthetic", "Gamma Other"}},
		{"none", nil},
	}
	for _, tt := range tests {
		got, err := f.Search(ctx, tt.query)
		if err != nil {
			t.Fatalf("search %q: %v", tt.query, err)
		}
		var names []string
		for _, c := range got {
			names = append(names, c.DisplayName)
		}
		if len(names) != len(tt.want) {
			t.Fatalf("search %q = %v, want %v", tt.query, names, tt.want)
		}
		for i := range names {
			if names[i] != tt.want[i] {
				t.Errorf("search %q = %v, want %v", tt.query, names, tt.want)
			}
		}
	}
}
