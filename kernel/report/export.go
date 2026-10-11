package report

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// PDF page layout: US Letter landscape, 8 pt Helvetica.
const (
	pdfWidth     = 792
	pdfHeight    = 612
	pdfMargin    = 36
	pdfFontSize  = 8
	pdfLeading   = 10
	pdfLinesPage = (pdfHeight - 2*pdfMargin) / pdfLeading
)

// Export renders the rows of run in format f with a sha256 manifest. rows must be the rows
// the run produced: a row count that differs from the run's is refused.
func (e *Reports) Export(_ context.Context, run Run, rows Rows, f Format) (Export, error) {
	if run.ID == "" || run.RowCount != len(rows.Rows) {
		return Export{}, fmt.Errorf("export run %q: rows do not match the run: %w", run.ID, ErrInvalid)
	}
	var (
		content []byte
		err     error
	)
	switch f {
	case FormatCSV:
		content, err = renderCSV(rows)
	case FormatPDF:
		content = renderPDF(run, rows)
	default:
		return Export{}, fmt.Errorf("export run %q: format %q: %w", run.ID, f, ErrFormat)
	}
	if err != nil {
		return Export{}, fmt.Errorf("export run %q: %w", run.ID, err)
	}
	sum := sha256.Sum256(content)
	return Export{
		Content: content,
		Manifest: Manifest{
			RunID: run.ID, ReportID: run.ReportID, Format: f,
			SHA256: hex.EncodeToString(sum[:]), Bytes: len(content), RowCount: len(rows.Rows),
		},
	}, nil
}

// VerifyExport reports whether content matches the manifest's size and sha256.
func VerifyExport(content []byte, m Manifest) bool {
	sum := sha256.Sum256(content)
	return len(content) == m.Bytes && hex.EncodeToString(sum[:]) == m.SHA256
}

// renderCSV writes the header and rows as RFC 4180 CSV. A text cell that a spreadsheet would
// read as a formula is prefixed with a quote; numbers are left as they are.
func renderCSV(rows Rows) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(safeCells(rows.Columns)); err != nil {
		return nil, fmt.Errorf("csv: %w", err)
	}
	for _, r := range rows.Rows {
		if err := w.Write(safeCells(r)); err != nil {
			return nil, fmt.Errorf("csv: %w", err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, fmt.Errorf("csv: %w", err)
	}
	return buf.Bytes(), nil
}

func safeCells(in []string) []string {
	out := make([]string, len(in))
	for i, c := range in {
		out[i] = c
		if c == "" || !strings.ContainsRune("=+-@\t\r", rune(c[0])) {
			continue
		}
		if _, err := strconv.ParseInt(c, 10, 64); err == nil {
			continue
		}
		out[i] = "'" + c
	}
	return out
}

// renderPDF writes a plain PDF 1.4 file: a title line, the header and one line per row,
// paginated, in the standard Helvetica font. The output depends only on its input, so the
// same run exports to the same bytes and hash.
func renderPDF(run Run, rows Rows) []byte {
	lines := []string{run.ReportID + "  run " + string(run.ID), strings.Join(rows.Columns, " | ")}
	for _, r := range rows.Rows {
		lines = append(lines, strings.Join(r, " | "))
	}
	var pages [][]string
	for len(lines) > 0 {
		n := min(pdfLinesPage, len(lines))
		pages = append(pages, lines[:n])
		lines = lines[n:]
	}

	// Objects: 1 catalog, 2 page tree, 3 font, then a page and its content stream per page.
	var objs []string
	kids := make([]string, len(pages))
	for i := range pages {
		kids[i] = strconv.Itoa(4+2*i) + " 0 R"
	}
	objs = append(objs,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids ["+strings.Join(kids, " ")+"] /Count "+strconv.Itoa(len(pages))+" >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
	)
	for i, page := range pages {
		var s strings.Builder
		fmt.Fprintf(&s, "BT /F1 %d Tf %d TL %d %d Td\n", pdfFontSize, pdfLeading, pdfMargin, pdfHeight-pdfMargin)
		for _, l := range page {
			s.WriteString("(" + pdfText(l) + ") Tj T*\n")
		}
		s.WriteString("ET")
		stream := s.String()
		objs = append(objs,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %d %d] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", pdfWidth, pdfHeight, 5+2*i),
			"<< /Length "+strconv.Itoa(len(stream))+" >>\nstream\n"+stream+"\nendstream",
		)
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// pdfText escapes s for a PDF literal string in WinAnsi encoding. Latin-1 characters are
// written as octal escapes; characters outside it become '?'.
func pdfText(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '(' || r == ')' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r >= 0x20 && r < 0x7f:
			b.WriteRune(r)
		case r >= 0xa0 && r <= 0xff:
			fmt.Fprintf(&b, "\\%03o", r)
		default:
			b.WriteByte('?')
		}
	}
	return b.String()
}
