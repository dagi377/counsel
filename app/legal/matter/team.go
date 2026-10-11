package matter

import (
	"errors"
	"fmt"
)

// ErrLastResponsible is returned when a change would leave a matter with no responsible
// lawyer. Handlers map it to 409.
var ErrLastResponsible = errors.New("matter: cannot remove the last responsible lawyer")

// CanRemoveMember checks that removing member id from the active team members leaves at least
// one responsible lawyer. It returns ErrNotFound when id is not on the team.
func CanRemoveMember(members []Member, id MemberID) error {
	var target *Member
	responsible := 0
	for i := range members {
		if members[i].ID == id {
			target = &members[i]
		}
		if members[i].Role == RoleResponsible {
			responsible++
		}
	}
	if target == nil {
		return fmt.Errorf("member %s: %w", id, ErrNotFound)
	}
	if target.Role == RoleResponsible && responsible <= 1 {
		return ErrLastResponsible
	}
	return nil
}

// CanChangeRole checks that giving member id the role to keeps at least one responsible lawyer.
func CanChangeRole(members []Member, id MemberID, to MemberRole) error {
	if to == RoleResponsible {
		for _, m := range members {
			if m.ID == id {
				return nil
			}
		}
		return fmt.Errorf("member %s: %w", id, ErrNotFound)
	}
	return CanRemoveMember(members, id)
}
