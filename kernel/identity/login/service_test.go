package login

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/kernel/registry"
)

const (
	devEmail = "dev@example.test"
	devUser  = "0190a000-0000-7000-8000-000000000001"
)

// memStore is an in-memory Store and Users.
type memStore struct {
	mu       sync.Mutex
	n        int
	sessions map[string]*Session
	reasons  map[string]string
	active   map[string]bool // userID -> active
	emails   map[string]string
	// lifetime and idle are the sign-in policy row's limits; zero is no row.
	lifetime, idle time.Duration
}

func newMemStore() *memStore {
	return &memStore{sessions: map[string]*Session{}, reasons: map[string]string{},
		active: map[string]bool{devUser: true}, emails: map[string]string{devEmail: devUser}}
}

func (m *memStore) Start(_ context.Context, userID, _, _ string, now time.Time) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.n++
	id := fmt.Sprintf("sess-%d", m.n)
	m.sessions[id] = &Session{ID: id, UserID: userID, StartedAt: now, LastSeenAt: now}
	return id, nil
}

func (m *memStore) Get(_ context.Context, id string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return Session{}, ErrNoSession
	}
	out := *s
	out.UserActive = m.active[s.UserID]
	out.Lifetime, out.Idle = m.lifetime, m.idle
	return out, nil
}

func (m *memStore) Touch(_ context.Context, id string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok && !s.Ended {
		s.LastSeenAt = now
	}
	return nil
}

func (m *memStore) End(_ context.Context, id string, _ time.Time, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok && !s.Ended {
		s.Ended = true
		m.reasons[id] = reason
	}
	return nil
}

func (m *memStore) ActiveByEmail(_ context.Context, email string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id, ok := m.emails[strings.ToLower(email)]; ok && m.active[id] {
		return id, nil
	}
	return "", ErrNoUser
}

type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *testClock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func devConfig() Config {
	return Config{Dev: true, DevLogin: true, DevUserEmail: devEmail, InsecureCookie: true,
		CookieKey: []byte(strings.Repeat("k", MinKeyBytes))}
}

// harness is a Service plus a protected route that answers 401 without a principal, the
// way every module route does.
type harness struct {
	svc   *Service
	store *memStore
	clock *testClock
	h     http.Handler
}

func newHarness(t *testing.T, cfg Config, client *http.Client) *harness {
	t.Helper()
	st := newMemStore()
	clk := &testClock{t: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)}
	svc, err := New(cfg, Deps{Store: st, Users: st, HTTP: client, Now: clk.now})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r := chi.NewRouter()
	svc.Routes(r)
	r.Get("/v1/items", func(w http.ResponseWriter, r *http.Request) {
		p, ok := svc.Authenticate(r)
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if p.Kind != registry.PrincipalUser {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(p.UserID))
	})
	return &harness{svc: svc, store: st, clock: clk, h: r}
}

func (h *harness) do(method, path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.h.ServeHTTP(rec, req)
	return rec
}

func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == CookieName && c.Value != "" {
			return c
		}
	}
	t.Fatalf("no %s cookie (status %d, body %s)", CookieName, rec.Code, rec.Body)
	return nil
}

func (h *harness) devSession(t *testing.T) *http.Cookie {
	t.Helper()
	rec := h.do(http.MethodPost, PathDevLogin)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("dev login = %d %s", rec.Code, rec.Body)
	}
	return sessionCookie(t, rec)
}

