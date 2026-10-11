// Command counsel-migrate applies schema migrations.
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
		slog.Error("counsel-migrate stopped", "err", err)
		os.Exit(1)
	}
}

// parseFlags turns command-line arguments into a boot.Config. It holds no other logic.
func parseFlags(args []string) (boot.Config, error) {
	fs := flag.NewFlagSet("counsel-migrate", flag.ContinueOnError)
	cfg := boot.Config{Role: boot.RoleMigrate}
	fs.StringVar(&cfg.DatabaseURL, "db", os.Getenv("COUNSEL_DATABASE_URL"), "Postgres DSN (default $COUNSEL_DATABASE_URL)")
	flagList := fs.String("flags", "", "comma-separated feature flags to turn on")
	// Expand-only by default; destructive contract steps need an explicit -mode contract.
	// rule: ARC-U-017
	mode := fs.String("mode", string(boot.MigrateExpand), "migration half to run: expand or contract")
	if err := fs.Parse(args); err != nil {
		return boot.Config{}, err
	}
	if *flagList != "" {
		cfg.Flags = strings.Split(*flagList, ",")
	}
	cfg.MigrateMode = boot.MigrateMode(*mode)
	return cfg, nil
}
