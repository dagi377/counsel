package idem

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/dagi377/counsel/kernel/api"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ReplayedHeader marks a replayed response.
const ReplayedHeader = "Idempotent-Replayed"

// Problem codes, as in api/openapi.yaml.
const (
	CodeKeyRequired = "idempotency_key_required"
	CodeKeyReused   = "idempotency_key_reused"
	CodeInProgress  = "request_in_progress"
)

// Conn is the part of a pgx pool or connection the store uses.
type Conn interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// PG is the Postgres Store over platform.idempotency_key. The consumer inbox over
// platform.inbox is outbox.PG.Claim.
type PG struct {
	conn Conn
}

var _ Store = (*PG)(nil)

// NewPG returns a store on conn. Store calls run on conn, never in a business tx.
func NewPG(conn Conn) *PG { return &PG{conn: conn} }

func checkKey(k Key) error {
	if k.PrincipalID == "" || len(k.Value) < MinKeyLen || len(k.Value) > MaxKeyLen {
		return ErrInvalidKey
	}
	return nil
}

// stored is the encoded form of a Response in response_ref.
type stored struct {
	Status int         `json:"s"`
	Header http.Header `json:"h,omitempty"`
	Body   []byte      `json:"b"`
}

// rule: ARC-I-008
// Begin reserves k. Records are keyed (principal, key), hold the request SHA-256 and the full
// response, and are kept for ttl.
func (s *PG) Begin(ctx context.Context, k Key, requestSHA256 [32]byte, ttl time.Duration) (*Response, error) {
	if err := checkKey(k); err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	var inserted bool
	err := s.conn.QueryRow(ctx, `
INSERT INTO platform.idempotency_key (principal_id, key, request_sha256, expires_at)
VALUES ($1, $2, $3, now() + make_interval(secs => $4))
ON CONFLICT (principal_id, key) DO UPDATE
   SET request_sha256 = EXCLUDED.request_sha256, status_code = NULL, response_ref = NULL,
       created_at = now(), expires_at = EXCLUDED.expires_at
 WHERE platform.idempotency_key.expires_at <= now()
RETURNING true`, k.PrincipalID, k.Value, requestSHA256[:], ttl.Seconds()).Scan(&inserted)
	if err == nil {
		return nil, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("begin: reserve: %w", err)
	}
	var (
		hash   []byte
		status *int
		ref    *string
	)
	err = s.conn.QueryRow(ctx, `
SELECT request_sha256, status_code, response_ref FROM platform.idempotency_key
 WHERE principal_id = $1 AND key = $2`, k.PrincipalID, k.Value).Scan(&hash, &status, &ref)
	if err != nil {
		return nil, fmt.Errorf("begin: load: %w", err)
	}
	return decide(hash, requestSHA256, status, ref)
}

func decide(hash []byte, want [32]byte, status *int, ref *string) (*Response, error) {
	if !bytes.Equal(hash, want[:]) {
		return nil, fmt.Errorf("begin: %w", ErrKeyReused)
	}
	if status == nil || ref == nil {
		return nil, fmt.Errorf("begin: %w", ErrInFlight)
	}
	var st stored
	if err := json.Unmarshal([]byte(*ref), &st); err != nil {
		return nil, fmt.Errorf("begin: decode stored response: %w", err)
	}
	return &Response{StatusCode: st.Status, Header: st.Header, Body: st.Body}, nil
}

