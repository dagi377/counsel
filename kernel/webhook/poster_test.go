package webhook_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/webhook"
)

var _ boot.WebhookPoster = webhook.HTTPPoster{}

const posterSecret = "whsec-poster-test-key-0123456789"

type mapTargets map[string]webhook.Target

func (m mapTargets) Target(_ context.Context, ep string) (webhook.Target, error) {
	t, ok := m[ep]
	if !ok {
		return webhook.Target{}, webhook.ErrUnknownEndpoint
	}
	return t, nil
}

type mapSecrets map[string]string

func (m mapSecrets) Secret(_ context.Context, ref string) ([]byte, error) {
	s, ok := m[ref]
	if !ok {
		// A careless source that echoes the reference and a key: the poster must drop it.
		return nil, errors.New("no secret for " + ref + " near " + posterSecret)
	}
	return []byte(s), nil
}

type seen struct {
	mu   sync.Mutex
	reqs []*http.Request
	body [][]byte
}

func (s *seen) add(r *http.Request, b []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqs = append(s.reqs, r)
	s.body = append(s.body, b)
}

func delivery() webhook.Delivery {
	return webhook.Delivery{
		ID: "d-1", Endpoint: "ep",
		Message: webhook.Message{
			ID: "msg-1", Type: "work.item_created", Subject: webhook.Subject{Kind: "item", ID: "42"},
			Timestamp: time.Now().Truncate(time.Second), Payload: json.RawMessage(`{"n":1}`),
		},
	}
}

func newPoster(srv *httptest.Server, url string) webhook.HTTPPoster {
	return webhook.HTTPPoster{
		Targets: mapTargets{"ep": {URL: url, SecretRef: "ref:ep"}},
		Secrets: mapSecrets{"ref:ep": posterSecret},
		Client:  srv.Client(),
		Timeout: 300 * time.Millisecond,
	}
}

func TestHTTPPosterStatus(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		wantErr bool
	}{
		{"200 ok", func(w http.ResponseWriter, _ *http.Request) {}, false},
		{"204 no content", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }, false},
		{"400 rejected", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(400) }, true},
		{"500 server error", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }, true},
		{"redirect not followed", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/elsewhere" {
				return
			}
			http.Redirect(w, r, "/elsewhere", http.StatusTemporaryRedirect)
		}, true},
		{"timeout", func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
			}
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var s seen
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				s.add(r, b)
				tc.handler(w, r)
			}))
			defer srv.Close()
			err := newPoster(srv, srv.URL+"/hook").Post(context.Background(), delivery())
			if (err != nil) != tc.wantErr {
				t.Fatalf("Post err = %v, wantErr %v", err, tc.wantErr)
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			if len(s.reqs) != 1 {
				t.Fatalf("requests = %d, want 1 (redirects not followed)", len(s.reqs))
			}
		})
	}
}

// rule: ARC-I-033
// rule: ARC-I-034
func TestHTTPPosterSignsAndKeepsIDAcrossRetries(t *testing.T) {
	var s seen
	fail := true
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.add(r, b)
		if fail {
			fail = false
			w.WriteHeader(503)
		}
	}))
	defer srv.Close()
	p := newPoster(srv, srv.URL)
	d := delivery()
	if err := p.Post(context.Background(), d); err == nil {
		t.Fatal("first attempt: want error on 503")
	}
	d.Attempts++
	if err := p.Post(context.Background(), d); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if len(s.reqs) != 2 {
		t.Fatalf("requests = %d, want 2", len(s.reqs))
	}
	for i, r := range s.reqs {
		if r.Method != http.MethodPost {
			t.Errorf("attempt %d: method %s", i, r.Method)
		}
		if got := r.Header.Get(webhook.HeaderID); got != d.Message.ID {
			t.Errorf("attempt %d: webhook-id %q, want %q", i, got, d.Message.ID)
		}
		ts, err := strconv.ParseInt(r.Header.Get(webhook.HeaderTimestamp), 10, 64)
		if err != nil {
			t.Fatalf("attempt %d: timestamp: %v", i, err)
		}
		if err := (webhook.HMACSigner{}).Verify([]byte(posterSecret), r.Header.Get(webhook.HeaderID), time.Unix(ts, 0), time.Now(), s.body[i], r.Header.Get(webhook.HeaderSignature)); err != nil {
			t.Errorf("attempt %d: signature: %v", i, err)
		}
		var got struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(s.body[i], &got); err != nil || got.Type != d.Message.Type || string(got.Data) != `{"n":1}` {
			t.Errorf("attempt %d: body %s (err %v)", i, s.body[i], err)
		}
	}
	if !bytes.Equal(s.body[0], s.body[1]) {
		t.Error("retry body differs from first attempt")
	}
}

func TestHTTPPosterRefusesNonHTTPS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("plain http endpoint was called")
	}))
	defer srv.Close()
	for _, u := range []string{srv.URL, "ftp://example.test/hook", "https://", "::bad"} {
		err := newPoster(srv, u).Post(context.Background(), delivery())
		if !errors.Is(err, webhook.ErrInsecureURL) {
			t.Errorf("url %q: err = %v, want ErrInsecureURL", u, err)
		}
	}
}

func TestHTTPPosterNeverLeaksSecret(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }))
	defer srv.Close()
	for _, tc := range []struct {
		name string
		p    webhook.HTTPPoster
	}{
		{"endpoint error", newPoster(srv, srv.URL)},
		{"secret lookup error", func() webhook.HTTPPoster {
			p := newPoster(srv, srv.URL)
			p.Secrets = mapSecrets{}
			return p
		}()},
		{"unknown endpoint", func() webhook.HTTPPoster {
			p := newPoster(srv, srv.URL)
			p.Targets = mapTargets{}
			return p
		}()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.p.Post(context.Background(), delivery())
			if err == nil {
				t.Fatal("want error")
			}
			for _, leak := range []string{posterSecret, "ref:ep"} {
				if strings.Contains(err.Error(), leak) {
					t.Errorf("error %q leaks %q", err, leak)
				}
				if strings.Contains(logs.String(), leak) {
					t.Errorf("logs leak %q", leak)
				}
			}
		})
	}
}
