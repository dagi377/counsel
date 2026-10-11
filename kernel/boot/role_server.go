package boot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Server role timeouts.
const (
	readHeaderTimeout = 10 * time.Second
	shutdownTimeout   = 10 * time.Second
)

// runServer serves k.Server.Handler() on cfg.Addr until ctx is cancelled, then shuts down
// gracefully. It returns nil on a clean shutdown.
func runServer(ctx context.Context, cfg Config, k *Kernel) error {
	if k == nil || k.Server == nil {
		return fmt.Errorf("server role: no server: %w", ErrConfig)
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", cfg.Addr)
	if err != nil {
		// The listen error names only the address; cfg is never formatted, so the DSN stays out.
		return fmt.Errorf("server role: listen on %q: %w", cfg.Addr, err)
	}
	return serveOn(ctx, ln, k)
}

// serveOn serves k.Server.Handler() on ln until ctx is cancelled, then waits up to
// shutdownTimeout for in-flight requests. It owns ln and closes it.
func serveOn(ctx context.Context, ln net.Listener, k *Kernel) error {
	if k == nil || k.Server == nil {
		_ = ln.Close()
		return fmt.Errorf("server role: no server: %w", ErrConfig)
	}
	srv := newHTTPServer(ctx, k.Server.Handler())
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	slog.InfoContext(ctx, "server listening", "addr", ln.Addr().String())

	select {
	case err := <-errc:
		return fmt.Errorf("server role: serve: %w", err)
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		return fmt.Errorf("server role: shutdown: %w", err)
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("server role: serve: %w", err)
	}
	slog.InfoContext(ctx, "server stopped")
	return nil
}

// newHTTPServer returns the role's http.Server for h. Request contexts keep ctx's values but
// not its cancellation, so shutdown drains in-flight requests instead of cancelling them.
func newHTTPServer(ctx context.Context, h http.Handler) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
		BaseContext:       func(net.Listener) context.Context { return context.WithoutCancel(ctx) },
	}
}
