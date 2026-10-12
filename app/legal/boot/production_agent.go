package boot

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/billing/batch"
	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/app/legal/billing/draft"
	legaldocs "github.com/dagi377/counsel/app/legal/docs"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/party"
	tk "github.com/dagi377/counsel/app/legal/timekeeping"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	"github.com/dagi377/counsel/kernel/agenttools"
	agentrun "github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/ai/httpclient"
	kboot "github.com/dagi377/counsel/kernel/boot"
	kdb "github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/deadline"
	kdocs "github.com/dagi377/counsel/kernel/docs"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/work"
	"github.com/jackc/pgx/v5"
)

// Readiness reasons of an agent feature that cannot run.
const (
	// ReasonNoModel: no model provider is configured, or its config does not resolve.
	ReasonNoModel = "no_model"
	// ReasonNoFirmAccount: the firm general account (payee of fee transfers) is not configured.
	ReasonNoFirmAccount = "no_firm_account"
	// ReasonToolUnwired: a tool the feature calls has a port with no implementation in this
	// process, so the tool is not served.
	ReasonToolUnwired = "tool_unwired"
	// ReasonNoJobQueue: the feature runs as a queued job and this process has no job queue.
	ReasonNoJobQueue = "no_job_queue"
)

// AgentReadiness collects why agent features cannot run in this process. Boot fills it; the
// readiness check reads it. The zero value is ready to fill.
type AgentReadiness struct {
	mu       sync.Mutex
	unready  []trigger.Unready
	bindings *trigger.Bindings
}

// Unready lists every agent feature that cannot run, with its reason, followed by the
// scheduled features with no designated lawyer. Empty means every agent feature can run.
func (r *AgentReadiness) Unready(ctx context.Context) ([]trigger.Unready, error) {
	if r == nil {
		return nil, nil
	}
	r.mu.Lock()
	out := slices.Clone(r.unready)
	b := r.bindings
	r.mu.Unlock()
	if b != nil {
		more, err := b.Readiness(ctx)
		if err != nil {
			return nil, fmt.Errorf("agent readiness: %w", err)
		}
		out = append(out, more...)
	}
	return out, nil
}

// Ready reports whether every agent feature can run.
func (r *AgentReadiness) Ready(ctx context.Context) (bool, error) {
	u, err := r.Unready(ctx)
	return len(u) == 0, err
}

func (r *AgentReadiness) add(featureID, reason string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	u := trigger.Unready{FeatureID: featureID, Reason: reason}
	if !slices.Contains(r.unready, u) {
		r.unready = append(r.unready, u)
	}
}

// addAll marks every feature of s unready for reason, or only those keep selects.
func (r *AgentReadiness) addAll(s *feature.Set, reason string, keep func(feature.Def) bool) {
	if s == nil {
		return
	}
	for _, d := range s.All() {
		if keep == nil || keep(d) {
			r.add(d.ID, reason)
		}
	}
}

func (r *AgentReadiness) setBindings(b *trigger.Bindings) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.bindings = b
	r.mu.Unlock()
}

// agentModel is the model client the agent runs on. ModelClient wins; else Model is resolved
// through httpclient.FromConfig. With no model configured outside dev, or a config that does
// not resolve, it returns nil and the agent features report not ready; boot never stops on
// it. A dev process with no config runs the in-process mock.
func (p Production) agentModel(ctx context.Context) ai.ModelClient {
	if p.ModelClient != nil {
		return p.ModelClient
	}
	cfg := p.Model
	if cfg == nil {
		if !p.Dev {
			slog.Warn("no model provider configured: agent features are not ready")
			return nil
		}
		cfg = &ai.Config{Mode: string(ai.ModeMock)}
	}
	env := p.ModelEnv
	if env == "" && p.Dev {
		env = "dev"
	}
	m, err := httpclient.FromConfig(ctx, *cfg, env, p.modelFixtures(), p.ModelKey)
	if err != nil {
		// The error names the mode and the missing field only; the key never reaches it.
		slog.Warn("model provider not usable: agent features are not ready", "error_type", fmt.Sprintf("%T", err))
		return nil
	}
	return m
}

func (p Production) modelFixtures() fs.FS {
	if p.ModelFixtures != nil {
		return p.ModelFixtures
	}
	return emptyFS{}
}

// emptyFS holds no fixture: the in-process mock then answers no feature.
type emptyFS struct{}

