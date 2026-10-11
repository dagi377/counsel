package main

import (
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/boot"
)

func TestARC_U_017_MigrateIsExpandOnlyByDefault(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    boot.MigrateMode
		wantErr bool
	}{
		{name: "default is expand", want: boot.MigrateExpand},
		{name: "explicit contract", args: []string{"-mode", "contract"}, want: boot.MigrateContract},
		{name: "wait-version", args: []string{"-mode=wait-version"}, want: boot.MigrateWaitVersion},
		{name: "wait-version with version", args: []string{"-mode=wait-version", "-version=0001_init.sql", "-timeout=5m"}, want: boot.MigrateWaitVersion},
		{name: "unknown flag", args: []string{"-drop"}, wantErr: true},
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
			if cfg.Role != boot.RoleMigrate || cfg.MigrateMode != tt.want {
				t.Fatalf("cfg = %+v, want mode %q", cfg, tt.want)
			}
			if err := cfg.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})
	}
	bad, err := parseFlags([]string{"-mode", "drop-everything"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := bad.Validate(); err == nil {
		t.Fatal("unknown migrate mode validated")
	}
}

func TestWaitVersionFlags(t *testing.T) {
	cfg, err := parseFlags([]string{"-mode=wait-version", "-version=0001_init.sql", "-timeout=5m"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SchemaVersion != "0001_init.sql" || cfg.Timeout != 5*time.Minute {
		t.Fatalf("cfg = %+v", cfg)
	}
	cfg, err = parseFlags([]string{"-mode=wait-version"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SchemaVersion != "" || cfg.Timeout != 0 {
		t.Fatalf("defaults: cfg = %+v, want newest version and no timeout", cfg)
	}
}
