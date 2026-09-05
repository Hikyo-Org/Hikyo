package isolation

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/app"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// approvalPeer opens independent connection pools on the SAME datastore. The
// SQLite leg exercises independent writers, not HA deployment support; real HA
// deployments require PostgreSQL. No process-local service state is shared.
func approvalPeer(t *testing.T, db *store.DB) *store.DB {
	t.Helper()
	cfg := store.Config{Engine: db.Engine()}
	if db.Engine() == store.EnginePostgres {
		cfg.DSN = db.PG().Config().ConnString()
	} else {
		var sequence int
		var name string
		if err := db.SQLiteRead().QueryRowContext(t.Context(), "PRAGMA database_list").Scan(&sequence, &name, &cfg.Path); err != nil {
			t.Fatal(err)
		}
	}
	peer, err := store.Open(t.Context(), cfg, isolationAdmission(t, db))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { peer.Close() })
	root, err := (probeRootSource{db: db}).Current(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	loadAndRegisterKeyring(t, peer, root)
	return peer
}

type approvalNode struct {
	approvals *service.Approvals
	revisions *service.Revisions
	values    *service.Values
}

func newApprovalNode(t *testing.T, db *store.DB) approvalNode {
	t.Helper()
	kr, auth := probeKeyring(t, db), authService(t, db)
	return approvalNode{
		approvals: &service.Approvals{DB: db, Auth: auth, Keyring: kr},
		revisions: &service.Revisions{DB: db, Auth: auth, Keyring: kr},
		values:    &service.Values{DB: db, Auth: auth, Keyring: kr},
	}
}

