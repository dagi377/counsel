package boot

import (
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/dagi377/counsel/app/legal/trust"
)

// ErrSealKeyMissing means a production boot found no seal key: it refuses to start rather
// than run without the trust account sealer and the money gate effects.
var ErrSealKeyMissing = errors.New("legal boot: no seal key configured ($COUNSEL_SEAL_KEY or a seal key file)")

// sealKeyID is the id byte the active seal key writes ahead of every sealed value.
const sealKeyID byte = 1

// rule: ARC-U-040
// Secret is a configuration value that must never reach a log line or an error. Every
// formatting path prints a fixed placeholder.
type Secret string

const redacted = "[redacted]"

// String implements fmt.Stringer.
func (Secret) String() string { return redacted }

// GoString implements fmt.GoStringer.
func (Secret) GoString() string { return redacted }

// LogValue implements slog.LogValuer.
func (Secret) LogValue() slog.Value { return slog.StringValue(redacted) }

// resolveSealKey is the seal key from SealKeyBase64, else SealKeyFile. Without either, a
// dev boot gets a random per-process key and a warning, and any other boot is refused. No
// path logs, prints or wraps the key or the file's contents.
func (p Production) resolveSealKey() (*trust.SealKey, error) {
	encoded := strings.TrimSpace(string(p.SealKeyBase64))
	if encoded == "" && p.SealKeyFile != "" {
		b, err := os.ReadFile(p.SealKeyFile)
		if err != nil {
			// The path is config, not a secret; the contents never appear.
			return nil, fmt.Errorf("seal key file: %w", errors.Unwrap(err))
		}
		encoded = strings.TrimSpace(string(b))
	}
	if encoded != "" {
		k, err := trust.SealKeyFromBase64(sealKeyID, encoded)
		if err != nil {
			return nil, err
		}
		// NewAESSealer checks the length; the error names no key material.
		if _, err := trust.NewAESSealer(k); err != nil {
			return nil, err
		}
		return &k, nil
	}
	if !p.Dev {
		return nil, ErrSealKeyMissing
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("dev seal key: %w", err)
	}
	slog.Warn("no seal key configured: dev boot uses a random per-process seal key; sealed trust account numbers will not open after a restart")
	return &trust.SealKey{ID: sealKeyID, Key: b}, nil
}
