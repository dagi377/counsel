package ai

import (
	"errors"
	"testing"
)

// rule: AI-R-001
func TestAI_R_001_ParseModeDefaultsToMock(t *testing.T) {
	tests := []struct {
		in      string
		want    Mode
		wantErr error
	}{
		{in: "", want: ModeMock},
		{in: "  ", want: ModeMock},
		{in: "mock", want: ModeMock},
		{in: "live", want: ModeLive},
		{in: "real", wantErr: ErrUnknownMode},
		{in: "LIVE", wantErr: ErrUnknownMode},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseMode(tc.in)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ParseMode(%q) err = %v, want %v", tc.in, err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("ParseMode(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// rule: AI-R-005
func TestAI_R_005_RequestNeedsFeatureID(t *testing.T) {
	tests := []struct {
		name    string
		req     Request
		embed   EmbedRequest
		wantErr error
	}{
		{name: "missing", wantErr: ErrNoFeature},
		{name: "blank", req: Request{FeatureID: " "}, embed: EmbedRequest{FeatureID: "\t"}, wantErr: ErrNoFeature},
		{name: "set", req: Request{FeatureID: "F-TIM-01"}, embed: EmbedRequest{FeatureID: "F-TIM-01"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.req.Validate(); !errors.Is(err, tc.wantErr) {
				t.Fatalf("Request.Validate() = %v, want %v", err, tc.wantErr)
			}
			if err := tc.embed.Validate(); !errors.Is(err, tc.wantErr) {
				t.Fatalf("EmbedRequest.Validate() = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
