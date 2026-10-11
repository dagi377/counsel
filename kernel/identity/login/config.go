// Package login signs members in and keeps their browser sessions (ARC-I-017, -036). A
// member signs in at the firm's OIDC identity provider (authorization code flow with PKCE);
// the server then starts a core.user_session row and sets the counsel_session cookie, an
// HMAC-signed handle on that row. Every request re-reads the row, so signing out, idle
// timeout, lifetime expiry, revocation and offboarding take effect on the next request.
//
// A dev-only login, behind an explicit flag that is off by default and refused outside a
// dev environment, signs in one seeded user so the app can be run without an identity
// provider.
package login

import (
	"errors"
	"fmt"
	"net/url"
	"time"
)

// CookieName is the session cookie the OpenAPI "session" security scheme names.
const CookieName = "counsel_session"

// Session limits used when Config leaves them zero. They are the core.sign_in_policy
// column defaults (session_lifetime_min 720, idle_timeout_min 30).
const (
	DefaultLifetime = 720 * time.Minute
	DefaultIdle     = 30 * time.Minute
)

// MinKeyBytes is the shortest cookie signing key a production config accepts.
const MinKeyBytes = 32

// Sentinel errors.
var (
	// ErrConfig means the sign-in configuration is incomplete or unsafe.
	ErrConfig = errors.New("login: bad config")
	// ErrDevLoginInProduction means the dev login was turned on outside a dev environment.
	ErrDevLoginInProduction = errors.New("login: dev login is refused outside a dev environment")
	// ErrNoSession means the request carries no live session.
	ErrNoSession = errors.New("login: no session")
	// ErrNoUser means the verified identity maps to no active member.
	ErrNoUser = errors.New("login: no active user for identity")
)

// Config configures sign-in. Secret fields are never logged or formatted into errors.
type Config struct {
	// Issuer is the OIDC issuer URL; its discovery document names the endpoints.
	Issuer string
	// ClientID and ClientSecret are the confidential client registered at the issuer.
	ClientID     string
	ClientSecret string
	// RedirectURL is this server's callback, https://<cell host>/auth/callback.
	RedirectURL string
	// StepUpACR, when set, is sent as acr_values on a step-up sign-in.
	StepUpACR string
	// CookieKey signs session and sign-in state cookies. A dev config without one gets a
	// random key per process.
	CookieKey []byte
	// InsecureCookie drops the Secure attribute; only a dev config may set it.
	InsecureCookie bool
	// Lifetime and Idle bound a session when the firm has no core.sign_in_policy row (a
	// policy row wins); zero uses DefaultLifetime and DefaultIdle.
	Lifetime time.Duration
	Idle     time.Duration

	// Dev marks a local or test environment. Production configs leave it false.
	Dev bool
	// DevLogin turns on POST /auth/dev-login. It is refused unless Dev is set.
	DevLogin bool
	// DevUserEmail is the seeded active user the dev login signs in.
	DevUserEmail string
}

// OIDC reports whether an identity provider is configured.
func (c Config) OIDC() bool { return c.Issuer != "" }

// Enabled reports whether any sign-in path is configured.
func (c Config) Enabled() bool { return c.OIDC() || c.DevLogin }

// Validate checks the config. It names fields, never their values.
func (c Config) Validate() error {
	// rule: ARC-I-036
	// The dev login signs anyone in as the seeded user, so only a dev environment may have it.
	if c.DevLogin && !c.Dev {
		return ErrDevLoginInProduction
	}
	if c.DevLogin && c.DevUserEmail == "" {
		return fmt.Errorf("dev login needs a user email: %w", ErrConfig)
	}
	if !c.Dev {
		if len(c.CookieKey) < MinKeyBytes {
			return fmt.Errorf("cookie key shorter than %d bytes: %w", MinKeyBytes, ErrConfig)
		}
		if c.InsecureCookie {
			return fmt.Errorf("insecure cookie outside a dev environment: %w", ErrConfig)
		}
	}
	if c.OIDC() {
		if c.ClientID == "" || c.ClientSecret == "" || c.RedirectURL == "" {
			return fmt.Errorf("oidc needs client id, client secret and redirect url: %w", ErrConfig)
		}
		for name, raw := range map[string]string{"issuer": c.Issuer, "redirect url": c.RedirectURL} {
			u, err := url.Parse(raw)
			if err != nil || u.Host == "" || (u.Scheme != "https" && !(c.Dev && u.Scheme == "http")) {
				return fmt.Errorf("oidc %s must be an absolute https url: %w", name, ErrConfig)
			}
		}
	}
	if c.Lifetime < 0 || c.Idle < 0 {
		return fmt.Errorf("negative session limit: %w", ErrConfig)
	}
	return nil
}

func (c Config) lifetime() time.Duration {
	if c.Lifetime > 0 {
		return c.Lifetime
	}
	return DefaultLifetime
}

func (c Config) idle() time.Duration {
	if c.Idle > 0 {
		return c.Idle
	}
	return DefaultIdle
}