// Both keys exist before any request pins its target, so later tests move only
// the value revision, not the schema or policy.
func approvalAcceptanceFixture(t *testing.T, db *store.DB, quorum int) (approvalNode, approvalNode, domain.Scope) {
	t.Helper()
	ctx := t.Context()
	a, b := newApprovalNode(t, db), newApprovalNode(t, approvalPeer(t, db))
	project := domain.Scope{Org: orgA, Project: prjA1}
	env, err := (&service.Environments{DB: db, Keyring: a.approvals.Keyring}).Create(ctx, service.LocalPrincipal(alice), project, "approval-acceptance", nil)
	if err != nil {
		t.Fatal(err)
	}
	scope := domain.Scope{Org: orgA, Project: prjA1, Env: domain.EnvID(env.ID)}
	for _, name := range []string{"ACCEPTANCE_A", "ACCEPTANCE_B"} {
		if _, err := (&service.Keys{DB: db, Keyring: a.approvals.Keyring}).Create(ctx, service.LocalPrincipal(alice), project, service.KeySpec{
			Name: name, Classification: string(schema.Config),
			Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString}},
			Presence:    schema.DefaultPresenceRules(),
		}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := (&service.Grants{DB: db}).Create(ctx, service.LocalPrincipal(orgAdmin), service.GrantSpec{
		Target: bob, Capability: domain.CapPublish, Scope: domain.Scope{Org: orgA},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.approvals.CreatePolicy(ctx, service.LocalPrincipal(orgAdmin), project, service.ApprovalPolicyInput{
		EnvironmentID: env.ID, MinApprovals: quorum, RequestTTLSeconds: 3600, Enabled: true,
		Approvers: []service.ApprovalApproverSpec{
			{Kind: "principal", SubjectID: string(custodian)},
			{Kind: "principal", SubjectID: string(bob)},
		},
	}); err != nil {
		t.Fatal(err)
	}
	return a, b, scope
}

func (n approvalNode) request(t *testing.T, scope domain.Scope, key string) string {
	t.Helper()
	staged, err := n.values.Set(t.Context(), service.LocalPrincipal(alice), scope, key, "reviewed-value", nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := n.revisions.PublishPlanned(t.Context(), service.LocalPrincipal(alice), scope,
		service.PublishRequest{VersionIDs: []string{staged.VersionID}})
	if err != nil || result.CreatedApprovalRequest == nil {
		t.Fatalf("create request: result=%+v err=%v", result, err)
	}
	return result.CreatedApprovalRequest.ID
}

func approvalAuditCount(t *testing.T, db *store.DB, request, event string) int64 {
	t.Helper()
	return queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE object_id = '"+request+"' AND type = '"+event+"'")
}

// A transport retry may arrive on another replica, concurrently with the first
// delivery or after its response was lost. Both must return the same decision,
// with one vote row and one audit event. Two independent voters then race the
// final quorum, followed by duplicate merge delivery on both replicas.
func TestApprovalVoteRetryAcrossNodes(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		a, b, scope := approvalAcceptanceFixture(t, db, 2)
		request := a.request(t, scope, "ACCEPTANCE_A")
		runVotes := func(actors []domain.PrincipalID) {
			t.Helper()
			start := make(chan struct{})
			results := make(chan error, 2)
			for i, node := range []approvalNode{a, b} {
				go func() {
					<-start
					_, err := node.approvals.Vote(t.Context(), service.LocalPrincipal(actors[i]), scope, request, "approve")
					results <- err
				}()
			}
			close(start)
			for range 2 {
				if err := <-results; err != nil {
					t.Fatalf("cross-node vote delivery: %v", err)
				}
			}
		}
		runVotes([]domain.PrincipalID{custodian, custodian})
		if state := requestState(t, db, request); state != "open" {
			t.Fatalf("duplicate voter reached quorum: state=%s", state)
		}
		if got := approvalAuditCount(t, db, request, "approval.voted"); got != 1 {
			t.Fatalf("duplicate vote audit events=%d, want 1", got)
		}
		// A second request lets two distinct voters race from zero votes, on
		// independent pools, rather than racing one retry against a new vote.
		request = a.request(t, scope, "ACCEPTANCE_B")
		runVotes([]domain.PrincipalID{custodian, bob})
		if state := requestState(t, db, request); state != "approved" {
			t.Fatalf("distinct voters did not reach quorum: state=%s", state)
		}
		if got := queryInt(t, db, "SELECT COUNT(*) FROM approval_votes WHERE request_id = '"+request+"'"); got != 2 {
			t.Fatalf("vote rows=%d, want 2", got)
		}
		if got := approvalAuditCount(t, db, request, "approval.voted"); got != 2 {
			t.Fatalf("vote audit events=%d, want 2", got)
		}
		if _, err := b.approvals.Vote(t.Context(), service.LocalPrincipal(custodian), scope, request, "approve"); err != nil {
			t.Fatalf("retry after quorum on the other node: %v", err)
		}
		if got := approvalAuditCount(t, db, request, "approval.voted"); got != 2 {
			t.Fatalf("quorum retry duplicated audit: events=%d, want 2", got)
		}
		start, results := make(chan struct{}), make(chan error, 2)
		for _, node := range []approvalNode{a, b} {
			go func() {
				<-start
				_, err := node.revisions.PublishPlanned(t.Context(), service.LocalPrincipal(alice), scope,
					service.PublishRequest{ApprovalRequestID: request})
				results <- err
			}()
		}
		close(start)
		commits, conflicts := 0, 0
		for range 2 {
			switch err := <-results; {
			case err == nil:
				commits++
			case errors.Is(err, domain.ErrConflict):
				conflicts++
			default:
				t.Fatalf("cross-node merge delivery: %v", err)
			}
		}
		if commits != 1 || conflicts != 1 || approvalAuditCount(t, db, request, "approval.merged") != 1 {
			t.Fatalf("duplicate merge: commits=%d conflicts=%d, want exactly one of each and one audit", commits, conflicts)
		}
	})
}

func TestApprovalTargetMovementInvalidation(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		a, b, scope := approvalAcceptanceFixture(t, db, 1)
		stale := a.request(t, scope, "ACCEPTANCE_A")
		advance := b.request(t, scope, "ACCEPTANCE_B")
		for _, request := range []string{stale, advance} {
			if _, err := b.approvals.Vote(t.Context(), service.LocalPrincipal(custodian), scope, request, "approve"); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := b.revisions.PublishPlanned(t.Context(), service.LocalPrincipal(alice), scope,
			service.PublishRequest{ApprovalRequestID: advance}); err != nil {
			t.Fatalf("advance target through an approved publish: %v", err)
		}
		before := rowCounts(t, db)
		for range 2 {
			if _, err := a.revisions.PublishPlanned(t.Context(), service.LocalPrincipal(alice), scope,
				service.PublishRequest{ApprovalRequestID: stale}); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("merge after target movement=%v, want conflict", err)
			}
		}
		if state := requestState(t, db, stale); state != "invalidated" {
			t.Fatalf("stale state=%s, want invalidated", state)
		}
		if cause := queryString(t, db, "SELECT invalidated_cause FROM approval_requests WHERE id = '"+stale+"'"); cause != "env_advanced" {
			t.Fatalf("invalidation cause=%s, want env_advanced", cause)
		}
		if got := approvalAuditCount(t, db, stale, "approval.invalidated"); got != 1 {
			t.Fatalf("invalidation audit events=%d, want 1", got)
		}
		for table, count := range before {
			if got := queryInt(t, db, "SELECT COUNT(*) FROM "+table); got != count {
				t.Errorf("stale merge changed %s: %d -> %d", table, count, got)
			}
		}
	})
}

