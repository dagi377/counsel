package boot

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal"
	"github.com/dagi377/counsel/app/legal/billing/payment"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/inbound"
	"github.com/dagi377/counsel/kernel/notify"
	"github.com/dagi377/counsel/kernel/registry"
)

// Name is the app name the kernel reports for Counsel.
const Name = "legal"

// ErrNoRegistry is returned when the kernel has no registry to register into.
var ErrNoRegistry = errors.New("legal boot: kernel has no registry")

// Options configures the legal app at boot. The zero value registers the legal modules only.
type Options struct {
	// Payment, when set, registers the payment provider's webhook as an inbound provider.
	Payment *PaymentHook
	// Gates, when set, builds the gate effect services and registers every gate kind that
	// has an effect. When nil no app gate kind is registered.
	Gates *GatePorts
	// Clearance, when set, builds conflict Clearance (gate G2) over the gate catalog.
	Clearance *ClearancePorts
	// Sender, when set, delivers rendered notifications. Nil keeps the kernel's count-only
	// sender.
	Sender notify.Sender
	// HTTP, when set, wires the module routes whose ports it carries; the rest keep their
	// placeholder mounts, which answer 503.
	HTTP *HTTPPorts
	// Webhooks, when set, makes a signed HTTPPoster over PGTargets the kernel's webhook
	// poster (PORT-07).
	Webhooks *WebhookPorts
}

// PaymentHook is the configured payment provider. Webhook.ID is the hook provider name from
// config: it is the {provider} segment of POST /hooks/{provider}.
type PaymentHook struct {
	Webhook payment.Webhook
	Service payment.Service
}

// hook is one boot step run against the kernel.
type hook func(ctx context.Context, k *kboot.Kernel) error

// App returns the legal app for kernel boot.
func App(opts Options) kboot.App {
	w := &wiring{opts: opts}
	return newApp(registerHooks(w), startHooks(opts))
}

// registerHooks lists the Register steps, in order. Later boot tasks append theirs here.
func registerHooks(w *wiring) []hook {
	return []hook{w.registerModules, w.opts.registerPayment, w.registerGates, w.buildClearance, w.opts.registerNotify, w.addWithdrawJobs, w.registerWebhooks}
}

// startHooks lists the Start steps (seed and start jobs), in order.
func startHooks(Options) []hook { return []hook{seedTemplates} }

func newApp(register, start []hook) kboot.App {
	return kboot.App{
		Name:   Name,
		Define: legal.DefineFlags,
		Register: func(ctx context.Context, k *kboot.Kernel) error {
			return run(ctx, k, register)
		},
		Start: func(ctx context.Context, k *kboot.Kernel) error {
			return run(ctx, k, start)
		},
	}
}

func run(ctx context.Context, k *kboot.Kernel, hooks []hook) error {
	for _, h := range hooks {
		if err := h(ctx, k); err != nil {
			return fmt.Errorf("legal boot: %w", err)
		}
	}
	return nil
}

// registerModules registers the legal modules. With Options.HTTP set, the route groups it
// wires replace the modules' placeholder mounts.
func (w *wiring) registerModules(_ context.Context, k *kboot.Kernel) error {
	if k.Registry == nil {
		return ErrNoRegistry
	}
	if w.opts.HTTP == nil {
		return legal.Register(k.Registry)
	}
	wired, err := wiredRoutes(k, *w.opts.HTTP)
	if err != nil {
		return err
	}
	if err := legal.Register(newRouteFilter(k.Registry, wired)); err != nil {
		return err
	}
	for _, m := range wired {
		k.Registry.Routes(m)
	}
	return nil
}

// registerPayment registers the configured payment webhook as an inbound provider.
func (o Options) registerPayment(_ context.Context, k *kboot.Kernel) error {
	if o.Payment == nil {
		return nil
	}
	if k.Registry == nil {
		return ErrNoRegistry
	}
	p, err := payment.NewProvider(o.Payment.Webhook, o.Payment.Service)
	if err != nil {
		return fmt.Errorf("payment hook: %w", err)
	}
	k.Registry.InboundProvider(registryProvider(p))
	return nil
}

// registryProvider adapts a kernel inbound provider to the registry's provider shape.
func registryProvider(p inbound.Provider) registry.Provider {
	return registry.Provider{
		ID:     p.ID,
		Verify: p.Verify,
		Handle: func(ctx context.Context, tx pgx.Tx, e registry.InboundEvent) error {
			return p.Handle(ctx, tx, inbound.InboundEvent{
				Provider: e.Provider, EventID: e.EventID, ReceivedAt: e.ReceivedAt,
				PayloadSHA256: sha256.Sum256(e.Body), Body: e.Body,
			})
		},
	}
}

// registerWebhooks wires the webhook poster when Options.Webhooks is set.
func (w *wiring) registerWebhooks(_ context.Context, k *kboot.Kernel) error {
	return wireWebhooks(k, w.opts.Webhooks)
}
