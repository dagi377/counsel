package clio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/dagi377/counsel/kernel/importer"
	"github.com/dagi377/counsel/kernel/money"
)

// SourceID is the importer source id of the practice-management system.
const SourceID = "clio"

// Command kinds the transform produces, one per target module API.
const (
	KindPartyCreate  = "party.create"
	KindMatterCreate = "matter.create"
	KindTimeCreate   = "timekeeping.entry_create"
	KindBillImport   = "billing.bill_import"
)

var _ importer.Source = (*Extractor)(nil)

// Extractor adapts a read-only Source to the kernel importer: it pages through contacts,
// matters, time entries and bills, and transforms each record into module API commands.
type Extractor struct {
	src Source
}

// NewExtractor returns an extractor over src.
func NewExtractor(src Source) *Extractor { return &Extractor{src: src} }

// Resources lists the resources in dependency order: parties before the matters that
// reference them, matters before time and bills.
func (e *Extractor) Resources() []string {
	return []string{string(ResourceContacts), string(ResourceMatters), string(ResourceActivities), string(ResourceBills)}
}

// Extract reads one page of res. The cursor is the source page token.
func (e *Extractor) Extract(ctx context.Context, res string, cur importer.Cursor) (importer.Page, error) {
	p, err := e.src.List(ctx, Resource(res), string(cur))
	if err != nil {
		return importer.Page{}, fmt.Errorf("extract %s: %w", res, err)
	}
	out := importer.Page{Next: importer.Cursor(p.Next), Done: p.Next == ""}
	for i, raw := range p.Records {
		fields := map[string]any{}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber() // money stays a decimal string, never float64 (MIG-005)
		if err := dec.Decode(&fields); err != nil {
			return importer.Page{}, fmt.Errorf("extract %s record %d: %w", res, i, err)
		}
		id := scalar(fields["id"])
		if id == "" {
			return importer.Page{}, fmt.Errorf("extract %s record %d: no id", res, i)
		}
		out.Rows = append(out.Rows, importer.Row{Resource: res, ExternalID: id, Fields: fields})
	}
	return out, nil
}

// Transform maps one staged record to the module API command that creates or updates it.
// The target id comes from the id map and every field is copied from the record, so a
// rerun on the same snapshot yields identical commands and changes nothing.
func (e *Extractor) Transform(ctx context.Context, r importer.Row, m importer.IDMap) ([]importer.Command, error) {
	id, err := m.Assign(ctx, SourceID, r.Resource, r.ExternalID)
	if err != nil {
		return nil, fmt.Errorf("transform %s:%s: %w", r.Resource, r.ExternalID, err)
	}
	f := r.Fields
	var c importer.Command
	switch Resource(r.Resource) {
	case ResourceContacts:
		c = importer.Command{Kind: KindPartyCreate, Fields: map[string]any{
			"kind":  scalar(f["type"]), // Person | Company
			"name":  scalar(f["name"]),
			"email": scalar(f["primary_email_address"]),
		}}
	case ResourceMatters:
		client, err := ref(ctx, m, f["client"], ResourceContacts)
		if err != nil {
			return nil, fmt.Errorf("transform matter %s: %w", r.ExternalID, err)
		}
		c = importer.Command{Kind: KindMatterCreate, Fields: map[string]any{
			"number":      scalar(f["display_number"]),
			"description": scalar(f["description"]),
			"status":      scalar(f["status"]),
			"client_id":   client,
		}}
	case ResourceActivities:
		if t := scalar(f["type"]); t != "TimeEntry" {
			return nil, fmt.Errorf("transform activity %s type %q: %w", r.ExternalID, t, importer.ErrUnmapped)
		}
		matter, err := ref(ctx, m, f["matter"], ResourceMatters)
		if err != nil {
			return nil, fmt.Errorf("transform activity %s: %w", r.ExternalID, err)
		}
		rate, err := cents(f["price"])
		if err != nil {
			return nil, fmt.Errorf("transform activity %s rate: %w", r.ExternalID, err)
		}
		c = importer.Command{Kind: KindTimeCreate, Fields: map[string]any{
			"matter_id":  matter,
			"date":       scalar(f["date"]), // date-only, never shifted (MIG-006)
			"seconds":    scalar(f["quantity"]),
			"rate_cents": rate,
			"note":       scalar(f["note"]),
		}}
	case ResourceBills:
		matter, err := ref(ctx, m, f["matter"], ResourceMatters)
		if err != nil {
			return nil, fmt.Errorf("transform bill %s: %w", r.ExternalID, err)
		}
		total, err := cents(f["total"])
		if err != nil {
			return nil, fmt.Errorf("transform bill %s total: %w", r.ExternalID, err)
		}
		c = importer.Command{Kind: KindBillImport, Fields: map[string]any{
			"matter_id":   matter,
			"number":      scalar(f["number"]),
			"issued_at":   scalar(f["issued_at"]),
			"state":       scalar(f["state"]),
			"total_cents": total,
			"currency":    scalar(f["currency"]),
		}}
	default:
		return nil, fmt.Errorf("transform %s:%s: %w", r.Resource, r.ExternalID, importer.ErrUnmapped)
	}
	c.TargetID = id
	return []importer.Command{c}, nil
}

// ref resolves a nested {"id": ...} reference to an internal id. A missing reference is
// empty; one with no id map entry is importer.ErrUnresolved.
func ref(ctx context.Context, m importer.IDMap, v any, res Resource) (string, error) {
	obj, ok := v.(map[string]any)
	if !ok {
		return "", nil
	}
	ext := scalar(obj["id"])
	if ext == "" {
		return "", nil
	}
	return importer.Resolve(ctx, m, SourceID, string(res), ext)
}

// cents converts a decoded money value to integer cents through ParseCents, so the decimal
// text is never parsed through float64.
//
// rule: MIG-005
func cents(v any) (money.Cents, error) {
	return ParseCents(scalar(v))
}

// scalar renders a decoded JSON scalar as a string: numbers keep their exact text.
func scalar(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case json.Number:
		return x.String()
	case bool:
		if x {
			return "true"
		}
		return "false"
	}
	return fmt.Sprint(v)
}
