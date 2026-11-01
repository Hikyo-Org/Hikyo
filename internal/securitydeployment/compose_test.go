package securitydeployment

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

const fixtureRepository = "ghcr.io/hikyo-org/hikyo"

func fixtureIdentity(sequence uint64) releaseidentity.Identity {
	version := "1.0.0"
	if sequence == 2 {
		version = "1.0.1"
	}
	return releaseidentity.Identity{Profile: releaseidentity.StableV1, Version: version, Sequence: sequence, Commit: strings.Repeat("a", 40), CompatibilitySHA256: releaseidentity.Hash([]byte(version + "compatibility")), ManifestSHA256: releaseidentity.Hash([]byte(version + "manifest"))}
}

func fixtureObservation() Observation {
	return Observation{Platform: runtime.GOOS + "/" + runtime.GOARCH, Engine: releaseidentity.SQLite, Identity: fixtureIdentity(1), Instance: "installation-1", Generation: 4, RestoreEpoch: 1, Incarnation: "recovery-incarnation-1", Phase: "healthy", Ready: true}
}

func privateDirectory(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("external deployment authority is qualified only on Linux/macOS")
	}
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	return directory
}

func encoded(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

type composeFixtureRunner struct {
	t               *testing.T
	config          Config
	target          Target
	observation     Observation
	image           string
	running         bool
	health          string
	events          []string
	commands        []Command
	stageMutation   func(*Observation)
	desiredMutation bool
	failUp          bool
	afterRender     func()
	afterStop       func()
}

func (f *composeFixtureRunner) Run(_ context.Context, command Command) ([]byte, error) {
	f.commands = append(f.commands, command)
	args := command.Args
	joined := strings.Join(args, " ")
	if len(args) > 0 && args[0] == "pull" {
		f.events = append(f.events, "pull")
		return nil, nil
	}
	if len(args) > 1 && args[0] == "image" && args[1] == "inspect" {
		image := f.image
		if args[len(args)-1] == f.target.Image {
			image = f.target.Image
		}
		return encoded(f.t, []string{image}), nil
	}
	if len(args) > 0 && args[0] == "inspect" {
		return encoded(f.t, composeContainer{ID: strings.Repeat("a", 64), Image: f.image, ImageID: "sha256:config", Running: f.running, Health: f.health, Labels: map[string]string{"com.docker.compose.project": "hikyo", "com.docker.compose.service": "server"}}), nil
	}
	if len(args) > 0 && args[0] == "exec" {
		return encoded(f.t, f.observation), nil
	}
	if strings.Contains(joined, " ps --all --quiet ") {
		return []byte(strings.Repeat("a", 64) + "\n"), nil
	}
	if strings.Contains(joined, " config --format json") {
		environment := map[string]string{"HIKYO_DB": "sqlite:/var/lib/hikyo/hikyo.db", "HIKYO_UPGRADE_STATE_DIR": "/var/lib/hikyo-upgrade", "HIKYO_UPGRADE_UNATTENDED": "true", "UNRELATED_SECRET": "CANARY_SECRET"}
		if f.desiredMutation {
			environment["UNRELATED_OPERATOR_EDIT"] = "preserve"
		}
		if f.afterRender != nil {
			f.afterRender()
		}
		return encoded(f.t, map[string]any{"services": map[string]any{"server": map[string]any{"image": f.image, "environment": environment, "volumes": []map[string]string{{"type": "bind", "source": "/srv/hikyo/data", "target": "/var/lib/hikyo"}, {"type": "bind", "source": "/srv/hikyo/upgrade", "target": "/var/lib/hikyo-upgrade"}}}}}), nil
	}
	if strings.Contains(joined, " run --rm --no-deps ") {
		f.events = append(f.events, "stage")
		observation := f.observation
		observation.Phase = "staged"
		observation.Ready = false
		observation.StagedTarget = f.target.Identity
		observation.RouteSHA256 = releaseidentity.Hash([]byte("exact-route"))
		observation.SchemaChanges = true
		observation.RequiresMaintenance = true
		if f.stageMutation != nil {
			f.stageMutation(&observation)
		}
		return encoded(f.t, observation), nil
	}
	if strings.Contains(joined, " stop --timeout ") {
		f.events = append(f.events, "stop")
		f.running = false
		if f.afterStop != nil {
			f.afterStop()
		}
		return nil, nil
	}
	if strings.Contains(joined, " up --detach --no-deps --force-recreate ") {
		f.events = append(f.events, "replace")
		if f.failUp {
			return nil, errors.New("platform reply lost")
		}
		f.running = true
		f.image = f.target.Image
		f.observation.Identity = f.target.Identity
		f.observation.Generation++
		return nil, nil
	}
	f.t.Fatalf("unexpected installed command: %v", args)
	return nil, nil
}

func newComposeFixture(t *testing.T) (*Executor, *composeFixtureRunner, Target) {
	t.Helper()
	config := Config{Kind: Compose, StateDirectory: privateDirectory(t), ImageRepository: fixtureRepository, Compose: &ComposeConfig{Docker: "/usr/bin/docker", Directory: "/srv/hikyo", Project: "hikyo", File: "/etc/hikyo/compose.yaml", EnvironmentFile: "/etc/hikyo/compose.env", Service: "server", Binary: "/usr/local/bin/hikyo"}}
	target := Target{Identity: fixtureIdentity(2), Image: fixtureRepository + "@sha256:" + strings.Repeat("b", 64), EligibilityUntil: time.Now().Add(time.Hour)}
	runner := &composeFixtureRunner{t: t, config: config, target: target, observation: fixtureObservation(), image: fixtureRepository + "@sha256:" + strings.Repeat("a", 64), running: true, health: "healthy"}
	executor, err := NewWithDependencies(config, runner, nil)
	if err != nil {
		t.Fatal(err)
	}
	return executor, runner, target
}

func TestComposeStagesBeforeReplacingLongLivedWorkload(t *testing.T) {
	e, runner, target := newComposeFixture(t)
	staged, err := e.Prepare(t.Context(), target)
	if err != nil {
		t.Fatal(err)
	}
	if staged.StagedTarget != target.Identity || staged.Identity == target.Identity || !staged.SchemaChanges || !staged.RequiresMaintenance {
		t.Fatalf("missing exact source/route impact: %+v", staged)
	}
	if !runner.running || runner.image == target.Image || !slices.Equal(runner.events, []string{"pull", "stage"}) {
		t.Fatalf("prepare changed live workload: %v", runner.events)
	}
	if err := e.Apply(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(runner.events, []string{"pull", "stage", "stop", "replace"}) {
		t.Fatalf("wrong replacement order: %v", runner.events)
	}
	observation, err := e.Observe(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if observation.Identity != target.Identity || observation.Image != target.Image || observation.Generation != 5 || !observation.Ready {
		t.Fatalf("pull/restart falsely passed as exact replacement: %+v", observation)
	}
	j, err := e.journal()
	if err != nil || j.Phase != "healthy" {
		t.Fatalf("healthy journal not committed: %+v %v", j, err)
	}
	for _, command := range runner.commands {
		joined := strings.Join(command.Args, " ")
		if strings.Contains(joined, "CANARY_SECRET") || strings.Contains(joined, " down ") || strings.Contains(joined, "--volumes") {
			t.Fatalf("authority or secret escaped installed contract: %v", command.Args)
		}
	}
	for _, name := range []string{"compose-prepared.json", "compose-stage.json", "compose-active.json", "deployment.json"} {
		info, err := os.Stat(filepath.Join(e.c.StateDirectory, name))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("unprotected durable input %s: %v", name, err)
		}
	}
}

func TestComposeBlocksSourceAndDesiredStateDrift(t *testing.T) {
	for _, change := range []string{"release", "restore", "incarnation", "generation", "configuration"} {
		t.Run(change, func(t *testing.T) {
			e, runner, target := newComposeFixture(t)
			if _, err := e.Prepare(t.Context(), target); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "release":
				runner.observation.Identity = fixtureIdentity(2)
			case "restore":
				runner.observation.RestoreEpoch++
			case "incarnation":
				runner.observation.Incarnation = "new-recovery"
			case "generation":
				runner.observation.Generation++
			case "configuration":
				runner.desiredMutation = true
			}
			if err := e.Apply(t.Context(), target); !errors.Is(err, ErrConflict) {
				t.Fatalf("drift did not refuse: %v", err)
			}
			if slices.Contains(runner.events, "stop") {
				t.Fatal("source stopped despite drift")
			}
		})
	}
}

func TestComposeRefusesWrongStageSourceAndTarget(t *testing.T) {
	for _, change := range []string{"source", "target", "route", "generation", "restore", "platform"} {
		t.Run(change, func(t *testing.T) {
			e, runner, target := newComposeFixture(t)
			runner.stageMutation = func(o *Observation) {
				switch change {
				case "source":
					o.Identity = target.Identity
				case "target":
					o.StagedTarget = fixtureIdentity(1)
				case "route":
					o.RouteSHA256 = ""
				case "generation":
					o.Generation++
				case "restore":
					o.RestoreEpoch++
				case "platform":
					o.Platform = "unsupported/architecture"
				}
			}
			if _, err := e.Prepare(t.Context(), target); err == nil {
				t.Fatal("mismatching stage admitted")
			}
			if slices.Contains(runner.events, "stop") {
				t.Fatal("failed stage stopped source")
			}
		})
	}
}

func TestComposeAmbiguousReplacementNeverRetriesOrRollsBack(t *testing.T) {
	e, runner, target := newComposeFixture(t)
	if _, err := e.Prepare(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	runner.failUp = true
	if err := e.Apply(t.Context(), target); err == nil {
		t.Fatal("replacement failure hidden")
	}
	before := len(runner.commands)
	if err := e.Apply(t.Context(), target); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("uncertain replacement retried: %v", err)
	}
	if len(runner.commands) != before {
		t.Fatal("recovery refusal executed a command")
	}
	if _, err := e.Prepare(t.Context(), target); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("new preparation bypassed pending operation: %v", err)
	}
}