func TestApprovalExpirySchedulerTakeover(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		a, b, scope := approvalAcceptanceFixture(t, db, 1)
		request := a.request(t, scope, "ACCEPTANCE_A")
		future := time.Now().UTC().Add(2 * time.Hour)
		a.approvals.Now = func() time.Time { return future }
		b.approvals.Now = func() time.Time { return future }
		startNode := func(node approvalNode, name string) (context.Context, func()) {
			t.Helper()
			terms := make(chan context.Context, 1)
			scheduler := &app.Scheduler{
				Lease: node.approvals.DB.Coordination(), NodeID: name, LeaseTTL: time.Minute,
				Heartbeat: 20 * time.Millisecond, Interval: time.Hour,
				Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
				Jobs: []app.ScheduledJob{{Name: "approval_expiry_sweep", Run: func(ctx context.Context) error {
					// Retain the real term identity while deliberately suppressing
					// cancellation, modeling a paused worker before its next write.
					select {
					case terms <- context.WithoutCancel(ctx):
					default:
					}
					return nil
				}}},
			}
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan struct{})
			go func() { defer close(done); scheduler.Run(ctx) }()
			var once sync.Once
			stop := func() { once.Do(func() { cancel(); <-done }) }
			t.Cleanup(stop)
			select {
			case term := <-terms:
				return term, stop
			case <-time.After(5 * time.Second):
				t.Fatal("scheduler did not acquire leadership")
				return nil, stop
			}
		}
		oldTerm, stopA := startNode(a, "approval-node-a")
		oldFence := queryInt(t, db, "SELECT fence_token FROM singleton_leases WHERE name = 'scheduler'")
		stopA()
		newTerm, _ := startNode(b, "approval-node-b")
		if fence := queryInt(t, db, "SELECT fence_token FROM singleton_leases WHERE name = 'scheduler'"); fence <= oldFence {
			t.Fatalf("takeover fence=%d, want > %d", fence, oldFence)
		}
		if err := a.approvals.ExpireDue(oldTerm); !errors.Is(err, store.ErrSingletonLeaseLost) {
			t.Fatalf("uncancelled stale leader expiry=%v, want lease lost", err)
		}
		if state := requestState(t, db, request); state != "open" || approvalAuditCount(t, db, request, "approval.expired") != 0 {
			t.Fatalf("stale leader changed request: state=%s", state)
		}
		start, results := make(chan struct{}), make(chan error, 2)
		for _, work := range []struct {
			node approvalNode
			ctx  context.Context
		}{{a, oldTerm}, {b, newTerm}} {
			go func() { <-start; results <- work.node.approvals.ExpireDue(work.ctx) }()
		}
		close(start)
		for range 2 {
			if err := <-results; err != nil && !errors.Is(err, store.ErrSingletonLeaseLost) {
				t.Fatalf("raced expiry: %v", err)
			}
		}
		if err := b.approvals.ExpireDue(newTerm); err != nil {
			t.Fatalf("takeover retry: %v", err)
		}
		if state := requestState(t, db, request); state != "expired" || approvalAuditCount(t, db, request, "approval.expired") != 1 {
			t.Fatalf("takeover expiry/retry: state=%s, want expired and one audit", state)
		}
	})
}

