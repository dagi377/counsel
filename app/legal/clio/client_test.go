package clio

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type recordRT struct {
	urls    []string
	methods []string
	body    string
	status  int
}

func (r *recordRT) RoundTrip(req *http.Request) (*http.Response, error) {
	r.urls = append(r.urls, req.URL.String())
	r.methods = append(r.methods, req.Method)
	st := r.status
	if st == 0 {
		st = http.StatusOK
	}
	return &http.Response{StatusCode: st, Body: io.NopCloser(strings.NewReader(r.body)), Header: http.Header{}}, nil
}

// rule: MIG-001
func TestMIG_001_RegionPinnedSource(t *testing.T) {
	tests := []struct {
		region Region
		prefix string
	}{
		{"", "https://ca.app.clio.com/api/v4/"},
		{RegionCA, "https://ca.app.clio.com/api/v4/"},
		{RegionUS, "https://app.clio.com/api/v4/"},
		{RegionEU, "https://eu.app.clio.com/api/v4/"},
		{RegionAU, "https://au.app.clio.com/api/v4/"},
	}
	for _, tc := range tests {
		t.Run(string(tc.region), func(t *testing.T) {
			rt := &recordRT{body: `{"data":[{"id":1}],"meta":{"paging":{"next":"https://x/api/v4/matters.json?page_token=abc"}}}`}
			c, err := NewClient(Config{Region: tc.region, Token: "tok", Scopes: []string{"matters:read"}}, &http.Client{Transport: rt})
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			p, err := c.List(ctx, ResourceMatters, "")
			if err != nil {
				t.Fatal(err)
			}
			if p.Next != "abc" || len(p.Records) != 1 {
				t.Fatalf("page = %+v", p)
			}
			if _, err := c.List(ctx, ResourceContacts, p.Next); err != nil {
				t.Fatal(err)
			}
			for i, u := range rt.urls {
				if !strings.HasPrefix(u, tc.prefix) {
					t.Errorf("url %s not under %s", u, tc.prefix)
				}
				if rt.methods[i] != http.MethodGet {
					t.Errorf("method %s, want GET only", rt.methods[i])
				}
			}
			if got := c.StagingRegion(); got != "ca-central" {
				t.Errorf("staging region = %q, want ca-central", got)
			}
		})
	}
	if _, err := NewClient(Config{Region: "mars"}, nil); !errors.Is(err, ErrUnknownRegion) {
		t.Errorf("unknown region err = %v", err)
	}
}

// rule: MIG-002
func TestMIG_002_ReadOnlyScopes(t *testing.T) {
	tests := []struct {
		name   string
		scopes []string
		want   error
	}{
		{"read only", []string{"matters:read", "contacts:read"}, nil},
		{"read_write", []string{"matters:read", "matters:read_write"}, ErrWriteScope},
		{"write", []string{"bills:write"}, ErrWriteScope},
		{"upper", []string{"Documents:WRITE"}, ErrWriteScope},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewClient(Config{Region: RegionCA, Token: "secret-tok-123", Scopes: tc.scopes}, nil)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if err != nil {
				if !strings.Contains(err.Error(), "write scope not allowed") {
					t.Errorf("err %q lacks 'write scope not allowed'", err)
				}
				if strings.Contains(err.Error(), "secret-tok-123") {
					t.Errorf("token leaked in error")
				}
			}
		})
	}
}

type failRT struct{ token string }

func (f failRT) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("dial failed with " + f.token)
}

// rule: MIG-002
func TestMIG_002_TokenNeverInErrors(t *testing.T) {
	tests := []struct {
		name string
		hc   *http.Client
		want error
	}{
		{"transport error", &http.Client{Transport: failRT{token: "secret-tok-123"}}, nil},
		{"429", &http.Client{Transport: &recordRT{status: 429}}, ErrRateLimited},
		{"500", &http.Client{Transport: &recordRT{status: 500}}, ErrSourceStatus},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewClient(Config{Region: RegionCA, Token: "secret-tok-123"}, tc.hc)
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.List(context.Background(), ResourceBills, "")
			if err == nil {
				t.Fatal("want error")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
			if strings.Contains(err.Error(), "secret-tok-123") {
				t.Errorf("token leaked: %v", err)
			}
		})
	}
}
