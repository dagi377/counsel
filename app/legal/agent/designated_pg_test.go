package agent

import (
	"context"
	"errors"
	"testing"
)

// Set refuses a malformed request before it touches the database.
func TestPGDesignatedLawyers_SetValidates(t *testing.T) {
	cases := []struct {
		name, feature, user, by string
	}{
		{"bad feature id", "rule-watch", "u-1", "u-admin"},
		{"no user", "F-DL-06", "", "u-admin"},
		{"no setter", "F-DL-06", "u-1", ""},
	}
	p := NewPGDesignatedLawyers(nil)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := p.Set(context.Background(), c.feature, c.user, c.by); !errors.Is(err, ErrNotDesignatable) {
				t.Fatalf("err = %v, want ErrNotDesignatable", err)
			}
		})
	}
}
