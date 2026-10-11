package party

import (
	"fmt"
	"time"
)

// ContactColumns is the party.contact field list, exactly the a4 columns in DDL order.
// Conflict and intake stubs (ThirdParty, MatterParty, IdVerification) add no columns here:
// a field that is not in this list does not exist on a contact.
var ContactColumns = []string{
	"id", "kind", "legal_name", "display_name", "aliases", "emails", "phones", "addresses",
	"date_of_birth", "occupation", "org_number", "preferred_language", "ok_to_leave_message",
	"marketing_consent", "is_client", "state", "merged_into_id", "payment_profile_id",
	"bill_layout_id", "custom_fields", "search_tsv", "created_at", "created_by", "updated_at",
	"updated_by", "row_version", "archived_at",
}

// RelationshipColumns is the party.contact_relationship field list, exactly the a4 columns
// in DDL order. It merges matters.Relationship and conflicts.PartyRelationship.
var RelationshipColumns = []string{
	"id", "from_contact_id", "to_contact_id", "kind", "source_ref", "starts_on", "ends_on",
	"proposed_by_agent", "confirmed_by", "confirmed_at", "created_at", "created_by", "archived_at",
}

// RelationshipID is a party.contact_relationship id.
type RelationshipID string

// RelationshipKind is how one contact relates to another.
type RelationshipKind string

// Relationship kinds, as the party.contact_relationship check allows.
const (
	Spouse        RelationshipKind = "spouse"
	Parent        RelationshipKind = "parent"
	Child         RelationshipKind = "child"
	DirectorOf    RelationshipKind = "director_of"
	OfficerOf     RelationshipKind = "officer_of"
	ShareholderOf RelationshipKind = "shareholder_of"
	SubsidiaryOf  RelationshipKind = "subsidiary_of"
	EmployeeOf    RelationshipKind = "employee_of"
	MemberOfFirm  RelationshipKind = "member_of_firm"
	OtherKind     RelationshipKind = "other"
)

var relationshipKinds = map[RelationshipKind]bool{
	Spouse: true, Parent: true, Child: true, DirectorOf: true, OfficerOf: true,
	ShareholderOf: true, SubsidiaryOf: true, EmployeeOf: true, MemberOfFirm: true, OtherKind: true,
}

// Relationship is one party.contact_relationship row. Dates are America/Toronto calendar
// dates; zero means not set.
type Relationship struct {
	ID              RelationshipID
	FromContactID   ContactID
	ToContactID     ContactID
	Kind            RelationshipKind
	SourceRef       string
	StartsOn        time.Time
	EndsOn          time.Time
	ProposedByAgent bool
	ConfirmedBy     string // core.app_user id; empty until a person confirms
	ConfirmedAt     time.Time
	CreatedAt       time.Time
	CreatedBy       string
	ArchivedAt      time.Time
}

// Confirmed reports whether a person has confirmed the relationship. An agent proposal
// counts for nothing until then.
func (r Relationship) Confirmed() bool {
	return r.ConfirmedBy != "" && !r.ConfirmedAt.IsZero()
}

// Validate checks a relationship against the table's constraints. The error never carries
// names.
func (r Relationship) Validate() error {
	if r.FromContactID == "" || r.ToContactID == "" {
		return fmt.Errorf("relationship: both contacts required: %w", ErrInvalid)
	}
	if r.FromContactID == r.ToContactID {
		return fmt.Errorf("relationship: contact related to itself: %w", ErrInvalid)
	}
	if !relationshipKinds[r.Kind] {
		return fmt.Errorf("relationship: kind %q: %w", r.Kind, ErrInvalid)
	}
	if !r.StartsOn.IsZero() && !r.EndsOn.IsZero() && r.EndsOn.Before(r.StartsOn) {
		return fmt.Errorf("relationship: ends before it starts: %w", ErrInvalid)
	}
	if (r.ConfirmedBy == "") != r.ConfirmedAt.IsZero() {
		return fmt.Errorf("relationship: confirmed_by and confirmed_at go together: %w", ErrInvalid)
	}
	return nil
}
