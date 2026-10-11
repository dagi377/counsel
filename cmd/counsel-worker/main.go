// Command counsel-worker runs background jobs.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/dagi377/counsel/kernel/boot"
)

func main() {
	cfg, err := parseFlags(os.Args[1:])
	if err != nil {
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := boot.Run(ctx, cfg); err != nil {
		slog.Error("counsel-worker stopped", "err", err)
		os.Exit(1)
	}
}

// parseFlags turns command-line arguments into a boot.Config. It holds no other logic.
func parseFlags(args []string) (boot.Config, error) {
	fs := flag.NewFlagSet("counsel-worker", flag.ContinueOnError)
	cfg := boot.Config{Role: boot.RoleWorker}
	fs.StringVar(&cfg.DatabaseURL, "db", os.Getenv("COUNSEL_DATABASE_URL"), "Postgres DSN (default $COUNSEL_DATABASE_URL)")
	flagList := fs.String("flags", "", "comma-separated feature flags to turn on")
	if err := fs.Parse(args); err != nil {
		return boot.Config{}, err
	}
	if *flagList != "" {
		cfg.Flags = strings.Split(*flagList, ",")
	}
	return cfg, nil
}
