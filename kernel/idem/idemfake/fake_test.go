package idemfake

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/idem"
)

func TestFakeBegin(t *testing.T) {
	ctx := context.Background()
	k := idem.Key{PrincipalID: "u1", Value: "key-00000001"}
	h1 := sha256.Sum256([]byte("a"))
	h2 := sha256.Sum256([]byte("b"))
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		setup    func(f *Fake)
		key      idem.Key
		hash     [32]byte
		advance  time.Duration
		wantErr  error
		wantResp bool
	}{
		{name: "fresh key runs", key: k, hash: h1},
		{name: "short key", key: idem.Key{PrincipalID: "u1", Value: "short"}, hash: h1, wantErr: idem.ErrInvalidKey},
		{name: "no principal", key: idem.Key{Value: "key-00000001"}, hash: h1, wantErr: idem.ErrInvalidKey},
		{name: "in flight", setup: func(f *Fake) { mustBegin(t, f, k, h1) }, key: k, hash: h1, wantErr: idem.ErrInFlight},
		{name: "reused with other body", setup: func(f *Fake) { mustBegin(t, f, k, h1) }, key: k, hash: h2, wantErr: idem.ErrKeyReused},
		{name: "replay", setup: func(f *Fake) {
			mustBegin(t, f, k, h1)
			if err := f.Complete(ctx, k, idem.Response{StatusCode: 201, Body: []byte("{}")}); err != nil {
				t.Fatal(err)
			}
		}, key: k, hash: h1, wantResp: true},
		{name: "released key runs again", setup: func(f *Fake) {
			mustBegin(t, f, k, h1)
			if err := f.Release(ctx, k); err != nil {
				t.Fatal(err)
			}
		}, key: k, hash: h1},
		{name: "expired key runs again", setup: func(f *Fake) { mustBegin(t, f, k, h1) }, key: k, hash: h2, advance: 2 * idem.DefaultTTL},
		{name: "other principal is separate", setup: func(f *Fake) { mustBegin(t, f, k, h1) }, key: idem.Key{PrincipalID: "u2", Value: k.Value}, hash: h2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := base
			f := New()
			f.Now = func() time.Time { return now }
			if tt.setup != nil {
				tt.setup(f)
			}
			now = now.Add(tt.advance)
			resp, err := f.Begin(ctx, tt.key, tt.hash, 0)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if (resp != nil) != tt.wantResp {
				t.Fatalf("resp = %v, want resp %v", resp, tt.wantResp)
			}
			if tt.wantResp && resp.StatusCode != 201 {
				t.Fatalf("status = %d, want 201", resp.StatusCode)
			}
		})
	}
}

func TestFakeCompleteRelease(t *testing.T) {
	ctx := context.Background()
	k := idem.Key{PrincipalID: "u1", Value: "key-00000001"}
	tests := []struct {
		name    string
		op      func(f *Fake) error
		wantErr error
	}{
		{name: "complete unknown", op: func(f *Fake) error { return f.Complete(ctx, k, idem.Response{}) }, wantErr: idem.ErrNotFound},
		{name: "release unknown", op: func(f *Fake) error { return f.Release(ctx, k) }, wantErr: idem.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.op(New()); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func mustBegin(t *testing.T, f *Fake, k idem.Key, h [32]byte) {
	t.Helper()
	if _, err := f.Begin(context.Background(), k, h, 0); err != nil {
		t.Fatal(err)
	}
}
