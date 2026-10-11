package inbound

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Signed-webhook header names (Standard Webhooks wire format).
const (
	HeaderWebhookID        = "webhook-id"
	HeaderWebhookTimestamp = "webhook-timestamp"
	HeaderWebhookSignature = "webhook-signature"
)

// signatureVersion prefixes each HMAC-SHA256 entry in HeaderWebhookSignature.
const signatureVersion = "v1"

// HMACVerifier checks an HMAC-SHA256 signed webhook: the signature covers
// "<id>.<unix seconds>.<body>", the header holds space-separated "v1,<base64>" entries, and
// the timestamp must be within MaxSkew of now. Any one of Secrets may match, so a signing
// secret can rotate with two values active.
type HMACVerifier struct {
	Secrets [][]byte
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// Verify satisfies Provider.Verify. It returns the webhook id, or an error wrapping
// ErrBadSignature or ErrStaleTimestamp. Errors never carry the secret or signature value.
// rule: ARC-U-033
func (v HMACVerifier) Verify(r *http.Request, body []byte) (string, error) {
	id := r.Header.Get(HeaderWebhookID)
	rawTS := r.Header.Get(HeaderWebhookTimestamp)
	if id == "" || rawTS == "" {
		return "", fmt.Errorf("verify: missing id or timestamp: %w", ErrBadSignature)
	}
	sec, err := strconv.ParseInt(rawTS, 10, 64)
	if err != nil {
		return "", fmt.Errorf("verify: timestamp not unix seconds: %w", ErrBadSignature)
	}
	now := time.Now
	if v.Now != nil {
		now = v.Now
	}
	// rule: ARC-U-033
	if d := now().Sub(time.Unix(sec, 0)); d > MaxSkew || d < -MaxSkew {
		return "", fmt.Errorf("verify: %w", ErrStaleTimestamp)
	}
	for _, entry := range strings.Fields(r.Header.Get(HeaderWebhookSignature)) {
		ver, sig, ok := strings.Cut(entry, ",")
		if !ok || ver != signatureVersion {
			continue
		}
		got, err := base64.StdEncoding.DecodeString(sig)
		if err != nil {
			continue
		}
		for _, secret := range v.Secrets {
			if len(secret) > 0 && hmac.Equal(got, mac(secret, id, rawTS, body)) {
				return id, nil
			}
		}
	}
	return "", fmt.Errorf("verify: %w", ErrBadSignature)
}

// Sign returns the HeaderWebhookSignature value for one delivery. Senders and test fakes use it.
func Sign(secret []byte, id string, at time.Time, body []byte) string {
	return signatureVersion + "," + base64.StdEncoding.EncodeToString(mac(secret, id, strconv.FormatInt(at.Unix(), 10), body))
}

func mac(secret []byte, id, ts string, body []byte) []byte {
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(id))
	h.Write([]byte("."))
	h.Write([]byte(ts))
	h.Write([]byte("."))
	h.Write(body)
	return h.Sum(nil)
}
