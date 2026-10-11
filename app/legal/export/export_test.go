package export

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/dagi377/counsel/kernel/flags"
)

func ptr(n int64) *int64 { return &n }

func findKey(node any, key string) any {
	switch v := node.(type) {
	case map[string]any:
		if got, ok := v[key]; ok {
			return got
		}
		for _, c := range v {
			if got := findKey(c, key); got != nil {
				return got
			}
		}
	case []any:
		for _, c := range v {
			if got := findKey(c, key); got != nil {
				return got
			}
		}
	}
	return nil
}

func specA3(t *testing.T) any {
	t.Helper()
	raw, err := os.ReadFile("../../../spec/eng/a3-migration.json")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	return doc
}

func onCtx(t *testing.T, on bool) context.Context {
	t.Helper()
	s := flags.New()
	if err := s.Define(flags.Def{Name: FlagPostCancelWindow, Owner: "test", Expiry: time.Now().Add(time.Hour), Doc: "test"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(FlagPostCancelWindow, on); err != nil {
		t.Fatal(err)
	}
	return flags.WithSet(context.Background(), s)
}

// rule: MIG-070
func TestMIG_070_ExportWindowAndLink(t *testing.T) {
	cancelled := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	tests := []struct {
		name      string
		on        bool
		cancelled time.Time
		now       time.Time
		want      error
	}{
		{"active firm", false, time.Time{}, cancelled, nil},
		{"day 89", true, cancelled, cancelled.Add(89 * day), nil},
		{"day 91 refused", true, cancelled, cancelled.Add(91 * day), ErrWindowClosed},
		{"flag off refuses cancelled", false, cancelled, cancelled.Add(day), ErrUnverified},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := CanRequest(onCtx(t, tc.on), tc.cancelled, tc.now); !errors.Is(err, tc.want) {
				t.Fatalf("CanRequest = %v, want %v", err, tc.want)
			}
		})
	}
	issued := cancelled
	links := []struct {
		after time.Duration
		want  error
	}{{23 * time.Hour, nil}, {24 * time.Hour, nil}, {25 * time.Hour, ErrLinkExpired}}
	for _, l := range links {
		if err := CheckLink(issued, issued.Add(l.after)); !errors.Is(err, l.want) {
			t.Fatalf("CheckLink(+%v) = %v, want %v", l.after, err, l.want)
		}
	}
	if !strings.Contains(ErrWindowClosed.Error(), "contract") {
		t.Fatal("window refusal must carry the contract reference")
	}
}

func fixture() (Header, []Content, map[string]int64) {
	h := Header{
		Firm:          Firm{ID: "7f1c7c2e-8f8c-4d4a-9a55-2f1b8c0e1a01", Name: "Fixture"},
		GeneratedAt:   time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC),
		AsOf:          time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC),
		TrustBalances: []TrustBalance{{AccountID: "acct-1", TotalCents: 123450}},
		AuditChain:    AuditChain{Events: 3, HeadHash: "abc"},
	}
	var files []Content
	live := map[string]int64{}
	for i, e := range Entities {
		n := int64(i % 4)
		live[e] = n
		files = append(files, Content{Path: JSONPath(e), Data: []byte(strings.Repeat("{}\n", int(n))), Entity: e, Rows: ptr(n)})
	}
	files = append(files,
		Content{Path: DocumentPath("m1", "d1", 1, "letter.pdf"), Data: []byte("%PDF-1 bytes")},
		Content{Path: GeneralJournalPath, Data: []byte("id\r\n")},
		Content{Path: TrustAuditPath, Data: []byte("{}\n")},
	)
	return h, files, live
}

