package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/clock"
)

// ProblemTypePrefix is the URN prefix of every problem type; the stable code follows it.
const ProblemTypePrefix = api.ProblemTypePrefix

// Stable problem codes written by this package.
const (
	CodeValidationFailed     = "validation_failed"
	CodeBadCursor            = "bad_cursor"
	CodeInvalidDate          = "invalid_date"
	CodeVersionConflict      = "version_conflict"
	CodePreconditionRequired = "precondition_required"
	CodeRateLimited          = "rate_limited"
	CodeInternal             = "internal_error"
)

// ErrBadDate means a business date was not a valid YYYY-MM-DD (HTTP 400).
var ErrBadDate = errors.New("server: business date must be YYYY-MM-DD")

// FieldError points at one invalid member of the request body.
type FieldError struct {
	Pointer string `json:"pointer"`
	Code    string `json:"code"`
}

// ProblemBody is the wire form of a problem: api.Problem plus the extension members.
// rule: ARC-I-009
type ProblemBody struct {
	api.Problem
	TraceID string       `json:"trace_id,omitempty"`
	GateID  string       `json:"gate_id,omitempty"`
	Errors  []FieldError `json:"errors,omitempty"`
}

// NewProblem returns a problem whose type is the URN of code.
// rule: ARC-I-009
func NewProblem(status int, code, title string) api.Problem {
	return api.Problem{Type: ProblemTypePrefix + code, Title: title, Status: status, Code: code}
}

// ProblemStatuses lists the problem statuses an operation must document for its method.
// ifMatch reports whether the operation requires If-Match, which adds 412 and 428.
// rule: ARC-I-051
func ProblemStatuses(method string, ifMatch bool) []int {
	switch method {
	case http.MethodGet, http.MethodHead:
		return []int{401, 403, 404, 429}
	}
	out := []int{400, 401, 403, 404, 409, 422, 429}
	if ifMatch {
		out = append(out, 412, 428)
	}
	return out
}

// Problems writes problem+json responses and logs them by id only.
type Problems struct {
	// Log receives one record per problem; nil means slog.Default().
	Log *slog.Logger
	// TraceID returns the request's trace id; nil means none.
	TraceID func(r *http.Request) string
}

var _ ProblemWriter = (*Problems)(nil)

// WriteProblem writes p with Content-Type application/problem+json and p.Status.
func (pw *Problems) WriteProblem(w http.ResponseWriter, r *http.Request, p api.Problem) {
	pw.WriteBody(w, r, ProblemBody{Problem: p})
}

// WriteBody writes b, filling type, instance and trace_id when empty.
// rule: ARC-I-009
func (pw *Problems) WriteBody(w http.ResponseWriter, r *http.Request, b ProblemBody) {
	if b.Type == "" && b.Code != "" {
		b.Type = ProblemTypePrefix + b.Code
	}
	if b.Instance == "" && r != nil && r.URL != nil {
		b.Instance = r.URL.Path
	}
	if b.TraceID == "" && pw.TraceID != nil && r != nil {
		b.TraceID = pw.TraceID(r)
	}
	// rule: ARC-I-010
	pw.logger().Info("problem", "status", b.Status, "code", b.Code, "rule", b.Rule, "trace_id", b.TraceID)
	w.Header().Set("Content-Type", api.ProblemContentType)
	w.WriteHeader(b.Status)
	_ = json.NewEncoder(w).Encode(b)
}

// WriteError maps err to a problem and writes it. A *api.Problem or *ProblemBody in the
// chain is written as is; known sentinels get their status and code; anything else is a 500
// whose detail and log record carry no part of err's text.
func (pw *Problems) WriteError(w http.ResponseWriter, r *http.Request, err error) {
	var body *ProblemBody
	var p *api.Problem
	switch {
	case errors.As(err, &body):
		pw.WriteBody(w, r, *body)
	case errors.As(err, &p):
		pw.WriteProblem(w, r, *p)
	// rule: ARC-I-005
	case errors.Is(err, api.ErrBadCursor):
		pw.WriteProblem(w, r, NewProblem(http.StatusBadRequest, CodeBadCursor, "Cursor is malformed or was altered"))
	// rule: ARC-I-004
	case errors.Is(err, ErrBadDate):
		pw.WriteProblem(w, r, NewProblem(http.StatusBadRequest, CodeInvalidDate, "Business date must be YYYY-MM-DD"))
	case errors.Is(err, ErrPreconditionRequired):
		pw.WriteProblem(w, r, NewProblem(http.StatusPreconditionRequired, CodePreconditionRequired, "If-Match is required"))
	case errors.Is(err, ErrPreconditionFailed):
		pw.WriteProblem(w, r, NewProblem(http.StatusPreconditionFailed, CodeVersionConflict, "If-Match does not match the current version"))
	default:
		// rule: ARC-I-010
		pw.logger().Error("unhandled error", "error_type", fmt.Sprintf("%T", err))
		pw.WriteProblem(w, r, NewProblem(http.StatusInternalServerError, CodeInternal, "Internal error"))
	}
}

// RateLimit describes the limit a principal exceeded.
type RateLimit struct {
	Limit      int           // requests per window
	Window     time.Duration // window length
	RetryAfter time.Duration // time until a request is allowed again
}

// WriteRateLimited writes 429 with Retry-After, RateLimit and RateLimit-Policy headers.
// rule: ARC-I-011
func (pw *Problems) WriteRateLimited(w http.ResponseWriter, r *http.Request, rl RateLimit) {
	secs := int((rl.RetryAfter + time.Second - 1) / time.Second)
	if secs < 1 {
		secs = 1
	}
	window := int(rl.Window / time.Second)
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	w.Header().Set("RateLimit-Policy", fmt.Sprintf("%d;w=%d", rl.Limit, window))
	w.Header().Set("RateLimit", fmt.Sprintf("limit=%d, remaining=0, reset=%d", rl.Limit, secs))
	pw.WriteProblem(w, r, NewProblem(http.StatusTooManyRequests, CodeRateLimited, "Rate limit exceeded"))
}

// ParseBusinessDate parses a YYYY-MM-DD business date. It does not convert time zones: a
// business date already names a day in the cell's business zone.
// rule: ARC-I-004
func ParseBusinessDate(s string) (clock.Date, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil || t.Format(time.DateOnly) != s {
		return clock.Date{}, ErrBadDate
	}
	return clock.Date{Year: t.Year(), Month: t.Month(), Day: t.Day()}, nil
}

// BusinessDateOf returns the business date of instant t in loc.
// rule: ARC-I-004
func BusinessDateOf(t time.Time, loc *time.Location) clock.Date {
	l := t.In(loc)
	return clock.Date{Year: l.Year(), Month: l.Month(), Day: l.Day()}
}

func (pw *Problems) logger() *slog.Logger {
	if pw.Log != nil {
		return pw.Log
	}
	return slog.Default()
}
