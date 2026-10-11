package trust

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/db"
)

// RequestID identifies a trust request (trust.trust_request.id).
type RequestID string

// RequestStatus is trust.trust_request.status.
type RequestStatus string

// Trust request statuses. Create writes only RequestDraft; approval, sending and funding are
// separate steps.
const (
	RequestDraft     RequestStatus = "draft"
	RequestApproved  RequestStatus = "approved"
	RequestSent      RequestStatus = "sent"
	RequestFunded    RequestStatus = "funded"
	RequestCancelled RequestStatus = "cancelled"
)

// Request is a retainer or funds-into-trust request to a client. Money is integer cents. It
// moves no money: funds arrive later as trust receipts.
type Request struct {
	ID              RequestID
	MatterID        string
	AmountCents     int64
	Reason          string
	DueOn           time.Time // zero when no due date; a date in America/Toronto
	Status          RequestStatus
	PreparedBy      string // the person who prepared it (the agent's principal when PreparedByAgent)
	PreparedByAgent bool
	ApprovedBy      string // empty until approved
	CreatedAt       time.Time
	RowVersion      int
}

// RequestActor is who acts on a trust request. Agent is true when the agent acts for UserID.
type RequestActor struct {
	UserID string
	Agent  bool
}

// RequestFilter selects one page of Requests.List.
type RequestFilter struct {
	After RequestID
	Limit int
}

// Requests prepares trust requests and reads them back.
type Requests interface {
	// Create saves r as a draft prepared by by inside the caller's tx.
	Create(ctx context.Context, tx pgx.Tx, r Request, by RequestActor) (RequestID, error)
	// Get returns one request, or ErrNotFound.
	Get(ctx context.Context, id RequestID) (Request, error)
	// List returns requests by id after f.After, at most f.Limit.
	List(ctx context.Context, f RequestFilter) ([]Request, error)
}

// Approval refusals for trust requests.
var (
	// ErrAgentApproval: the agent never approves a trust request.
	ErrAgentApproval = errors.New("trust: the agent never approves")
	// ErrSelfApproval: the person who prepared a trust request never approves it.
	ErrSelfApproval = errors.New("trust: the preparer never approves")
)

// ValidateRequest checks the fields trust.trust_request constrains. Errors name the field,
// never its value.
func ValidateRequest(r Request) error {
	switch {
	case r.MatterID == "":
		return fmt.Errorf("matter required: %w", ErrInvalid)
	case r.PreparedBy == "":
		return fmt.Errorf("preparer required: %w", ErrInvalid)
	case r.AmountCents <= 0:
		return fmt.Errorf("amount must be > 0: %w", ErrInvalid)
	}
	return nil
}

// CheckApprover reports whether by may approve r: never the agent, never the preparer.
func CheckApprover(r Request, by RequestActor) error {
	// rule: ARC-I-051
	// The agent never approves; the preparer never approves their own request.
	switch {
	case by.Agent:
		return ErrAgentApproval
	case by.UserID == "" || by.UserID == r.PreparedBy:
		return ErrSelfApproval
	}
	return nil
}

// trust.trust_request is owned by this module; the store touches no other table.
const (
	requestCols = `id::text, matter_id::text, amount_cents, coalesce(reason, ''), due_on, status,
  prepared_by::text, prepared_by_agent, coalesce(approved_by::text, ''), created_at, row_version`
	sqlInsertRequest = `INSERT INTO trust.trust_request
  (matter_id, amount_cents, reason, due_on, prepared_by, prepared_by_agent)
VALUES ($1::uuid, $2, NULLIF($3, ''), $4::date, $5::uuid, $6)
RETURNING id::text`
	sqlGetRequest   = `SELECT ` + requestCols + ` FROM trust.trust_request WHERE id = $1::uuid`
	sqlListRequests = `SELECT ` + requestCols + ` FROM trust.trust_request
WHERE ($1 = '' OR id > NULLIF($1, '')::uuid) ORDER BY id LIMIT $2`
)

// RequestStore is the Postgres Requests over trust.trust_request. Rows are never deleted.
type RequestStore struct {
	db  db.DB
	loc *time.Location
}

var _ Requests = (*RequestStore)(nil)

// NewRequestStore returns a store on pool. loc is the business time zone due dates are read
// in (America/Toronto).
func NewRequestStore(pool db.DB, loc *time.Location) *RequestStore {
	return &RequestStore{db: pool, loc: loc}
}

func (s *RequestStore) scan(row pgx.Row) (Request, error) {
	var r Request
	var due *time.Time
	var status string
	if err := row.Scan(&r.ID, &r.MatterID, &r.AmountCents, &r.Reason, &due, &status, &r.PreparedBy,
		&r.PreparedByAgent, &r.ApprovedBy, &r.CreatedAt, &r.RowVersion); err != nil {
		return Request{}, err
	}
	if due != nil {
		r.DueOn = time.Date(due.Year(), due.Month(), due.Day(), 0, 0, 0, 0, s.loc)
	}
	r.Status = RequestStatus(status)
	return r, nil
}

// requestNotFound maps a missing row or a malformed id to ErrNotFound.
func requestNotFound(err error) error {
	var pg *pgconn.PgError
	if errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pg) && pg.Code == pgInvalidText) {
		return ErrNotFound
	}
	return err
}

// Create implements Requests inside the caller's tx. The row is always a draft with no approver.
func (s *RequestStore) Create(ctx context.Context, tx pgx.Tx, r Request, by RequestActor) (RequestID, error) {
	r.PreparedBy, r.PreparedByAgent = by.UserID, by.Agent
	if err := ValidateRequest(r); err != nil {
		return "", fmt.Errorf("create trust request: %w", err)
	}
	var due any
	if !r.DueOn.IsZero() {
		due = r.DueOn.In(s.loc).Format(time.DateOnly)
	}
	var id string
	err := tx.QueryRow(ctx, sqlInsertRequest, r.MatterID, r.AmountCents, strings.TrimSpace(r.Reason), due,
		r.PreparedBy, r.PreparedByAgent).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create trust request: %w", err)
	}
	return RequestID(id), nil
}

// Get implements Requests.
func (s *RequestStore) Get(ctx context.Context, id RequestID) (Request, error) {
	var out Request
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = s.scan(tx.QueryRow(ctx, sqlGetRequest, string(id)))
		return requestNotFound(err)
	})
	if err != nil {
		return Request{}, fmt.Errorf("get trust request: %w", err)
	}
	return out, nil
}

// List implements Requests.
func (s *RequestStore) List(ctx context.Context, f RequestFilter) ([]Request, error) {
	out := []Request{}
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, sqlListRequests, string(f.After), f.Limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := s.scan(rows)
			if err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list trust requests: %w", requestNotFound(err))
	}
	return out, nil
}