// TestARC_I_036_SessionAuth covers the session cookie on a protected route.
func TestARC_I_036_SessionAuth(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(t *testing.T, h *harness) []*http.Cookie
		status int
	}{
		{"no cookie is 401", func(*testing.T, *harness) []*http.Cookie { return nil }, http.StatusUnauthorized},
		{"valid session reaches the route", func(t *testing.T, h *harness) []*http.Cookie {
			return []*http.Cookie{h.devSession(t)}
		}, http.StatusOK},
		{"active session survives within idle window", func(t *testing.T, h *harness) []*http.Cookie {
			c := h.devSession(t)
			for range 4 {
				h.clock.add(20 * time.Minute)
				if rec := h.do(http.MethodGet, "/v1/items", c); rec.Code != http.StatusOK {
					t.Fatalf("touch = %d", rec.Code)
				}
			}
			return []*http.Cookie{c}
		}, http.StatusOK},
		{"forged cookie is 401", func(t *testing.T, h *harness) []*http.Cookie {
			h.devSession(t)
			forged := signer{key: []byte(strings.Repeat("x", MinKeyBytes))}.seal(purposeSession, "sess-1", "0")
			return []*http.Cookie{{Name: CookieName, Value: forged}}
		}, http.StatusUnauthorized},
		{"state cookie value is not a session", func(t *testing.T, h *harness) []*http.Cookie {
			h.devSession(t)
			return []*http.Cookie{{Name: CookieName, Value: h.svc.sign.seal(purposeState, "sess-1", "0")}}
		}, http.StatusUnauthorized},
		{"idle session is 401", func(t *testing.T, h *harness) []*http.Cookie {
			c := h.devSession(t)
			h.clock.add(DefaultIdle)
			return []*http.Cookie{c}
		}, http.StatusUnauthorized},
		{"expired session is 401", func(t *testing.T, h *harness) []*http.Cookie {
			c := h.devSession(t)
			for elapsed := time.Duration(0); elapsed < DefaultLifetime; elapsed += 20 * time.Minute {
				h.clock.add(20 * time.Minute)
				h.do(http.MethodGet, "/v1/items", c)
			}
			return []*http.Cookie{c}
		}, http.StatusUnauthorized},
		{"revoked session is 401", func(t *testing.T, h *harness) []*http.Cookie {
			c := h.devSession(t)
			_ = h.store.End(context.Background(), "sess-1", h.clock.now(), EndRevoked)
			return []*http.Cookie{c}
		}, http.StatusUnauthorized},
		{"offboarded user is 401", func(t *testing.T, h *harness) []*http.Cookie {
			c := h.devSession(t)
			h.store.active[devUser] = false
			return []*http.Cookie{c}
		}, http.StatusUnauthorized},
		{"signed-out session is 401", func(t *testing.T, h *harness) []*http.Cookie {
			c := h.devSession(t)
			if rec := h.do(http.MethodPost, PathLogout, c); rec.Code != http.StatusNoContent {
				t.Fatalf("logout = %d", rec.Code)
			}
			return []*http.Cookie{c}
		}, http.StatusUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, devConfig(), nil)
			rec := h.do(http.MethodGet, "/v1/items", tc.setup(t, h)...)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
			if tc.status == http.StatusOK && rec.Body.String() != devUser {
				t.Errorf("principal = %q, want %q", rec.Body, devUser)
			}
		})
	}
}

// TestARC_I_036_SignInPolicyLimits: the firm's core.sign_in_policy limits bound a session;
// without a policy row the config limits (the DDL defaults) apply.
func TestARC_I_036_SignInPolicyLimits(t *testing.T) {
	tests := []struct {
		name           string
		lifetime, idle time.Duration
		advance        time.Duration
		status         int
		reason         string
	}{
		{"no policy: default idle still live", 0, 0, DefaultIdle - time.Minute, http.StatusOK, ""},
		{"policy idle 10 min ends the session", 0, 10 * time.Minute, 10 * time.Minute, http.StatusUnauthorized, EndIdle},
		{"policy idle 60 min keeps it past the default", 0, 60 * time.Minute, 45 * time.Minute, http.StatusOK, ""},
		{"policy lifetime 20 min expires it", 20 * time.Minute, 60 * time.Minute, 20 * time.Minute, http.StatusUnauthorized, EndExpired},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, devConfig(), nil)
			h.store.lifetime, h.store.idle = tc.lifetime, tc.idle
			c := h.devSession(t)
			h.clock.add(tc.advance)
			if rec := h.do(http.MethodGet, "/v1/items", c); rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
			if got := h.store.reasons["sess-1"]; got != tc.reason {
				t.Errorf("end reason = %q, want %q", got, tc.reason)
			}
		})
	}
}

