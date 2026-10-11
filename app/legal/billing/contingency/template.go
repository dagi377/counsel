package contingency

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/dagi377/counsel/app/legal/legalflags"
	"github.com/dagi377/counsel/kernel/api"
)

// templateFlag is legalflags.ContingencyFees (V-CFA). Every Templates method refuses with
// ErrTemplatesOff while it is off.
const templateFlag = legalflags.ContingencyFees

// ErrTemplatesOff is returned by every Templates method while billing.contingency_fees is off.
var ErrTemplatesOff = errors.New("contingency: fee agreement templates are not enabled")

// TagLiabilityWaiver tags a clause that relieves the lawyer of liability for negligence.
const TagLiabilityWaiver = "liability_waiver"

// AgreementType is the kind of written fee agreement a template produces.
type AgreementType string

// Agreement types. Only fixed, percentage and other s.16 agreements need assessment-officer
// approval; contingency agreements never do (BIL-L-013).
const (
	TypeHourly      AgreementType = "hourly"
	TypeFixed       AgreementType = "fixed"
	TypePercentage  AgreementType = "percentage"
	TypeOther       AgreementType = "other"
	TypeContingency AgreementType = "contingency"
)

// CourtSmallClaims is the court for which no assessment-officer approval is needed.
const CourtSmallClaims = "small_claims"

// WarnS17Approval is the preflight warning code of BIL-L-013.
const WarnS17Approval = "W-S17-APPROVAL"

// Clause is one clause of a fee agreement template. Text is never logged.
type Clause struct {
	ID   string
	Tags []string
	Text string
}

// Template is an engagement letter or fee agreement template.
type Template struct {
	ID      string
	Type    AgreementType
	Clauses []Clause
}

// FeeAgreement is a fee agreement in force on a matter, as the payment preflight sees it.
type FeeAgreement struct {
	Type AgreementType
	// Court is where the matter's business is conducted; empty for non-court work.
	Court string
	// AssessmentOfficerApproved is set once an assessment officer examined and allowed it.
	AssessmentOfficerApproved bool
}

// Templates checks fee agreement templates. It is pure: callers load and store.
type Templates struct {
}

func (s Templates) on(ctx context.Context) error {
	if !legalflags.Enabled(ctx, templateFlag) {
		return ErrTemplatesOff
	}
	return nil
}

// CheckPublish refuses to publish a template that contains a clause relieving the lawyer of
// liability for negligence. The agent drafting path runs the same check before proposing.
func (s Templates) CheckPublish(ctx context.Context, t Template) error {
	if err := s.on(ctx); err != nil {
		return err
	}
	// rule: BIL-L-014
	for _, c := range t.Clauses {
		if slices.Contains(c.Tags, TagLiabilityWaiver) {
			return &api.Problem{Status: http.StatusUnprocessableEntity, Code: "TEMPLATE_PROHIBITED_CLAUSE",
				Rule: "BIL-L-014", Title: "a fee agreement may not relieve the lawyer of liability for negligence"}
		}
	}
	return nil
}

// CourtApprovalWarnings returns the preflight warnings for applying a payment to bills under a.
// It warns, never blocks; hourly retainers and contingency agreements get no warning.
func (s Templates) CourtApprovalWarnings(ctx context.Context, a FeeAgreement) ([]string, error) {
	if err := s.on(ctx); err != nil {
		return nil, err
	}
	// rule: BIL-L-013 (verify)
	switch a.Type {
	case TypeFixed, TypePercentage, TypeOther:
		if a.Court != "" && a.Court != CourtSmallClaims && !a.AssessmentOfficerApproved {
			return []string{WarnS17Approval}, nil
		}
	}
	return nil, nil
}
