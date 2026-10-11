package login

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// ErrToken means the provider's token response or ID token did not verify.
var ErrToken = errors.New("login: id token rejected")

// maxBody caps provider responses.
const maxBody = 1 << 20

// clockSkew is the leeway on exp and iat.
const clockSkew = 2 * time.Minute

// mfaMethods are the RFC 8176 amr values that show a second factor: totp, webauthn and sms
// are the sign-in policy's methods.
var mfaMethods = []string{"mfa", "otp", "hwk", "swk", "sms"}

// Identity is what a verified ID token says about the member.
type Identity struct {
	Subject       string
	Email         string
	EmailVerified bool
	AuthTime      time.Time
	// MFA is true when amr names a second factor.
	MFA bool
}

// provider is an OIDC relying party built on the standard library: discovery, the
// authorization code flow with PKCE (S256), and RS256 ID-token verification against the
// issuer's JWKS.
type provider struct {
	cfg  Config
	http *http.Client
	now  func() time.Time

	mu   sync.Mutex
	meta *discovery
	keys map[string]*rsa.PublicKey
}

type discovery struct {
	Issuer   string `json:"issuer"`
	AuthURL  string `json:"authorization_endpoint"`
	TokenURL string `json:"token_endpoint"`
	JWKSURL  string `json:"jwks_uri"`
}

func (p *provider) discover(ctx context.Context) (*discovery, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.meta != nil {
		return p.meta, nil
	}
	var d discovery
	if err := p.getJSON(ctx, strings.TrimSuffix(p.cfg.Issuer, "/")+"/.well-known/openid-configuration", &d); err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	if d.Issuer != p.cfg.Issuer || d.AuthURL == "" || d.TokenURL == "" || d.JWKSURL == "" {
		return nil, fmt.Errorf("oidc discovery: issuer mismatch or missing endpoint: %w", ErrToken)
	}
	p.meta = &d
	return p.meta, nil
}

// authURL is the provider redirect for one sign-in. A step-up asks for fresh
// authentication (max_age=0, prompt=login) and the configured acr.
func (p *provider) authURL(ctx context.Context, state, nonce, verifier string, stepUp bool) (string, error) {
	d, err := p.discover(ctx)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {p.cfg.ClientID},
		"redirect_uri":          {p.cfg.RedirectURL},
		"scope":                 {"openid email profile"},
		"state":                 {state},
		"nonce":                 {nonce},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
	}
	if stepUp {
		q.Set("max_age", "0")
		q.Set("prompt", "login")
		if p.cfg.StepUpACR != "" {
			q.Set("acr_values", p.cfg.StepUpACR)
		}
	}
	sep := "?"
	if strings.Contains(d.AuthURL, "?") {
		sep = "&"
	}
	return d.AuthURL + sep + q.Encode(), nil
}

// exchange trades the code for tokens and verifies the ID token's signature, issuer,
// audience, expiry and nonce. The tokens themselves never leave this function.
func (p *provider) exchange(ctx context.Context, code, verifier, nonce string) (Identity, error) {
	d, err := p.discover(ctx)
	if err != nil {
		return Identity{}, err
	}
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {p.cfg.RedirectURL},
		"code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Identity{}, fmt.Errorf("token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(url.QueryEscape(p.cfg.ClientID), url.QueryEscape(p.cfg.ClientSecret))
	var tok struct {
		IDToken string `json:"id_token"`
	}
	if err := p.doJSON(req, &tok); err != nil {
		return Identity{}, fmt.Errorf("token exchange: %w", err)
	}
	return p.verify(ctx, tok.IDToken, nonce)
}

type idClaims struct {
	Iss           string          `json:"iss"`
	Sub           string          `json:"sub"`
	Aud           json.RawMessage `json:"aud"`
	Exp           int64           `json:"exp"`
	Iat           int64           `json:"iat"`
	Nonce         string          `json:"nonce"`
	AuthTime      int64           `json:"auth_time"`
	AMR           []string        `json:"amr"`
	Email         string          `json:"email"`
	EmailVerified bool            `json:"email_verified"`
}

