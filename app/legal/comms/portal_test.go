package comms_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/comms"
	"github.com/dagi377/counsel/app/legal/comms/commsfake"
	"github.com/dagi377/counsel/kernel/registry"
)

func setup(t *testing.T) map[string]registry.PortalPage {
	t.Helper()
	f := commsfake.New()
	at := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	for _, it := range []comms.SharedItem{
		{ID: "d1", MatterID: "m1", Kind: comms.ItemDocument, SharedAt: at},
		{ID: "b1", MatterID: "m1", Kind: comms.ItemBill, SharedAt: at.Add(time.Minute)},
		{ID: "d2", MatterID: "m2", Kind: comms.ItemDocument, SharedAt: at},
		{ID: "d3", MatterID: "m3", Kind: comms.ItemDocument, SharedAt: at},
	} {
		if err := f.Share(context.Background(), it); err != nil {
			t.Fatal(err)
		}
	}
	access := map[string][]string{"c1": {"m1", "m2"}, "c2": {"m3"}}
	resolve := func(_ context.Context, id string) (comms.Guest, error) {
		return comms.Guest{ContactID: id, MatterIDs: access[id]}, nil
	}
	pages := map[string]registry.PortalPage{}
	for _, p := range comms.PortalPages(f, resolve) {
		pages[p.Route] = p
	}
	return pages
}

func guest(id string) registry.Principal {
	return registry.Principal{Kind: registry.PrincipalGuest, UserID: id}
}

func ids(v any) []string {
	var out []string
	for _, it := range v.([]comms.SharedItem) {
		out = append(out, it.ID)
	}
	return out
}

func TestPortalMattersOwnOnly(t *testing.T) {
	tests := []struct {
		name string
		p    registry.Principal
		want []string
		err  error
	}{
		{"client with two matters", guest("c1"), []string{"m1", "m2"}, nil},
		{"client with one matter", guest("c2"), []string{"m3"}, nil},
		{"guest with no access", guest("c9"), nil, nil},
		{"firm user is not a portal guest", registry.Principal{Kind: registry.PrincipalUser, UserID: "c1"}, nil, comms.ErrNotFound},
		{"agent is not a portal guest", registry.Principal{Kind: registry.PrincipalAgent, ActingFor: "c1"}, nil, comms.ErrNotFound},
	}
	pages := setup(t)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := pages[comms.RouteMatters].Load(context.Background(), tc.p, nil)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if err == nil && !slices.Equal(got.([]string), tc.want) {
				t.Fatalf("matters = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPortalItemsOwnMattersOnly(t *testing.T) {
	tests := []struct {
		name   string
		p      registry.Principal
		params map[string]string
		want   []string
		err    error
	}{
		{"documents and bills of own matter", guest("c1"), map[string]string{"matter": "m1"}, []string{"d1", "b1"}, nil},
		{"bills only", guest("c1"), map[string]string{"matter": "m1", "kind": "bill"}, []string{"b1"}, nil},
		{"documents only", guest("c1"), map[string]string{"matter": "m1", "kind": "document"}, []string{"d1"}, nil},
		{"other client's matter does not exist", guest("c1"), map[string]string{"matter": "m3"}, nil, comms.ErrNotFound},
		{"no matter param", guest("c1"), map[string]string{}, nil, comms.ErrNotFound},
		{"unknown kind", guest("c1"), map[string]string{"matter": "m1", "kind": "note"}, nil, comms.ErrInvalid},
		{"firm user refused", registry.Principal{Kind: registry.PrincipalUser, UserID: "c1"}, map[string]string{"matter": "m1"}, nil, comms.ErrNotFound},
	}
	pages := setup(t)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := pages[comms.RouteItems].Load(context.Background(), tc.p, tc.params)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if err == nil && !slices.Equal(ids(got), tc.want) {
				t.Fatalf("items = %v, want %v", ids(got), tc.want)
			}
		})
	}
}

func TestRegisterPortal(t *testing.T) {
	rec := &registry.Recorder{}
	comms.RegisterPortal(rec, commsfake.New(), nil)
	tests := []struct {
		method string
		want   int
	}{
		{registry.MethodCapability, 1},
		{registry.MethodPortalPage, 2},
	}
	for _, tc := range tests {
		if got := len(rec.Method(tc.method)); got != tc.want {
			t.Errorf("%s calls = %d, want %d", tc.method, got, tc.want)
		}
	}
}