func (emptyFS) Open(name string) (fs.File, error) {
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

// bindAgent builds the agent layer on k: the ports over Postgres, the model, the tool
// registry the runs call, the job bindings, the action route and the HTTP-25 run ports. A
// wiring the caller set keeps its ports; boot fills the nil ones. With no model the agent
// layer is left out and readiness reports every agent feature not ready.
func (p Production) bindAgent(ctx context.Context, k *kboot.Kernel, opts *Options, preset *AgentWiring) (*AgentWiring, error) {
	features, err := Features()
	if err != nil {
		return nil, err
	}
	ready := p.Readiness
	if ready == nil {
		ready = &AgentReadiness{}
	}
	model := p.agentModel(ctx)
	if model == nil {
		ready.addAll(features, ReasonNoModel, nil)
		return nil, nil
	}
	if opts.HTTP == nil {
		return nil, nil
	}
	if strings.TrimSpace(p.FirmAccount) == "" && (preset == nil || preset.Write.FirmAccount == nil) {
		slog.Warn("no firm general account configured: agent features are not ready")
		ready.addAll(features, ReasonNoFirmAccount, nil)
		return nil, nil
	}
	w := preset
	if w == nil {
		w = &AgentWiring{}
	}
	if err := p.agentPorts(k, opts, w); err != nil {
		return nil, fmt.Errorf("agent: %w", err)
	}
	if w.Ports.WriteTools == nil {
		wt, err := AgentWriteTools(w.Ports, w.Write)
		if err != nil {
			return nil, err
		}
		w.Ports.WriteTools = wt
	}
	built, err := servedTools(w.Ports, ready, features)
	if err != nil {
		return nil, err
	}
	tools := agenttools.NewRegistry(w.Ports.Plans)
	for _, t := range built {
		if err := tools.Register(t); err != nil {
			return nil, fmt.Errorf("agent tools: %w", err)
		}
	}
	w.Unready = ready
	runner := ForgetOnEnd(w.Ports.Provenance, agentrun.New(model, tools))
	if w.Actions == nil {
		w.Actions = &trigger.Actions{}
	}
	if w.Actions.Runner == nil {
		w.Actions.Runner = runner
	}
	if w.Actions.Access == nil {
		if a, ok := opts.HTTP.Access.(trigger.Access); ok {
			w.Actions.Access = a
		}
	}
	if w.Actions.NewRunID == nil {
		w.Actions.NewRunID = newRunID
	}
	if w.Actions.Principal == nil {
		w.Actions.Principal = opts.HTTP.Principal
	}
	if w.Actions.Problems == nil {
		w.Actions.Problems = opts.HTTP.Problems
	}
	jobs := p.jobs(k)
	if w.Queue == nil {
		// rule: AI-R-005
		// Without a job queue a queued feature fails closed (500) and readiness names it.
		w.Queue = RiverActionQueue{Q: jobs, NewRunID: newRunID}
	}
	if w.Bindings == nil && jobs != nil && k.Clock != nil {
		w.Bindings = trigger.New(jobs, k.Clock, nil)
	}
	if jobs == nil {
		ready.addAll(features, ReasonNoJobQueue, func(d feature.Def) bool {
			return d.Source == feature.SourceJob || d.Kind != ai.KindChat
		})
	}
	ready.setBindings(w.Bindings)
	// The agent_feature_run worker runs on the same model and tools as the action route.
	if w.RunModel == nil {
		w.RunModel = model
	}
	if w.RunTools == nil {
		w.RunTools = tools
	}
	opts.HTTP.AgentModel = model
	opts.HTTP.AgentTools = tools
	return w, nil
}

// jobs is the enqueuer the agent queues runs on: Jobs when set, else the kernel's
// insert-only job client. Nil when neither exists.
func (p Production) jobs(k *kboot.Kernel) trigger.Enqueuer {
	if p.Jobs != nil {
		return p.Jobs
	}
	if k != nil && k.Enqueuer != nil {
		return k.Enqueuer
	}
	return nil
}

// servedTools builds every handler tool over p. A tool whose port has no implementation in
// this process is not served: every feature that calls it is reported not ready instead of
// stopping boot. Any other build error fails boot.
func servedTools(p AgentPorts, ready *AgentReadiness, features *feature.Set) ([]registry.Tool, error) {
	set := agent.Handlers()
	deps := p.Deps()
	var out []registry.Tool
	for _, name := range set.Names() {
		h, _ := set.For(name)
		t, err := h.Build(deps)
		if err != nil {
			if !errors.Is(err, agent.ErrMissingDep) {
				return nil, fmt.Errorf("agent tool %s: %w", name, err)
			}
			slog.Warn("agent tool not served: a port has no implementation", "tool", name)
			for _, d := range features.All() {
				if slices.Contains(d.Tools, name) {
					ready.add(d.ID, ReasonToolUnwired)
				}
			}
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

// agentPorts fills every nil port of w from Postgres on k: the module stores, the matter-side
// ports (WireMatterPorts) and the billing and trust ports (WireBillingAgentPorts).
func (p Production) agentPorts(k *kboot.Kernel, opts *Options, w *AgentWiring) error {
	if k.DB == nil || k.Clock == nil {
		return fmt.Errorf("kernel DB and Clock: %w", ErrMissingDependency)
	}
	ports := &w.Ports
	if ports.Can == nil {
		ports.Can = capabilityCheck(opts.HTTP.Access)
	}
	if ports.Walls == nil {
		ports.Walls = wallsVisible{w: matter.NewPGWalls(k.DB)}
	}
	contacts := party.NewStore(k.DB)
	matters := matter.NewStore(k.DB, contacts)
	if ports.Contacts == nil {
		ports.Contacts = contacts
	}
	if ports.ClientMatters == nil {
		ports.ClientMatters = matters
	}
	if ports.Matters == nil {
		ports.Matters = matters
	}
	if ports.DocLookup == nil || ports.Folders == nil || ports.Documents == nil {
		folders, err := legaldocs.NewFolderStore(k.DB, nil)
		if err != nil {
			return fmt.Errorf("folders: %w", err)
		}
		if ports.DocLookup == nil {
			ports.DocLookup = folders
		}
		if ports.Folders == nil {
			ports.Folders = folders
		}
		if ports.Documents == nil {
			ports.Documents = filedDocuments{s: folders}
		}
	}
	if ports.Text == nil {
		ports.Text = kdocs.PGText{DB: k.DB}
	}
	if ports.Entries == nil {
		ports.Entries = tk.PGEntries{DB: k.DB, RateTable: matterRateTable(matters)}
	}
	if ports.DraftCalc == nil {
		ports.DraftCalc = draft.Calculator{}
	}
	if ports.DraftStore == nil || ports.BatchDrafts == nil {
		drafts := postgresDrafts(k, matters)
		if ports.DraftStore == nil {
			ports.DraftStore = draft.PGStore{DB: k.DB, Drafts: drafts}
		}
		if ports.BatchDrafts == nil {
			ports.BatchDrafts = draft.PGBatch{DB: k.DB, Drafts: drafts, PreparedBy: runActingFor(k.DB)}
		}
	}
	items := &work.PG{DB: k.DB}
	if ports.Work == nil {
		ports.Work = items
	}
	if ports.Tasks == nil {
		ports.Tasks = items
	}
	if ports.Bills == nil {
		ports.Bills = bill.NewPGService(k.DB, k.Clock.Now)
	}
	ledgers := trust.NewLedgerStore(k.DB, nil, nil, k.Clock)
	wd := withdraw.PGService{DB: k.DB, Ledgers: ledgers}
	if g, ok := k.Gates.(withdraw.Gates); ok {
		wd.Gates = g
	}
	if ports.Trust == nil {
		ports.Trust = wd
	}
	if w.Write.Withdrawals == nil {
		w.Write.Withdrawals = wd
	}
	if ports.TrustTransfers == nil && wd.Gates != nil {
		// prepare_trust_transfer records the transfer pending authorization and opens its
		// trust.transfer gate in one tx; with no gate opener the tool is not served.
		ports.TrustTransfers = withdraw.TransferRequests{DB: k.DB, Store: withdraw.PGTransferStore{}, Gates: wd.Gates}
	}
	if ports.Payments == nil {
		ports.Payments = opts.HTTP.Payments
	}
	if ports.Payments == nil {
		// The HTTP ports fill Payments when the routes mount, after the agent is bound.
		ports.Payments = pgPayments(k)
	}
	book := deadline.NewPGBook(k.DB)
	if ports.Deadlines == nil {
		ports.Deadlines = book
	}
	if ports.DeadlineCompute == nil {
		ports.DeadlineCompute = book
	}
	if ports.DeadlineSaver == nil {
		ports.DeadlineSaver = book
	}
	if ports.DeadlineGetter == nil {
		ports.DeadlineGetter = book
	}
	if ports.Conflicts == nil {
		// The agent runs checks on the Checks Clearance decides, never a copy of its own.
		if opts.Clearance != nil && opts.Clearance.Checks != nil {
			ports.Conflicts = opts.Clearance.Checks
		} else {
			ports.Conflicts = conflictChecks(k)
		}
	}
	if ports.OpenGate == nil && k.Gates != nil {
		ports.OpenGate = gateOpener(k)
	}
	bodies, err := p.draftBodies(k)
	if err != nil {
		return err
	}
	if err := WireMatterPorts(ports, k.DB, matters, bodies); err != nil {
		return err
	}
	if k.Emitter != nil {
		SetMatterPortEvents(ports, k.Emitter)
	}
	if ports.BillBalances == nil || w.Write.Bills == nil {
		src, err := NewBillingAgentSources(k.DB, p.FirmAccount)
		if err != nil {
			return err
		}
		if err := WireBillingAgentPorts(src, ports, &w.Write); err != nil {
			return err
		}
	}
	return nil
}

// capabilityCheck checks a user's capability at firm level through the shared Access.
func capabilityCheck(a Access) agent.CapabilityCheck {
	return func(ctx context.Context, userID, capability string) (bool, error) {
		if a == nil {
			return false, fmt.Errorf("capability check: %w", agent.ErrMissingDep)
		}
		u := registry.Principal{Kind: registry.PrincipalUser, UserID: userID}
		return a.Can(ctx, u, capability, registry.Subject{Kind: FirmSubject})
	}
}

// wallsVisible reads walls through matter.PGWalls: a walled matter is not visible.
type wallsVisible struct{ w *matter.PGWalls }

// Visible implements roles.Walls.
func (v wallsVisible) Visible(ctx context.Context, userID, matterID string) (bool, error) {
	// rule: ARC-I-036
	walled, err := v.w.Walled(ctx, userID, matterID)
	if err != nil {
		return false, err
	}
	return !walled, nil
}

// gateOpener opens a gate on the kernel gate engine in its own transaction.
func gateOpener(k *kboot.Kernel) agenttools.GateOpener {
	return func(ctx context.Context, kind string, subject registry.Subject, payload json.RawMessage, preparer registry.Principal) (gate.GateID, error) {
		var id gate.GateID
		err := k.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			var err error
			id, err = k.Gates.Open(ctx, tx, kind, subject, payload, preparer)
			return err
		})
		if err != nil {
			return "", fmt.Errorf("open gate %s: %w", kind, err)
		}
		return id, nil
	}
}

// filedDocuments lists a matter's filed documents through legaldocs.FolderStore.
type filedDocuments struct{ s *legaldocs.FolderStore }

// MatterDocuments implements agent.MatterDocuments.
func (f filedDocuments) MatterDocuments(ctx context.Context, matterID string) ([]agent.ListedDocument, error) {
	filed, err := f.s.Filed(ctx, matterID)
	if err != nil {
		return nil, err
	}
	out := make([]agent.ListedDocument, len(filed))
	for i, d := range filed {
		out[i] = agent.ListedDocument{Document: d.Document, VersionID: kdocs.VersionID(d.VersionID)}
	}
	return out, nil
}

// runActingForSQL reads the user an agent run acts for (agent.run_record, ids only).
const runActingForSQL = `SELECT acting_for::text FROM agent.run_record WHERE id = $1::uuid`

// runActingFor names the preparer of a draft bill an agent run builds: the user the run acts
// for, read from the run in ctx. A call outside a recorded run has no preparer and fails
// closed; the agent itself is never the preparer or the approver.
func runActingFor(pool kdb.DB) func(ctx context.Context, t batch.Target) (string, error) {
	return func(ctx context.Context, t batch.Target) (string, error) {
		s, ok := agenttools.ScopeOf(ctx)
		if !ok || s.RunID == "" {
			return "", fmt.Errorf("draft of %s: no agent run: %w", t.MatterID, agent.ErrMissingDep)
		}
		var user string
		err := pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, runActingForSQL, s.RunID).Scan(&user)
		})
		if err != nil {
			return "", fmt.Errorf("draft of %s: run %s: %w", t.MatterID, s.RunID, err)
		}
		return user, nil
	}
}

