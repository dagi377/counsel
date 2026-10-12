package conflict

// Test-only exports for the external test package: the in-memory stores stand in for the
// Postgres tables two service instances share.

// NewMemRuns returns an in-memory RunStore.
func NewMemRuns() RunStore { return &memRuns{} }

// NewMemDecisions returns an in-memory DecisionStore.
func NewMemDecisions() DecisionStore { return newMemDecisions() }
