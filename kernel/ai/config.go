package ai

import (
	"fmt"
	"strings"
)

// Config is the model section of a deployment config (spec/ai/mocks.md sec. 8).
type Config struct {
	Mode     string            `yaml:"mode"`     // mock | live; empty = mock
	BaseURL  string            `yaml:"base_url"` // mock: empty = in-process fake
	Protocol string            `yaml:"protocol"` // messages | chat | responses
	Models   map[string]string `yaml:"models"`   // logical name -> provider id
	KeyRef   string            `yaml:"key_ref"`  // live only: vault reference, never the key
	// BudgetMicroCentsPerFeatureDay caps spend per feature and day; 0 = unlimited.
	BudgetMicroCentsPerFeatureDay int64 `yaml:"budget_micro_cents_per_feature_day"`
}

// Environments in which a live model is never allowed.
var forbiddenLiveEnvs = map[string]bool{"test": true, "ci": true}

// Check validates the config for the deployment environment env (the value of the
// environment's COUNSEL_ENV-style setting) and returns the resolved mode. A live mode in a
// test or CI environment, or without base_url or key_ref, fails with ErrLiveModelForbidden.
func (c Config) Check(env string) (Mode, error) {
	mode, err := ParseMode(c.Mode)
	if err != nil {
		return "", err
	}
	if mode == ModeMock {
		return ModeMock, nil
	}
	// rule: AI-R-001
	e := strings.ToLower(strings.TrimSpace(env))
	switch {
	case forbiddenLiveEnvs[e]:
		return "", fmt.Errorf("model config: environment %q: %w", e, ErrLiveModelForbidden)
	case strings.TrimSpace(c.BaseURL) == "":
		return "", fmt.Errorf("model config: live mode needs base_url: %w", ErrLiveModelForbidden)
	case strings.TrimSpace(c.KeyRef) == "":
		return "", fmt.Errorf("model config: live mode needs key_ref: %w", ErrLiveModelForbidden)
	}
	return ModeLive, nil
}