// Complete stores the full response for k.
func (s *PG) Complete(ctx context.Context, k Key, resp Response) error {
	b, err := json.Marshal(stored{Status: resp.StatusCode, Header: resp.Header, Body: resp.Body})
	if err != nil {
		return fmt.Errorf("complete: encode: %w", err)
	}
	tag, err := s.conn.Exec(ctx, `
UPDATE platform.idempotency_key SET status_code = $3, response_ref = $4
 WHERE principal_id = $1 AND key = $2 AND status_code IS NULL`,
		k.PrincipalID, k.Value, resp.StatusCode, string(b))
	if err != nil {
		return fmt.Errorf("complete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("complete: %w", ErrNotFound)
	}
	return nil
}

// Release drops an unfinished reservation; a completed one is kept.
func (s *PG) Release(ctx context.Context, k Key) error {
	if _, err := s.conn.Exec(ctx, `
DELETE FROM platform.idempotency_key
 WHERE principal_id = $1 AND key = $2 AND status_code IS NULL`, k.PrincipalID, k.Value); err != nil {
		return fmt.Errorf("release: %w", err)
	}
	return nil
}

// RequestHash is the SHA-256 of a request body.
func RequestHash(body []byte) [32]byte { return sha256.Sum256(body) }

// PrincipalFunc returns the id of the authenticated principal of r, or "".
type PrincipalFunc func(r *http.Request) string

// New returns the middleware for money and external-effect POSTs.
func New(store Store, principal PrincipalFunc, ttl time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			serve(store, principal, ttl, next, w, r)
		})
	}
}

func serve(store Store, principal PrincipalFunc, ttl time.Duration, next http.Handler, w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// rule: ARC-I-007
	k := Key{PrincipalID: principal(r), Value: r.Header.Get(HeaderName)}
	if k.Value == "" || checkKey(k) != nil {
		writeProblem(w, http.StatusBadRequest, CodeKeyRequired, "ARC-I-007", "Idempotency-Key header is required")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "validation_failed", "", "request body could not be read")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	prior, err := store.Begin(ctx, k, RequestHash(body), ttl)
	switch {
	case errors.Is(err, ErrKeyReused):
		// rule: ARC-I-008
		writeProblem(w, http.StatusUnprocessableEntity, CodeKeyReused, "ARC-I-008", "Idempotency-Key was used with a different request")
		return
	case errors.Is(err, ErrInFlight):
		writeProblem(w, http.StatusConflict, CodeInProgress, "ARC-I-008", "a request with this Idempotency-Key is in progress")
		return
	case err != nil:
		slog.ErrorContext(ctx, "idempotency begin failed", "err", err)
		writeProblem(w, http.StatusInternalServerError, "internal", "", "idempotency store unavailable")
		return
	case prior != nil:
		// rule: ARC-I-008
		replay(w, prior)
		return
	}
	rec := &recorder{header: http.Header{}}
	// rule: ARC-U-021
	// The handler runs its own business tx; the key is reserved before it and the response
	// stored after it commits, so no store call sits inside that tx.
	next.ServeHTTP(rec, r)
	if rec.status == 0 {
		rec.status = http.StatusOK
	}
	resp := Response{StatusCode: rec.status, Header: rec.header.Clone(), Body: rec.body.Bytes()}
	if rec.status >= 500 {
		if err := store.Release(ctx, k); err != nil {
			slog.ErrorContext(ctx, "idempotency release failed", "err", err)
		}
	} else if err := store.Complete(ctx, k, resp); err != nil {
		slog.ErrorContext(ctx, "idempotency complete failed", "err", err)
	}
	write(w, resp)
}

func replay(w http.ResponseWriter, resp *Response) {
	r := *resp
	r.Header = resp.Header.Clone()
	if r.Header == nil {
		r.Header = http.Header{}
	}
	r.Header.Set(ReplayedHeader, "true")
	write(w, r)
}

