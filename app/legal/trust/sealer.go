package trust

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// Sealer errors. They never carry key material, plaintext or ciphertext.
var (
	// ErrSealKey means a key is not a 32-byte AES-256 key, or two keys share an id.
	ErrSealKey = errors.New("trust: invalid seal key")
	// ErrSealUnknownKey means a sealed value names a key id the sealer does not hold.
	ErrSealUnknownKey = errors.New("trust: sealed with an unknown key")
	// ErrSealCorrupt means a sealed value is truncated or fails authentication.
	ErrSealCorrupt = errors.New("trust: sealed value is corrupt")
)

const sealKeyBytes = 32

// SealKey is one AES-256 key and the id byte written ahead of every value it seals. Keys
// come from boot options (environment or a mounted file), never from the database.
type SealKey struct {
	ID  byte
	Key []byte
}

// SealKeyFromBase64 decodes a standard base64 key, as read from the environment or a file.
// The error never includes the input.
func SealKeyFromBase64(id byte, encoded string) (SealKey, error) {
	k, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return SealKey{}, fmt.Errorf("seal key %d: not base64: %w", id, ErrSealKey)
	}
	return SealKey{ID: id, Key: k}, nil
}

// AESSealer seals bank account numbers with AES-256-GCM. A sealed value is
// key id (1 byte) | nonce (12 random bytes) | ciphertext and tag. The key id byte is the
// additional authenticated data, so it cannot be swapped. Seal always uses the active key;
// Open also accepts the retired keys, so keys can rotate without rewriting stored rows.
type AESSealer struct {
	active byte
	aeads  map[byte]cipher.AEAD
	rand   io.Reader
}

var _ Sealer = (*AESSealer)(nil)

// NewAESSealer returns a sealer that seals with active and opens with active or any retired
// key. Every key must be exactly 32 bytes, and ids must be distinct.
func NewAESSealer(active SealKey, retired ...SealKey) (*AESSealer, error) {
	s := &AESSealer{active: active.ID, aeads: map[byte]cipher.AEAD{}, rand: rand.Reader}
	for _, k := range append([]SealKey{active}, retired...) {
		if len(k.Key) != sealKeyBytes {
			return nil, fmt.Errorf("seal key %d: want %d bytes: %w", k.ID, sealKeyBytes, ErrSealKey)
		}
		if _, dup := s.aeads[k.ID]; dup {
			return nil, fmt.Errorf("seal key %d: duplicate id: %w", k.ID, ErrSealKey)
		}
		block, err := aes.NewCipher(k.Key)
		if err != nil {
			return nil, fmt.Errorf("seal key %d: %w", k.ID, ErrSealKey)
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, fmt.Errorf("seal key %d: %w", k.ID, ErrSealKey)
		}
		s.aeads[k.ID] = aead
	}
	return s, nil
}

// Seal implements Sealer.
func (s *AESSealer) Seal(_ context.Context, plaintext []byte) ([]byte, error) {
	aead := s.aeads[s.active]
	head := []byte{s.active}
	out := make([]byte, 1+aead.NonceSize(), 1+aead.NonceSize()+len(plaintext)+aead.Overhead())
	out[0] = s.active
	if _, err := io.ReadFull(s.rand, out[1:]); err != nil {
		return nil, fmt.Errorf("seal: nonce: %w", err)
	}
	return aead.Seal(out, out[1:], plaintext, head), nil
}

// Open reverses Seal. A value sealed under an id the sealer does not hold returns
// ErrSealUnknownKey; a truncated or tampered value returns ErrSealCorrupt.
func (s *AESSealer) Open(_ context.Context, sealed []byte) ([]byte, error) {
	if len(sealed) < 1 {
		return nil, fmt.Errorf("open: %w", ErrSealCorrupt)
	}
	aead, ok := s.aeads[sealed[0]]
	if !ok {
		return nil, fmt.Errorf("open: key %d: %w", sealed[0], ErrSealUnknownKey)
	}
	n := aead.NonceSize()
	if len(sealed) < 1+n+aead.Overhead() {
		return nil, fmt.Errorf("open: %w", ErrSealCorrupt)
	}
	p, err := aead.Open(nil, sealed[1:1+n], sealed[1+n:], sealed[:1])
	if err != nil {
		// The cipher's own error is dropped: it adds nothing and must not echo input.
		return nil, fmt.Errorf("open: %w", ErrSealCorrupt)
	}
	return p, nil
}
