// Package docsfake is an in-memory kernel/docs for tests: no network, no Postgres, no
// bucket. Uploaded bytes are treated as UTF-8 text: pages split on form feed, lines on
// newline. It does not NFC-normalise; callers in tests pass normalised text.
package docsfake

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/dagi377/counsel/kernel/docs"
	"github.com/jackc/pgx/v5"
)

var _ docs.Service = (*Fake)(nil)

type version struct {
	kind    string
	subject docs.Subject
	pages   [][]string
}

// Fake stores kinds and versions in memory.
type Fake struct {
	mu       sync.Mutex
	kinds    map[string]docs.Kind
	versions map[docs.VersionID]*version
	next     int
}

// New returns an empty fake.
func New() *Fake {
	return &Fake{kinds: map[string]docs.Kind{}, versions: map[docs.VersionID]*version{}}
}

// RegisterKind implements docs.Kinds.
func (f *Fake) RegisterKind(k docs.Kind) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.kinds[k.ID]; ok {
		return fmt.Errorf("register kind %s: %w", k.ID, docs.ErrDuplicateKind)
	}
	f.kinds[k.ID] = k
	return nil
}

// Kind implements docs.Kinds.
func (f *Fake) Kind(id string) (docs.Kind, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k, ok := f.kinds[id]
	if !ok {
		return docs.Kind{}, fmt.Errorf("kind %s: %w", id, docs.ErrUnknownKind)
	}
	return k, nil
}

// Put implements docs.Store. tx is ignored.
func (f *Fake) Put(ctx context.Context, _ pgx.Tx, kind string, subject docs.Subject, r io.Reader) (docs.VersionID, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("put: %w", err)
	}
	if _, err := f.Kind(kind); err != nil {
		return "", fmt.Errorf("put: %w", err)
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("put: read: %w", err)
	}
	v := &version{kind: kind, subject: subject}
	for _, p := range strings.Split(string(b), "\f") {
		v.pages = append(v.pages, strings.Split(strings.TrimSuffix(p, "\n"), "\n"))
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	id := docs.VersionID("version-" + strconv.Itoa(f.next))
	f.versions[id] = v
	return id, nil
}

// Replace swaps the text of a stored version, to simulate a changed version in tests.
func (f *Fake) Replace(v docs.VersionID, page, line int, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.versions[v].pages[page-1][line-1] = text
}

func (f *Fake) page(v docs.VersionID, page int) ([]string, error) {
	ver, ok := f.versions[v]
	if !ok {
		return nil, fmt.Errorf("version %s: %w", v, docs.ErrNotFound)
	}
	if page < 1 || page > len(ver.pages) {
		return nil, fmt.Errorf("version %s page %d: %w", v, page, docs.ErrBadRange)
	}
	return ver.pages[page-1], nil
}

// Lines implements docs.Store.
func (f *Fake) Lines(ctx context.Context, v docs.VersionID, page int) ([]docs.Line, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("lines: %w", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	p, err := f.page(v, page)
	if err != nil {
		return nil, fmt.Errorf("lines: %w", err)
	}
	out := make([]docs.Line, len(p))
	for i, t := range p {
		out[i] = docs.Line{Page: page, No: i + 1, Text: t}
	}
	return out, nil
}

func (f *Fake) hash(v docs.VersionID, page, l0, l1 int) (string, error) {
	p, err := f.page(v, page)
	if err != nil {
		return "", err
	}
	if l0 < 1 || l1 < l0 || l1 > len(p) {
		return "", fmt.Errorf("version %s lines %d-%d: %w", v, l0, l1, docs.ErrBadRange)
	}
	// rule: ARC-I-037
	return docs.QuoteHash(strings.Join(p[l0-1:l1], "\n")), nil
}

// Cite implements docs.Store.
func (f *Fake) Cite(ctx context.Context, v docs.VersionID, page, l0, l1 int) (docs.Citation, error) {
	if err := ctx.Err(); err != nil {
		return docs.Citation{}, fmt.Errorf("cite: %w", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	h, err := f.hash(v, page, l0, l1)
	if err != nil {
		return docs.Citation{}, fmt.Errorf("cite: %w", err)
	}
	return docs.Citation{VersionID: v, Page: page, FromLine: l0, ToLine: l1, QuoteHash: h}, nil
}

// Verify implements docs.Store.
func (f *Fake) Verify(ctx context.Context, c docs.Citation) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("verify: %w", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	h, err := f.hash(c.VersionID, c.Page, c.FromLine, c.ToLine)
	if err != nil {
		return fmt.Errorf("verify: %w", err)
	}
	if h != c.QuoteHash {
		return fmt.Errorf("verify %s: %w", c.VersionID, docs.ErrCitationMismatch)
	}
	return nil
}