func write(w http.ResponseWriter, resp Response) {
	for k, v := range resp.Header {
		w.Header()[k] = append([]string(nil), v...)
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(resp.Body)
}

func writeProblem(w http.ResponseWriter, status int, code, rule, title string) {
	w.Header().Set("Content-Type", api.ProblemContentType)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(api.Problem{Title: title, Status: status, Code: code, Rule: rule})
}

// recorder buffers a handler's response so it can be stored before it is sent.
type recorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (r *recorder) Header() http.Header { return r.header }

func (r *recorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.body.Write(b)
	if err != nil {
		return n, fmt.Errorf("buffer response: %w", err)
	}
	return n, nil
}

// rule: ARC-U-025
// rule: ARC-I-031
// NewKey returns a UUIDv7 for an outgoing money or external call. It is made once, before
// the first attempt, stored with the attempt and sent unchanged on every retry.
func NewKey(now time.Time, rnd io.Reader) (string, error) {
	var u [16]byte
	if rnd == nil {
		rnd = crand.Reader
	}
	if _, err := io.ReadFull(rnd, u[6:]); err != nil {
		return "", fmt.Errorf("new key: %w", err)
	}
	ms := uint64(now.UnixMilli())
	var ts [8]byte
	binary.BigEndian.PutUint64(ts[:], ms)
	copy(u[:6], ts[2:])
	u[6] = 0x70 | u[6]&0x0f
	u[8] = 0x80 | u[8]&0x3f
	h := hex.EncodeToString(u[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}

// rule: ARC-I-032
// Fresh reports whether an event with subject sequence seq is new for a consumer that last
// saw lastSeen. Sequences only grow, so seq <= lastSeen is a replay and is dropped.
func Fresh(lastSeen, seq int64) bool { return seq > lastSeen }

// Action is what a sender does next with a failed external call.
type Action int

// Actions after a failed attempt.
const (
	// Retry sends again, with the same key, after Delay.
	Retry Action = iota
	// Lookup asks the provider for the operation by our key before any further send.
	Lookup
	// DeadLetter stops: the message goes to the dead-letter table and on-call is paged.
	DeadLetter
)

// Policy is the retry policy for external calls.
type Policy struct {
	Base        time.Duration
	Cap         time.Duration
	MaxAttempts int
	MaxAge      time.Duration
	// MoneyExternal marks payment, refund and payout calls: they stop at MaxAge and look
	// the operation up at the provider instead of dead-lettering blind.
	MoneyExternal bool
}

// rule: ARC-U-023
// External is the policy for external calls: base 2 s, cap 15 min, 20 attempts or 24 h.
var External = Policy{Base: 2 * time.Second, Cap: 15 * time.Minute, MaxAttempts: 20, MaxAge: 24 * time.Hour}

// rule: ARC-I-047
// MoneyExternal stops at 20 h, below a 24 h provider key lifetime.
var MoneyExternal = Policy{Base: 2 * time.Second, Cap: 15 * time.Minute, MaxAttempts: 20, MaxAge: 20 * time.Hour, MoneyExternal: true}

// rule: ARC-U-023
// Delay is the full-jitter backoff before attempt n+1 after attempt n (n >= 1): a uniform
// value in [0, min(Cap, Base*2^(n-1))]. jitter returns a value in [0, max]; nil uses math/rand.
func (p Policy) Delay(n int, jitter func(max int64) int64) time.Duration {
	if n < 1 {
		n = 1
	}
	ceil := p.Cap
	if n-1 < 32 {
		if d := p.Base << (n - 1); d > 0 && d < p.Cap {
			ceil = d
		}
	}
	if jitter == nil {
		jitter = func(max int64) int64 { return rand.Int64N(max + 1) }
	}
	return time.Duration(jitter(int64(ceil)))
}

// rule: ARC-U-023
// Retryable reports whether an HTTP status is worth retrying: 5xx, 408, 409, 425 and 429.
func Retryable(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusConflict, http.StatusTooEarly, http.StatusTooManyRequests:
		return true
	}
	return status >= 500
}

// rule: ARC-I-047
// Next decides what follows a failed attempt n (n >= 1) of a call first tried at first. A
// money call past its limit is looked up at the provider by our key, never re-sent with a
// fresh key; other calls dead-letter.
func (p Policy) Next(n, status int, first, now time.Time) Action {
	limit := n >= p.MaxAttempts || now.Sub(first) >= p.MaxAge
	switch {
	case p.MoneyExternal && (limit || !Retryable(status)):
		return Lookup
	case limit || !Retryable(status):
		return DeadLetter
	default:
		return Retry
	}
}
