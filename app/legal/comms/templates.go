package comms

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
)

// ChannelEmail is the comms.message_template channel the legal notice templates use.
const ChannelEmail = "email"

// Message template ids for the legal notices the app ships.
const (
	TemplateBillDelivered = "legal.notice.bill_delivered"
	TemplateTrustReceipt  = "legal.notice.trust_receipt"
	TemplateCourtFiling   = "legal.notice.court_filing"
	TemplatePortalShared  = "legal.notice.portal_shared"
)

// LegalNoticeTemplates returns the message templates the app seeds at registration. Bodies
// hold placeholders only; client data reaches them as render vars, never as template text.
func LegalNoticeTemplates() []Template {
	return []Template{
		{
			ID:      TemplateBillDelivered,
			Name:    "Bill delivered",
			Subject: "Your account for matter {{matter_number}}",
			Body: "Dear {{client_name}},\n\nOur account {{bill_number}} for matter {{matter_number}} " +
				"was delivered on {{delivered_on}}. You can view it in the client portal.\n\n{{firm_name}}",
		},
		{
			ID:      TemplateTrustReceipt,
			Name:    "Funds received in trust",
			Subject: "Funds received in trust for matter {{matter_number}}",
			Body: "Dear {{client_name}},\n\nWe received {{amount}} in trust for matter {{matter_number}} " +
				"on {{received_on}}.\n\n{{firm_name}}",
		},
		{
			ID:      TemplateCourtFiling,
			Name:    "Document filed with the court",
			Subject: "Filed with {{court}}: matter {{matter_number}}",
			Body: "Dear {{client_name}},\n\nOn {{filed_on}} we filed a document with {{court}} " +
				"for matter {{matter_number}} (court reference {{reference}}).\n\n{{firm_name}}",
		},
		{
			ID:      TemplatePortalShared,
			Name:    "New item in the client portal",
			Subject: "New {{item_kind}} for matter {{matter_number}}",
			Body: "Dear {{client_name}},\n\nA new {{item_kind}} was shared with you for matter " +
				"{{matter_number}}. Sign in to the client portal to view it.\n\n{{firm_name}}",
		},
	}
}

