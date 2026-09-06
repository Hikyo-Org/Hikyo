package service

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/operation"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

type installerProbe struct {
	mu               sync.Mutex
	prepared         []*installerActivationProbe
	prepareFailure   string
	activateFailures map[string]int
	activateHook     func(context.Context, string) error
	active           string
}

type installerActivationProbe struct {
	installer   *installerProbe
	value       string
	activations int
	closes      int
}

func (p *installerProbe) Prepare(_ context.Context, bundle *runtimeconfig.Bundle) (runtimeconfig.PreparedActivation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	value := bundle.OwnerValues()["HIKYO_ARGON2_TIME"]
	if value == p.prepareFailure {
		return nil, errors.New("injected preparation failure")
	}
	prepared := &installerActivationProbe{installer: p, value: value}
	p.prepared = append(p.prepared, prepared)
	return prepared, nil
}

func (p *installerActivationProbe) Activate(ctx context.Context) error {
	owner := p.installer
	owner.mu.Lock()
	p.activations++
	hook := owner.activateHook
	fail := owner.activateFailures[p.value] > 0
	if fail {
		owner.activateFailures[p.value]--
	}
	owner.mu.Unlock()
	if hook != nil {
		if err := hook(ctx, p.value); err != nil {
			return err
		}
	}
	if fail {
		return errors.New("injected activation failure")
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	owner.active = p.value
	return nil
}

func (p *installerActivationProbe) Close() error {
	p.installer.mu.Lock()
	defer p.installer.mu.Unlock()
	p.closes++
	// Closing preparation never tears down a transferred application.
	return nil
}

func (p *installerProbe) stats(value string) (prepared, activations, closes int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, attempt := range p.prepared {
		if attempt.value == value {
			prepared++
			activations += attempt.activations
			closes += attempt.closes
		}
	}
	return
}

func installerFixture(t *testing.T, engine store.Engine) (*SelfConfig, Actor, *installerProbe) {
	t.Helper()
	cfg := store.Config{Engine: engine, Path: filepath.Join(t.TempDir(), "installer.db")}
	if engine == store.EnginePostgres {
		cfg = selfConfigPostgres(t)
	}
	s, actor := selfConfigFixtureConfig(t, cfg, map[string]string{"HIKYO_UPDATE_CHANNEL": "nightly", "HIKYO_ARGON2_TIME": "3"})
	probe := &installerProbe{activateFailures: make(map[string]int)}
	s.Installer = probe
	t.Cleanup(func() {
		if err := s.CloseRuntime(); err != nil {
			t.Error(err)
		}
	})
	return s, actor, probe
}

func publishInstallerCandidate(t *testing.T, s *SelfConfig, actor Actor, value string) SelfConfigStatus {
	t.Helper()
	status, err := s.Status(t.Context(), actor)
	if err != nil {
		t.Fatal(err)
	}
	scope := domain.Scope{Org: domain.OrgID(status.Binding.OrgID), Project: domain.ProjectID(status.Binding.ProjectID), Env: domain.EnvID(status.Binding.EnvironmentID)}
	values := &Values{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}
	draft, err := values.Set(t.Context(), actor, scope, "HIKYO_ARGON2_TIME", value, nil)
	if err != nil {
		t.Fatal(err)
	}
	revisions := &Revisions{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}
	if _, err := revisions.PublishPlanned(t.Context(), actor, scope, PublishRequest{VersionIDs: []string{draft.VersionID}}); err != nil {
		t.Fatal(err)
	}
	status, err = s.Status(t.Context(), actor)
	if err != nil {
		t.Fatal(err)
	}
	return status
}

type installerApplyResult struct {
	status SelfConfigStatus
	err    error
}

func beginInstallerApply(t *testing.T, s *SelfConfig, actor Actor, req SelfConfigApplyRequest) <-chan installerApplyResult {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan installerApplyResult, 1)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		status, err := s.Apply(ctx, actor, req)
		done <- installerApplyResult{status, err}
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			t.Error("Apply did not stop after cancellation")
		}
	})
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		status, err := s.Status(t.Context(), actor)
		if err != nil {
			t.Fatal(err)
		}
		if status.Job != nil && status.Job.State == "preparing" && status.Job.Revision == req.Revision {
			return done
		}
		select {
		case result := <-done:
			t.Fatalf("Apply ended before durable preparation: %v", result.err)
		case <-deadline.C:
			t.Fatal("Apply did not persist preparation job")
		case <-tick.C:
		}
	}
}

