package payment

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/inbound"
)

// ErrNoDecoder is returned when a payment provider is built without Verify or Decode.
var ErrNoDecoder = errors.New("payment: provider needs verify and decode")

// Webhook describes one payment provider's webhook: how to check a delivery and how to read
// the payment it reports. Decode returns the capture without Provider or ProviderEventID;
// those always come from the verified delivery.
type Webhook struct {
	ID     string
	Verify func(r *http.Request, body []byte) (eventID string, err error)
	Decode func(body []byte) (Capture, error)
}

// NewProvider returns the inbound provider that captures the payment each first-seen
// delivery reports. Register it with the kernel through Registry.InboundProvider.
func NewProvider(w Webhook, svc Service) (inbound.Provider, error) {
	if w.ID == "" || w.Verify == nil || w.Decode == nil || svc == nil {
		return inbound.Provider{}, fmt.Errorf("payment provider %q: %w", w.ID, ErrNoDecoder)
	}
	handle := func(ctx context.Context, _ pgx.Tx, e inbound.InboundEvent) error {
		c, err := w.Decode(e.Body)
		if err != nil {
			return fmt.Errorf("decode %s webhook: %w", e.Provider, err)
		}
		// rule: BIL-I-020
		// The dedup key is the verified (provider, event_id), never what the body claims, so
		// a redelivery that slips past the receiver still returns the first payment.
		c.Provider, c.ProviderEventID, c.IdempotencyKey = e.Provider, e.EventID, ""
		if _, _, err := svc.Capture(ctx, c); err != nil {
			return fmt.Errorf("capture %s webhook: %w", e.Provider, err)
		}
		return nil
	}
	return inbound.Provider{ID: w.ID, Verify: w.Verify, Handle: handle}, nil
}