// matterRateTable names a matter's own rate table through matter.Query (ARC-I-041: the
// matter module's port, never its table). A matter without one gives "", so the entry is
// priced from the default rate table; an unknown matter is an error and prices nothing.
func matterRateTable(q matter.Query) func(ctx context.Context, matterID string) (string, error) {
	return func(ctx context.Context, matterID string) (string, error) {
		m, err := q.Get(ctx, matter.ID(matterID))
		if err != nil {
			return "", fmt.Errorf("matter %s: %w", matterID, err)
		}
		return m.RateTableID, nil
	}
}

// draftBodies is the agent draft body store: agent.draft_body on k.DB, sealed with the boot
// seal key. With no seal key it is unstoredBodies, so every drafting tool fails closed.
func (p Production) draftBodies(k *kboot.Kernel) (matter.Bodies, error) {
	if p.SealKey == nil {
		return unstoredBodies{}, nil
	}
	sealer, err := trust.NewAESSealer(*p.SealKey)
	if err != nil {
		return nil, fmt.Errorf("draft bodies: %w", err)
	}
	return PGDraftBodies{DB: k.DB, Sealer: sealer}, nil
}

// unstoredBodies has no store: a drafted body is refused, so its tool fails closed.
type unstoredBodies struct{}

// Put implements matter.Bodies.
func (unstoredBodies) Put(context.Context, string) (string, error) {
	return "", fmt.Errorf("draft body store: %w", agent.ErrMissingDep)
}

// newRunID is a random UUID (version 4) for a run started from a screen action.
func newRunID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