func awaitInstallerApply(t *testing.T, done <-chan installerApplyResult) installerApplyResult {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("Apply did not complete its preparation decision")
		return installerApplyResult{}
	}
}

func installerRequest(status SelfConfigStatus, key string) SelfConfigApplyRequest {
	return SelfConfigApplyRequest{Revision: *status.LatestRevision, ExpectedGeneration: status.Generation, SchemaVersion: runtimeconfig.SchemaVersion, IdempotencyKey: key}
}

func TestSelfConfigInstallerFailedTargetCanBeRepaired(t *testing.T) {
	t.Parallel()
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			s, local, probe := installerFixture(t, engine)
			if err := s.LoadRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			initialSnapshot := s.installed.Load().snapshotID
			actor, sessionID := selfConfigSession(t, s, local)
			status := publishInstallerCandidate(t, s, local, "4")
			req := installerRequest(status, "failed-target")
			authorizeInstallerApply(t, s, sessionID, status, req)
			done := beginInstallerApply(t, s, actor, req)
			if err := s.ReconcileRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			if result := awaitInstallerApply(t, done); result.err != nil {
				t.Fatal(result.err)
			}
			probe.mu.Lock()
			probe.activateFailures["4"] = 100
			probe.mu.Unlock()
			if err := s.ReconcileRuntime(t.Context()); err == nil {
				t.Fatal("injected target failure was not returned")
			}
			status, err := s.Status(t.Context(), local)
			if err != nil || status.Job.State != "partial" || status.Job.Error != "activation_failed" {
				t.Fatalf("failed target did not become repairable: %+v, %v", status.Job, err)
			}
			failedID := status.Job.ID
			status = publishInstallerCandidate(t, s, local, "5")
			repair := installerRequest(status, "repair-target")
			done = beginInstallerApply(t, s, actor, repair)
			if err := s.ReconcileRuntime(t.Context()); err != nil {
				t.Fatalf("failed desired target blocked repair preparation: %v", err)
			}
			if result := awaitInstallerApply(t, done); !errors.Is(result.err, ErrReauthUnitMismatch) {
				t.Fatalf("repair reused previous authorization: %v", result.err)
			}
			if _, err := s.Capture(t.Context()); !errors.Is(err, ErrSelfConfigUnavailable) {
				t.Fatal("repair preparation unfenced failed target")
			}
			current, err := s.Status(t.Context(), local)
			if err != nil || current.Generation != 2 || current.DesiredRevision == nil || status.DesiredRevision == nil || *current.DesiredRevision != *status.DesiredRevision {
				t.Fatalf("preparation changed committed target: %+v, %v", current, err)
			}
			authorizeInstallerApply(t, s, sessionID, status, repair)
			if _, err := s.Apply(t.Context(), actor, repair); err != nil {
				t.Fatal(err)
			}
			if err := s.ReconcileRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			if bundle, err := s.Capture(t.Context()); err != nil || bundle.OwnerValues()["HIKYO_ARGON2_TIME"] != "5" {
				t.Fatalf("repair did not activate: %v", err)
			}
			current, err = s.Status(t.Context(), local)
			if err != nil || current.Generation != 3 || current.Job.State != "completed" {
				t.Fatalf("repair not acknowledged: %+v, %v", current.Job, err)
			}
			err = tx.Read(t.Context(), s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
				p, err := az.SelfConfigRuntimeAuthority(ctx, "")
				if err != nil {
					return err
				}
				b, err := r.SelfConfig().Binding(ctx, p)
				if err != nil {
					return err
				}
				if b.PreviousSnapshotID != initialSnapshot {
					return errors.New("repair discarded the last completed target")
				}
				retained, err := r.SelfConfig().Retained(ctx, p)
				if err != nil {
					return err
				}
				if len(retained) > 3 {
					return errors.New("repair exceeded retention bound")
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			old, err := s.status(t.Context(), local, failedID)
			if err != nil || old.Job.State != "failed" || old.Job.Error != "superseded" {
				t.Fatalf("superseded failure history lost: %+v, %v", old.Job, err)
			}
			if _, activated, _ := probe.stats("4"); activated != 1 {
				t.Fatalf("repair tried to install failed target again: %d", activated)
			}
		})
	}
}

func authorizeInstallerApply(t *testing.T, s *SelfConfig, sessionID string, status SelfConfigStatus, req SelfConfigApplyRequest) {
	t.Helper()
	selfConfigReauthenticate(t, s, sessionID, SelfConfigReauthTarget{Action: "apply", OwnerInstanceID: status.OwnerInstanceID, Revision: req.Revision, SchemaVersion: req.SchemaVersion, ExpectedGeneration: req.ExpectedGeneration})
}

