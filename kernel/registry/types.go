// CONTRACT: the data an app hands to Registry. Frozen once on main; changes are additive
// only (a new optional field or a new type) and need a docs/QUESTIONS.md entry.
// Source: spec/eng/k1-kernel.html ("Kernel modules and interfaces").

package registry

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

// Subject is an opaque reference to a business object. The kernel never interprets Kind or
// ID; the app that registered the SubjectKind resolves it.
type Subject struct {
	Kind string
	ID   string
}

// PrincipalKind says whether a principal is a person, an agent run or a portal guest.
type PrincipalKind string

// Principal kinds.
const (
	PrincipalUser  PrincipalKind = "user"
	PrincipalAgent PrincipalKind = "agent"
	PrincipalGuest PrincipalKind = "guest"
)

// Principal is who acts. An agent principal only drafts and proposes; ActingFor is the user
// it runs for, and that user counts as a preparer. Grant narrows the agent's capabilities.
type Principal struct {
	Kind      PrincipalKind
	UserID    string   // the user (user, guest)
	RunID     string   // agent only: the agent run
	ActingFor string   // agent only: the user the run acts for
	Grant     []string // agent only: capability ids granted to the run
}

// Capability is one permission the app can check with identity.Can.
type Capability struct {
	ID          string // module-prefixed, e.g. "billing.approve"
	Description string
}

// Role is a named set of capabilities.
type Role struct {
	ID           string
	Name         string
	Capabilities []string // Capability ids
}

// SubjectKind declares a kind of Subject the app owns.
type SubjectKind struct {
	ID          string
	Description string
}

// SubjectResolver decides whether a principal may see a subject (for example an access wall).
type SubjectResolver interface {
	Visible(ctx context.Context, p Principal, s Subject) (bool, error)
}

// Risk is the cost of a gate's effect if it is wrong.
type Risk string

// Risk levels. RiskIrreversible needs MinApprovers >= 2.
const (
	RiskLow          Risk = "low"
	RiskMedium       Risk = "medium"
	RiskHigh         Risk = "high"
	RiskIrreversible Risk = "irreversible"
)

// Approved is a gate that has all the approvals it needs, as passed to its effect handler.
type Approved struct {
	GateID    string
	Kind      string
	Version   int
	Subject   Subject
	Payload   json.RawMessage
	Preparer  Principal
	Approvers []Principal
}

// EffectFunc applies an approved gate. It runs at most once per gate, inside tx, while the
// kernel holds the gate lock.
type EffectFunc func(ctx context.Context, tx pgx.Tx, g Approved) error

// GateKind declares one kind of approval gate.
type GateKind struct {
	Slug          string // module-prefixed kind, e.g. "billing.write_off"
	Approver      string // Capability id an approver must hold
	Risk          Risk
	MinApprovers  int
	TTL           time.Duration
	PayloadSchema []byte // JSON Schema for the gate payload
	Effect        EffectFunc
	CoolingOff    time.Duration
}

// InboundEvent is one verified provider event.
type InboundEvent struct {
	Provider   string
	EventID    string
	Body       []byte
	ReceivedAt time.Time
}

// Provider is an inbound webhook source. Events dedup on (Provider.ID, eventID).
type Provider struct {
	ID string
	// Verify authenticates the request and returns the provider's event id.
	Verify func(r *http.Request, body []byte) (eventID string, err error)
	// Handle applies one verified event inside tx.
	Handle func(ctx context.Context, tx pgx.Tx, e InboundEvent) error
}

// DocumentKind declares a kind of stored document.
type DocumentKind struct {
	ID             string
	Description    string
	RetentionClass string // RetentionClass id
}

// RetentionClass declares how long records of a class are kept.
type RetentionClass struct {
	ID              string
	MinYears        int
	NeverPurge      bool
	DestructionGate string // GateKind slug that approves a purge
}

// Rows is a report result: a header and string-formatted cells.
type Rows struct {
	Columns []string
	Rows    [][]string
}

// ReportDef declares one report.
type ReportDef struct {
	ID         string
	Capability string // Capability id needed to run it
	Params     []byte // JSON Schema for params
	Run        func(ctx context.Context, tx pgx.Tx, params json.RawMessage) (Rows, error)
}

// LoaderFunc loads the data a portal page shows to a principal.
type LoaderFunc func(ctx context.Context, p Principal, params map[string]string) (any, error)

// PortalPage declares one page in the guest portal shell.
type PortalPage struct {
	ID         string
	Route      string
	Capability string
	Load       LoaderFunc
}

// ToolKind says what an agent tool may do.
type ToolKind string

// Tool kinds. A write tool only drafts or opens a gate; it never applies an effect.
const (
	ToolRead   ToolKind = "read"
	ToolReport ToolKind = "report"
	ToolWrite  ToolKind = "write"
)

// Citation points at lines of a stored document version.
type Citation struct {
	VersionID string
	Page      int
	FromLine  int
	ToLine    int
}

// ToolResult is what an agent tool returns.
type ToolResult struct {
	Output    json.RawMessage
	Citations []Citation
	GateID    string // set when a write tool opened a gate
}

// Tool declares one agent tool.
type Tool struct {
	Name       string
	Kind       ToolKind
	Input      []byte // JSON Schema for the input
	Capability string
	Handle     func(ctx context.Context, p Principal, in json.RawMessage) (ToolResult, error)
}

// AccountType is the ledger class of an account.
type AccountType string

// Account types.
const (
	AccountAsset     AccountType = "asset"
	AccountLiability AccountType = "liability"
	AccountEquity    AccountType = "equity"
	AccountRevenue   AccountType = "revenue"
	AccountExpense   AccountType = "expense"
)

// Account is one line of a chart of accounts.
type Account struct {
	Code string
	Name string
	Type AccountType
}

// Chart is a chart of accounts for one book.
type Chart struct {
	Book     string
	Accounts []Account
}

// CheckFunc is a smoke or restore check. It returns nil when the check passes.
type CheckFunc func(ctx context.Context) error