func (p *provider) verify(ctx context.Context, raw, nonce string) (Identity, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return Identity{}, fmt.Errorf("malformed id token: %w", ErrToken)
	}
	var hdr struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := decodeSegment(parts[0], &hdr); err != nil || hdr.Alg != "RS256" {
		return Identity{}, fmt.Errorf("id token header: %w", ErrToken)
	}
	key, err := p.key(ctx, hdr.Kid)
	if err != nil {
		return Identity{}, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Identity{}, fmt.Errorf("id token signature: %w", ErrToken)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], sig) != nil {
		return Identity{}, fmt.Errorf("id token signature: %w", ErrToken)
	}
	var c idClaims
	if err := decodeSegment(parts[1], &c); err != nil {
		return Identity{}, fmt.Errorf("id token claims: %w", ErrToken)
	}
	now := p.now()
	switch {
	case c.Iss != p.cfg.Issuer:
		return Identity{}, fmt.Errorf("id token issuer: %w", ErrToken)
	case !audienceHas(c.Aud, p.cfg.ClientID):
		return Identity{}, fmt.Errorf("id token audience: %w", ErrToken)
	case c.Exp == 0 || now.After(time.Unix(c.Exp, 0).Add(clockSkew)):
		return Identity{}, fmt.Errorf("id token expired: %w", ErrToken)
	case c.Iat != 0 && time.Unix(c.Iat, 0).After(now.Add(clockSkew)):
		return Identity{}, fmt.Errorf("id token issued in the future: %w", ErrToken)
	case nonce == "" || c.Nonce != nonce:
		return Identity{}, fmt.Errorf("id token nonce: %w", ErrToken)
	case c.Sub == "":
		return Identity{}, fmt.Errorf("id token subject: %w", ErrToken)
	}
	id := Identity{Subject: c.Sub, Email: c.Email, EmailVerified: c.EmailVerified, AuthTime: now}
	if c.AuthTime != 0 {
		id.AuthTime = time.Unix(c.AuthTime, 0)
	}
	for _, m := range c.AMR {
		if slices.Contains(mfaMethods, m) {
			id.MFA = true
		}
	}
	return id, nil
}

// key returns the issuer's RSA key kid, refetching the JWKS once on a miss (key rotation).
func (p *provider) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	p.mu.Lock()
	k, ok := p.keys[kid]
	p.mu.Unlock()
	if ok {
		return k, nil
	}
	d, err := p.discover(ctx)
	if err != nil {
		return nil, err
	}
	var set struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := p.getJSON(ctx, d.JWKSURL, &set); err != nil {
		return nil, fmt.Errorf("oidc jwks: %w", err)
	}
	keys := map[string]*rsa.PublicKey{}
	for _, jk := range set.Keys {
		if jk.Kty != "RSA" {
			continue
		}
		n, errN := base64.RawURLEncoding.DecodeString(jk.N)
		e, errE := base64.RawURLEncoding.DecodeString(jk.E)
		if errN != nil || errE != nil || len(e) > 4 {
			continue
		}
		keys[jk.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	}
	p.mu.Lock()
	p.keys = keys
	p.mu.Unlock()
	if k, ok := keys[kid]; ok {
		return k, nil
	}
	return nil, fmt.Errorf("id token key not in jwks: %w", ErrToken)
}

func (p *provider) getJSON(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	return p.doJSON(req, out)
}

// doJSON sends req and decodes a 200 JSON body. Bodies are never echoed into errors: a
// token endpoint error body may carry credentials.
func (p *provider) doJSON(req *http.Request, out any) error {
	c := p.http
	if c == nil {
		c = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", req.Method, req.URL.Host, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s %s: status %d: %w", req.Method, req.URL.Host, resp.StatusCode, ErrToken)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(out); err != nil {
		return fmt.Errorf("%s %s: decode: %w", req.Method, req.URL.Host, ErrToken)
	}
	return nil
}

func decodeSegment(seg string, out any) error {
	b, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func audienceHas(raw json.RawMessage, client string) bool {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return one == client
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		return slices.Contains(many, client)
	}
	return false
}
