package boot

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"

	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/notify"
	"github.com/dagi377/counsel/kernel/notify/notifyfake"
)

// recordingNotifier records the template ids registered on a notifyfake.
type recordingNotifier struct {
	*notifyfake.Fake
	ids []string
}

func (r *recordingNotifier) RegisterTemplate(t notify.Template) error {
	if err := r.Fake.RegisterTemplate(t); err != nil {
		return err
	}
	r.ids = append(r.ids, t.ID)
	return nil
}

// captureLogs sends slog output to a buffer at debug level for the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func templateIDs(ts []notify.Template) []string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.ID)
	}
	return out
}

func TestRegisterTemplates(t *testing.T) {
	app := notifyTemplates()
	dup := append(slices.Clone(app), app[0])
	tests := []struct {
		name    string
		ts      []notify.Template
		wantIDs []string
		wantErr error
	}{
		{name: "every app template registered", ts: app, wantIDs: templateIDs(app)},
		{name: "duplicate id fails naming it", ts: dup, wantErr: notify.ErrDuplicateTemplate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := captureLogs(t)
			n := &recordingNotifier{Fake: notifyfake.New()}
			err := registerTemplates(context.Background(), n, tt.ts)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("registerTemplates() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if !strings.Contains(err.Error(), app[0].ID) {
					t.Fatalf("error %q does not name id %q", err, app[0].ID)
				}
				return
			}
			if !slices.Equal(n.ids, tt.wantIDs) {
				t.Fatalf("registered ids = %v, want %v", n.ids, tt.wantIDs)
			}
			for _, tpl := range tt.ts {
				for _, text := range []string{tpl.Subject, tpl.Body} {
					if strings.Contains(logs.String(), text) {
						t.Fatalf("slog output holds template text %q", text)
					}
				}
			}
		})
	}
}

func TestNotifyTemplatesValidOnKernelService(t *testing.T) {
	// The real service checks ids, channels and placeholders; every app template must pass.
	svc := notify.New(nil, nil)
	if err := registerTemplates(context.Background(), svc, notifyTemplates()); err != nil {
		t.Fatalf("registerTemplates(kernel service) = %v", err)
	}
	for _, id := range templateIDs(notifyTemplates()) {
		if _, ok := svc.Template(id); !ok {
			t.Fatalf("template %q not registered", id)
		}
	}
}

// fakeSender is a notify.Sender that records nothing.
type fakeSender struct{}

func (fakeSender) Deliver(context.Context, notify.Rendered) error { return nil }

func TestRegisterNotifySender(t *testing.T) {
	kernelSender := fakeSender{}
	tests := []struct {
		name   string
		opts   Options
		before notify.Sender
		want   notify.Sender
	}{
		{name: "sender set -> becomes kernel sender", opts: Options{Sender: fakeSender{}}, want: fakeSender{}},
		{name: "sender unset -> kernel keeps nil (count-only)", opts: Options{}, want: nil},
		{name: "sender unset -> kernel keeps its own", opts: Options{}, before: kernelSender, want: kernelSender},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := &kboot.Kernel{Notify: notify.New(nil, nil), NotifySender: tt.before}
			if err := tt.opts.registerNotify(context.Background(), k); err != nil {
				t.Fatalf("registerNotify() = %v", err)
			}
			if k.NotifySender != tt.want {
				t.Fatalf("NotifySender = %v, want %v", k.NotifySender, tt.want)
			}
			if _, ok := k.Notify.Template(NotifySubscriptionRenewalFailed); !ok {
				t.Fatalf("template %q not registered on the kernel service", NotifySubscriptionRenewalFailed)
			}
		})
	}
}

func TestRegisterNotifyTwiceFailsBoot(t *testing.T) {
	k := &kboot.Kernel{Notify: notify.New(nil, nil)}
	if err := (Options{}).registerNotify(context.Background(), k); err != nil {
		t.Fatal(err)
	}
	err := (Options{}).registerNotify(context.Background(), k)
	if !errors.Is(err, notify.ErrDuplicateTemplate) || !strings.Contains(err.Error(), NotifySubscriptionRenewalFailed) {
		t.Fatalf("second registerNotify() = %v, want duplicate naming %q", err, NotifySubscriptionRenewalFailed)
	}
}
