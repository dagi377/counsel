package docs

import (
	"fmt"
	"slices"

	"github.com/dagi377/counsel/kernel/registry"
)

// Retention class ids beyond the two fixed ones in contract.go.
// Source: spec/eng retention table; periods marked (verify) there are not yet confirmed.
const (
	RetentionFile      = "RC-FILE"
	RetentionConflict  = "RC-CONFLICT"
	RetentionIdentity  = "RC-ID"
	RetentionLead      = "RC-LEAD"
	RetentionOps       = "RC-OPS"
	RetentionConfig    = "RC-CONFIG"
	RetentionPersonnel = "RC-PERSONNEL"
	RetentionPermanent = "RC-PERM"
)

// retentionClasses are the classes Counsel registers. RC-FIN and RC-AUDIT are never purged
// in v1 (CF-15): their periods are unverified and no purge job exists for them.
var retentionClasses = []RetentionClass{
	{ID: RetentionFinancial, MinYears: 10, NeverPurge: true},
	{ID: RetentionAudit, MinYears: 10, NeverPurge: true},
	{ID: RetentionFile, MinYears: 15, DestructionGate: DestructionGate},
	{ID: RetentionConflict, NeverPurge: true},
	{ID: RetentionIdentity, MinYears: 6, DestructionGate: DestructionGate},
	{ID: RetentionLead, MinYears: 2, DestructionGate: DestructionGate},
	{ID: RetentionOps, MinYears: 1, DestructionGate: DestructionGate},
	{ID: RetentionConfig, NeverPurge: true},
	{ID: RetentionPersonnel, MinYears: 7, DestructionGate: DestructionGate},
	{ID: RetentionPermanent, NeverPurge: true},
}

// documentKinds are the legal document kinds. Matter documents follow the matter file class.
var documentKinds = []DocumentKind{
	{ID: KindCourtForm, Description: "Court or tribunal form", RetentionClass: RetentionFile},
	{ID: KindEngagementLetter, Description: "Engagement or retainer letter", RetentionClass: RetentionFile},
	{ID: KindPrecedent, Description: "Firm precedent document", RetentionClass: RetentionFile},
	{ID: KindResearchMemo, Description: "Research memo", RetentionClass: RetentionFile},
	{ID: KindBundle, Description: "Compiled document bundle", RetentionClass: RetentionFile},
	{ID: KindAnalysisTable, Description: "Analysis table", RetentionClass: RetentionFile},
}

// Kinds lists the document kinds Counsel registers.
func Kinds() []DocumentKind { return slices.Clone(documentKinds) }

// RetentionClasses lists the retention classes Counsel registers.
func RetentionClasses() []RetentionClass { return slices.Clone(retentionClasses) }

func init() { add(registerKinds) }

// registerKinds registers every retention class, then every document kind that uses one.
func registerKinds(r registry.Registry) error {
	known := map[string]bool{}
	for _, c := range retentionClasses {
		if c.NeverPurge && c.DestructionGate != "" {
			return fmt.Errorf("retention class %s: never-purge class has a destruction gate: %w", c.ID, ErrInvalid)
		}
		r.RetentionClass(registry.RetentionClass{
			ID:              c.ID,
			MinYears:        c.MinYears,
			NeverPurge:      c.NeverPurge,
			DestructionGate: c.DestructionGate,
		})
		known[c.ID] = true
	}
	for _, k := range documentKinds {
		if !known[k.RetentionClass] {
			return fmt.Errorf("document kind %s: retention class %s: %w", k.ID, k.RetentionClass, ErrInvalid)
		}
		r.DocumentKind(registry.DocumentKind{ID: k.ID, Description: k.Description, RetentionClass: k.RetentionClass})
	}
	return nil
}
