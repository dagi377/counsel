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

	legalboot "github.com/dagi377/counsel/app/legal/boot"
	"github.com/dagi377/counsel/kernel/boot"
)

func main() {
	cfg, err := parseFlags(os.Args[1:])
	if err != nil {
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := boot.Run(ctx, cfg, legalboot.App(legalboot.Options{Production: &legalboot.Production{}})); err != nil {
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
	fs.StringVar(&cfg.Zone, "zone", zoneDefault(), "IANA business time zone (default $COUNSEL_ZONE, else "+defaultZone+")")
	fs.IntVar(&cfg.MaxWorkers, "max-workers", 0, "concurrent jobs on the default queue (0 means the kernel default)")
	if err := fs.Parse(args); err != nil {
		return boot.Config{}, err
	}
	if *flagList != "" {
		cfg.Flags = strings.Split(*flagList, ",")
	}
	return cfg, nil
}

// defaultZone is the business time zone when neither -zone nor $COUNSEL_ZONE names one.
const defaultZone = "America/Toronto"

// zoneDefault is $COUNSEL_ZONE, else defaultZone.
func zoneDefault() string {
	if z := os.Getenv("COUNSEL_ZONE"); z != "" {
		return z
	}
	return defaultZone
}
