package recon

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// CSVHeader is the header row a generic bank statement CSV starts with. Every following row
// is one of: an opening balance, a transaction, or a closing balance. Amounts are signed
// integer cents (+ deposit, - withdrawal); a decimal point is refused so no amount is ever
// read as a float.
//
//	kind,date,description,amount_cents
//	opening,2026-08-01,,100000
//	txn,2026-08-03,Deposit Smith retainer,25000
//	txn,2026-08-05,Cheque 101,-5000
//	closing,2026-08-31,,120000
var CSVHeader = []string{"kind", "date", "description", "amount_cents"}

// Row kinds of the generic statement CSV.
const (
	csvOpening = "opening"
	csvTxn     = "txn"
	csvClosing = "closing"
)

// maxDescription bounds a transaction description, in characters.
const maxDescription = 500

// maxAmountDigits bounds an amount's digits so the running total cannot overflow int64.
const maxAmountDigits = 15

// ParseError says why and where a statement file is refused. It matches ErrUnparseable (or
// ErrStatementMismatch when the lines do not sum to the closing balance). Its text names the
// line and the reason only; it never quotes the file.
type ParseError struct {
	Line   int // 1-based CSV line; 0 when the whole file is at fault
	Reason string
	cause  error
}

func (e *ParseError) Error() string {
	if e.Line == 0 {
		return "statement: " + e.Reason
	}
	return fmt.Sprintf("statement line %d: %s", e.Line, e.Reason)
}

// Unwrap lets errors.Is match ErrUnparseable or ErrStatementMismatch.
func (e *ParseError) Unwrap() error { return e.cause }

func bad(line int, format string, a ...any) error {
	return &ParseError{Line: line, Reason: fmt.Sprintf(format, a...), cause: ErrUnparseable}
}

// CSVParser is the generic CSV bank statement Parser (STMT-IMPORT): one row per transaction
// (date, description, amount in cents) plus one opening and one closing balance row. The
// statement month is the closing row's month; every row must fall in it. Opening plus the
// transactions must equal the closing balance.
type CSVParser struct{}

var _ Parser = CSVParser{}

// Parse implements Parser. A malformed file is a *ParseError matching ErrUnparseable.
func (CSVParser) Parse(_ context.Context, accountID string, file []byte) (Statement, error) {
	if !utf8.Valid(file) {
		return Statement{}, bad(0, "file is not UTF-8 text")
	}
	file = bytes.TrimPrefix(file, []byte("\xef\xbb\xbf"))
	r := csv.NewReader(bytes.NewReader(file))
	r.FieldsPerRecord = len(CSVHeader)
	r.TrimLeadingSpace = true
	header, err := r.Read()
	if errors.Is(err, io.EOF) {
		return Statement{}, bad(0, "file is empty")
	}
	if err != nil {
		return Statement{}, csvErr(err)
	}
	for i, h := range CSVHeader {
		if !strings.EqualFold(strings.TrimSpace(header[i]), h) {
			return Statement{}, bad(1, "header must be %s", strings.Join(CSVHeader, ","))
		}
	}
	type dated struct {
		line int
		date time.Time
	}
	var (
		s                Statement
		opening, closing *dated
		dates            []dated
		sum              int64
	)
	s.AccountID = accountID
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Statement{}, csvErr(err)
		}
		line, _ := r.FieldPos(0)
		kind := strings.ToLower(strings.TrimSpace(rec[0]))
		date, err := time.Parse(time.DateOnly, strings.TrimSpace(rec[1]))
		if err != nil {
			return Statement{}, bad(line, "date must be YYYY-MM-DD")
		}
		amount, err := parseCents(rec[3])
		if err != nil {
			return Statement{}, bad(line, "%s", err.Error())
		}
		desc := strings.TrimSpace(rec[2])
		switch kind {
		case csvOpening, csvClosing:
			if desc != "" {
				return Statement{}, bad(line, "a balance row has no description")
			}
			d := &dated{line, date}
			if kind == csvOpening {
				if opening != nil {
					return Statement{}, bad(line, "second opening balance (first on line %d)", opening.line)
				}
				opening, s.OpeningCents = d, amount
			} else {
				if closing != nil {
					return Statement{}, bad(line, "second closing balance (first on line %d)", closing.line)
				}
				closing, s.ClosingCents = d, amount
			}
		case csvTxn:
			if desc == "" {
				return Statement{}, bad(line, "transaction needs a description")
			}
			if utf8.RuneCountInString(desc) > maxDescription {
				return Statement{}, bad(line, "description is longer than %d characters", maxDescription)
			}
			if amount == 0 {
				return Statement{}, bad(line, "transaction amount is zero")
			}
			sum += amount
			s.Lines = append(s.Lines, StatementLine{Date: date, AmountCents: amount, Reference: desc})
			dates = append(dates, dated{line, date})
		default:
			return Statement{}, bad(line, "kind must be opening, txn or closing")
		}
	}
	if opening == nil {
		return Statement{}, bad(0, "no opening balance row")
	}
	if closing == nil {
		return Statement{}, bad(0, "no closing balance row")
	}
	s.Period = Period{Year: closing.date.Year(), Month: closing.date.Month()}
	for _, d := range append([]dated{*opening}, dates...) {
		if d.date.Year() != s.Period.Year || d.date.Month() != s.Period.Month {
			return Statement{}, bad(d.line, "date is outside the statement month %04d-%02d", s.Period.Year, int(s.Period.Month))
		}
	}
	if s.OpeningCents+sum != s.ClosingCents {
		return Statement{}, &ParseError{Reason: fmt.Sprintf("opening %d plus transactions %d is not the closing balance %d",
			s.OpeningCents, sum, s.ClosingCents), cause: ErrStatementMismatch}
	}
	return s, nil
}

// parseCents reads a signed integer amount of cents. A decimal point, a thousands separator
// or a currency sign is refused: the file states cents, never a float.
func parseCents(raw string) (int64, error) {
	v := strings.TrimSpace(raw)
	digits := strings.TrimLeft(v, "+-")
	if digits == "" || len(v)-len(digits) > 1 {
		return 0, errors.New("amount_cents must be a signed whole number of cents")
	}
	for _, c := range digits {
		if c < '0' || c > '9' {
			return 0, errors.New("amount_cents must be a signed whole number of cents")
		}
	}
	if len(digits) > maxAmountDigits {
		return 0, fmt.Errorf("amount_cents has more than %d digits", maxAmountDigits)
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, errors.New("amount_cents must be a signed whole number of cents")
	}
	return n, nil
}

// csvErr turns a csv reader error into a ParseError naming its line, without the file text.
func csvErr(err error) error {
	var pe *csv.ParseError
	if errors.As(err, &pe) {
		if errors.Is(pe.Err, csv.ErrFieldCount) {
			return bad(pe.Line, "row must have %d fields", len(CSVHeader))
		}
		return bad(pe.Line, "row is not valid CSV")
	}
	return bad(0, "file cannot be read")
}
