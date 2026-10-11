// Package counselplugin holds Counsel's Paploo plugin. This test lints its manifest.
package counselplugin

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

type connection struct {
	Connector string `yaml:"connector"`
	Scope     string `yaml:"scope"`
	Access    string `yaml:"access"`
}

type manifest struct {
	App struct {
		ID          string       `yaml:"id"`
		Connections []connection `yaml:"connections"`
		Data        struct {
			Read  []string `yaml:"read"`
			Write []string `yaml:"write"`
		} `yaml:"data"`
	} `yaml:"app"`
}

// appConnector is the Counsel app-API connector, the only connection the agent may hold.
const appConnector = "counsel/app-api"

// dataWrite are the write scopes the app-API connection may carry (manifest_scope data.write).
var dataWrite = map[string]bool{"drafts": true, "notes": true, "tasks": true, "agent_runs": true, "gates": true}

var errSendConnector = errors.New("manifest grants a connector that can send outside the firm")

// lint checks a manifest against ARC-I-046.
func lint(m manifest) error {
	// rule: ARC-I-046
	for _, c := range m.App.Connections {
		if c.Connector != appConnector {
			return fmt.Errorf("connection %q (access %q): %w", c.Connector, c.Access, errSendConnector)
		}
	}
	for _, w := range m.App.Data.Write {
		if !dataWrite[w] {
			return fmt.Errorf("data.write scope %q: %w", w, errSendConnector)
		}
	}
	return nil
}

func load(t *testing.T, raw []byte) manifest {
	t.Helper()
	var m manifest
	if err := yaml.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	return m
}

// rule: ARC-I-046
func TestARC_I_046_ManifestGrantsNoSendConnector(t *testing.T) {
	raw, err := os.ReadFile("paploo.yaml")
	if err != nil {
		t.Fatal(err)
	}
	shipped := load(t, raw)
	if shipped.App.ID != "counsel" || len(shipped.App.Connections) != 1 {
		t.Fatalf("shipped manifest: id %q, %d connections", shipped.App.ID, len(shipped.App.Connections))
	}
	withConn := func(c connection) manifest {
		m := load(t, raw)
		m.App.Connections = append(m.App.Connections, c)
		return m
	}
	withWrite := func(scope string) manifest {
		m := load(t, raw)
		m.App.Data.Write = append(m.App.Data.Write, scope)
		return m
	}
	tests := []struct {
		name string
		m    manifest
		want error
	}{
		{name: "shipped manifest", m: shipped},
		{name: "mail send connection", m: withConn(connection{Connector: "mail/imap-smtp", Scope: "user", Access: "write"}), want: errSendConnector},
		{name: "payment connection", m: withConn(connection{Connector: "payments/card", Scope: "org_service", Access: "write"}), want: errSendConnector},
		{name: "e-filing connection", m: withConn(connection{Connector: "courts/efile", Scope: "org_service", Access: "write"}), want: errSendConnector},
		{name: "read-only mail connection", m: withConn(connection{Connector: "mail/imap-smtp", Scope: "user", Access: "read"}), want: errSendConnector},
		{name: "write scope outside data.write", m: withWrite("emails"), want: errSendConnector},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := lint(tc.m)
			if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("lint() = %v, want %v", err, tc.want)
			}
		})
	}
}
