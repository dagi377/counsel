package notifyfake

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/dagi377/counsel/kernel/notify"
)

func TestSend(t *testing.T) {
	both := notify.Template{ID: "mod.ready", Channels: []notify.Channel{notify.InApp, notify.Email}}
	tests := []struct {
		name     string
		prefs    []notify.Preference
		template string
		want     []notify.Channel
		err      error
	}{
		{name: "default all channels", template: "mod.ready", want: []notify.Channel{notify.InApp, notify.Email}},
		{name: "email turned off is never sent", template: "mod.ready",
			prefs: []notify.Preference{{UserID: "u1", Event: "mod.ready", Channel: notify.Email, Enabled: false}},
			want:  []notify.Channel{notify.InApp}},
		{name: "other user's preference ignored", template: "mod.ready",
			prefs: []notify.Preference{{UserID: "u2", Event: "mod.ready", Channel: notify.Email, Enabled: false}},
			want:  []notify.Channel{notify.InApp, notify.Email}},
		{name: "unknown template", template: "mod.nope", err: notify.ErrUnknownTemplate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New()
			if err := f.RegisterTemplate(both); err != nil {
				t.Fatal(err)
			}
			for _, p := range tt.prefs {
				if err := f.SetPreference(context.Background(), p); err != nil {
					t.Fatal(err)
				}
			}
			err := f.Send(context.Background(), nil, "u1", tt.template, map[string]string{"n": "1"})
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			var got []notify.Channel
			for _, m := range f.Sent() {
				got = append(got, m.Channel)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("channels = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPreferencesAndTemplates(t *testing.T) {
	tests := []struct {
		name  string
		setup func(f *Fake) error
		err   error
	}{
		{name: "duplicate template", setup: func(f *Fake) error {
			_ = f.RegisterTemplate(notify.Template{ID: "a"})
			return f.RegisterTemplate(notify.Template{ID: "a"})
		}, err: notify.ErrDuplicateTemplate},
		{name: "unknown channel in template", setup: func(f *Fake) error {
			return f.RegisterTemplate(notify.Template{ID: "a", Channels: []notify.Channel{"fax"}})
		}, err: notify.ErrUnknownChannel},
		{name: "locked cannot be turned off", setup: func(f *Fake) error {
			_ = f.SetPreference(context.Background(), notify.Preference{UserID: "u", Event: "a", Channel: notify.Email, Enabled: true, Locked: true})
			return f.SetPreference(context.Background(), notify.Preference{UserID: "u", Event: "a", Channel: notify.Email, Enabled: false})
		}, err: notify.ErrLocked},
		{name: "locked and disabled is invalid", setup: func(f *Fake) error {
			return f.SetPreference(context.Background(), notify.Preference{UserID: "u", Event: "a", Channel: notify.Email, Locked: true})
		}, err: notify.ErrLocked},
		{name: "plain toggle", setup: func(f *Fake) error {
			return f.SetPreference(context.Background(), notify.Preference{UserID: "u", Event: "a", Channel: notify.SMS})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.setup(New()); !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
		})
	}
}