func TestSelfConfigInstallerRetainsPreparationAndAcknowledgesOnlyAfterActivation(t *testing.T) {
	t.Parallel()
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			s, local, probe := installerFixture(t, engine)
			if err := s.LoadRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			actor, sessionID := selfConfigSession(t, s, local)
			status := publishInstallerCandidate(t, s, local, "4")
			req := installerRequest(status, "retained-preparation")
			done := beginInstallerApply(t, s, actor, req)
			if err := s.ReconcileRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			if result := awaitInstallerApply(t, done); !errors.Is(result.err, ErrNoReauthWindow) {
				t.Fatalf("missing ceremony result: %v", result.err)
			}
			if err := s.ReconcileRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			if prepared, activated, closed := probe.stats("4"); prepared != 1 || activated != 0 || closed != 0 {
				t.Fatalf("preflight not retained/reused: prepared=%d activated=%d closed=%d", prepared, activated, closed)
			}
			if bundle, err := s.Capture(t.Context()); err != nil || bundle.OwnerValues()["HIKYO_ARGON2_TIME"] != "3" {
				t.Fatalf("preflight changed active graph: %v", err)
			}
			authorizeInstallerApply(t, s, sessionID, status, req)
			committed, err := s.Apply(t.Context(), actor, req)
			if err != nil {
				t.Fatal(err)
			}
			if committed.Generation != 2 || committed.Job.State == "completed" {
				t.Fatal("target commit acknowledged uninstalled application")
			}
			if _, err := s.Capture(operation.WithNetwork(t.Context())); !errors.Is(err, ErrSelfConfigUnavailable) {
				t.Fatalf("stale graph was not fenced: %v", err)
			}
			entered, release := make(chan struct{}), make(chan struct{})
			probe.mu.Lock()
			probe.activateHook = func(ctx context.Context, value string) error {
				if value != "4" {
					return nil
				}
				close(entered)
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			probe.mu.Unlock()
			activationDone := make(chan error, 1)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			go func() { activationDone <- s.ReconcileRuntime(ctx) }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("activation did not start")
			}
			current, err := s.Status(t.Context(), local)
			if err != nil {
				t.Fatal(err)
			}
			if len(current.Nodes) != 1 || current.Nodes[0].ActiveGeneration == 2 || current.Job.State == "completed" {
				t.Fatal("node acknowledged before Activate completed")
			}
			if _, err := s.Capture(t.Context()); !errors.Is(err, ErrSelfConfigUnavailable) {
				t.Fatal("Capture opened before activation completed")
			}
			close(release)
			if err := <-activationDone; err != nil {
				t.Fatal(err)
			}
			if prepared, activated, closed := probe.stats("4"); prepared != 1 || activated != 1 || closed != 1 {
				t.Fatalf("committed preflight not transferred exactly once: %d/%d/%d", prepared, activated, closed)
			}
			current, err = s.Status(t.Context(), local)
			if err != nil || current.Job.State != "completed" || current.Nodes[0].ActiveGeneration != 2 {
				t.Fatalf("installed application not acknowledged: %v", err)
			}
			if bundle, err := s.Capture(t.Context()); err != nil || bundle.OwnerValues()["HIKYO_ARGON2_TIME"] != "4" {
				t.Fatalf("installed bundle unavailable: %v", err)
			}
		})
	}
}

func TestSelfConfigInstallerPreparationFailureAbortsBeforeTargetCommit(t *testing.T) {
	t.Parallel()
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			s, local, probe := installerFixture(t, engine)
			if err := s.LoadRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			actor, sessionID := selfConfigSession(t, s, local)
			status := publishInstallerCandidate(t, s, local, "4")
			req := installerRequest(status, "preparation-refused")
			authorizeInstallerApply(t, s, sessionID, status, req)
			probe.mu.Lock()
			probe.prepareFailure = "4"
			probe.mu.Unlock()
			done := beginInstallerApply(t, s, actor, req)
			if err := s.ReconcileRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			result := awaitInstallerApply(t, done)
			if result.err != nil || result.status.Generation != 1 || result.status.Job.State != "failed" || result.status.Job.Error != "preparation_failed" {
				t.Fatalf("failed preflight committed or lost refusal: %+v, %v", result.status.Job, result.err)
			}
			if bundle, err := s.Capture(t.Context()); err != nil || bundle.OwnerValues()["HIKYO_ARGON2_TIME"] != "3" {
				t.Fatalf("failed preflight displaced prior graph: %v", err)
			}
			if prepared, activated, _ := probe.stats("4"); prepared != 0 || activated != 0 {
				t.Fatal("failed preparation became an active resource")
			}
		})
	}
}