// A fence guard must hold its row lock through the SAME transaction as the
// tenant mutation. A preflight lookup alone would let takeover slip between
// validation and commit. ClaimLease uses a future instant to deterministically
// model expiry while the old transaction is paused, without waiting a TTL.
func TestApprovalFenceSerializesTakeover(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		a, b, _ := approvalAcceptanceFixture(t, db, 1)
		now := time.Now().UTC()
		fence, held, err := db.Coordination().ClaimLease(t.Context(), "approval-fence-test", "node-a", now, now.Add(time.Minute))
		if err != nil || !held {
			t.Fatalf("claim: held=%v err=%v", held, err)
		}
		term := store.WithSingletonLease(t.Context(), "approval-fence-test", "node-a", fence)
		entered, release, writeDone := make(chan struct{}), make(chan struct{}), make(chan error, 1)
		var releaseOnce sync.Once
		releaseWrite := func() { releaseOnce.Do(func() { close(release) }) }
		t.Cleanup(releaseWrite)
		go func() {
			writeDone <- tx.Write(term, a.approvals.DB, func(context.Context, store.Repos, *authz.TxAuthorizer) error {
				close(entered)
				<-release
				return nil
			})
		}()
		select {
		case <-entered:
		case err := <-writeDone:
			t.Fatalf("fenced write did not start: %v", err)
		}
		takeover := make(chan error, 1)
		go func() {
			newFence, held, err := b.approvals.DB.Coordination().ClaimLease(t.Context(), "approval-fence-test", "node-b", now.Add(2*time.Minute), now.Add(3*time.Minute))
			if err == nil && (!held || newFence <= fence) {
				err = errors.New("takeover did not advance the fence")
			}
			takeover <- err
		}()
		select {
		case err := <-takeover:
			t.Fatalf("takeover escaped the in-flight transaction lock: %v", err)
		case <-time.After(50 * time.Millisecond):
		}
		releaseWrite()
		if err := <-writeDone; err != nil {
			t.Fatal(err)
		}
		if err := <-takeover; err != nil {
			t.Fatal(err)
		}
		if err := a.approvals.ExpireDue(term); !errors.Is(err, store.ErrSingletonLeaseLost) {
			t.Fatalf("old term after takeover=%v, want lease lost", err)
		}
	})
}

func TestApprovalExpiryRejectsExpiredAndReusedOwner(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		a, b, scope := approvalAcceptanceFixture(t, db, 1)
		request := a.request(t, scope, "ACCEPTANCE_A")
		future := time.Now().UTC().Add(2 * time.Hour)
		a.approvals.Now = func() time.Time { return future }
		b.approvals.Now = func() time.Time { return future }
		now := time.Now().UTC()
		oldFence, held, err := db.Coordination().ClaimLease(t.Context(), "scheduler", "restarted-node", now.Add(-time.Hour), now.Add(-time.Minute))
		if err != nil || !held {
			t.Fatalf("seed expired lease: held=%v err=%v", held, err)
		}
		oldTerm := store.WithSingletonLease(t.Context(), "scheduler", "restarted-node", oldFence)
		if err := a.approvals.ExpireDue(oldTerm); !errors.Is(err, store.ErrSingletonLeaseLost) {
			t.Fatalf("expired lease without successor=%v, want lease lost", err)
		}
		newFence, held, err := b.approvals.DB.Coordination().ClaimLease(t.Context(), "scheduler", "restarted-node", now, now.Add(time.Minute))
		if err != nil || !held || newFence <= oldFence {
			t.Fatalf("reused owner claim: held=%v fence=%d err=%v", held, newFence, err)
		}
		if err := a.approvals.ExpireDue(oldTerm); !errors.Is(err, store.ErrSingletonLeaseLost) {
			t.Fatalf("same owner with old fence=%v, want lease lost", err)
		}
		if state := requestState(t, db, request); state != "open" || approvalAuditCount(t, db, request, "approval.expired") != 0 {
			t.Fatalf("expired/superseded term changed request: state=%s", state)
		}
		newTerm := store.WithSingletonLease(t.Context(), "scheduler", "restarted-node", newFence)
		if err := b.approvals.ExpireDue(newTerm); err != nil {
			t.Fatal(err)
		}
		if state := requestState(t, db, request); state != "expired" || approvalAuditCount(t, db, request, "approval.expired") != 1 {
			t.Fatalf("new term did not expire exactly once: state=%s", state)
		}
	})
}
