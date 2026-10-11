package trust

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func sealKey(id, fill byte) SealKey {
	return SealKey{ID: id, Key: bytes.Repeat([]byte{fill}, 32)}
}

func mustSealer(t *testing.T, active SealKey, retired ...SealKey) *AESSealer {
	t.Helper()
	s, err := NewAESSealer(active, retired...)
	if err != nil {
		t.Fatalf("NewAESSealer: %v", err)
	}
	return s
}

// noLeak fails when err's text holds the key, the plaintext or either in hex/base64.
func noLeak(t *testing.T, err error, secrets ...[]byte) {
	t.Helper()
	if err == nil {
		return
	}
	msg := err.Error()
	for _, s := range secrets {
		for _, form := range []string{string(s), hex.EncodeToString(s), base64.StdEncoding.EncodeToString(s)} {
			if form != "" && strings.Contains(msg, form) {
				t.Fatalf("error %q leaks secret material", msg)
			}
		}
	}
}

func TestNewAESSealerKeys(t *testing.T) {
	tests := []struct {
		name    string
		active  SealKey
		retired []SealKey
		err     error
	}{
		{name: "32-byte key", active: sealKey(1, 'k')},
		{name: "with retired key", active: sealKey(2, 'k'), retired: []SealKey{sealKey(1, 'o')}},
		{name: "empty key", active: SealKey{ID: 1}, err: ErrSealKey},
		{name: "16-byte key", active: SealKey{ID: 1, Key: bytes.Repeat([]byte{'k'}, 16)}, err: ErrSealKey},
		{name: "24-byte key", active: SealKey{ID: 1, Key: bytes.Repeat([]byte{'k'}, 24)}, err: ErrSealKey},
		{name: "33-byte key", active: SealKey{ID: 1, Key: bytes.Repeat([]byte{'k'}, 33)}, err: ErrSealKey},
		{name: "short retired key", active: sealKey(2, 'k'), retired: []SealKey{{ID: 1, Key: []byte("short-secret")}}, err: ErrSealKey},
		{name: "duplicate id", active: sealKey(1, 'k'), retired: []SealKey{sealKey(1, 'o')}, err: ErrSealKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := NewAESSealer(tt.active, tt.retired...)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if (err == nil) != (s != nil) {
				t.Fatalf("sealer %v with err %v", s, err)
			}
			secrets := [][]byte{tt.active.Key}
			for _, k := range tt.retired {
				secrets = append(secrets, k.Key)
			}
			noLeak(t, err, secrets...)
		})
	}
}

func TestSealKeyFromBase64(t *testing.T) {
	raw := bytes.Repeat([]byte{'k'}, 32)
	tests := []struct {
		name    string
		encoded string
		err     error
	}{
		{name: "valid", encoded: base64.StdEncoding.EncodeToString(raw)},
		{name: "not base64", encoded: "!!not-base64-secret!!", err: ErrSealKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, err := SealKeyFromBase64(3, tt.encoded)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if err != nil {
				if strings.Contains(err.Error(), tt.encoded) {
					t.Fatalf("error leaks input: %v", err)
				}
				return
			}
			if k.ID != 3 || !bytes.Equal(k.Key, raw) {
				t.Fatalf("key = %+v", k)
			}
		})
	}
}

func TestAESSealerRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := mustSealer(t, sealKey(7, 'k'))
	tests := []struct {
		name  string
		plain []byte
	}{
		{name: "account number", plain: []byte("000111234")},
		{name: "empty", plain: []byte{}},
		{name: "long", plain: bytes.Repeat([]byte("9"), 64)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sealed, err := s.Seal(ctx, tt.plain)
			if err != nil {
				t.Fatal(err)
			}
			if sealed[0] != 7 {
				t.Fatalf("key id byte = %d, want 7", sealed[0])
			}
			if want := 1 + 12 + len(tt.plain) + 16; len(sealed) != want {
				t.Fatalf("sealed length = %d, want %d", len(sealed), want)
			}
			if len(tt.plain) > 0 && bytes.Contains(sealed, tt.plain) {
				t.Fatal("sealed value holds the plaintext")
			}
			got, err := s.Open(ctx, sealed)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, tt.plain) {
				t.Fatalf("Open = %q, want %q", got, tt.plain)
			}
		})
	}
}

