package app

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/hostupgrade"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/selfupdate"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	bundlefixture "github.com/Hikyo-Org/hikyo/internal/upgradebundle/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

type automaticTestInspection struct {
	state          upgrade.State
	absent         bool
	installed      upgrade.InstalledSource
	installedCalls int
}

func (d *automaticTestInspection) Control(context.Context) (upgrade.State, error) {
	if d.absent {
		return upgrade.State{}, upgrade.ErrAbsent
	}
	return d.state, nil
}
func (d *automaticTestInspection) Installed(context.Context, releaseidentity.MigrationManifest) (upgrade.InstalledSource, error) {
	d.installedCalls++
	return d.installed, nil
}
func (d *automaticTestInspection) RequireRestore(_ context.Context, expected upgrade.State) error {
	if expected.Pending == nil || expected.Pending.Phase != upgrade.SchemaApplied {
		return errors.New("invalid restore-required transition")
	}
	d.state.Pending.Phase = upgrade.RestoreRequired
	return nil
}

type automaticTestHost struct {
	t         *testing.T
	db        *automaticTestInspection
	plan      upgradecompat.Plan
	journal   *automaticJournal
	path      string
	events    []string
	failAt    string
	failPhase upgrade.Phase
	running   bool
	completed bool
	current   int
}

func (h *automaticTestHost) event(name string) error {
	h.events = append(h.events, name)
	if h.failAt == name {
		return errors.New("injected host interruption")
	}
	return nil
}
func (h *automaticTestHost) FenceAndStop(context.Context) error {
	h.running = false
	return h.event("fence")
}
func (h *automaticTestHost) Migrate(_ context.Context, candidate string, _ hostupgrade.RuntimeEvidence) ([]byte, error) {
	index := h.candidate(candidate)
	persisted, err := readAutomaticJournal(h.path)
	if err != nil || persisted.Phase != "write-intent" || persisted.Hop != index {
		h.t.Fatalf("migration preceded durable exact write intent: %+v %v", persisted, err)
	}
	h.current = index
	h.db.absent = false
	h.db.state = automaticState(h.plan, h.journal, index, upgrade.SchemaApplied)
	if h.failPhase != "" {
		h.db.state.Pending.Phase = h.failPhase
	}
	return nil, h.event(fmt.Sprintf("migrate-%d", index))
}
func (h *automaticTestHost) candidate(candidate string) int {
	h.t.Helper()
	for n := range h.plan.Steps() {
		if candidate == fmt.Sprintf("candidate-%d", n) {
			return n
		}
	}
	h.t.Fatalf("unknown candidate %q", candidate)
	return 0
}
func (h *automaticTestHost) InstallBinary(_ context.Context, candidate, digest string) error {
	h.current = h.candidate(candidate)
	if digest != string(releaseidentity.Hash([]byte(candidate))) {
		h.t.Fatal("wrong executable digest")
	}
	return h.event(fmt.Sprintf("install-%d", h.current))
}
func (h *automaticTestHost) ConfigureRuntime(_ context.Context, e hostupgrade.RuntimeEvidence) error {
	if e.EvidenceDirectory == "" {
		return h.event("configure-restart")
	}
	return h.event("configure-upgrade")
}
func (h *automaticTestHost) StartCandidate(_ context.Context, _ string, final bool, _ time.Duration) error {
	if final != (h.current == len(h.plan.Steps())-1) {
		h.t.Fatal("wrong final-hop readiness requirement")
	}
	h.db.state = automaticState(h.plan, h.journal, h.current, upgrade.Healthy)
	step := h.plan.Steps()[h.current]
	digest, _ := step.TargetMigrations.Digest()
	h.db.installed = upgrade.InstalledSource{Source: releaseidentity.Source{Release: step.Target}, MigrationDigest: digest, SchemaDigest: step.TargetSchemaSHA256, InstanceID: h.journal.Instance}
	h.running = true
	return h.event(fmt.Sprintf("start-%d", h.current))
}
func (h *automaticTestHost) Complete(context.Context) error {
	if !h.running {
		h.t.Fatal("completed upgrade while service stopped")
	}
	h.completed = true
	return h.event("complete")
}