func TestEndReasons(t *testing.T) {
	tests := []struct {
		name    string
		advance time.Duration
		revoke  bool
		want    string
	}{
		{"idle", DefaultIdle + time.Second, false, EndIdle},
		{"user offboarded", 0, true, EndRevoked},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, devConfig(), nil)
			c := h.devSession(t)
			h.clock.add(tc.advance)
			if tc.revoke {
				h.store.active[devUser] = false
			}
			h.do(http.MethodGet, "/v1/items", c)
			if got := h.store.reasons["sess-1"]; got != tc.want {
				t.Errorf("end reason = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestARC_I_036_DevLoginGuard: the dev login is off by default and refused in production.
func TestARC_I_036_DevLoginGuard(t *testing.T) {
	key := []byte(strings.Repeat("k", MinKeyBytes))
	tests := []struct {
		name    string
		cfg     Config
		wantErr error
		status  int // POST /auth/dev-login when New succeeds
	}{
		{"flag off in dev answers 404", Config{Dev: true, DevUserEmail: devEmail, CookieKey: key}, nil, http.StatusNotFound},
		{"flag off in production answers 404", Config{CookieKey: key, Issuer: "https://idp.example.test",
			ClientID: "c", ClientSecret: "s", RedirectURL: "https://cell.example.test/auth/callback"}, nil, http.StatusNotFound},
		{"flag on in production is refused at boot", Config{DevLogin: true, DevUserEmail: devEmail, CookieKey: key}, ErrDevLoginInProduction, 0},
		{"flag on in dev signs in", devConfig(), nil, http.StatusNoContent},
		{"flag on for an unknown user is 401", Config{Dev: true, DevLogin: true, DevUserEmail: "nobody@example.test"}, nil, http.StatusUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := newMemStore()
			svc, err := New(tc.cfg, Deps{Store: st, Users: st})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("New err = %v, want %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			r := chi.NewRouter()
			svc.Routes(r)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, PathDevLogin, nil))
			if rec.Code != tc.status {
				t.Fatalf("dev login = %d, want %d", rec.Code, tc.status)
			}
			if tc.status != http.StatusNoContent && len(st.sessions) != 0 {
				t.Errorf("a refused dev login started a session")
			}
		})
	}
}

func TestConfigValidate(t *testing.T) {
	key := []byte(strings.Repeat("k", MinKeyBytes))
	oidc := func(mod func(*Config)) Config {
		c := Config{CookieKey: key, Issuer: "https://idp.example.test", ClientID: "c", ClientSecret: "s",
			RedirectURL: "https://cell.example.test/auth/callback"}
		mod(&c)
		return c
	}
	tests := []struct {
		name    string
		cfg     Config
		wantErr error
	}{
		{"production oidc", oidc(func(*Config) {}), nil},
		{"short key", oidc(func(c *Config) { c.CookieKey = key[:8] }), ErrConfig},
		{"no secret", oidc(func(c *Config) { c.ClientSecret = "" }), ErrConfig},
		{"http issuer in production", oidc(func(c *Config) { c.Issuer = "http://idp.example.test" }), ErrConfig},
		{"insecure cookie in production", oidc(func(c *Config) { c.InsecureCookie = true }), ErrConfig},
		{"dev login without email", Config{Dev: true, DevLogin: true}, ErrConfig},
		{"dev login in production", oidc(func(c *Config) { c.DevLogin = true; c.DevUserEmail = devEmail }), ErrDevLoginInProduction},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Validate = %v, want %v", err, tc.wantErr)
			}
			if err != nil && strings.Contains(err.Error(), "s\"") {
				t.Errorf("error formats a secret: %v", err)
			}
		})
	}
}

// idp is a fake OIDC provider: discovery, JWKS and a token endpoint that issues one ID
// token per code with the claims the test set.
type idp struct {
	srv    *httptest.Server
	key    *rsa.PrivateKey
	mu     sync.Mutex
	claims map[string]any // overrides; "nonce" is filled from the authorize request
	codes  map[string]string
	badSig bool
}