func TestSelfConfigInstallerActivationFailureFencesAndRetriesCommittedTarget(t *testing.T) {
	t.Parallel()
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			s, local, probe := installerFixture(t, engine)
			if err := s.LoadRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			actor, sessionID := selfConfigSession(t, s, local)
			status := publishInstallerCandidate(t, s, local, "4")
			req := installerRequest(status, "activation-retry")
			authorizeInstallerApply(t, s, sessionID, status, req)
			done := beginInstallerApply(t, s, actor, req)
			if err := s.ReconcileRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			if result := awaitInstallerApply(t, done); result.err != nil || result.status.Generation != 2 {
				t.Fatalf("commit failed: %v", result.err)
			}
			probe.mu.Lock()
			probe.activateFailures["4"] = 1
			probe.mu.Unlock()
			if err := s.ReconcileRuntime(t.Context()); err == nil {
				t.Fatal("activation failure was ignored")
			}
			if _, err := s.Capture(t.Context()); !errors.Is(err, ErrSelfConfigUnavailable) {
				t.Fatal("failed activation left Capture open")
			}
			current, err := s.Status(t.Context(), local)
			if err != nil {
				t.Fatal(err)
			}
			if current.Nodes[0].ActiveGeneration == 2 || current.Nodes[0].Error != "activation_failed" || current.Job.State == "completed" {
				t.Fatal("failed activation acknowledged target")
			}
			if prepared, activated, closed := probe.stats("4"); prepared != 1 || activated != 1 || closed != 1 {
				t.Fatalf("failed activation leaked prepared resources: %d/%d/%d", prepared, activated, closed)
			}
			probe.mu.Lock()
			prior := probe.active
			probe.mu.Unlock()
			if prior != "3" {
				t.Fatal("failed activation tore down prior recovery graph")
			}
			if err := s.ReconcileRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			if prepared, activated, closed := probe.stats("4"); prepared != 2 || activated != 2 || closed != 2 {
				t.Fatalf("retry did not create and transfer fresh preparation: %d/%d/%d", prepared, activated, closed)
			}
			if bundle, err := s.Capture(t.Context()); err != nil || bundle.OwnerValues()["HIKYO_ARGON2_TIME"] != "4" {
				t.Fatalf("retry did not restore target traffic: %v", err)
			}
		})
	}
}

func TestSelfConfigInstallerDisposesAbortedSupersededAndShutdownCandidates(t *testing.T) {
	t.Parallel()
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			s, local, probe := installerFixture(t, engine)
			if err := s.LoadRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			actor, _ := selfConfigSession(t, s, local)
			status := publishInstallerCandidate(t, s, local, "4")
			done := beginInstallerApply(t, s, actor, installerRequest(status, "abort-candidate"))
			if err := s.ReconcileRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			if result := awaitInstallerApply(t, done); !errors.Is(result.err, ErrNoReauthWindow) {
				t.Fatalf("preparation decision: %v", result.err)
			}
			current, err := s.Status(t.Context(), local)
			if err != nil {
				t.Fatal(err)
			}
			// Simulate another participant's durable refusal, preserving the real
			// published snapshot and human Apply job rather than mutating memory.
			err = tx.Write(t.Context(), s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
				proof, err := az.SelfConfigRuntimeAuthority(ctx, "")
				if err != nil {
					return err
				}
				return r.SelfConfig().FinishJob(ctx, proof, current.Job.ID, "aborted", "preparation_failed", time.Now())
			})
			if err != nil {
				t.Fatal(err)
			}
			status = publishInstallerCandidate(t, s, local, "5")
			done = beginInstallerApply(t, s, actor, installerRequest(status, "replacement-candidate"))
			if err := s.ReconcileRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			if result := awaitInstallerApply(t, done); !errors.Is(result.err, ErrNoReauthWindow) {
				t.Fatalf("replacement preparation decision: %v", result.err)
			}
			if prepared, activated, closed := probe.stats("4"); prepared != 1 || activated != 0 || closed != 1 {
				t.Fatalf("aborted/superseded candidate leaked: %d/%d/%d", prepared, activated, closed)
			}
			if prepared, activated, closed := probe.stats("5"); prepared != 1 || activated != 0 || closed != 0 {
				t.Fatalf("replacement candidate not retained: %d/%d/%d", prepared, activated, closed)
			}
			// The worker owns shutdown disposal; cancellation must retain the active
			// recovery graph while releasing the unused replacement candidate.
			ctx, cancel := context.WithCancel(t.Context())
			stopped := make(chan struct{})
			go func() { s.Run(ctx); close(stopped) }()
			cancel()
			select {
			case <-stopped:
			case <-time.After(5 * time.Second):
				t.Fatal("configuration worker did not stop")
			}
			if prepared, activated, closed := probe.stats("5"); prepared != 1 || activated != 0 || closed != 1 {
				t.Fatalf("shutdown candidate leaked: %d/%d/%d", prepared, activated, closed)
			}
			if err := s.CloseRuntime(); err != nil {
				t.Fatal(err)
			}
			if _, _, closed := probe.stats("5"); closed != 1 {
				t.Fatal("repeat cleanup closed candidate twice")
			}
			probe.mu.Lock()
			prior := probe.active
			probe.mu.Unlock()
			if prior != "3" {
				t.Fatal("candidate cleanup removed installed graph")
			}
		})
	}
}