func automaticState(plan upgradecompat.Plan, journal *automaticJournal, index int, phase upgrade.Phase) upgrade.State {
	step := plan.Steps()[index]
	sourceDigest, _ := step.SourceMigrations.Digest()
	targetDigest, _ := step.TargetMigrations.Digest()
	state := upgrade.State{InstanceID: journal.Instance, Applied: step.Source, MigrationDigest: sourceDigest, SchemaDigest: step.SourceSchemaSHA256, Maintenance: true, Pending: &upgrade.Operation{Kind: upgrade.UpgradeOperation, RouteSource: journal.Source.Identity, Source: step.Source, Target: step.Target, RouteDigest: journal.Route, Hop: int64(index), RouteLength: int64(len(plan.Steps())), SourceMigrationDigest: sourceDigest, TargetMigrationDigest: targetDigest, SourceSchemaDigest: step.SourceSchemaSHA256, TargetSchemaDigest: step.TargetSchemaSHA256, Phase: phase}}
	if phase == upgrade.Healthy {
		state.Applied = releaseidentity.Source{Release: step.Target}
		state.MigrationDigest = targetDigest
		state.SchemaDigest = step.TargetSchemaSHA256
		state.Maintenance = index+1 < len(plan.Steps())
	}
	return state
}

func automaticApplyFixture(t *testing.T, hops int) (automaticRoute, *automaticJournal, *automaticTestHost, map[releaseidentity.Identity]string) {
	t.Helper()
	migrations := releaseidentity.MigrationManifest{Engine: releaseidentity.SQLite, Entries: []releaseidentity.Migration{{Version: 1, SHA256: releaseidentity.Hash([]byte("source SQL"))}}}
	source := upgradecompat.InstalledSource{Identity: releaseidentity.Source{Genesis: releaseidentity.LegacyGenesisV1}, Migrations: migrations, SchemaSHA256: releaseidentity.Hash([]byte("source catalog"))}
	targets := make([]bundlefixture.Target, 0, hops)
	for n := range hops {
		targets = append(targets, bundlefixture.Target{Version: fmt.Sprintf("1.%d.0", n), Sequence: uint64(n + 1), Commit: strings.Repeat("a", 40), Migrations: migrations, SchemaSHA256: source.SchemaSHA256})
	}
	signed := bundlefixture.Write(t, source, targets)
	journal := &automaticJournal{Format: "hikyo.host-upgrade/v1", Phase: "proved", Target: signed.Target, Source: source, Instance: "fixture-installation", Route: signed.Plan.Digest(), Runtime: hostupgrade.RuntimeEvidence{EvidenceDirectory: "public-evidence", CiphertextPath: "backup.age"}}
	digest, _ := source.Migrations.Digest()
	db := &automaticTestInspection{absent: true, installed: upgrade.InstalledSource{InstanceID: journal.Instance, Source: source.Identity, MigrationDigest: digest, SchemaDigest: source.SchemaSHA256}}
	route := automaticRoute{Bundle: signed.Bundle, Directory: signed.Directory, Plan: signed.Plan, Instance: journal.Instance, Executables: map[releaseidentity.Identity]selfupdate.PreparedNightly{}}
	staged := map[releaseidentity.Identity]string{}
	for n, step := range route.Plan.Steps() {
		candidate := fmt.Sprintf("candidate-%d", n)
		staged[step.Target] = candidate
		route.Executables[step.Target] = selfupdate.PreparedNightly{Identity: step.Target, BinaryPath: candidate, BinarySHA256: releaseidentity.Hash([]byte(candidate))}
	}
	host := &automaticTestHost{t: t, db: db, plan: route.Plan, journal: journal, path: filepath.Join(t.TempDir(), "journal.json")}
	return route, journal, host, staged
}

func TestAutomaticRouteMaintainsFenceAcrossHopsAndPersistsExactIntent(t *testing.T) {
	route, journal, host, staged := automaticApplyFixture(t, 2)
	if err := applyAutomaticRoute(t.Context(), host, host.db, route, staged, journal, host.path, io.Discard); err != nil {
		t.Fatal(err)
	}
	want := "migrate-0,install-0,configure-upgrade,start-0,fence,migrate-1,install-1,configure-upgrade,start-1,configure-restart,complete"
	if strings.Join(host.events, ",") != want || journal.Phase != "complete" || !host.running {
		t.Fatalf("incorrect coordinated route: %v %+v", host.events, journal)
	}
}