func TestDeploymentRejectsUnknownPlatformAndArbitraryTarget(t *testing.T) {
	e, runner, target := newComposeFixture(t)
	target.Image = "attacker.invalid/hikyo@sha256:" + strings.Repeat("b", 64)
	if _, err := e.Prepare(t.Context(), target); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unowned repository accepted: %v", err)
	}
	if len(runner.commands) != 0 {
		t.Fatal("invalid target ran commands")
	}
	config := e.c
	config.Kind = "other-managed"
	if err := Validate(config); !errors.Is(err, ErrUnsupported) {
		t.Fatal("unimplemented provider advertised")
	}
}

func TestComposeHealthyDatabaseDoesNotSubstituteForContainerHealth(t *testing.T) {
	for _, health := range []string{"", "starting", "unhealthy"} {
		t.Run(health, func(t *testing.T) {
			e, runner, target := newComposeFixture(t)
			runner.health = health
			o, err := e.Observe(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if o.Ready {
				t.Fatal("database-only proof falsely established serving health")
			}
			if _, err := e.Prepare(t.Context(), target); !errors.Is(err, ErrNotReady) {
				t.Fatalf("unhealthy source was staged: %v", err)
			}
			if len(runner.events) != 0 {
				t.Fatal("unhealthy source received update side effects")
			}
		})
	}
}

func TestDeploymentRejectsReplaceableStateAuthority(t *testing.T) {
	e, runner, _ := newComposeFixture(t)
	if err := os.Chmod(e.c.StateDirectory, 0770); err != nil {
		t.Fatal(err)
	}
	if _, err := NewWithDependencies(e.c, runner, nil); err == nil {
		t.Fatal("group-replaceable state accepted")
	}
}

func TestComposeRefusesUnsupportedActualDatabaseEngine(t *testing.T) {
	e, runner, target := newComposeFixture(t)
	runner.observation.Engine = releaseidentity.Postgres
	if _, err := e.Prepare(t.Context(), target); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("unsupported actual topology admitted: %v", err)
	}
	if len(runner.events) != 0 {
		t.Fatal("unsupported topology received side effects")
	}
}

