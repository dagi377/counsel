package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
)

// DefaultPostTimeout bounds one delivery attempt when HTTPPoster.Timeout is zero.
const DefaultPostTimeout = 10 * time.Second

// ErrInsecureURL means an endpoint URL is not https; nothing is sent to it.
var ErrInsecureURL = errors.New("webhook: endpoint url is not https")

// ErrDeliveryFailed means the endpoint did not answer 2xx (a redirect counts as a failure).
var ErrDeliveryFailed = errors.New("webhook: delivery not accepted")

// Target is where one endpoint's deliveries go: its URL and the reference of its signing
// secret. The secret itself is resolved only at send time through a SecretSource.
type Target struct {
	URL       string
	SecretRef string
}

// Targets looks up the target of an endpoint id.
type Targets interface {
	Target(ctx context.Context, endpoint string) (Target, error)
}

// SecretSource resolves a secret_ref to the raw signing key. Implementations must not put
// the key in errors or logs.
type SecretSource interface {
	Secret(ctx context.Context, ref string) ([]byte, error)
}

// HTTPPoster sends one delivery as a signed Standard Webhooks POST (kernel/boot.WebhookPoster).
type HTTPPoster struct {
	Targets Targets
	Secrets SecretSource
	// Client sends the request; nil means a default client. Redirects are never followed,
	// whatever the client's CheckRedirect says.
	Client *http.Client
	// Signer signs the body; nil means HMACSigner.
	Signer Signer
	// Timeout bounds one attempt; zero means DefaultPostTimeout.
	Timeout time.Duration
}

// body is the JSON document posted for one delivery.
type body struct {
	Type      string          `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	Subject   Subject         `json:"subject"`
	Data      json.RawMessage `json:"data"`
}

// Post sends d to its endpoint. A nil error means the endpoint answered 2xx. The
// webhook-id is d.Message.ID, so every retry of one delivery carries the same id. Errors
// name the delivery and endpoint, never the secret, its reference or a signature.
func (p HTTPPoster) Post(ctx context.Context, d Delivery) error {
	if p.Targets == nil || p.Secrets == nil {
		return fmt.Errorf("post %s: poster needs targets and secrets", d.ID)
	}
	t, err := p.Targets.Target(ctx, d.Endpoint)
	if err != nil {
		return fmt.Errorf("post %s: endpoint %s: %w", d.ID, d.Endpoint, err)
	}
	u, err := url.Parse(t.URL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("post %s: endpoint %s: %w", d.ID, d.Endpoint, ErrInsecureURL)
	}
	secret, err := p.Secrets.Secret(ctx, t.SecretRef)
	if err != nil {
		// The source's error is dropped: it could echo the reference or the key.
		return fmt.Errorf("post %s: endpoint %s: signing secret unavailable", d.ID, d.Endpoint)
	}
	data := d.Message.Payload
	if len(data) == 0 {
		data = json.RawMessage("null")
	}
	raw, err := json.Marshal(body{Type: d.Message.Type, Timestamp: d.Message.Timestamp.UTC(), Subject: d.Message.Subject, Data: data})
	if err != nil {
		return fmt.Errorf("post %s: encode: %w", d.ID, err)
	}
	signer := p.Signer
	if signer == nil {
		signer = HMACSigner{}
	}
	// rule: ARC-I-033
	// rule: ARC-I-034
	h := Headers(signer, [][]byte{secret}, d.Message, raw)
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = DefaultPostTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("post %s: endpoint %s: build request", d.ID, d.Endpoint)
	}
	req.Header = h
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client().Do(req)
	if err != nil {
		// A *url.Error carries the full URL, which may hold credentials; keep only the cause.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("post %s: endpoint %s: %w", d.ID, d.Endpoint, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("post %s: endpoint %s: status %d: %w", d.ID, d.Endpoint, resp.StatusCode, ErrDeliveryFailed)
	}
	return nil
}

// client returns a copy of the configured client that never follows redirects.
func (p HTTPPoster) client() *http.Client {
	c := http.Client{}
	if p.Client != nil {
		c = *p.Client
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &c
}

// PGTargets reads endpoint targets from platform.webhook. It reads secret_ref only to hand
// it to a SecretSource; the reference is never logged.
type PGTargets struct {
	DB db.DB
}

const sqlTarget = `SELECT url, secret_ref FROM platform.webhook WHERE id = $1::uuid`

// Target returns the url and secret_ref of endpoint, or ErrUnknownEndpoint.
func (p PGTargets) Target(ctx context.Context, endpoint string) (Target, error) {
	var t Target
	err := p.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, sqlTarget, endpoint).Scan(&t.URL, &t.SecretRef)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Target{}, fmt.Errorf("webhook target %s: %w", endpoint, ErrUnknownEndpoint)
	}
	if err != nil {
		return Target{}, fmt.Errorf("webhook target %s: %w", endpoint, err)
	}
	return t, nil
}
