package main

import (
	"slices"
	"testing"

	"github.com/dagi377/counsel/kernel/boot"
)

func TestParseFlags(t *testing.T) {
	tests := []struct {
		name        string
		env         string
		args        []string
		wantFlags   []string
		wantZone    string
		wantWorkers int
		wantErr     bool
	}{
		{name: "defaults", wantZone: "America/Toronto"},
		{name: "flags split", args: []string{"-flags", "a.b,c.d"}, wantFlags: []string{"a.b", "c.d"}, wantZone: "America/Toronto"},
		{name: "zone from env", env: "America/Vancouver", wantZone: "America/Vancouver"},
		{name: "zone flag overrides env", env: "America/Vancouver", args: []string{"-zone", "America/Halifax"}, wantZone: "America/Halifax"},
		{name: "max workers", args: []string{"-max-workers", "4"}, wantZone: "America/Toronto", wantWorkers: 4},
		{name: "unknown flag", args: []string{"-addr", ":1"}, wantErr: true},
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
			if cfg.Role != boot.RoleWorker || cfg.Zone != tt.wantZone || cfg.MaxWorkers != tt.wantWorkers || !slices.Equal(cfg.Flags, tt.wantFlags) {
				t.Fatalf("cfg = %+v", cfg)
			}
		})
	}
}
