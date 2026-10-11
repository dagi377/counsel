package login

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// Paths the service serves, outside the versioned API (they are browser redirects).
const (
	PathLogin    = "/auth/login"
	PathCallback = "/auth/callback"
	PathLogout   = "/auth/logout"
	PathDevLogin = "/auth/dev-login"
)

// Problem codes.
const (
	CodeUnauthenticated = "unauthenticated"
	CodeStepUpRequired  = "step_up_required"
	CodeNotFound        = "not_found"
)

// stateCookie carries one sign-in attempt's state, nonce, PKCE verifier and return path.
const stateCookie = "counsel_login"

// stateTTL bounds one sign-in attempt.
const stateTTL = 10 * time.Minute

// touchEvery throttles last_seen_at writes.
const touchEvery = time.Minute

// Cookie purposes for the signer.
const (
	purposeSession = "session"
	purposeState   = "state"
)

// Service signs members in and authenticates their requests.
type Service struct {
	cfg      Config
	store    Store
	users    Users
	problems server.ProblemWriter
	sign     signer
	oidc     *provider
	now      func() time.Time
}

// Deps are the Service's collaborators. HTTP is the client for the identity provider; nil
// uses a 10 s timeout client. Now is the clock; nil uses time.Now.
type Deps struct {
	Store    Store
	Users    Users
	Problems server.ProblemWriter
	HTTP     *http.Client
	Now      func() time.Time
}

// New validates cfg and returns the Service.
func New(cfg Config, d Deps) (*Service, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if d.Store == nil || d.Users == nil {
		return nil, fmt.Errorf("session store and users are required: %w", ErrConfig)
	}
	key := cfg.CookieKey
	if len(key) == 0 {
		key = make([]byte, MinKeyBytes)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("cookie key: %w", err)
		}
	}
	s := &Service{cfg: cfg, store: d.Store, users: d.Users, problems: d.Problems, sign: signer{key: key}, now: d.Now}
	if s.problems == nil {
		s.problems = &server.Problems{}
	}
	if s.now == nil {
		s.now = time.Now
	}
	if cfg.OIDC() {
		s.oidc = &provider{cfg: cfg, http: d.HTTP, now: s.now}
	}
	if cfg.DevLogin {
		slog.Warn("dev login is on: POST /auth/dev-login signs in the seeded user without a password")
	}
	return s, nil
}

// Routes mounts the sign-in routes. The dev login route is always mounted and answers 404
// unless the dev login is on, so turning it off is visible to a caller.
func (s *Service) Routes(r chi.Router) {
	r.Get(PathLogin, s.login)
	r.Get(PathCallback, s.callback)
	r.Post(PathLogout, s.logout)
	r.Post(PathDevLogin, s.devLogin)
}

// Authenticate returns the member a request's session cookie names. It is false for a
// missing, forged, signed-out, idle, expired or revoked session, or a user who is no longer
// active; the routes then answer 401.
func (s *Service) Authenticate(r *http.Request) (registry.Principal, bool) {
	sess, _, err := s.session(r)
	if err != nil {
		return registry.Principal{}, false
	}
	return registry.Principal{Kind: registry.PrincipalUser, UserID: sess.UserID}, true
}

// StepUp returns when the request's session last passed multi-factor authentication; zero
// means never or no live session (ARC-I-017).
func (s *Service) StepUp(r *http.Request) time.Time {
	_, mfa, err := s.session(r)
	if err != nil {
		return time.Time{}
	}
	return mfa
}

