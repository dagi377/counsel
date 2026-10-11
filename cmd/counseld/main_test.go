package main

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
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
		args      []string
		wantAddr  string
		wantFlags int
		wantErr   bool
	}{
		{name: "defaults", wantAddr: ":8080"},
		{name: "addr and flags", args: []string{"-addr", ":9000", "-flags", "a.b,c.d"}, wantAddr: ":9000", wantFlags: 2},
		{name: "unknown flag", args: []string{"-nope"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := parseFlags(tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if cfg.Role != boot.RoleServer || cfg.Addr != tt.wantAddr || len(cfg.Flags) != tt.wantFlags {
				t.Fatalf("cfg = %+v", cfg)
			}
		})
	}
}