// rule: MIG-071
func TestMIG_071_ManifestVerifies(t *testing.T) {
	h, files, live := fixture()
	m := BuildManifest(h, files)
	disk := map[string][]byte{}
	for _, f := range files {
		disk[f.Path] = append([]byte(nil), f.Data...)
	}
	doc := DocumentPath("m1", "d1", 1, "letter.pdf")
	tampered := func(d map[string][]byte) { d[doc][0] ^= 1 }
	tests := []struct {
		name    string
		mutate  func(map[string][]byte)
		live    map[string]int64
		wantErr string
	}{
		{"clean", nil, live, ""},
		{"one byte altered", tampered, live, doc + " sha256"},
		{"count differs", nil, map[string]int64{"users": 9}, "json/users.ndjson rows"},
		{"extra file", func(d map[string][]byte) { d["stray.txt"] = nil }, live, "stray.txt not in manifest"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := map[string][]byte{}
			for k, v := range disk {
				d[k] = append([]byte(nil), v...)
			}
			if tc.mutate != nil {
				tc.mutate(d)
			}
			err := Verify(m, d, tc.live)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Verify = %v", err)
				}
				return
			}
			if !errors.Is(err, ErrMismatch) || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Verify = %v, want %q", err, tc.wantErr)
			}
		})
	}
	entityFiles := 0
	for _, f := range m.Files {
		if f.Entity != "" {
			entityFiles++
		}
	}
	if entityFiles != len(Entities) {
		t.Fatalf("entity files = %d, want %d", entityFiles, len(Entities))
	}
}

// rule: MIG-071
func TestMIG_071_ManifestMatchesSchema(t *testing.T) {
	spec := specA3(t)
	raw, err := json.Marshal(findKey(spec, "export_manifest_schema"))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat = true
	if err := c.AddResource("manifest.json", bytes.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	schema, err := c.Compile("manifest.json")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	h, files, _ := fixture()
	b, err := BuildManifest(h, files).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(v); err != nil {
		t.Fatalf("manifest does not match schema: %v", err)
	}
	layout, _ := findKey(spec, "export_layout").(string)
	for _, e := range Entities {
		if !strings.Contains(layout, e) {
			t.Errorf("entity %s not in spec export_layout", e)
		}
	}
	for _, p := range []string{ManifestPath, SignaturePath, ReadmePath, GeneralJournalPath, TrustAuditPath, AppAuditPath} {
		if !strings.Contains(layout, p) {
			t.Errorf("path %s not in spec export_layout", p)
		}
	}
	if got := Root("acme", time.Date(2026, 3, 1, 9, 5, 7, 0, time.FixedZone("x", -5*3600))); got != "counsel-export-acme-20260301T140507Z" {
		t.Errorf("Root = %s", got)
	}
}

// rule: MIG-072
func TestMIG_072_OpenFormats(t *testing.T) {
	b, err := json.Marshal(NewMoney(123450))
	if err != nil {
		t.Fatal(err)
	}
	if s := string(b); strings.Contains(s, "1234.5") || !strings.Contains(s, "123450") || !strings.Contains(s, `"CAD"`) {
		t.Fatalf("money json = %s", s)
	}
	if got := Instant(time.Date(2026, 3, 1, 9, 0, 0, 0, time.FixedZone("x", -5*3600))); got != "2026-03-01T14:00:00Z" {
		t.Fatalf("Instant = %s", got)
	}
	rows := [][]string{
		{"a, b", `say "hi"`},
		{"line\nbreak", "plain"},
	}
	for _, tc := range []struct {
		name string
		rows [][]string
	}{{"comma and quote", rows[:1]}, {"newline", rows[1:]}} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := CSV([]string{"x", "y"}, tc.rows)
			if err != nil {
				t.Fatal(err)
			}
			got, err := csv.NewReader(bytes.NewReader(out)).ReadAll()
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 2 || got[1][0] != tc.rows[0][0] || got[1][1] != tc.rows[0][1] {
				t.Fatalf("round trip = %q", got)
			}
		})
	}
	if got := Sanitise("../a/b:c.pdf"); strings.ContainsAny(got, "/:") || strings.HasPrefix(got, ".") {
		t.Fatalf("Sanitise = %s", got)
	}
}