// session reads and checks the session cookie.
// rule: ARC-I-036
func (s *Service) session(r *http.Request) (Session, time.Time, error) {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return Session{}, time.Time{}, ErrNoSession
	}
	f, ok := s.sign.open(purposeSession, c.Value, 2)
	if !ok {
		return Session{}, time.Time{}, ErrNoSession
	}
	id := f[0]
	ctx := r.Context()
	sess, err := s.store.Get(ctx, id)
	if err != nil {
		if !errors.Is(err, ErrNoSession) {
			slog.ErrorContext(ctx, "session lookup failed", "err", err)
		}
		return Session{}, time.Time{}, ErrNoSession
	}
	if sess.Ended {
		return Session{}, time.Time{}, ErrNoSession
	}
	now := s.now()
	reason := ""
	switch {
	case !sess.UserActive:
		reason = EndRevoked
	case !now.Before(sess.StartedAt.Add(limit(sess.Lifetime, s.cfg.lifetime()))):
		reason = EndExpired
	case !now.Before(sess.LastSeenAt.Add(limit(sess.Idle, s.cfg.idle()))):
		reason = EndIdle
	}
	if reason != "" {
		if err := s.store.End(ctx, id, now, reason); err != nil {
			slog.ErrorContext(ctx, "session end failed", "err", err)
		}
		return Session{}, time.Time{}, ErrNoSession
	}
	if now.Sub(sess.LastSeenAt) >= touchEvery {
		if err := s.store.Touch(ctx, id, now); err != nil {
			slog.ErrorContext(ctx, "session touch failed", "err", err)
		}
	}
	var mfa time.Time
	if sec, err := strconv.ParseInt(f[1], 10, 64); err == nil && sec > 0 {
		mfa = time.Unix(sec, 0)
	}
	return sess, mfa, nil
}

// limit is the sign-in policy's limit when the firm set one, else the config limit.
func limit(policy, config time.Duration) time.Duration {
	if policy > 0 {
		return policy
	}
	return config
}

// login starts the OIDC flow. ?step_up=1 asks the provider for fresh multi-factor
// authentication; ?return_to= names a same-site path to land on afterwards.
func (s *Service) login(w http.ResponseWriter, r *http.Request) {
	if s.oidc == nil {
		s.fail(w, r, http.StatusNotFound, CodeNotFound, "Sign-in is not configured")
		return
	}
	state, nonce, verifier := randomToken(), randomToken(), randomToken()
	stepUp := r.URL.Query().Get("step_up") == "1"
	u, err := s.oidc.authURL(r.Context(), state, nonce, verifier, stepUp)
	if err != nil {
		slog.ErrorContext(r.Context(), "oidc sign-in start failed", "err", err)
		s.fail(w, r, http.StatusServiceUnavailable, "idp_unavailable", "Identity provider unavailable")
		return
	}
	exp := strconv.FormatInt(s.now().Add(stateTTL).Unix(), 10)
	s.setCookie(w, stateCookie, s.sign.seal(purposeState, state, nonce, verifier, flag(stepUp), safeReturn(r.URL.Query().Get("return_to")), exp), stateTTL)
	http.Redirect(w, r, u, http.StatusFound)
}

// callback finishes the OIDC flow and starts the session.
func (s *Service) callback(w http.ResponseWriter, r *http.Request) {
	if s.oidc == nil {
		s.fail(w, r, http.StatusNotFound, CodeNotFound, "Sign-in is not configured")
		return
	}
	s.clearCookie(w, stateCookie)
	c, err := r.Cookie(stateCookie)
	if err != nil {
		s.unauthenticated(w, r)
		return
	}
	f, ok := s.sign.open(purposeState, c.Value, 6)
	q := r.URL.Query()
	if !ok || q.Get("state") == "" || q.Get("state") != f[0] || q.Get("code") == "" || expired(f[5], s.now()) {
		s.unauthenticated(w, r)
		return
	}
	nonce, verifier, stepUp, returnTo := f[1], f[2], f[3] == "1", f[4]
	ctx := r.Context()
	id, err := s.oidc.exchange(ctx, q.Get("code"), verifier, nonce)
	if err != nil {
		slog.WarnContext(ctx, "oidc sign-in rejected", "err", err)
		s.unauthenticated(w, r)
		return
	}
	if !id.EmailVerified || id.Email == "" {
		s.unauthenticated(w, r)
		return
	}
	// rule: ARC-I-017
	// A step-up must come back with a second factor, or it proves nothing new.
	if stepUp && !id.MFA {
		w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_user_authentication"`)
		s.fail(w, r, http.StatusUnauthorized, CodeStepUpRequired, "Step-up authentication is required")
		return
	}
	var mfa time.Time
	if id.MFA {
		mfa = id.AuthTime
	}
	if err := s.start(ctx, w, r, id.Email, mfa); err != nil {
		if errors.Is(err, ErrNoUser) {
			s.unauthenticated(w, r)
			return
		}
		slog.ErrorContext(ctx, "session start failed", "err", err)
		s.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
		return
	}
	http.Redirect(w, r, returnTo, http.StatusFound)
}

