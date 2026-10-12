package boot

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"

	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	"github.com/dagi377/counsel/kernel/ai/aifake"
)

// rule: ARC-U-027
// The F-TRU-01 job fails closed without a port, a run id, an acting user or a matter: no
// model call is made and nothing is proposed.
func TestBankFeedSyncJobFailsClosed(t *testing.T) {
	model := aifake.New(fstest.MapFS{})
	tools := agenttools.NewRegistry(agenttoolsfake.New())
	full := BankFeedSyncJob{Model: model, Embedder: model, Tools: tools}
	tests := []struct {
		name                     string
		job                      BankFeedSyncJob
		run, actingFor, matterID string
	}{
		{"no model", BankFeedSyncJob{Embedder: model, Tools: tools}, "run-1", "user-1", "matter-1"},
		{"no embedder", BankFeedSyncJob{Model: model, Tools: tools}, "run-1", "user-1", "matter-1"},
		{"no tools", BankFeedSyncJob{Model: model, Embedder: model}, "run-1", "user-1", "matter-1"},
		{"no run id", full, "", "user-1", "matter-1"},
		{"no acting user", full, "run-1", "", "matter-1"},
		{"no matter", full, "run-1", "user-1", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, err := tt.job.Run(context.Background(), tt.run, tt.actingFor, tt.matterID, nil)
			if !errors.Is(err, trust.ErrPortMissing) || items != nil {
				t.Fatalf("items = %v, err = %v, want ErrPortMissing", items, err)
			}
		})
	}
}
