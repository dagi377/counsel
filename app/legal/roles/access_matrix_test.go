package roles

import (
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/dagi377/counsel/kernel/registry"
)

const accessPath = "../../../spec/legal/access.json"

var allLevels = []Level{None, View, Own, All, Approve}

func loadTable(t *testing.T) (*Table, []byte) {
	t.Helper()
	b, err := os.ReadFile(accessPath)
	if err != nil {
		t.Fatalf("read access.json: %v", err)
	}
	tbl, err := ParseAccess(b)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return tbl, b
}

// TestAccessMatrix generates one case per (role, capability) cell of access.json and checks
// Level and Can for every need level against the matrix value.
func TestAccessMatrix(t *testing.T) {
	tbl, b := loadTable(t)
	var raw struct {
		Roles  []struct{ ID string } `json:"roles"`
		Matrix struct {
			Capabilities []struct{ ID string }        `json:"capabilities"`
			Grants       map[string]map[string]string `json:"grants"`
		} `json:"matrix"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	type cell struct {
		role RoleID
		cap  string
		want Level
	}
	var cells []cell
	for _, r := range raw.Roles {
		for _, c := range raw.Matrix.Capabilities {
			want := Level(raw.Matrix.Grants[r.ID][c.ID])
			if want == "" {
				want = None
			}
			cells = append(cells, cell{RoleID(r.ID), c.ID, want})
		}
	}
	if len(cells) == 0 {
		t.Fatal("access.json has no matrix cells")
	}
	for _, tc := range cells {
		t.Run(string(tc.role)+"/"+tc.cap, func(t *testing.T) {
			got, err := tbl.Level(tc.role, tc.cap)
			if err != nil || got != tc.want {
				t.Fatalf("Level = (%s, %v), want %s", got, err, tc.want)
			}
			for _, need := range allLevels {
				ok, err := tbl.Can(tc.role, tc.cap, need)
				if err != nil {
					t.Fatalf("Can(%s): %v", need, err)
				}
				if ok != tc.want.Covers(need) {
					t.Fatalf("Can(%s) = %v, want %v", need, ok, tc.want.Covers(need))
				}
			}
		})
	}
}

func TestAccessMatrixErrors(t *testing.T) {
	tbl, _ := loadTable(t)
	tests := []struct {
		name    string
		role    RoleID
		cap     string
		need    Level
		wantErr error
	}{
		{"unknown role", "janitor", "matters.view", View, ErrUnknownRole},
		{"unknown capability", Owner, "matters.nope", View, ErrUnknownCapability},
		{"invalid level", Owner, "matters.view", "super", ErrInvalidLevel},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tbl.Can(tc.role, tc.cap, tc.need); !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestParseAccessRejects(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr error
	}{
		{"unknown role", `{"roles":[],"matrix":{"capabilities":[{"id":"a.b"}],"grants":{"x":{"a.b":"all"}}}}`, ErrUnknownRole},
		{"unknown capability", `{"roles":[{"id":"x"}],"matrix":{"capabilities":[],"grants":{"x":{"a.b":"all"}}}}`, ErrUnknownCapability},
		{"invalid level", `{"roles":[{"id":"x"}],"matrix":{"capabilities":[{"id":"a.b"}],"grants":{"x":{"a.b":"most"}}}}`, ErrInvalidLevel},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseAccess([]byte(tc.in)); !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestTableRegister(t *testing.T) {
	tbl, _ := loadTable(t)
	rec := &registry.Recorder{}
	tbl.Register(rec)
	if got, want := len(rec.Method(registry.MethodCapability)), len(tbl.caps); got != want {
		t.Fatalf("capabilities registered = %d, want %d", got, want)
	}
	if got, want := len(rec.Method(registry.MethodRole)), len(tbl.roles); got != want {
		t.Fatalf("roles registered = %d, want %d", got, want)
	}
}
