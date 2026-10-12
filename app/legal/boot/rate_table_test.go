package boot

import (
	"context"
	"errors"
	"testing"

	"github.com/dagi377/counsel/app/legal/matter"
)

// rateMatters is a matter.Query holding matters by id.
type rateMatters map[matter.ID]matter.Matter

func (q rateMatters) Get(_ context.Context, id matter.ID) (matter.Matter, error) {
	m, ok := q[id]
	if !ok {
		return matter.Matter{}, matter.ErrNotFound
	}
	return m, nil
}

func (rateMatters) List(context.Context, matter.Filter) ([]matter.Matter, error) { return nil, nil }

func (rateMatters) Members(context.Context, matter.ID) ([]matter.Member, error) { return nil, nil }

// TestMatterRateTable: a time entry is priced from the matter's own rate table, read through
// matter.Query; a matter without one gives "" so the default table applies, and an unknown
// matter prices nothing.
func TestMatterRateTable(t *testing.T) {
	q := rateMatters{
		"m-own":     {ID: "m-own", RateTableID: "rt-1"},
		"m-default": {ID: "m-default"},
	}
	tests := []struct {
		name, matter, want string
		err                error
	}{
		{"matter with its own table", "m-own", "rt-1", nil},
		{"matter without a table uses the default", "m-default", "", nil},
		{"unknown matter", "m-none", "", matter.ErrNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := matterRateTable(q)(context.Background(), tc.matter)
			if !errors.Is(err, tc.err) || got != tc.want {
				t.Fatalf("rate table = %q, %v; want %q, %v", got, err, tc.want, tc.err)
			}
		})
	}
}
