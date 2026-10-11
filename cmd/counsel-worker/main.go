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
	cfg, prod, err := parseFlags(os.Args[1:])
	if err != nil {
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := boot.Run(ctx, cfg, legalboot.App(legalboot.Options{Production: &prod})); err != nil {
		slog.Error("counsel-worker stopped", "err", err)
		os.Exit(1)
	}
}

// parseFlags turns command-line arguments into a boot.Config and the production config (the
// seal key the gate effects run with). It holds no other logic: legalboot refuses to boot
// outside dev without a seal key. The key comes only from the environment or a mounted file.
func parseFlags(args []string) (boot.Config, legalboot.Production, error) {
	fs := flag.NewFlagSet("counsel-worker", flag.ContinueOnError)
	cfg := boot.Config{Role: boot.RoleWorker}
	fs.StringVar(&cfg.DatabaseURL, "db", os.Getenv("COUNSEL_DATABASE_URL"), "Postgres DSN (default $COUNSEL_DATABASE_URL)")
	flagList := fs.String("flags", "", "comma-separated feature flags to turn on")
	fs.StringVar(&cfg.Zone, "zone", zoneDefault(), "IANA business time zone (default $COUNSEL_ZONE, else "+defaultZone+")")
	fs.IntVar(&cfg.MaxWorkers, "max-workers", 0, "concurrent jobs on the default queue (0 means the kernel default)")
	prod := legalboot.Production{SealKeyBase64: legalboot.Secret(os.Getenv("COUNSEL_SEAL_KEY"))}
	fs.StringVar(&prod.SealKeyFile, "seal-key-file", os.Getenv("COUNSEL_SEAL_KEY_FILE"), "file holding the base64 trust seal key (default $COUNSEL_SEAL_KEY_FILE); $COUNSEL_SEAL_KEY wins")
	env := fs.String("env", envOr("COUNSEL_ENV", "production"), "environment: production or dev (default $COUNSEL_ENV, else production)")
	if err := fs.Parse(args); err != nil {
		return boot.Config{}, legalboot.Production{}, err
	}
	if *flagList != "" {
		cfg.Flags = strings.Split(*flagList, ",")
	}
	prod.Dev = *env == "dev"
	return cfg, prod, nil
}

// envOr is $key, else def.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
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
