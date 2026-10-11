package gates

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dagi377/counsel/kernel/registry"
)

var (
	// gateCode is the shape of a gate kind slug: <module>.<slug>.
	gateCode = regexp.MustCompile(`^[a-z]+\.[a-z_]+$`)
	// specGateCode is the shape of a spec gate id (G2, G-B2, G11t, GA1). It is a label,
	// never a gate_code (CF-04).
	specGateCode = regexp.MustCompile(`^G(-?[A-Za-z0-9]+)+`)
	// dualWhen is the only dual_when form spec/gates.json uses (Q-002).
	dualWhen = regexp.MustCompile(`^signatories>=([2-9])$`)
)

// SpecKind is one entry of spec/gates.json, with durations parsed.
type SpecKind struct {
	Kind
	Risk           registry.Risk
	MinApprovers   int
	TTL            time.Duration
	PayloadSchema  json.RawMessage
	DualApprovers  int           // 0, or approvers needed once the firm has that many eligible
	SoleOwnerDelay time.Duration // 0, or the delay before a sole eligible approver may decide
}

type specEntry struct {
	Slug           string          `json:"slug"`
	SpecID         string          `json:"spec_id"`
	Approver       string          `json:"approver"`
	Risk           string          `json:"risk"`
	MinApprovers   int             `json:"min_approvers"`
	TTL            string          `json:"ttl"`
	CoolingOff     string          `json:"cooling_off"`
	DualWhen       string          `json:"dual_when"`
	SoleOwnerDelay string          `json:"sole_owner_delay"`
	PayloadSchema  json.RawMessage `json:"payload_schema"`
}

// ParseKinds parses spec/gates.json. gate_code is the slug; a spec id in the slug position
// is rejected with ErrSpecCodeAsGateCode. Kinds come back ordered by Code.
func ParseKinds(data []byte) ([]SpecKind, error) {
	var entries []specEntry
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&entries); err != nil {
		return nil, fmt.Errorf("parse gate kinds: %w", err)
	}
	seen := map[string]bool{}
	out := make([]SpecKind, 0, len(entries))
	for _, e := range entries {
		k, err := e.kind()
		if err != nil {
			return nil, fmt.Errorf("parse gate kinds: %w", err)
		}
		if seen[k.Code] {
			return nil, fmt.Errorf("parse gate kinds: duplicate gate code %q", k.Code)
		}
		seen[k.Code] = true
		out = append(out, k)
	}
	slices.SortFunc(out, func(a, b SpecKind) int { return strings.Compare(a.Code, b.Code) })
	return out, nil
}

func (e specEntry) kind() (SpecKind, error) {
	if !gateCode.MatchString(e.Slug) {
		if specGateCode.MatchString(e.Slug) {
			return SpecKind{}, fmt.Errorf("gate code %q: %w", e.Slug, ErrSpecCodeAsGateCode)
		}
		return SpecKind{}, fmt.Errorf("gate code %q is not <module>.<slug>", e.Slug)
	}
	if !specGateCode.MatchString(e.SpecID) {
		return SpecKind{}, fmt.Errorf("gate %q: spec id %q is not a spec gate id", e.Slug, e.SpecID)
	}
	if e.Approver == "" {
		return SpecKind{}, fmt.Errorf("gate %q: no approver capability", e.Slug)
	}
	k := SpecKind{
		Kind:          Kind{Code: e.Slug, SpecGate: e.SpecID, Approver: e.Approver},
		Risk:          registry.Risk(e.Risk),
		MinApprovers:  e.MinApprovers,
		PayloadSchema: e.PayloadSchema,
	}
	var err error
	if k.TTL, err = duration(e.TTL); err != nil || k.TTL <= 0 {
		return SpecKind{}, fmt.Errorf("gate %q: ttl %q: bad duration", e.Slug, e.TTL)
	}
	if k.CoolingOff, err = duration(e.CoolingOff); err != nil {
		return SpecKind{}, fmt.Errorf("gate %q: cooling_off: %w", e.Slug, err)
	}
	if k.SoleOwnerDelay, err = duration(e.SoleOwnerDelay); err != nil {
		return SpecKind{}, fmt.Errorf("gate %q: sole_owner_delay: %w", e.Slug, err)
	}
	if e.DualWhen != "" {
		m := dualWhen.FindStringSubmatch(e.DualWhen)
		if m == nil {
			return SpecKind{}, fmt.Errorf("gate %q: dual_when %q is not signatories>=N", e.Slug, e.DualWhen)
		}
		k.DualApprovers, _ = strconv.Atoi(m[1])
	}
	if k.SoleOwnerDelay > 0 && k.MinApprovers < 2 {
		return SpecKind{}, fmt.Errorf("gate %q: sole_owner_delay needs min_approvers >= 2", e.Slug)
	}
	if len(k.PayloadSchema) == 0 {
		return SpecKind{}, fmt.Errorf("gate %q: no payload schema", e.Slug)
	}
	return k, nil
}

func duration(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("parse duration: %w", err)
	}
	return d, nil
}

// GateKind is the kernel registration for k, applied by effect. A kind with no effect yet
// still needs one to register: the feature module that owns the kind supplies it.
func (k SpecKind) GateKind(effect registry.EffectFunc) registry.GateKind {
	return registry.GateKind{
		Slug:          k.Code,
		Approver:      k.Approver,
		Risk:          k.Risk,
		MinApprovers:  k.MinApprovers,
		TTL:           k.TTL,
		PayloadSchema: slices.Clone([]byte(k.PayloadSchema)),
		Effect:        effect,
		CoolingOff:    k.CoolingOff,
	}
}

// RegisterKinds registers every kind in data (spec/gates.json) with r. effects maps a gate
// code to its effect handler; a kind with no handler registers with nil Effect and fails
// kernel validation at Seal, so a missing handler stops boot.
func RegisterKinds(r registry.Registry, data []byte, effects map[string]registry.EffectFunc) (*Catalog, error) {
	kinds, err := ParseKinds(data)
	if err != nil {
		return nil, err
	}
	for _, k := range kinds {
		r.GateKind(k.GateKind(effects[k.Code]))
	}
	return NewCatalog(kinds), nil
}

// Catalog is the read side of the legal gate kinds.
type Catalog struct {
	kinds []SpecKind // ordered by Code
}

// NewCatalog returns a catalog over kinds, which must be ordered by Code.
func NewCatalog(kinds []SpecKind) *Catalog {
	return &Catalog{kinds: slices.Clone(kinds)}
}

// Kinds lists every legal gate kind, ordered by Code.
func (c *Catalog) Kinds(_ context.Context) ([]Kind, error) {
	out := make([]Kind, len(c.kinds))
	for i, k := range c.kinds {
		out[i] = k.Kind
	}
	return out, nil
}

// Kind returns one kind by its slug. A spec id (G-B2) is never a key.
func (c *Catalog) Kind(_ context.Context, code string) (Kind, error) {
	k, err := c.Spec(code)
	if err != nil {
		return Kind{}, err
	}
	return k.Kind, nil
}

// Spec returns the full spec entry for one slug.
func (c *Catalog) Spec(code string) (SpecKind, error) {
	if !gateCode.MatchString(code) && specGateCode.MatchString(code) {
		return SpecKind{}, fmt.Errorf("gate code %q: %w", code, ErrSpecCodeAsGateCode)
	}
	i, ok := slices.BinarySearchFunc(c.kinds, code, func(k SpecKind, code string) int { return strings.Compare(k.Code, code) })
	if !ok {
		return SpecKind{}, fmt.Errorf("gate code %q: %w", code, ErrUnknownKind)
	}
	return c.kinds[i], nil
}