func TestSelfConfigResolveRuntimeBundleDoesNotAcknowledgeOrAcceptNetworkAuthority(t *testing.T) {
	t.Parallel()
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			s, local, probe := installerFixture(t, engine)
			before, err := s.Status(t.Context(), local)
			if err != nil {
				t.Fatal(err)
			}
			bundle, err := s.ResolveRuntimeBundle(t.Context())
			if err != nil || bundle.OwnerValues()["HIKYO_ARGON2_TIME"] != "3" {
				t.Fatalf("startup resolution failed: %v", err)
			}
			after, err := s.Status(t.Context(), local)
			if err != nil {
				t.Fatal(err)
			}
			if len(before.Nodes) != 0 || len(after.Nodes) != 0 || after.State != "pending" {
				t.Fatal("resolution falsely acknowledged an installed node")
			}
			if prepared, activated, _ := probe.stats("3"); prepared != 0 || activated != 0 {
				t.Fatal("resolution invoked installer")
			}
			if _, err := s.Capture(t.Context()); !errors.Is(err, ErrSelfConfigUnavailable) {
				t.Fatal("resolution enabled traffic without activation")
			}
			if _, err := s.ResolveRuntimeBundle(operation.WithNetwork(t.Context())); !errors.Is(err, ErrSelfConfigUnavailable) {
				t.Fatalf("network acquired startup authority: %v", err)
			}
			s.Installer = nil
			if err := s.LoadRuntime(t.Context()); err == nil {
				t.Fatal("owner configuration activated without a consumer")
			}
			if _, err := s.Capture(t.Context()); !errors.Is(err, ErrSelfConfigUnavailable) {
				t.Fatal("missing consumer acknowledged runtime")
			}
		})
	}
}

func TestSelfConfigInstallerDisposesAbortedCandidateWithoutReplacement(t *testing.T) {
	t.Parallel()
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			s, local, probe := installerFixture(t, engine)
			if err := s.LoadRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			actor, _ := selfConfigSession(t, s, local)
			status := publishInstallerCandidate(t, s, local, "4")
			done := beginInstallerApply(t, s, actor, installerRequest(status, "abort-without-replacement"))
			if err := s.ReconcileRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			if result := awaitInstallerApply(t, done); !errors.Is(result.err, ErrNoReauthWindow) {
				t.Fatalf("preparation decision: %v", result.err)
			}
			current, err := s.Status(t.Context(), local)
			if err != nil {
				t.Fatal(err)
			}
			err = tx.Write(t.Context(), s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
				proof, err := az.SelfConfigRuntimeAuthority(ctx, "")
				if err != nil {
					return err
				}
				return r.SelfConfig().FinishJob(ctx, proof, current.Job.ID, "aborted", "preparation_failed", time.Now())
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.ReconcileRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			if prepared, activated, closed := probe.stats("4"); prepared != 1 || activated != 0 || closed != 1 {
				t.Fatalf("aborted candidate retained without replacement: %d/%d/%d", prepared, activated, closed)
			}
			if bundle, err := s.Capture(t.Context()); err != nil || bundle.OwnerValues()["HIKYO_ARGON2_TIME"] != "3" {
				t.Fatalf("abort cleanup removed prior application: %v", err)
			}
		})
	}
}