// placeholder matches {{name}}; name is letters, digits, '_' and '.'.
var placeholder = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.]+)\s*\}\}`)

// Placeholders returns the distinct placeholder names in s, sorted.
func Placeholders(s string) []string {
	var out []string
	for _, m := range placeholder.FindAllStringSubmatch(s, -1) {
		if !slices.Contains(out, m[1]) {
			out = append(out, m[1])
		}
	}
	slices.Sort(out)
	return out
}

// fill replaces every placeholder in s from vars. The first placeholder with no var is
// reported by name (a name, never a value) with ErrMissingVar.
func fill(s string, vars map[string]string) (string, error) {
	var missing string
	out := placeholder.ReplaceAllStringFunc(s, func(m string) string {
		name := placeholder.FindStringSubmatch(m)[1]
		v, ok := vars[name]
		if !ok && missing == "" {
			missing = name
		}
		return v
	})
	if missing != "" {
		return "", fmt.Errorf("%s: %w", missing, ErrMissingVar)
	}
	return out, nil
}

// validTemplate checks t has an id, a subject and a body, and that every brace pair in them
// is a well-formed placeholder.
func validTemplate(t Template) error {
	if strings.TrimSpace(t.ID) == "" || strings.TrimSpace(t.Subject) == "" || strings.TrimSpace(t.Body) == "" {
		return fmt.Errorf("template %q: id, subject and body: %w", t.ID, ErrInvalid)
	}
	for _, s := range []string{t.Subject, t.Body} {
		rest := placeholder.ReplaceAllString(s, "")
		if strings.Contains(rest, "{{") || strings.Contains(rest, "}}") {
			return fmt.Errorf("template %q: malformed placeholder: %w", t.ID, ErrInvalid)
		}
	}
	return nil
}

// bodyRef is the comms.message_template body_ref for t: where the text lives (this
// package's catalog) and a hash of subject and body, so changed text gets a new version.
func bodyRef(t Template) string {
	h := sha256.New()
	h.Write([]byte(t.Subject))
	h.Write([]byte{0})
	h.Write([]byte(t.Body))
	return "app:comms/templates/" + t.ID + "#sha256=" + hex.EncodeToString(h.Sum(nil))
}

// nextVersion decides the row to write for a template whose current body_ref is ref, given
// the latest stored version (found is false when the key has no row yet). Unchanged text
// writes nothing.
func nextVersion(found bool, latestVersion int, latestRef, ref string) (version int, write bool) {
	if !found {
		return 1, true
	}
	if latestRef == ref {
		return latestVersion, false
	}
	return latestVersion + 1, true
}

// TemplateStore renders message templates from its catalog and records each template's
// versions in comms.message_template. The table holds a body_ref, not text.
type TemplateStore struct {
	db db.DB

	mu        sync.RWMutex
	templates map[string]Template
}

var _ Templates = (*TemplateStore)(nil)

// NewTemplateStore returns a store on pool holding the given templates.
func NewTemplateStore(pool db.DB, templates []Template) (*TemplateStore, error) {
	s := &TemplateStore{db: pool, templates: map[string]Template{}}
	for _, t := range templates {
		if err := s.PutTemplate(context.Background(), t); err != nil {
			return nil, fmt.Errorf("new template store: %w", err)
		}
	}
	return s, nil
}

// PutTemplate adds or replaces t in the catalog. Seed records the change as a new version.
func (s *TemplateStore) PutTemplate(_ context.Context, t Template) error {
	if err := validTemplate(t); err != nil {
		return fmt.Errorf("put template: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.templates[t.ID] = t
	return nil
}

// Render fills every placeholder of template id from vars.
func (s *TemplateStore) Render(_ context.Context, id string, vars map[string]string) (string, string, error) {
	s.mu.RLock()
	t, ok := s.templates[id]
	s.mu.RUnlock()
	if !ok {
		return "", "", fmt.Errorf("render %s: %w", id, ErrNotFound)
	}
	subject, err := fill(t.Subject, vars)
	if err != nil {
		return "", "", fmt.Errorf("render %s subject: %w", id, err)
	}
	body, err := fill(t.Body, vars)
	if err != nil {
		return "", "", fmt.Errorf("render %s body: %w", id, err)
	}
	return subject, body, nil
}

// Seed writes a comms.message_template row (key, version, channel, body_ref) for every
// catalog template whose text is new or changed. Running it again with the same catalog
// writes nothing. New versions are unapproved until a user approves them.
func (s *TemplateStore) Seed(ctx context.Context) error {
	s.mu.RLock()
	ts := make([]Template, 0, len(s.templates))
	for _, t := range s.templates {
		ts = append(ts, t)
	}
	s.mu.RUnlock()
	slices.SortFunc(ts, func(a, b Template) int { return strings.Compare(a.ID, b.ID) })
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		for _, t := range ts {
			ref := bodyRef(t)
			var latest int
			var latestRef string
			found := true
			err := tx.QueryRow(ctx,
				`SELECT version, body_ref FROM comms.message_template
				  WHERE key = $1 ORDER BY version DESC LIMIT 1`, t.ID).Scan(&latest, &latestRef)
			if errors.Is(err, pgx.ErrNoRows) {
				found = false
			} else if err != nil {
				return fmt.Errorf("template %s: latest: %w", t.ID, err)
			}
			v, write := nextVersion(found, latest, latestRef, ref)
			if !write {
				continue
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO comms.message_template (key, version, channel, body_ref)
				 VALUES ($1, $2, $3, $4) ON CONFLICT (key, version) DO NOTHING`,
				t.ID, v, ChannelEmail, ref); err != nil {
				return fmt.Errorf("template %s: insert: %w", t.ID, err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("seed templates: %w", err)
	}
	return nil
}
