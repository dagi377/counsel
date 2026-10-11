package ai

import (
	"errors"
	"testing"
)

// rule: AI-R-001
func TestAI_R_001_ConfigCheck(t *testing.T) {
	live := Config{Mode: "live", BaseURL: "https://model.example", KeyRef: "vault://model/api_key"}
	tests := []struct {
		name    string
		cfg     Config
		env     string
		want    Mode
		wantErr error
	}{
		{"empty is mock", Config{}, "dev", ModeMock, nil},
		{"mock in ci", Config{Mode: "mock"}, "ci", ModeMock, nil},
		{"live in prod", live, "prod", ModeLive, nil},
		{"live in test", live, "test", "", ErrLiveModelForbidden},
		{"live in ci upper", live, " CI ", "", ErrLiveModelForbidden},
		{"live without base url", Config{Mode: "live", KeyRef: "vault://k"}, "prod", "", ErrLiveModelForbidden},
		{"live without key ref", Config{Mode: "live", BaseURL: "https://m"}, "prod", "", ErrLiveModelForbidden},
		{"unknown mode", Config{Mode: "replay"}, "dev", "", ErrUnknownMode},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.cfg.Check(tt.env)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil && err != nil) {
				t.Fatalf("Check err = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("Check mode = %q, want %q", got, tt.want)
			}
		})
	}
}
