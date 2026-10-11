package conflict_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/conflict"
	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/ai/aifake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/registry"
)

// conSecondLawyer is a second synthetic lawyer who may approve identity destruction.
const conSecondLawyer = "user-synthetic-records-lawyer"

// conSpecKind returns the configured kind for slug from the embedded spec/gates.json.
func conSpecKind(t *testing.T, slug string) gates.SpecKind {
	t.Helper()
	kinds, err := gates.ParseKinds(gates.SpecData())
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range kinds {
		if k.Code == slug {
			return k
		}
	}
	t.Fatalf("gate kind %s not in spec/gates.json", slug)
	return gates.SpecKind{}
}

// rule: ARC-I-019
// rule: ARC-I-039
// conflict.id_destruction is irreversible, so it is configured for two approvers and the
// kernel gate engine holds it pending until two different lawyers approve.
func TestARC_I_019_IDDestructionNeedsTwoApprovers(t *testing.T) {
	k := conSpecKind(t, conflict.GateIDDestruction)
	if k.Risk != registry.RiskIrreversible || k.MinApprovers != 2 {
		t.Fatalf("%s: risk %s, min approvers %d; want irreversible with 2", k.Code, k.Risk, k.MinApprovers)
	}

	fd, _ := conFeature(t, conflict.ExpiryRetentionDef())
	caps := gatefake.Caps{}
	for _, u := range []string{agenttest.FeatureActingFor, conOtherLawyer, conSecondLawyer} {
		caps[u] = []string{k.Approver}
	}
	effects := 0
	fd.Seed.Gates = gatefake.New(func() time.Time { return conDecideAt }, caps)
	if err := fd.Seed.Gates.Register(k.GateKind(func(context.Context, pgx.Tx, gate.Approved) error {
		effects++
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	read := conRead(fd, conMatter, con10Record)
	model := aifake.New(conScript("F-CON-10", "expiry-and-retention", con10Calls(read, conflict.GateIDDestruction), "Drafted the destruction batch for approval."))
	run := fd.Fire(context.Background(), model, "run-fcon10-two", "schedule: weekly")
	if run.Err != nil || len(run.Refused) != 0 || len(run.GateIDs) != 1 {
		t.Fatalf("run err = %v, refused = %v, gates = %v", run.Err, run.Refused, run.GateIDs)
	}
	id := run.GateIDs[0]
	user := func(u string) registry.Principal { return registry.Principal{Kind: registry.PrincipalUser, UserID: u} }

	// Each step runs after the previous one, on the same gate.
	steps := []struct {
		name       string
		by         registry.Principal
		wantErr    error
		wantHTTP   int
		wantStatus gate.Status
		wantEffect int
	}{
		{"acting user cannot approve own batch", user(agenttest.FeatureActingFor), gate.ErrSelfApproval, http.StatusForbidden, gate.StatusPending, 0},
		{"first lawyer approves: still pending", user(conOtherLawyer), nil, http.StatusOK, gate.StatusPending, 0},
		{"same lawyer again is refused", user(conOtherLawyer), gate.ErrAlreadyDecided, http.StatusConflict, gate.StatusPending, 0},
		{"different lawyer approves: approved", user(conSecondLawyer), nil, http.StatusOK, gate.StatusApproved, 1},
	}
	for _, s := range steps {
		err := conDecide(context.Background(), fd.Seed.Gates, id, s.by)
		if !errors.Is(err, s.wantErr) || conStatus(err) != s.wantHTTP {
			t.Fatalf("%s: decide = %v (%d), want %v (%d)", s.name, err, conStatus(err), s.wantErr, s.wantHTTP)
		}
		g, err := fd.Seed.Gates.Get(context.Background(), gate.GateID(id))
		if err != nil {
			t.Fatal(err)
		}
		if g.Status != s.wantStatus || effects != s.wantEffect {
			t.Fatalf("%s: status %s, effects %d; want %s, %d", s.name, g.Status, effects, s.wantStatus, s.wantEffect)
		}
	}
}