func newIDP(t *testing.T) *idp {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &idp{key: key, codes: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"issuer": p.srv.URL, "authorization_endpoint": p.srv.URL + "/authorize",
			"token_endpoint": p.srv.URL + "/token", "jwks_uri": p.srv.URL + "/jwks"})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		enc := base64.RawURLEncoding
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{"kty": "RSA", "kid": "k1",
			"n": enc.EncodeToString(key.N.Bytes()), "e": enc.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "client" || pass != "secret" || r.FormValue("code_verifier") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		p.mu.Lock()
		nonce, ok := p.codes[r.FormValue("code")]
		p.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id_token": p.token(t, nonce), "access_token": "at"})
	})
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	return p
}

func (p *idp) token(t *testing.T, nonce string) string {
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	c := map[string]any{"iss": p.srv.URL, "sub": "s1", "aud": "client", "exp": now.Add(time.Hour).Unix(),
		"iat": now.Unix(), "auth_time": now.Unix(), "nonce": nonce, "email": devEmail, "email_verified": true,
		"amr": []string{"pwd", "otp"}}
	p.mu.Lock()
	for k, v := range p.claims {
		c[k] = v
	}
	bad := p.badSig
	p.mu.Unlock()
	enc := base64.RawURLEncoding
	hdr, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": "k1", "typ": "JWT"})
	body, _ := json.Marshal(c)
	signing := enc.EncodeToString(hdr) + "." + enc.EncodeToString(body)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, p.key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	if bad {
		sig[0] ^= 0xff
	}
	return signing + "." + enc.EncodeToString(sig)
}

// signIn runs the redirect flow and returns the callback response.
func (p *idp) signIn(t *testing.T, h *harness, loginPath string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	rec := h.do(http.MethodGet, loginPath, cookies...)
	if rec.Code != http.StatusFound {
		t.Fatalf("login = %d %s", rec.Code, rec.Body)
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	q := loc.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("client_id") != "client" {
		t.Fatalf("authorize query = %v", q)
	}
	var state *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == stateCookie {
			state = c
		}
	}
	p.mu.Lock()
	p.codes["code-1"] = q.Get("nonce")
	p.mu.Unlock()
	return h.do(http.MethodGet, PathCallback+"?code=code-1&state="+url.QueryEscape(q.Get("state")), append(cookies, state)...)
}

// TestARC_I_017_OIDCSignIn covers the OIDC sign-in, its token checks and step-up.
func TestARC_I_017_OIDCSignIn(t *testing.T) {
	tests := []struct {
		name     string
		claims   map[string]any
		badSig   bool
		stepUp   bool
		status   int
		wantMFA  bool
		wantCode string
	}{
		{"signs in with mfa", nil, false, false, http.StatusFound, true, ""},
		{"signs in without mfa", map[string]any{"amr": []string{"pwd"}}, false, false, http.StatusFound, false, ""},
		{"bad signature", nil, true, false, http.StatusUnauthorized, false, CodeUnauthenticated},
		{"wrong audience", map[string]any{"aud": "other"}, false, false, http.StatusUnauthorized, false, CodeUnauthenticated},
		{"wrong issuer", map[string]any{"iss": "https://evil.example.test"}, false, false, http.StatusUnauthorized, false, CodeUnauthenticated},
		{"expired token", map[string]any{"exp": int64(1)}, false, false, http.StatusUnauthorized, false, CodeUnauthenticated},
		{"nonce mismatch", map[string]any{"nonce": "other"}, false, false, http.StatusUnauthorized, false, CodeUnauthenticated},
		{"unverified email", map[string]any{"email_verified": false}, false, false, http.StatusUnauthorized, false, CodeUnauthenticated},
		{"unknown member", map[string]any{"email": "stranger@example.test"}, false, false, http.StatusUnauthorized, false, CodeUnauthenticated},
		{"step-up without mfa", map[string]any{"amr": []string{"pwd"}}, false, true, http.StatusUnauthorized, false, CodeStepUpRequired},
		{"step-up with mfa", nil, false, true, http.StatusFound, true, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newIDP(t)
			p.claims, p.badSig = tc.claims, tc.badSig
			cfg := Config{Dev: true, InsecureCookie: true, Issuer: p.srv.URL, ClientID: "client", ClientSecret: "secret",
				RedirectURL: "http://cell.example.test/auth/callback"}
			h := newHarness(t, cfg, p.srv.Client())
			path := PathLogin + "?return_to=/items"
			if tc.stepUp {
				path += "&step_up=1"
			}
			rec := p.signIn(t, h, path)
			if rec.Code != tc.status {
				t.Fatalf("callback = %d, want %d (%s)", rec.Code, tc.status, rec.Body)
			}
			if tc.wantCode != "" {
				if !strings.Contains(rec.Body.String(), `"`+tc.wantCode+`"`) {
					t.Errorf("body = %s, want code %s", rec.Body, tc.wantCode)
				}
				if strings.Contains(rec.Body.String(), "secret") {
					t.Errorf("problem leaks the client secret")
				}
				return
			}
			if got := rec.Header().Get("Location"); got != "/items" {
				t.Errorf("redirect = %q, want /items", got)
			}
			c := sessionCookie(t, rec)
			if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
				t.Errorf("cookie attributes = %+v", c)
			}
			if r := h.do(http.MethodGet, "/v1/items", c); r.Code != http.StatusOK {
				t.Fatalf("route = %d", r.Code)
			}
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.AddCookie(c)
			if mfa := h.svc.StepUp(req); mfa.IsZero() == tc.wantMFA {
				t.Errorf("StepUp = %v, want mfa %v", mfa, tc.wantMFA)
			}
		})
	}
}

