package api_test

import (
	"encoding/json"
	"testing"

	"github.com/dagi377/counsel/kernel/api"
)

func TestPageEncodesOpenAPIListShape(t *testing.T) {
	tests := []struct {
		name string
		page api.Page[int]
		want string
	}{
		{"nil items, last page", api.Page[int]{}, `{"data":[],"next_cursor":null}`},
		{"empty items, last page", api.Page[int]{Items: []int{}}, `{"data":[],"next_cursor":null}`},
		{"more pages", api.Page[int]{Items: []int{1, 2}, Next: "c1"}, `{"data":[1,2],"next_cursor":"c1"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.page)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Errorf("Marshal = %s, want %s", got, tc.want)
			}
			// A pointer encodes the same way, and the encoding decodes back.
			if gotPtr, _ := json.Marshal(&tc.page); string(gotPtr) != tc.want {
				t.Errorf("Marshal(&p) = %s, want %s", gotPtr, tc.want)
			}
			var back api.Page[int]
			if err := json.Unmarshal(got, &back); err != nil || back.Next != tc.page.Next || len(back.Items) != len(tc.page.Items) {
				t.Errorf("round trip = %+v (%v), want %+v", back, err, tc.page)
			}
		})
	}
}
