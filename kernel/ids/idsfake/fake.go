// Package idsfake is a deterministic in-memory ids.Generator.
package idsfake

import (
	"fmt"
	"sync"

	"github.com/dagi377/counsel/kernel/ids"
)

var _ ids.Generator = (*Fake)(nil)

// Fake issues UUIDv7-shaped IDs from a counter, so tests get stable, strictly increasing ids.
type Fake struct {
	mu     sync.Mutex
	next   uint64
	issued []ids.ID
}

// New returns a Fake whose first ID uses counter start.
func New(start uint64) *Fake { return &Fake{next: start} }

// New returns the next ID: 00000000-0000-7000-8000-<counter as 12 hex digits>.
func (f *Fake) New() ids.ID {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := ids.ID(fmt.Sprintf("00000000-0000-7000-8000-%012x", f.next))
	f.next++
	f.issued = append(f.issued, id)
	return id
}

// Issued returns every ID issued so far, in order.
func (f *Fake) Issued() []ids.ID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ids.ID(nil), f.issued...)
}
