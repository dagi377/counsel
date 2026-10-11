package docs

import (
	"fmt"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// Text is the extracted text of one stored version: pages of lines. It is held per
// version, so a new version never changes the text an older citation points at.
type Text struct {
	Version VersionID
	Pages   [][]Line
}

// ParseText splits extracted UTF-8 text into pages (on form feed) and lines (on newline).
// Each line is NFC-normalised; page and line numbers start at 1.
func ParseText(v VersionID, b []byte) Text {
	t := Text{Version: v}
	for pi, p := range strings.Split(string(b), "\f") {
		raw := strings.Split(strings.TrimSuffix(strings.ReplaceAll(p, "\r\n", "\n"), "\n"), "\n")
		lines := make([]Line, len(raw))
		for li, s := range raw {
			lines[li] = Line{Page: pi + 1, No: li + 1, Text: norm.NFC.String(s)}
		}
		t.Pages = append(t.Pages, lines)
	}
	return t
}

// Page returns the lines of one page. A page outside the version returns ErrBadRange.
func (t Text) Page(page int) ([]Line, error) {
	if page < 1 || page > len(t.Pages) {
		return nil, fmt.Errorf("version %s page %d: %w", t.Version, page, ErrBadRange)
	}
	return append([]Line(nil), t.Pages[page-1]...), nil
}

// span returns lines l0..l1 (inclusive) of a page.
func (t Text) span(page, l0, l1 int) ([]Line, error) {
	p, err := t.Page(page)
	if err != nil {
		return nil, err
	}
	if l0 < 1 || l1 < l0 || l1 > len(p) {
		return nil, fmt.Errorf("version %s page %d lines %d-%d: %w", t.Version, page, l0, l1, ErrBadRange)
	}
	return p[l0-1 : l1], nil
}
