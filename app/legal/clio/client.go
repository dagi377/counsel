package clio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// StagingRegion is where imported data is staged, whatever the source region (MIG-001).
const StagingRegion = "ca-central"

// ErrSourceStatus is returned when the source answers with a non-2xx status.
var ErrSourceStatus = errors.New("clio: source returned an error status")

var _ Source = (*Client)(nil)

// Client is the read-only HTTP source client for one region. It only ever sends GET.
type Client struct {
	base  string
	token string
	http  *http.Client
}

// BaseURL returns the v4 API base for a region.
func BaseURL(r Region) (string, error) {
	// rule: MIG-001
	switch r {
	case RegionCA:
		return "https://ca.app.clio.com/api/v4/", nil
	case RegionUS:
		return "https://app.clio.com/api/v4/", nil
	case RegionEU:
		return "https://eu.app.clio.com/api/v4/", nil
	case RegionAU:
		return "https://au.app.clio.com/api/v4/", nil
	}
	return "", fmt.Errorf("base url %q: %w", string(r), ErrUnknownRegion)
}

// CheckScopes rejects a token that carries any write scope.
func CheckScopes(scopes []string) error {
	// rule: MIG-002
	for _, s := range scopes {
		if strings.Contains(strings.ToLower(s), "write") {
			return fmt.Errorf("write scope not allowed (%s): %w", s, ErrWriteScope)
		}
	}
	return nil
}

// NewClient checks the region and scopes and returns a client. An empty region means
// RegionCA. hc may be nil for http.DefaultClient.
func NewClient(cfg Config, hc *http.Client) (*Client, error) {
	if cfg.Region == "" {
		cfg.Region = RegionCA
	}
	base, err := BaseURL(cfg.Region)
	if err != nil {
		return nil, fmt.Errorf("new client: %w", err)
	}
	if err := CheckScopes(cfg.Scopes); err != nil {
		return nil, fmt.Errorf("new client: %w", err)
	}
	if hc == nil {
		hc = http.DefaultClient
	}
	return &Client{base: base, token: cfg.Token, http: hc}, nil
}

// StagingRegion returns the staging bucket region; it is Canadian for every source region.
func (c *Client) StagingRegion() string {
	// rule: MIG-001
	return StagingRegion
}

// List reads one page of res. The token goes only in the Authorization header and never
// into an error.
func (c *Client) List(ctx context.Context, res Resource, pageToken string) (Page, error) {
	u := c.base + url.PathEscape(string(res)) + ".json"
	if pageToken != "" {
		u += "?page_token=" + url.QueryEscape(pageToken)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Page{}, fmt.Errorf("list %s: %w", res, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return Page{}, fmt.Errorf("list %s: %w", res, c.redact(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return Page{}, fmt.Errorf("list %s: %w", res, ErrRateLimited)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return Page{}, fmt.Errorf("list %s: status %d: %w", res, resp.StatusCode, ErrSourceStatus)
	}
	var body struct {
		Data []json.RawMessage `json:"data"`
		Meta struct {
			Paging struct {
				Next string `json:"next"`
			} `json:"paging"`
		} `json:"meta"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(&body); err != nil {
		return Page{}, fmt.Errorf("list %s: decode: %w", res, err)
	}
	return Page{Records: body.Data, Next: nextToken(body.Meta.Paging.Next)}, nil
}

// nextToken extracts page_token from Clio's next-page URL, or returns it as is.
func nextToken(next string) string {
	if next == "" {
		return ""
	}
	if u, err := url.Parse(next); err == nil && u.Query().Get("page_token") != "" {
		return u.Query().Get("page_token")
	}
	return next
}

// redact removes the token from an error message.
func (c *Client) redact(err error) error {
	if c.token == "" || !strings.Contains(err.Error(), c.token) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), c.token, "[redacted]"))
}
