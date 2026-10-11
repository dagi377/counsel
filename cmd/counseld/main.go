// Command counseld serves the Counsel HTTP API.
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
	cfg, login, err := parseFlags(os.Args[1:])
	if err != nil {
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Modules wire their own tables and routes through boot; main never touches a store.
	// rule: ARC-I-040
	if err := boot.Run(ctx, cfg, legalboot.App(legalboot.Options{Production: &legalboot.Production{Login: &login}})); err != nil {
		slog.Error("counseld stopped", "err", err)
		os.Exit(1)
	}
}

// parseFlags turns command-line arguments into a boot.Config and the member sign-in config.
// It holds no other logic: legalboot validates the sign-in config, and refuses the dev login
// unless -env=dev. Secrets come only from the environment, never from flags.
func parseFlags(args []string) (boot.Config, legalboot.Login, error) {
	fs := flag.NewFlagSet("counseld", flag.ContinueOnError)
	cfg := boot.Config{Role: boot.RoleServer}
	fs.StringVar(&cfg.DatabaseURL, "db", os.Getenv("COUNSEL_DATABASE_URL"), "Postgres DSN (default $COUNSEL_DATABASE_URL)")
	flagList := fs.String("flags", "", "comma-separated feature flags to turn on")
	fs.StringVar(&cfg.Zone, "zone", zoneDefault(), "IANA business time zone (default $COUNSEL_ZONE, else "+defaultZone+")")
	fs.StringVar(&cfg.Addr, "addr", ":8080", "HTTP listen address")
	login := legalboot.Login{ClientSecret: os.Getenv("COUNSEL_OIDC_CLIENT_SECRET"), CookieKey: []byte(os.Getenv("COUNSEL_SESSION_KEY"))}
	env := fs.String("env", envOr("COUNSEL_ENV", "production"), "environment: production or dev (default $COUNSEL_ENV, else production)")
	fs.StringVar(&login.Issuer, "oidc-issuer", os.Getenv("COUNSEL_OIDC_ISSUER"), "OIDC issuer URL (default $COUNSEL_OIDC_ISSUER)")
	fs.StringVar(&login.ClientID, "oidc-client-id", os.Getenv("COUNSEL_OIDC_CLIENT_ID"), "OIDC client id (default $COUNSEL_OIDC_CLIENT_ID); the secret is $COUNSEL_OIDC_CLIENT_SECRET")
	fs.StringVar(&login.RedirectURL, "oidc-redirect-url", os.Getenv("COUNSEL_OIDC_REDIRECT_URL"), "OIDC callback URL, https://<host>/auth/callback (default $COUNSEL_OIDC_REDIRECT_URL)")
	fs.StringVar(&login.StepUpACR, "oidc-step-up-acr", os.Getenv("COUNSEL_OIDC_STEP_UP_ACR"), "acr_values sent on a step-up sign-in")
	fs.BoolVar(&login.DevLogin, "dev-login", false, "dev only: serve POST /auth/dev-login for the seeded user; refused unless -env=dev")
	fs.StringVar(&login.DevUserEmail, "dev-login-email", os.Getenv("COUNSEL_DEV_LOGIN_EMAIL"), "dev only: the seeded user the dev login signs in")
	if err := fs.Parse(args); err != nil {
		return boot.Config{}, legalboot.Login{}, err
	}
	if *flagList != "" {
		cfg.Flags = strings.Split(*flagList, ",")
	}
	login.Dev = *env == "dev"
	login.InsecureCookie = login.Dev
	return cfg, login, nil
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