func TestAESSealerFreshNonce(t *testing.T) {
	ctx := context.Background()
	s := mustSealer(t, sealKey(1, 'k'))
	tests := []struct {
		name  string
		plain []byte
	}{
		{name: "account number", plain: []byte("000111234")},
		{name: "empty", plain: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := s.Seal(ctx, tt.plain)
			if err != nil {
				t.Fatal(err)
			}
			b, err := s.Seal(ctx, tt.plain)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(a, b) {
				t.Fatal("two seals of the same input are equal")
			}
			if bytes.Equal(a[1:13], b[1:13]) {
				t.Fatal("two seals reused a nonce")
			}
		})
	}
}

func TestAESSealerOpenRejects(t *testing.T) {
	ctx := context.Background()
	key := sealKey(1, 'k')
	plain := []byte("000111234")
	s := mustSealer(t, key)
	good, err := s.Seal(ctx, plain)
	if err != nil {
		t.Fatal(err)
	}
	flip := func(i int) []byte {
		b := bytes.Clone(good)
		b[i] ^= 0x01
		return b
	}
	other, err := mustSealer(t, sealKey(1, 'x')).Seal(ctx, plain)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		sealed []byte
		err    error
	}{
		{name: "empty", sealed: nil, err: ErrSealCorrupt},
		{name: "header only", sealed: good[:1], err: ErrSealCorrupt},
		{name: "truncated tag", sealed: good[:len(good)-1], err: ErrSealCorrupt},
		{name: "tampered ciphertext", sealed: flip(14), err: ErrSealCorrupt},
		{name: "tampered tag", sealed: flip(len(good) - 1), err: ErrSealCorrupt},
		{name: "tampered nonce", sealed: flip(2), err: ErrSealCorrupt},
		{name: "unknown key id", sealed: append([]byte{9}, good[1:]...), err: ErrSealUnknownKey},
		{name: "same id, different key", sealed: other, err: ErrSealCorrupt},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.Open(ctx, tt.sealed)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if got != nil {
				t.Fatalf("Open returned %q on error", got)
			}
			noLeak(t, err, key.Key, plain, tt.sealed)
		})
	}
}

func TestAESSealerRotation(t *testing.T) {
	ctx := context.Background()
	oldKey, newKey := sealKey(1, 'o'), sealKey(2, 'n')
	plain := []byte("000111234")
	old, err := mustSealer(t, oldKey).Seal(ctx, plain)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		sealer  *AESSealer
		err     error
		sealsID byte
	}{
		{name: "rotated sealer opens the old value", sealer: mustSealer(t, newKey, oldKey), sealsID: 2},
		{name: "old key dropped", sealer: mustSealer(t, newKey), err: ErrSealUnknownKey, sealsID: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.sealer.Open(ctx, old)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if err == nil && !bytes.Equal(got, plain) {
				t.Fatalf("Open = %q", got)
			}
			fresh, err := tt.sealer.Seal(ctx, plain)
			if err != nil {
				t.Fatal(err)
			}
			if fresh[0] != tt.sealsID {
				t.Fatalf("seals with key %d, want %d", fresh[0], tt.sealsID)
			}
		})
	}
}

type failReader struct{}

func (failReader) Read([]byte) (int, error) { return 0, errors.New("entropy unavailable") }

func TestAESSealerNonceFailure(t *testing.T) {
	tests := []struct {
		name  string
		plain []byte
	}{
		{name: "account number", plain: []byte("000111234")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := mustSealer(t, sealKey(1, 'k'))
			s.rand = failReader{}
			got, err := s.Seal(context.Background(), tt.plain)
			if err == nil || got != nil {
				t.Fatalf("Seal = %q, %v; want an error", got, err)
			}
			noLeak(t, err, tt.plain)
		})
	}
}
