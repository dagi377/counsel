package boot

import (
	"context"
	"fmt"

	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/kernel/agenttools"
	agentrun "github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/registry"
)

// BankFeedSyncJob is F-TRU-01's pipeline job as boot wires it: the bank-feed pipeline over
// the model and embedder, with trust.ReceiptGates as its sink, calling the kernel agent
// registry as the run's agent principal (docs/QUESTIONS.md Q-FEAT-TRU-02, Q-TRU-FIX).
type BankFeedSyncJob struct {
	Model    ai.ModelClient
	Embedder ai.Embedder
	// Tools is the kernel agent registry: the pipeline's loop and the sink call through it,
	// so capability, schema, plan-before-write and run scope are checked there.
	Tools agenttools.Tools
}

// Run runs one F-TRU-01 job for runID acting for actingFor on matterID's quoted bank-feed
// lines. The run's scope holds the Def's gate kinds only (trust.receipt_classification), and
// the agent principal holds the Def's grant, so it prepares and never decides. A missing
// port fails closed before any model call.
func (j BankFeedSyncJob) Run(ctx context.Context, runID, actingFor, matterID string, lines []agenttools.Quoted) ([]trust.ProposedReceipt, error) {
	def := trust.BankFeedSyncDef()
	// rule: ARC-U-027
	// Fail closed: without every port, a run id and an acting user nothing runs.
	if j.Model == nil || j.Embedder == nil || j.Tools == nil {
		return nil, fmt.Errorf("%s job: %w", def.ID, trust.ErrPortMissing)
	}
	if runID == "" || actingFor == "" || matterID == "" {
		return nil, fmt.Errorf("%s job: run, acting user and matter: %w", def.ID, trust.ErrPortMissing)
	}
	// rule: ARC-I-035
	// The run's scope allows the Def's gate kinds only.
	ctx = agenttools.WithScope(ctx, agenttools.NewScope(runID, def.GateKinds))
	sink := trust.ReceiptGates{
		Tools:    j.Tools,
		Run:      registry.Principal{Kind: registry.PrincipalAgent, RunID: runID, ActingFor: actingFor, Grant: def.Grant},
		MatterID: matterID,
		Sources:  lines,
	}
	p := trust.BankFeedPipeline{Loop: agentrun.New(j.Model, j.Tools), Embedder: j.Embedder, Sink: sink}
	return p.Run(ctx, lines)
}