func TestStepUpKeepsSession(t *testing.T) {
	p := newIDP(t)
	p.claims = map[string]any{"amr": []string{"pwd"}}
	cfg := Config{Dev: true, InsecureCookie: true, Issuer: p.srv.URL, ClientID: "client", ClientSecret: "secret",
		RedirectURL: "http://cell.example.test/auth/callback"}
	h := newHarness(t, cfg, p.srv.Client())
	first := sessionCookie(t, p.signIn(t, h, PathLogin))
	p.claims = nil
	second := sessionCookie(t, p.signIn(t, h, PathLogin+"?step_up=1", first))
	if len(h.store.sessions) != 1 {
		t.Fatalf("step-up started %d sessions, want 1", len(h.store.sessions))
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(second)
	if h.svc.StepUp(req).IsZero() {
		t.Error("step-up did not record mfa")
	}
}

func TestCallbackRejectsBadState(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"no state cookie", PathCallback + "?code=c&state=s"},
		{"no code", PathCallback + "?state=s"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newIDP(t)
			cfg := Config{Dev: true, Issuer: p.srv.URL, ClientID: "client", ClientSecret: "secret",
				RedirectURL: "http://cell.example.test/auth/callback"}
			h := newHarness(t, cfg, p.srv.Client())
			if rec := h.do(http.MethodGet, tc.path); rec.Code != http.StatusUnauthorized {
				t.Fatalf("callback = %d, want 401", rec.Code)
			}
		})
	}
}

func TestSafeReturn(t *testing.T) {
	tests := []struct{ in, want string }{
		{"/items", "/items"}, {"", "/"}, {"//evil.example", "/"}, {"https://evil.example", "/"}, {"/a|b", "/"},
	}
	for _, tc := range tests {
		if got := safeReturn(tc.in); got != tc.want {
			t.Errorf("safeReturn(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestARC_I_036_CookieMaxAgeFromPolicy: the session cookie lives as long as the firm's
// sign-in policy lifetime, or the config lifetime when the firm has no policy row.
func TestARC_I_036_CookieMaxAgeFromPolicy(t *testing.T) {
	tests := []struct {
		name     string
		lifetime time.Duration
		want     int
	}{
		{"no policy uses the config lifetime", 0, int(DefaultLifetime.Seconds())},
		{"policy lifetime 20 min", 20 * time.Minute, 1200},
		{"policy lifetime 24 h", 24 * time.Hour, 86400},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, devConfig(), nil)
			h.store.lifetime = tc.lifetime
			// rule: ARC-I-036
			if c := h.devSession(t); c.MaxAge != tc.want {
				t.Fatalf("Max-Age = %d, want %d", c.MaxAge, tc.want)
			}
		})
	}
}
