package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dagi377/counsel/kernel/boot"
)

// mainImports is every import a cmd/*/main.go may use: flag parsing, signals, logging and
// the boot entry points. Anything else means main has grown logic.
var mainImports = map[string]bool{
	"context":                                true,
	"flag":                                   true,
	"log/slog":                               true,
	"os":                                     true,
	"os/signal":                              true,
	"strings":                                true,
	"syscall":                                true,
	"github.com/dagi377/counsel/kernel/boot": true,
	"github.com/dagi377/counsel/app/legal":   true,
	"github.com/dagi377/counsel/app/legal/boot": true,
}

func TestARC_I_040_MainsOnlyParseFlagsAndCallBoot(t *testing.T) {
	files, err := filepath.Glob("../*/main.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob cmd mains: %v (found %d)", err, len(files))
	}
	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			parsed, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			for _, imp := range parsed.Imports {
				p, _ := strconv.Unquote(imp.Path.Value)
				if !mainImports[p] {
					t.Errorf("%s imports %q; main may only parse flags and call kernel/boot", f, p)
				}
			}
		})
	}
}

func TestParseFlags(t *testing.T) {
	tests := []struct {
		name      string
		env       string
		args      []string
		wantAddr  string
		wantFlags []string
		wantZone  string
		wantErr   bool
	}{
		{name: "defaults", wantAddr: ":8080", wantZone: "America/Toronto"},
		{name: "addr and flags split", args: []string{"-addr", ":9000", "-flags", "a.b,c.d"}, wantAddr: ":9000", wantFlags: []string{"a.b", "c.d"}, wantZone: "America/Toronto"},
		{name: "zone from env", env: "America/Vancouver", wantAddr: ":8080", wantZone: "America/Vancouver"},
		{name: "zone flag overrides env", env: "America/Vancouver", args: []string{"-zone", "America/Halifax"}, wantAddr: ":8080", wantZone: "America/Halifax"},
		{name: "unknown flag", args: []string{"-nope"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("COUNSEL_ZONE", tt.env)
			cfg, _, err := parseFlags(tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if cfg.Role != boot.RoleServer || cfg.Addr != tt.wantAddr || cfg.Zone != tt.wantZone || !slices.Equal(cfg.Flags, tt.wantFlags) {
				t.Fatalf("cfg = %+v", cfg)
			}
		})
	}
}

// TestParseFlagsDocsBucket: the bucket location comes from flags with an env fallback; its
// credentials only from the environment, and they never format into text.
func TestParseFlagsDocsBucket(t *testing.T) {
	tests := []struct {
		name       string
		envURL     string
		args       []string
		wantURL    string
		wantBucket bool
	}{
		{name: "env fallback", envURL: "https://s3.example/env-bucket", wantURL: "https://s3.example/env-bucket", wantBucket: true},
		{name: "flag wins over env", envURL: "https://s3.example/env-bucket", args: []string{"-docs-bucket-url", "https://s3.example/flag-bucket"}, wantURL: "https://s3.example/flag-bucket", wantBucket: true},
		{name: "no url: no bucket", wantURL: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("COUNSEL_DOCS_BUCKET_URL", tt.envURL)
			t.Setenv("COUNSEL_DOCS_BUCKET_REGION", "ca-central-1")
			t.Setenv("AWS_ACCESS_KEY_ID", "AKIDSECRETVALUE")
			t.Setenv("AWS_SECRET_ACCESS_KEY", "verysecretkeyvalue")
			_, prod, err := parseFlags(tt.args)
			if err != nil {
				t.Fatal(err)
			}
			if prod.Docs.URL != tt.wantURL || prod.Docs.Region != "ca-central-1" {
				t.Fatalf("docs = %s %s", prod.Docs.URL, prod.Docs.Region)
			}
			if got := prod.Docs.Bucket() != nil; got != tt.wantBucket {
				t.Fatalf("bucket configured = %v, want %v", got, tt.wantBucket)
			}
			// rule: ARC-U-040
			for _, s := range []string{fmt.Sprintf("%v", prod.Docs), fmt.Sprintf("%+v", prod.Docs), fmt.Sprintf("%#v", prod.Docs)} {
				if strings.Contains(s, "AKIDSECRETVALUE") || strings.Contains(s, "verysecretkeyvalue") {
					t.Fatalf("credential formatted: %s", s)
				}
			}
		})
	}
}