// start maps email to an active user and sets a session cookie. A step-up by the user who
// already holds a live session keeps that session and refreshes only its MFA time.
func (s *Service) start(ctx context.Context, w http.ResponseWriter, r *http.Request, email string, mfa time.Time) error {
	userID, err := s.users.ActiveByEmail(ctx, email)
	if err != nil {
		return err
	}
	if cur, _, err := s.session(r); err == nil && cur.UserID == userID && !mfa.IsZero() {
		s.setSession(w, cur.ID, mfa)
		return nil
	}
	id, err := s.store.Start(ctx, userID, clientIP(r), deviceOf(r), s.now())
	if err != nil {
		return err
	}
	slog.InfoContext(ctx, "member signed in", "user", userID, "session", id, "mfa", !mfa.IsZero())
	s.setSession(w, id, mfa)
	return nil
}

// logout ends the session and clears the cookie.
func (s *Service) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(CookieName); err == nil {
		if f, ok := s.sign.open(purposeSession, c.Value, 2); ok {
			if err := s.store.End(r.Context(), f[0], s.now(), EndLogout); err != nil {
				slog.ErrorContext(r.Context(), "session end failed", "err", err)
			}
		}
	}
	s.clearCookie(w, CookieName)
	w.WriteHeader(http.StatusNoContent)
}

// devLogin signs in the configured seeded user. It answers 404 unless the dev login is on,
// and Config.Validate refuses it outside a dev environment.
func (s *Service) devLogin(w http.ResponseWriter, r *http.Request) {
	// rule: ARC-I-036
	if !s.cfg.DevLogin || !s.cfg.Dev {
		s.fail(w, r, http.StatusNotFound, CodeNotFound, "No such resource")
		return
	}
	if err := s.start(r.Context(), w, r, s.cfg.DevUserEmail, s.now()); err != nil {
		if errors.Is(err, ErrNoUser) {
			s.fail(w, r, http.StatusUnauthorized, CodeUnauthenticated, "Dev user is not an active member")
			return
		}
		slog.ErrorContext(r.Context(), "dev session start failed", "err", err)
		s.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) setSession(w http.ResponseWriter, id string, mfa time.Time) {
	m := "0"
	if !mfa.IsZero() {
		m = strconv.FormatInt(mfa.Unix(), 10)
	}
	s.setCookie(w, CookieName, s.sign.seal(purposeSession, id, m), s.cfg.lifetime())
}

func (s *Service) setCookie(w http.ResponseWriter, name, value string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: int(ttl.Seconds()),
		HttpOnly: true, Secure: !s.cfg.InsecureCookie, SameSite: http.SameSiteLaxMode})
}

func (s *Service) clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: !s.cfg.InsecureCookie, SameSite: http.SameSiteLaxMode})
}

func (s *Service) unauthenticated(w http.ResponseWriter, r *http.Request) {
	s.fail(w, r, http.StatusUnauthorized, CodeUnauthenticated, "Authentication required")
}

func (s *Service) fail(w http.ResponseWriter, r *http.Request, status int, code, title string) {
	s.problems.WriteProblem(w, r, server.NewProblem(status, code, title))
}

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("login: crypto/rand: %v", err))
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func flag(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func expired(unix string, now time.Time) bool {
	sec, err := strconv.ParseInt(unix, 10, 64)
	return err != nil || !now.Before(time.Unix(sec, 0))
}

// safeReturn keeps only same-site absolute paths, so the callback is no open redirect.
func safeReturn(p string) string {
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.ContainsAny(p, "\\|\r\n") {
		return "/"
	}
	return p
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || net.ParseIP(host) == nil {
		return ""
	}
	return host
}

// deviceOf is the user agent, capped; it is device metadata, never a credential.
func deviceOf(r *http.Request) string {
	ua := r.UserAgent()
	if len(ua) > 200 {
		ua = ua[:200]
	}
	return ua
}