func TestComposeRefreshesSameStagedRouteAndUsesFreshLocalDeadline(t *testing.T) {
	e, runner, target := newComposeFixture(t)
	prepared := target
	prepared.EligibilityUntil = time.Time{}
	first, err := e.Prepare(t.Context(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.Prepare(t.Context(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	if first.RouteSHA256 != second.RouteSHA256 || !sameSource(first, second) {
		t.Fatal("same-source route refresh changed its identity")
	}
	if !slices.Equal(runner.events, []string{"pull", "stage", "pull", "stage"}) {
		t.Fatalf("refresh changed running deployment: %v", runner.events)
	}
	if err := e.Apply(t.Context(), target); err != nil {
		t.Fatalf("fresh deadline incorrectly substituted immutable target: %v", err)
	}
}

func TestComposeExpiryDuringValidationCannotStopSource(t *testing.T) {
	e, runner, target := newComposeFixture(t)
	prepared := target
	prepared.EligibilityUntil = time.Time{}
	if _, err := e.Prepare(t.Context(), prepared); err != nil {
		t.Fatal(err)
	}
	runner.afterRender = func() { e.now = func() time.Time { return target.EligibilityUntil.Add(time.Nanosecond) } }
	if err := e.Apply(t.Context(), target); !errors.Is(err, ErrEligibilityExpired) {
		t.Fatalf("expired authorization passed final stop gate: %v", err)
	}
	if slices.Contains(runner.events, "stop") || slices.Contains(runner.events, "replace") || !runner.running {
		t.Fatalf("expiry mutated source: %v", runner.events)
	}
	j, err := e.journal()
	if err != nil || j.Phase != "prepared" {
		t.Fatalf("no-op expiry incorrectly claimed replacement began: %+v %v", j, err)
	}
}

func TestComposeExpiryDuringJournalPublicationRestoresPreparedState(t *testing.T) {
	e, runner, target := newComposeFixture(t)
	if _, err := e.Prepare(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	calls := 0
	e.now = func() time.Time {
		calls++
		if calls == 1 {
			return target.EligibilityUntil.Add(-time.Nanosecond)
		}
		return target.EligibilityUntil
	}
	if err := e.Apply(t.Context(), target); !errors.Is(err, ErrEligibilityExpired) {
		t.Fatalf("expired journal publication passed stop gate: %v", err)
	}
	if slices.Contains(runner.events, "stop") {
		t.Fatal("source stopped after eligibility expired while journalling")
	}
	j, err := e.journal()
	if err != nil || j.Phase != "prepared" {
		t.Fatalf("pre-stop expiry retained false applying state: %+v %v", j, err)
	}
}

func TestComposeAdmittedReplacementCompletesAfterMetadataExpiry(t *testing.T) {
	e, runner, target := newComposeFixture(t)
	if _, err := e.Prepare(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	runner.afterStop = func() { e.now = func() time.Time { return target.EligibilityUntil.Add(time.Hour) } }
	if err := e.Apply(t.Context(), target); err != nil {
		t.Fatalf("metadata expiry stranded an admitted replacement: %v", err)
	}
	observation, err := e.Observe(t.Context())
	if err != nil || observation.Identity != target.Identity || !observation.Ready {
		t.Fatalf("admitted target did not finish: %+v %v", observation, err)
	}
}

func TestComposeDeadlineCannotBypassImmutableTargetPins(t *testing.T) {
	for _, field := range []string{"identity", "image", "runtime"} {
		t.Run(field, func(t *testing.T) {
			e, runner, target := newComposeFixture(t)
			if _, err := e.Prepare(t.Context(), target); err != nil {
				t.Fatal(err)
			}
			switch field {
			case "identity":
				target.Identity = fixtureIdentity(1)
			case "image":
				target.Image = fixtureRepository + "@sha256:" + strings.Repeat("c", 64)
			case "runtime":
				target.RuntimeImageSHA256 = strings.Repeat("c", 64)
			}
			target.EligibilityUntil = target.EligibilityUntil.Add(time.Hour)
			if err := e.Apply(t.Context(), target); !errors.Is(err, ErrConflict) {
				t.Fatalf("fresh deadline bypassed target pin: %v", err)
			}
			if slices.Contains(runner.events, "stop") {
				t.Fatal("substituted target stopped source")
			}
		})
	}
}
