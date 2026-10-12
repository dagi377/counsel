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
// seal key the gate effects run with, and the model and firm account the agent feature runs
// use). It holds no other logic: legalboot refuses to boot
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
	// The agent_feature_run worker runs the agent features on this model, as counseld does.
	model := legalboot.ModelConfig{KeyRef: "COUNSEL_MODEL_KEY"}
	fs.StringVar(&model.Mode, "model-mode", os.Getenv("COUNSEL_MODEL_MODE"), "model provider mode: mock or live (default $COUNSEL_MODEL_MODE); empty leaves the agent not ready outside dev")
	fs.StringVar(&model.BaseURL, "model-base-url", os.Getenv("COUNSEL_MODEL_BASE_URL"), "model provider base URL (default $COUNSEL_MODEL_BASE_URL); the key is $COUNSEL_MODEL_KEY")
	fs.StringVar(&model.Protocol, "model-protocol", envOr("COUNSEL_MODEL_PROTOCOL", "messages"), "model provider protocol: messages, chat or responses")
	modelID := fs.String("model", os.Getenv("COUNSEL_MODEL"), "provider model id the agent runs on (default $COUNSEL_MODEL)")
	fs.StringVar(&prod.FirmAccount, "firm-account", os.Getenv("COUNSEL_FIRM_ACCOUNT"), "payee id of the firm general account (default $COUNSEL_FIRM_ACCOUNT)")
	if err := fs.Parse(args); err != nil {
		return boot.Config{}, legalboot.Production{}, err
	}
	if model.Mode != "" {
		if *modelID != "" {
			model.Models = map[string]string{"default": *modelID}
		}
		prod.Model = &model
		prod.ModelEnv = *env
		// The key comes only from the environment; it is never a flag and never logged.
		prod.ModelKey = func(_ context.Context, ref string) (string, error) { return os.Getenv(ref), nil }
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
