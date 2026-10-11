package report_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/dagi377/counsel/kernel/report"
)

func exportRun(t *testing.T) (*report.Reports, report.Run, report.Rows) {
	t.Helper()
	e, _ := newEngine(t)
	run, rows, err := e.Run(context.Background(), nil, "ops.aging", json.RawMessage(`{"period":"2026-09"}`), "ann")
	if err != nil {
		t.Fatal(err)
	}
	return e, run, rows
}

func TestExportManifest(t *testing.T) {
	for _, f := range []report.Format{report.FormatCSV, report.FormatPDF} {
		t.Run(string(f), func(t *testing.T) {
			e, run, rows := exportRun(t)
			x, err := e.Export(context.Background(), run, rows, f)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(x.Content)
			want := report.Manifest{
				RunID: run.ID, ReportID: "ops.aging", Format: f,
				SHA256: hex.EncodeToString(sum[:]), Bytes: len(x.Content), RowCount: 2,
			}
			if x.Manifest != want {
				t.Fatalf("manifest = %+v, want %+v", x.Manifest, want)
			}
			if !report.VerifyExport(x.Content, x.Manifest) {
				t.Fatal("VerifyExport rejected its own export")
			}
			tampered := append([]byte(nil), x.Content...)
			tampered[len(tampered)-2] ^= 1
			if report.VerifyExport(tampered, x.Manifest) {
				t.Fatal("VerifyExport accepted tampered content")
			}
			again, err := e.Export(context.Background(), run, rows, f)
			if err != nil || again.Manifest.SHA256 != x.Manifest.SHA256 {
				t.Fatalf("export is not deterministic: %v", err)
			}
		})
	}
}

func TestExportErrors(t *testing.T) {
	e, run, rows := exportRun(t)
	short := report.Rows{Columns: rows.Columns, Rows: rows.Rows[:1]}
	tests := []struct {
		name string
		run  report.Run
		rows report.Rows
		f    report.Format
		want error
	}{
		{"unknown format", run, rows, "xlsx", report.ErrFormat},
		{"rows from another run", run, short, report.FormatCSV, report.ErrInvalid},
		{"no run", report.Run{}, report.Rows{}, report.FormatCSV, report.ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := e.Export(context.Background(), tt.run, tt.rows, tt.f); !errors.Is(err, tt.want) {
				t.Fatalf("Export: %v, want %v", err, tt.want)
			}
		})
	}
}

func TestExportCSV(t *testing.T) {
	tests := []struct {
		name string
		cell string
		want string
	}{
		{"plain", "secret-a", "secret-a"},
		{"comma and quote", `a,"b"`, `a,"b"`},
		{"negative number", "-1500", "-1500"},
		{"formula", "=HYPERLINK(1)", "'=HYPERLINK(1)"},
		{"plus formula", "+1+1", "'+1+1"},
		{"at formula", "@SUM(A1)", "'@SUM(A1)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, _ := newEngine(t)
			rows := report.Rows{Columns: []string{"item"}, Rows: [][]string{{tt.cell}}}
			run := report.Run{ID: "run-x", ReportID: "ops.aging", RowCount: 1}
			x, err := e.Export(context.Background(), run, rows, report.FormatCSV)
			if err != nil {
				t.Fatal(err)
			}
			recs, err := csv.NewReader(bytes.NewReader(x.Content)).ReadAll()
			if err != nil {
				t.Fatal(err)
			}
			if len(recs) != 2 || recs[0][0] != "item" || recs[1][0] != tt.want {
				t.Fatalf("csv = %q, want cell %q", recs, tt.want)
			}
		})
	}
}

var objRe = regexp.MustCompile(`(?m)^(\d+) 0 obj$`)

func TestExportPDF(t *testing.T) {
	tests := []struct {
		name  string
		rows  int
		pages int
		cell  string
		want  string
	}{
		{"one page", 2, 1, "Café (x)", `Caf\351 \(x\)`},
		{"paginates", 120, 3, "row", "row"},
		{"non latin", 1, 1, "Ω", "?"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, _ := newEngine(t)
			rows := report.Rows{Columns: []string{"item", "cents"}}
			for i := 0; i < tt.rows; i++ {
				rows.Rows = append(rows.Rows, []string{tt.cell, strconv.Itoa(i)})
			}
			run := report.Run{ID: "run-x", ReportID: "ops.aging", RowCount: tt.rows}
			x, err := e.Export(context.Background(), run, rows, report.FormatPDF)
			if err != nil {
				t.Fatal(err)
			}
			pdf := string(x.Content)
			if !strings.HasPrefix(pdf, "%PDF-1.4\n") || !strings.HasSuffix(pdf, "%%EOF\n") {
				t.Fatal("not a PDF file")
			}
			if got := strings.Count(pdf, "/Type /Page "); got != tt.pages {
				t.Fatalf("pages = %d, want %d", got, tt.pages)
			}
			if !strings.Contains(pdf, "("+tt.want) {
				t.Fatalf("pdf lacks text %q", tt.want)
			}
			checkXref(t, x.Content)
		})
	}
}

// checkXref checks that every xref entry points at its object and startxref at the table.
func checkXref(t *testing.T, pdf []byte) {
	t.Helper()
	s := string(pdf)
	i := strings.LastIndex(s, "startxref\n")
	var xref int
	if _, err := fmt.Sscanf(s[i+len("startxref\n"):], "%d", &xref); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s[xref:], "xref\n") {
		t.Fatal("startxref does not point at the xref table")
	}
	n := len(objRe.FindAllString(s, -1))
	entries := strings.Split(s[xref:], "\n")[3 : 3+n]
	for k, ent := range entries {
		off, err := strconv.Atoi(ent[:10])
		if err != nil {
			t.Fatal(err)
		}
		if want := strconv.Itoa(k+1) + " 0 obj\n"; !strings.HasPrefix(s[off:], want) {
			t.Fatalf("xref entry %d points at %q", k+1, s[off:off+10])
		}
	}
}
