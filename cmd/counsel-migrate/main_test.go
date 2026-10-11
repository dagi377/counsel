package main

import (
	"testing"

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
