package recon

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const csvAccount = "0190a000-0000-7000-8000-000000000001"

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "statements", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCSVParserFixtures(t *testing.T) {
	aug := Period{Year: 2026, Month: time.August}
	tests := []struct {
		file      string
		wantErr   error
		wantLine  int    // ParseError line, when wantErr is set
		wantInMsg string // part of the ParseError reason
		wantOpen  int64
		wantClose int64
		wantLines int
	}{
		{file: "good.csv", wantOpen: 100000, wantClose: 115000, wantLines: 3},
		{file: "bom_no_lines.csv", wantOpen: -500, wantClose: -500},
		{file: "mismatch.csv", wantErr: ErrStatementMismatch, wantInMsg: "closing balance 120000"},
		{file: "decimal.csv", wantErr: ErrUnparseable, wantLine: 3, wantInMsg: "whole number of cents"},
		{file: "bad_header.csv", wantErr: ErrUnparseable, wantLine: 1, wantInMsg: "row must have 4 fields"},
		{file: "other_month.csv", wantErr: ErrUnparseable, wantLine: 3, wantInMsg: "outside the statement month 2026-08"},
		{file: "no_closing.csv", wantErr: ErrUnparseable, wantInMsg: "no closing balance"},
		{file: "short_row.csv", wantErr: ErrUnparseable, wantLine: 3, wantInMsg: "row must have 4 fields"},
		{file: "bad_date.csv", wantErr: ErrUnparseable, wantLine: 3, wantInMsg: "YYYY-MM-DD"},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			s, err := CSVParser{}.Parse(context.Background(), csvAccount, fixture(t, tt.file))
			if tt.wantErr != nil {
				var pe *ParseError
				if !errors.Is(err, tt.wantErr) || !errors.As(err, &pe) {
					t.Fatalf("err = %v, want ParseError matching %v", err, tt.wantErr)
				}
				if pe.Line != tt.wantLine || !strings.Contains(pe.Error(), tt.wantInMsg) {
					t.Fatalf("ParseError = line %d %q, want line %d containing %q", pe.Line, pe.Error(), tt.wantLine, tt.wantInMsg)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if s.AccountID != csvAccount || s.Period != aug || s.OpeningCents != tt.wantOpen || s.ClosingCents != tt.wantClose || len(s.Lines) != tt.wantLines {
				t.Fatalf("statement = %+v", s)
			}
			if err := checkStatement(s); err != nil {
				t.Fatalf("parsed statement fails the import check: %v", err)
			}
		})
	}
}

func TestCSVParserGoodLines(t *testing.T) {
	s, err := CSVParser{}.Parse(context.Background(), csvAccount, fixture(t, "good.csv"))
	if err != nil {
		t.Fatal(err)
	}
	want := []StatementLine{
		{Date: time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC), AmountCents: 25000, Reference: "Deposit Smith retainer"},
		{Date: time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC), AmountCents: -5000, Reference: "Cheque 101, Jones"},
		{Date: time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC), AmountCents: -5000, Reference: "Cheque 101, Jones"},
	}
	for i, l := range s.Lines {
		if !l.Date.Equal(want[i].Date) || l.AmountCents != want[i].AmountCents || l.Reference != want[i].Reference {
			t.Fatalf("line %d = %+v, want %+v", i, l, want[i])
		}
	}
}

func TestParseCents(t *testing.T) {
	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"0", 0, false},
		{"12345", 12345, false},
		{"-12345", -12345, false},
		{"+7", 7, false},
		{" 42 ", 42, false},
		{"12.50", 0, true},
		{"1,000", 0, true},
		{"$5", 0, true},
		{"1e3", 0, true},
		{"--5", 0, true},
		{"-", 0, true},
		{"", 0, true},
		{"1234567890123456", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseCents(tt.in)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("parseCents(%q) = %d, %v", tt.in, got, err)
			}
		})
	}
}

func TestCSVParserRefusesBinaryAndEmpty(t *testing.T) {
	tests := []struct {
		name string
		file []byte
	}{
		{"empty", nil},
		{"not utf-8", []byte{0xff, 0xfe, 0x00}},
		{"header only", []byte("kind,date,description,amount_cents\n")},
		{"unknown kind", []byte("kind,date,description,amount_cents\nfee,2026-08-01,x,1\n")},
		{"zero txn", []byte("kind,date,description,amount_cents\nopening,2026-08-01,,0\ntxn,2026-08-02,x,0\nclosing,2026-08-31,,0\n")},
		{"two openings", []byte("kind,date,description,amount_cents\nopening,2026-08-01,,0\nopening,2026-08-01,,0\nclosing,2026-08-31,,0\n")},
		{"txn without description", []byte("kind,date,description,amount_cents\nopening,2026-08-01,,0\ntxn,2026-08-02,,5\nclosing,2026-08-31,,5\n")},
		{"long description", []byte("kind,date,description,amount_cents\nopening,2026-08-01,,0\ntxn,2026-08-02," + strings.Repeat("x", maxDescription+1) + ",5\nclosing,2026-08-31,,5\n")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CSVParser{}.Parse(context.Background(), csvAccount, tt.file)
			var pe *ParseError
			if !errors.Is(err, ErrUnparseable) || !errors.As(err, &pe) {
				t.Fatalf("err = %v, want ParseError matching ErrUnparseable", err)
			}
			if bytes.Contains([]byte(pe.Error()), []byte("xxxxxxxx")) {
				t.Fatalf("ParseError quotes the file: %q", pe.Error())
			}
		})
	}
}

func TestCheckStatementAndDigest(t *testing.T) {
	aug := Period{Year: 2026, Month: time.August}
	line := StatementLine{Date: time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC), AmountCents: 500, Reference: "Deposit"}
	good := Statement{AccountID: csvAccount, Period: aug, OpeningCents: 100, ClosingCents: 600, Lines: []StatementLine{line}}
	tests := []struct {
		name string
		edit func(s *Statement)
		want error
	}{
		{"good", func(*Statement) {}, nil},
		{"bad account", func(s *Statement) { s.AccountID = "acct" }, ErrUnparseable},
		{"bad month", func(s *Statement) { s.Period.Month = 13 }, ErrUnparseable},
		{"unbalanced", func(s *Statement) { s.ClosingCents = 601 }, ErrStatementMismatch},
		{"line outside month", func(s *Statement) {
			s.Lines = []StatementLine{{Date: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), AmountCents: 500, Reference: "x"}}
		}, ErrUnparseable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := good
			s.Lines = append([]StatementLine(nil), good.Lines...)
			tt.edit(&s)
			if err := checkStatement(s); !errors.Is(err, tt.want) || (tt.want == nil && err != nil) {
				t.Fatalf("checkStatement = %v, want %v", err, tt.want)
			}
		})
	}
	other := good
	other.ClosingCents, other.Lines = 601, []StatementLine{{Date: line.Date, AmountCents: 501, Reference: "Deposit"}}
	if !bytes.Equal(statementDigest(good), statementDigest(good)) || bytes.Equal(statementDigest(good), statementDigest(other)) {
		t.Fatal("digest must be stable and differ by content")
	}
	if _, err := (PGImports{}).ImportStatement(context.Background(), good); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("ImportStatement without DB = %v, want ErrInvalidState", err)
	}
}
