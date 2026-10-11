package webhook_test

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/webhook"
)

// Standard Webhooks reference test vector (standard-webhooks spec, libraries' shared tests).
const (
	vecSecret = "MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw" // "whsec_" prefix stripped
	vecMsgID  = "msg_p5jXN8AQM9LWM0D4loKWxJek"
	vecTS     = 1614265330
	vecBody   = `{"test": 2432232314}`
	vecSig    = "v1,g0hM9SsE+OTPJTGt/tmIKtSyZlE3uFJELVlNIOLJ1OE="
)

func vecKey(t *testing.T) []byte {
	t.Helper()
	k, err := base64.StdEncoding.DecodeString(vecSecret)
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}
	return k
}

// rule: ARC-I-033
func TestARC_I_033_StandardWebhooksSignature(t *testing.T) {
	s := webhook.HMACSigner{}
	key := vecKey(t)
	ts := time.Unix(vecTS, 0)
	if got := s.Sign(key, vecMsgID, ts, []byte(vecBody)); got != vecSig {
		t.Fatalf("Sign = %q, want %q", got, vecSig)
	}
	other := []byte("other-secret")
	otherSig := s.Sign(other, vecMsgID, ts, []byte(vecBody))

	tests := []struct {
		name   string
		now    time.Time
		body   string
		header string
		want   error
	}{
		{"reference vector", ts, vecBody, vecSig, nil},
		{"at tolerance edge", ts.Add(300 * time.Second), vecBody, vecSig, nil},
		{"older than 5 min rejected", ts.Add(301 * time.Second), vecBody, vecSig, webhook.ErrTimestampSkew},
		{"future beyond 5 min rejected", ts.Add(-301 * time.Second), vecBody, vecSig, webhook.ErrTimestampSkew},
		{"tampered body", ts, `{"test": 1}`, vecSig, webhook.ErrBadSignature},
		{"rotation old first", ts, vecBody, vecSig + " " + otherSig, nil},
		{"rotation new first", ts, vecBody, otherSig + " " + vecSig, nil},
		{"only wrong key", ts, vecBody, otherSig, webhook.ErrBadSignature},
		{"unknown version", ts, vecBody, "v1a," + strings.TrimPrefix(vecSig, "v1,"), webhook.ErrBadSignature},
		{"garbage", ts, vecBody, "v1,!!!", webhook.ErrBadSignature},
		{"empty header", ts, vecBody, "", webhook.ErrBadSignature},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := s.Verify(key, vecMsgID, ts, tc.now, []byte(tc.body), tc.header)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Verify err = %v, want %v", err, tc.want)
			}
			if err != nil && strings.Contains(err.Error(), vecSecret) {
				t.Fatalf("error leaks secret: %v", err)
			}
		})
	}
}

// rule: ARC-I-034
func TestARC_I_034_RetryKeepsWebhookID(t *testing.T) {
	s := webhook.HMACSigner{}
	key := vecKey(t)
	m := webhook.Message{ID: vecMsgID, Type: "x.y", Timestamp: time.Unix(vecTS, 0)}
	body := []byte(vecBody)

	tests := []struct {
		name    string
		secrets [][]byte
		sigs    int
	}{
		{"one key", [][]byte{key}, 1},
		{"rotation overlap", [][]byte{key, []byte("next")}, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			first := webhook.Headers(s, tc.secrets, m, body)
			retry := webhook.Headers(s, tc.secrets, m, body)
			if first.Get(webhook.HeaderID) != vecMsgID || retry.Get(webhook.HeaderID) != vecMsgID {
				t.Fatalf("webhook-id changed across attempts: %q, %q", first.Get(webhook.HeaderID), retry.Get(webhook.HeaderID))
			}
			if got := first.Get(webhook.HeaderTimestamp); got != "1614265330" {
				t.Fatalf("webhook-timestamp = %q", got)
			}
			sig := first.Get(webhook.HeaderSignature)
			if n := len(strings.Fields(sig)); n != tc.sigs {
				t.Fatalf("signatures = %d, want %d", n, tc.sigs)
			}
			if !strings.HasPrefix(sig, vecSig) {
				t.Fatalf("signature header %q does not start with reference signature", sig)
			}
			if err := s.Verify(key, m.ID, m.Timestamp, m.Timestamp, body, sig); err != nil {
				t.Fatalf("Verify: %v", err)
			}
		})
	}
}
