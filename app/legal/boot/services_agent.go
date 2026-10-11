package boot

import (
	"fmt"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/featureset"
	agentrun "github.com/dagi377/counsel/kernel/agenttools/run"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/webhook"
)

// withAgentRuns fills the HTTP-25 run ports: with a kernel DB, a model and the agent tools,
// Runs is agent.RunStarter over the app's features, recorded in agent.run_record. RunAccess is the
// shared Access when it also builds agent principals. Ports already set are kept.
func (p HTTPPorts) withAgentRuns(k *kboot.Kernel) (HTTPPorts, error) {
	if p.RunAccess == nil {
		if ra, ok := p.Access.(agent.RunAccess); ok {
			p.RunAccess = ra
		}
	}
	if p.Runs != nil || k == nil || k.DB == nil || p.AgentModel == nil || p.AgentTools == nil {
		return p, nil
	}
	features, err := featureset.App()
	if err != nil {
		return p, fmt.Errorf("agent runs: features: %w", err)
	}
	r := &agent.RunStarter{Features: features, Model: p.AgentModel, Tools: p.AgentTools, Records: agentrun.PGRecords{DB: k.DB}}
	if k.Clock != nil {
		r.Now = k.Clock.Now
	}
	p.Runs = r
	return p, nil
}

// webhookMount is the endpoint routes (HTTP-26) over the Postgres reads and expiry store, or
// nil when a port is missing. PGExpiry renews only at the If-Match version.
func webhookMount(k *kboot.Kernel, p HTTPPorts, minApprovers int) mount {
	reads := p.WebhookEndpoints
	var expiry webhook.ExpiryStore
	if k != nil && k.DB != nil {
		if reads == nil {
			reads = webhook.PGEndpointReads{DB: k.DB}
		}
		expiry = webhook.PGExpiry{DB: k.DB}
	}
	if k != nil && k.WebhookExpiry != nil {
		expiry = k.WebhookExpiry
	}
	if reads == nil || expiry == nil || p.DB == nil || p.Gates == nil || p.Idem == nil {
		return nil
	}
	return (&webhook.HTTP{Endpoints: reads, Expiry: expiry, Access: p.Access, DB: p.DB, Gates: p.Gates, Idem: p.Idem,
		Cursors: p.Cursors, Problems: p.Problems, Principal: p.Principal, MinApprovers: minApprovers}).Routes
}
