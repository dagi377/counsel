package boot

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/kernel/server/serverfake"
)

const secretDSN = "postgres://counsel:s3cr3t-pw@db.internal:5432/counsel"

func pingKernel(t *testing.T) *Kernel {
	t.Helper()
	srv := serverfake.New()
	if err := srv.Mount(func(r chi.Router) {
		r.Get("/v1/ping", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	}); err != nil {
		t.Fatal(err)
	}
	return &Kernel{Server: srv}
}

// captureLogs routes slog.Default to a buffer for the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestServeOnServesAndShutsDown(t *testing.T) {
	logs := captureLogs(t)
	tests := []struct {
		name, path string
		want       int
	}{
		{"mounted route", "/v1/ping", http.StatusOK},
		{"unknown route", "/v1/nope", http.StatusNotFound},
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serveOn(ctx, ln, pingKernel(t)) }()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Get("http://" + ln.Addr().String() + tc.path)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Errorf("GET %s = %d, want %d", tc.path, resp.StatusCode, tc.want)
			}
		})
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serveOn after cancel = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("serveOn did not return within 1s of cancel")
	}
	if strings.Contains(logs.String(), "s3cr3t") {
		t.Errorf("logs carry the database URL: %s", logs)
	}
}

func TestServerTimeouts(t *testing.T) {
	srv := newHTTPServer(context.Background(), http.NotFoundHandler())
	if srv.ReadHeaderTimeout <= 0 {
		t.Errorf("ReadHeaderTimeout = %v, want > 0", srv.ReadHeaderTimeout)
	}
}

func TestRunServer(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	tests := []struct {
		name    string
		addr    string
		kernel  *Kernel
		wantErr error
		wantMsg string
	}{
		{"address in use", busy.Addr().String(), &Kernel{Server: serverfake.New()}, nil, "listen"},
		{"malformed address", "not an address", &Kernel{Server: serverfake.New()}, nil, "listen"},
		{"no server", "127.0.0.1:0", &Kernel{}, ErrConfig, "no server"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureLogs(t)
			cfg := Config{Role: RoleServer, Addr: tc.addr, DatabaseURL: secretDSN}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err := runServer(ctx, cfg, tc.kernel)
			if err == nil {
				t.Fatal("runServer = nil, want error")
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("runServer = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil && errors.Unwrap(err) == nil {
				t.Errorf("runServer = %v, want a wrapped error", err)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("runServer = %v, want it to mention %q", err, tc.wantMsg)
			}
			if out := err.Error() + logs.String(); strings.Contains(out, "s3cr3t") || strings.Contains(out, "db.internal") {
				t.Errorf("database URL leaked: %s", out)
			}
		})
	}
}