func TestAutomaticFinalHealthyResumeRestartsBeforeCompletion(t *testing.T) {
	route, journal, host, staged := automaticApplyFixture(t, 2)
	journal.Phase, journal.Hop = "hop-healthy", 2
	host.db.absent = false
	host.db.state = automaticState(route.Plan, journal, 1, upgrade.Healthy)
	if err := applyAutomaticRoute(t.Context(), host, host.db, route, staged, journal, host.path, io.Discard); err != nil {
		t.Fatal(err)
	}
	if strings.Join(host.events, ",") != "install-1,configure-restart,start-1,configure-restart,complete" || !host.running {
		t.Fatalf("final resume did not restart: %v", host.events)
	}
}

func TestAutomaticWriteIntentReconcilesOnlyProvenPrewriteOrAppliedStates(t *testing.T) {
	for _, phase := range []upgrade.Phase{"absent", upgrade.Prepared, upgrade.SchemaApplied, upgrade.Healthy, upgrade.SchemaWriteStarted, upgrade.RestoreRequired} {
		t.Run(string(phase), func(t *testing.T) {
			route, journal, host, staged := automaticApplyFixture(t, 1)
			journal.Phase = "write-intent"
			host.db.absent = phase == "absent"
			if !host.db.absent {
				host.db.state = automaticState(route.Plan, journal, 0, phase)
			}
			err := applyAutomaticRoute(t.Context(), host, host.db, route, staged, journal, host.path, io.Discard)
			refused := phase == upgrade.SchemaWriteStarted || phase == upgrade.RestoreRequired
			if refused {
				if err == nil || host.running || host.completed || strings.Join(host.events, ",") != "fence" {
					t.Fatalf("ambiguous write outcome restarted: %v %v", host.events, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			migrated := strings.Contains(strings.Join(host.events, ","), "migrate-0")
			if migrated != (phase == "absent" || phase == upgrade.Prepared) {
				t.Fatalf("incorrect migration retry for %s: %v", phase, host.events)
			}
		})
	}
}

func TestAutomaticIntermediatePreAdmissionCrashRetainsFullOriginalRoute(t *testing.T) {
	route, journal, host, staged := automaticApplyFixture(t, 2)
	journal.Phase, journal.Hop = "write-intent", 1
	host.db.absent = false
	host.db.state = automaticState(route.Plan, journal, 0, upgrade.Healthy)
	step := route.Plan.Steps()[1]
	digest, _ := step.SourceMigrations.Digest()
	host.db.installed = upgrade.InstalledSource{InstanceID: journal.Instance, Source: step.Source, MigrationDigest: digest, SchemaDigest: step.SourceSchemaSHA256}
	if err := applyAutomaticRoute(t.Context(), host, host.db, route, staged, journal, host.path, io.Discard); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(host.events, ","), "migrate-0") || journal.Route != route.Plan.Digest() || journal.Source.Identity != route.Plan.Source() {
		t.Fatal("resumption lost original route")
	}
}

func TestAutomaticFailuresNeverRestartOldExecutable(t *testing.T) {
	for _, failure := range []string{"migrate-0", "install-0", "configure-upgrade", "start-0"} {
		t.Run(failure, func(t *testing.T) {
			route, journal, host, staged := automaticApplyFixture(t, 1)
			host.failAt = failure
			if failure == "migrate-0" {
				host.failPhase = upgrade.SchemaWriteStarted
			}
			var out bytes.Buffer
			err := applyAutomaticRoute(t.Context(), host, host.db, route, staged, journal, host.path, &out)
			if err == nil || host.running || host.completed || host.events[len(host.events)-1] != "fence" {
				t.Fatalf("failure left active service: %v %v", host.events, err)
			}
			for _, event := range host.events {
				if strings.Contains(event, "old") {
					t.Fatal("old executable restarted")
				}
			}
		})
	}
}

func TestAutomaticPendingRejectsMismatchedHopAndBindings(t *testing.T) {
	for _, mutation := range []string{"hop", "length", "source schema", "target migrations", "instance", "invalidated"} {
		t.Run(mutation, func(t *testing.T) {
			route, journal, host, staged := automaticApplyFixture(t, 1)
			journal.Phase = "schema-applied"
			host.db.absent = false
			host.db.state = automaticState(route.Plan, journal, 0, upgrade.SchemaApplied)
			state := &host.db.state
			switch mutation {
			case "hop":
				state.Pending.Hop++
			case "length":
				state.Pending.RouteLength++
			case "source schema":
				state.Pending.SourceSchemaDigest = releaseidentity.Hash([]byte("different"))
			case "target migrations":
				state.Pending.TargetMigrationDigest = releaseidentity.Hash([]byte("different"))
			case "instance":
				state.InstanceID = "different"
			case "invalidated":
				state.Pending.Invalidated = true
			}
			if err := applyAutomaticRoute(t.Context(), host, host.db, route, staged, journal, host.path, io.Discard); err == nil || strings.Join(host.events, ",") != "fence" {
				t.Fatalf("wrong pending binding accepted: %v %v", host.events, err)
			}
		})
	}
}

func TestAutomaticPrewriteRetryMeasuresRealSQLiteAndRestoreProof(t *testing.T) {
	f := newUpgradeDrillFixture(t, store.EngineSQLite, true, true)
	result, err := DrillUpgrade(t.Context(), f.request)
	if err != nil || !result.HierarchyReadable || result.SecretProof != "existing-secret-readable" || result.CredentialProof != "reconciled-minted-revoked" {
		t.Fatalf("real encrypted backup restore proof failed: %v", err)
	}
	sourceManifest, err := f.bundle.Plan.SourceManifest(releaseidentity.SQLite)
	if err != nil {
		t.Fatal(err)
	}
	journal := &automaticJournal{Phase: "write-intent", Target: f.bundle.Target, Source: upgradecompat.InstalledSource{Identity: f.source.Source, Migrations: sourceManifest, SchemaSHA256: f.source.SchemaDigest}, Instance: f.source.InstanceID, Route: f.bundle.Plan.Digest()}
	database := automaticStore{upgrade.Config{Engine: releaseidentity.SQLite, Path: f.cfg.Path}}
	migrate, err := automaticMigrationDisposition(t.Context(), database, journal, f.bundle.Plan, 0)
	if err != nil || !migrate {
		t.Fatalf("unchanged actual legacy DB cannot retry: %v", err)
	}
	journal.Instance = "different-installation"
	if _, err := automaticMigrationDisposition(t.Context(), database, journal, f.bundle.Plan, 0); err == nil {
		t.Fatal("different DB instance accepted")
	}
	journal.Instance = f.source.InstanceID
	changed, err := sql.Open("sqlite", f.cfg.Path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := changed.ExecContext(t.Context(), "CREATE TABLE unexpected_after_intent (id INTEGER)"); err != nil {
		changed.Close()
		t.Fatal(err)
	}
	if err := changed.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := automaticMigrationDisposition(t.Context(), database, journal, f.bundle.Plan, 0); err == nil {
		t.Fatal("changed real SQLite catalog accepted as pre-write retry")
	}

}

func TestAutomaticHealthFailureIsTerminalWithoutRewritingHealthyDB(t *testing.T) {
	for _, phase := range []upgrade.Phase{upgrade.SchemaApplied, upgrade.Healthy} {
		t.Run(string(phase), func(t *testing.T) {
			route, journal, host, staged := automaticApplyFixture(t, 1)
			journal.Phase = "schema-applied"
			host.db.absent = false
			host.db.state = automaticState(route.Plan, journal, 0, phase)
			before := host.db.state.Applied
			err := recordAutomaticHealthFailure(host, host.db, route.Plan, journal, host.path, 0, errors.New("readiness unavailable"))
			if err == nil || host.running || journal.Phase != "restore-required" {
				t.Fatalf("health failure was not terminal: %v", err)
			}
			want := upgrade.RestoreRequired
			if phase == upgrade.Healthy {
				want = upgrade.Healthy
			}
			if host.db.state.Pending.Phase != want || host.db.state.Applied != before {
				t.Fatal("health failure rewrote applied DB history")
			}
			persisted, err := readAutomaticJournal(host.path)
			if err != nil || persisted.Phase != "restore-required" {
				t.Fatal("terminal host journal was not durable", err)
			}
			host.events = nil
			if err := applyAutomaticRoute(t.Context(), host, host.db, route, staged, journal, host.path, io.Discard); err == nil || strings.Join(host.events, ",") != "fence" {
				t.Fatalf("terminal host journal retried automatically: %v %v", host.events, err)
			}
		})
	}
}
