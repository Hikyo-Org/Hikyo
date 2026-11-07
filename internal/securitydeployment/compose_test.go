package securitydeployment

import (
	"cmp"
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
	t           *testing.T
	config      Config
	target      Target
	observation Observation
	image       string
	sourceImage string
	// containerImage, when set, is the reference a container was started with, as a tag install shows it.
	containerImage string
	// secondImage, when set, is a second live container's image, listed after the first.
	secondImage     string
	running         bool
	health          string
	events          []string
	commands        []Command
	stageMutation   func(*Observation)
	desiredMutation bool
	failUp          bool
	failStop        bool
	// observationField simulates a newer target image's additional JSON field.
	observationField bool
	afterRender      func()
	afterStop        func()
	// removed is set by "rm"; stuck keeps a container listed after it, failRollback
	// fails the rollback one-shot, and reportless prints nothing from it.
	removed, stuck, failRollback, reportless, exitedOneOff bool
	// inspectRow is what a one-off `security-maintenance inspect` prints; nil makes it fail.
	inspectRow *Observation
	// logLine precedes the one-shot's report, as a merged stderr does. afterRollback runs when
	// the rollback one-shot returns, and restoreErr is the state of the context the write-back got.
	logLine       string
	notNeeded     bool
	afterRollback func()
	restoreErr    error
	// hardcodedImage is a Compose file naming the image itself, so the variable does not reach it.
	hardcodedImage bool
	// renderedImage, when set, is what the environment file selects instead of the running image.
	renderedImage string
	// oneShotLeft is a rollback one-shot container that exists by name; outlive leaves it behind when
	// the one-shot's command fails, as a killed CLI does, and oneShotStuck keeps it after removal.
	oneShotLeft, outlive, oneShotStuck bool
	// manifest is the platform manifest a multi-platform image store reports for the container.
	manifest string
	// stateDirectory, mounts and tmpfs change the rendered service's state directory and add mounts.
	stateDirectory string
	mounts         []map[string]string
	tmpfs          []string
	command        []string
}

// selectedImage is the image the last of a command's Compose files names, as Compose merges them.
func (f *composeFixtureRunner) selectedImage(args []string) string {
	image := ""
	for i := 0; i+1 < len(args); i++ {
		if args[i] != "--file" {
			continue
		}
		raw, err := os.ReadFile(args[i+1])
		if err != nil {
			f.t.Fatal(err)
		}
		var file composeConfig
		if err := json.Unmarshal(raw, &file); err != nil {
			f.t.Fatal(err)
		}
		if service := file.Services["server"]; service.Image != "" {
			image = service.Image
		}
	}
	return image
}

func (f *composeFixtureRunner) Run(ctx context.Context, command Command) ([]byte, error) {
	f.commands = append(f.commands, command)
	args := command.Args
	joined := strings.Join(args, " ")
	if len(args) > 1 && args[0] == "container" && args[1] == "ls" {
		if !f.oneShotLeft {
			return nil, nil
		}
		name, _ := strings.CutPrefix(args[slices.Index(args, "--filter")+1], "name=")
		return []byte(name + "\n"), nil
	}
	if len(args) > 1 && args[0] == "container" && args[1] == "rm" {
		f.events = append(f.events, "clear")
		f.oneShotLeft = f.oneShotLeft && f.oneShotStuck
		return nil, nil
	}
	if len(args) > 0 && args[0] == "pull" {
		f.events = append(f.events, "pull")
		return nil, nil
	}
	if len(args) > 1 && args[0] == "image" && args[1] == "inspect" {
		image := f.image
		if args[len(args)-1] == f.target.Image {
			image = f.target.Image
		}
		if args[len(args)-1] == "sha256:second" {
			image = f.secondImage
		}
		return encoded(f.t, []string{image}), nil
	}
	if len(args) > 0 && args[0] == "inspect" && f.secondImage != "" && args[len(args)-1] == strings.Repeat("b", 64) {
		return encoded(f.t, composeContainer{ID: strings.Repeat("b", 64), Image: f.secondImage, ImageID: "sha256:second", Running: true, Labels: map[string]string{"com.docker.compose.project": "hikyo", "com.docker.compose.service": "server"}}), nil
	}
	if len(args) > 0 && args[0] == "inspect" {
		started := f.image
		if f.containerImage != "" {
			started = f.containerImage
		}
		return encoded(f.t, composeContainer{ID: strings.Repeat("a", 64), Image: started, ImageID: "sha256:config", Running: f.running, Health: f.health, Labels: map[string]string{"com.docker.compose.project": "hikyo", "com.docker.compose.service": "server"}, Manifest: f.manifest}), nil
	}
	if len(args) > 0 && args[0] == "exec" {
		return f.targetOutput(f.observation), nil
	}
	if strings.Contains(joined, " rm --stop --force ") {
		f.events = append(f.events, "rm")
		f.running, f.removed = false, true
		return nil, nil
	}
	if strings.Contains(joined, " ps --all --quiet ") {
		if f.exitedOneOff && !strings.Contains(joined, " --status running ") {
			// A crashed `run --rm` left an exited one-off: `ps --all` lists it, a status filter does not.
			return []byte(strings.Repeat("a", 64) + "\n" + strings.Repeat("b", 64) + "\n"), nil
		}
		if f.removed && !f.stuck {
			return nil, nil
		}
		if f.secondImage != "" {
			return []byte(strings.Repeat("a", 64) + "\n" + strings.Repeat("b", 64) + "\n"), nil
		}
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
		image := cmp.Or(f.renderedImage, f.image)
		for _, variable := range command.Env {
			if value, ok := strings.CutPrefix(variable, composeImageVariable+"="); ok && !f.hardcodedImage {
				image = value
			}
		}
		environment["HIKYO_UPGRADE_STATE_DIR"] = cmp.Or(f.stateDirectory, environment["HIKYO_UPGRADE_STATE_DIR"])
		volumes := append([]map[string]string{{"type": "bind", "source": "/srv/hikyo/data", "target": "/var/lib/hikyo"}, {"type": "bind", "source": "/srv/hikyo/upgrade", "target": "/var/lib/hikyo-upgrade"}}, f.mounts...)
		return encoded(f.t, map[string]any{"services": map[string]any{"server": map[string]any{"image": image, "environment": environment, "volumes": volumes, "tmpfs": f.tmpfs, "command": f.command}}}), nil
	}
	if strings.Contains(joined, " security-maintenance rollback --expected-manifest "+string(f.target.Identity.ManifestSHA256)) {
		f.events = append(f.events, "rollback")
		if f.oneShotLeft && slices.Contains(args, "--name") {
			return nil, errors.New("the container name is already in use")
		}
		if f.failRollback {
			f.oneShotLeft = f.outlive
			return nil, errors.New("rollback one-shot failed")
		}
		if f.afterRollback != nil {
			f.afterRollback()
		}
		if f.reportless {
			return nil, nil
		}
		return append([]byte(f.logLine), encoded(f.t, RollbackReport{Format: RollbackReportFormat, Result: map[bool]string{true: "not-needed", false: "restored"}[f.notNeeded], Reason: "test"})...), nil
	}
	if strings.Contains(joined, " security-maintenance inspect") && strings.Contains(joined, " run --rm --no-deps ") {
		f.events = append(f.events, "inspect")
		if f.inspectRow == nil {
			return nil, errors.New("the one-off cannot read the row")
		}
		return append([]byte(f.logLine), f.targetOutput(*f.inspectRow)...), nil
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
		return f.targetOutput(observation), nil
	}
	if strings.Contains(joined, " stop --timeout ") {
		if f.failStop {
			return nil, errors.New("stop refused")
		}
		f.events = append(f.events, "stop")
		f.running = false
		if f.afterStop != nil {
			f.afterStop()
		}
		return nil, nil
	}
	if strings.Contains(joined, " up --detach --no-deps --force-recreate ") && !strings.Contains(joined, "compose-stage.json") {
		f.events = append(f.events, "restore")
		f.restoreErr = ctx.Err()
		f.running, f.removed = true, false
		f.image = f.selectedImage(args)
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

func (f *composeFixtureRunner) targetOutput(observation Observation) []byte {
	raw := encoded(f.t, observation)
	if !f.observationField {
		return raw
	}
	return append(raw[:len(raw)-1], []byte(`,"added_by_a_newer_target":true}`)...)
}

func newComposeFixture(t *testing.T) (*Executor, *composeFixtureRunner, Target) {
	t.Helper()
	environment := filepath.Join(privateDirectory(t), "install.env")
	sourceImage := fixtureRepository + "@sha256:" + strings.Repeat("a", 64)
	if err := os.WriteFile(environment, []byte("HIKYO_EXTERNAL_ORIGIN=https://hikyo.example\nHIKYO_SERVER_IMAGE="+sourceImage+"\nHIKYO_SERVER_DATA_DIR=/srv/hikyo/data\n"), 0600); err != nil {
		t.Fatal(err)
	}
	config := Config{Kind: Compose, StateDirectory: privateDirectory(t), ImageRepository: fixtureRepository, Compose: &ComposeConfig{Docker: "/usr/bin/docker", Directory: "/srv/hikyo", Project: "hikyo", File: "/etc/hikyo/compose.yaml", EnvironmentFile: environment, Service: "server", Binary: "/usr/local/bin/hikyo"}}
	target := Target{Identity: fixtureIdentity(2), Image: fixtureRepository + "@sha256:" + strings.Repeat("b", 64), EligibilityUntil: time.Now().Add(time.Hour)}
	runner := &composeFixtureRunner{t: t, config: config, target: target, observation: fixtureObservation(), image: sourceImage, running: true, health: "healthy", sourceImage: sourceImage}
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
	for _, name := range []string{"compose-prepared.json", "compose-stage.json", "deployment.json"} {
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

// A plain operator `docker compose up` reads only the installed files, so the
// proven target must reach them without changing anything else in that file.
func TestComposePersistsProvenImageForPlainComposeUp(t *testing.T) {
	e, _, target := newComposeFixture(t)
	path := e.c.Compose.EnvironmentFile
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Prepare(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	if err := e.Apply(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	unproven, err := os.ReadFile(path)
	if err != nil || string(unproven) != string(before) {
		t.Fatalf("desired image changed before target health was proven: %s %v", unproven, err)
	}
	if _, err := e.Observe(t.Context()); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.ReplaceAll(string(before), fixtureRepository+"@sha256:"+strings.Repeat("a", 64), target.Image)
	if string(after) != want {
		t.Fatalf("environment file not updated exactly:\n%s", after)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("environment permissions changed: %v %v", info.Mode(), err)
	}
	if _, err := os.Lstat(filepath.Join(e.c.StateDirectory, "compose-active.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private image override retained: %v", err)
	}
	// The next operation derives its desired state from those same plain files.
	if _, err := e.composeRendered(t.Context()); err != nil {
		t.Fatalf("persisted image refused by the next desired-state check: %v", err)
	}
}

// An operator edit made while the target was replacing the source is never overwritten.
func TestComposeKeepsAnImageEditMadeDuringReplacement(t *testing.T) {
	e, _, target := newComposeFixture(t)
	if _, err := e.Prepare(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	if err := e.Apply(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	path := e.c.Compose.EnvironmentFile
	edited := "HIKYO_SERVER_IMAGE=" + fixtureRepository + ":operator-choice\n"
	if err := os.WriteFile(path, []byte(edited), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Observe(t.Context()); !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), target.Image) {
		t.Fatalf("an operator edit was not refused with what to fix: %v", err)
	}
	if after, err := os.ReadFile(path); err != nil || string(after) != edited {
		t.Fatalf("the operator edit was overwritten: %s %v", after, err)
	}
	if j, err := e.journal(); err != nil || j.Phase != "applying" {
		t.Fatalf("the target was released despite the edit: %+v %v", j, err)
	}
	// Once the operator sets the target, the healthy target is committed.
	if err := os.WriteFile(path, []byte("HIKYO_SERVER_IMAGE="+target.Image+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Observe(t.Context()); err != nil {
		t.Fatal(err)
	}
	if j, err := e.journal(); err != nil || j.Phase != "healthy" {
		t.Fatalf("the fixed file did not release the target: %+v %v", j, err)
	}
}

func TestComposeRefusesImageItCannotPersist(t *testing.T) {
	for name, contents := range map[string]string{
		// The value matches, but the Compose file names the image itself.
		"hardcoded": "HIKYO_SERVER_IMAGE=" + fixtureRepository + "@sha256:" + strings.Repeat("a", 64) + "\n",
		"absent":    "HIKYO_EXTERNAL_ORIGIN=https://hikyo.example\n",
		"ambiguous": "HIKYO_SERVER_IMAGE=" + fixtureRepository + "@sha256:" + strings.Repeat("a", 64) + "\nHIKYO_SERVER_IMAGE=" + fixtureRepository + "@sha256:" + strings.Repeat("a", 64) + "\n",
		"foreign":   "HIKYO_SERVER_IMAGE=registry.example/other@sha256:" + strings.Repeat("a", 64) + "\n",
		// The rendered image differs, so this file does not select the workload.
		"unused": "HIKYO_SERVER_IMAGE=" + fixtureRepository + "@sha256:" + strings.Repeat("d", 64) + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			e, runner, target := newComposeFixture(t)
			runner.hardcodedImage = name == "hardcoded"
			if err := os.WriteFile(e.c.Compose.EnvironmentFile, []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := e.Prepare(t.Context(), target); !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), "HIKYO_SERVER_IMAGE") {
				t.Fatalf("unpersistable image admitted: %v", err)
			}
			if slices.Contains(runner.events, "stop") || slices.Contains(runner.events, "stage") {
				t.Fatalf("refusal came after staging or mutation: %v", runner.events)
			}
		})
	}
}

// A journal that cannot be restored still says applying, so reporting a refusal would
// drop the coordinator's pin while Prepare goes on refusing.
func TestRefusalWithUnrestoredJournalIsNotARefusal(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("directory permissions do not bind root")
	}
	e, _, target := newComposeFixture(t)
	if _, err := e.Prepare(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	j, err := e.journal()
	if err != nil || j == nil {
		t.Fatalf("no journal: %v", err)
	}
	if err := os.Chmod(e.c.StateDirectory, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(e.c.StateDirectory, 0700) })
	err = e.refuseMutation(j, ErrPreconditionRefused)
	if err == nil || errors.Is(err, ErrPreconditionRefused) || j.Phase != "applying" {
		t.Fatalf("unrestored journal reported as a refusal: %v phase=%s", err, j.Phase)
	}
	if err := os.Chmod(e.c.StateDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := e.refuseMutation(j, ErrPreconditionRefused); !errors.Is(err, ErrPreconditionRefused) || j.Phase != "prepared" {
		t.Fatalf("restored journal lost its refusal: %v phase=%s", err, j.Phase)
	}
}

func TestReconcileClearsApplyingJournalOnlyWhenHealthy(t *testing.T) {
	e, runner, target := newComposeFixture(t)
	runner.failStop = true
	if _, err := e.Prepare(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	environment, err := os.ReadFile(e.c.Compose.EnvironmentFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Apply(t.Context(), target); err == nil || errors.Is(err, ErrPreconditionRefused) {
		t.Fatalf("failed stop lost its ambiguous classification: %v", err)
	}
	if _, err := e.Prepare(t.Context(), target); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("failed stop did not pin recovery: %v", err)
	}
	source := fixtureObservation()
	source.Image = runner.image
	// The coordinator decides whether the pin is stale; the journal only refuses
	// to be released while the installation itself is not healthy.
	for name, change := range map[string]func(*Observation){
		"phase":       func(o *Observation) { o.Phase = "migrating" },
		"maintenance": func(o *Observation) { o.Maintenance = true },
		"not-ready":   func(o *Observation) { o.Ready = false },
	} {
		t.Run(name, func(t *testing.T) {
			observed := source
			change(&observed)
			if err := e.Reconcile(t.Context(), observed); !errors.Is(err, ErrConflict) {
				t.Fatalf("unhealthy observation cleared recovery: %v", err)
			}
			if j, err := e.journal(); err != nil || j == nil || j.Phase != "applying" {
				t.Fatalf("refused reconciliation changed the journal: %+v %v", j, err)
			}
		})
	}
	if err := e.Reconcile(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(e.c.Compose.EnvironmentFile)
	if err != nil || string(after) != string(environment) {
		t.Fatalf("reconciliation left a changed desired state: %s %v", after, err)
	}
	if _, err := os.Lstat(filepath.Join(e.c.StateDirectory, "compose-active.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reconciliation left a private image override: %v", err)
	}
	// Retry needs a complete fresh preparation, not an adopted stale proof.
	if err := e.Apply(t.Context(), target); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale preparation applied after reconciliation: %v", err)
	}
	runner.failStop = false
	runner.running = true
	if _, err := e.Prepare(t.Context(), target); err != nil {
		t.Fatalf("retry refused after reconciliation: %v", err)
	}
	if err := e.Apply(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	// Nothing to clear: an unpinned journal does not need the exact source.
	if _, err := e.Observe(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := e.Reconcile(t.Context(), source); err != nil {
		t.Fatalf("unpinned reconciliation failed: %v", err)
	}
	if j, err := e.journal(); err != nil || j == nil || j.Phase != "healthy" {
		t.Fatalf("unpinned reconciliation changed a healthy journal: %+v %v", j, err)
	}
}

func TestDeploymentDecodesTargetObservationsTolerantlyAndJournalsStrictly(t *testing.T) {
	observation := encoded(t, fixtureObservation())
	future := append(observation[:len(observation)-1:len(observation)-1], []byte(`,"added_by_a_newer_target":{"nested":[1]}}`)...)
	var decoded Observation
	if err := decode(future, &decoded); err != nil || decoded.Instance != fixtureObservation().Instance {
		t.Fatalf("new target observation field refused: %+v %v", decoded, err)
	}
	if err := decode(append(append([]byte(nil), future...), []byte(`{"second":"document"}`)...), new(Observation)); err == nil {
		t.Fatal("observation trailing data accepted")
	}
	if err := decode(append(future, make([]byte, maxRecordBytes)...), new(Observation)); err == nil {
		t.Fatal("oversized observation accepted")
	}
	if err := decode([]byte(`{"format":"x","unknown_journal_field":true}`), new(deploymentJournal)); err == nil {
		t.Fatal("journal accepted an unknown field")
	}
	e, runner, target := newComposeFixture(t)
	runner.observationField = true
	if _, err := e.Prepare(t.Context(), target); err != nil {
		t.Fatalf("Compose refused a newer target's observation: %v", err)
	}
}

func TestComposeRestoresTheSourceFromItsSnapshotOnlyAfterAProvenAbort(t *testing.T) {
	e, runner, target := newComposeFixture(t)
	if _, err := e.Prepare(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	aborted := fixtureObservation()
	aborted.Phase, aborted.Maintenance, aborted.Ready, aborted.AbortedBeforeMutation = "backup-preparing", true, false, true
	// A prepared journal has replaced nothing, so there is nothing to restore.
	if err := e.RestoreSource(t.Context(), aborted); !errors.Is(err, ErrConflict) {
		t.Fatalf("restored a source that was never replaced: %v", err)
	}
	if err := e.Apply(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	commands := len(runner.commands)
	snapshot := filepath.Join(e.c.StateDirectory, "compose-prepared.json")
	original, err := os.ReadFile(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Observation){
		"without the target's report": func(o *Observation) { o.AbortedBeforeMutation = false },
		"another generation":          func(o *Observation) { o.Generation++ },
		"another release":             func(o *Observation) { o.Identity = fixtureIdentity(2) },
		"another installation":        func(o *Observation) { o.Instance = "other" },
		"another recovery":            func(o *Observation) { o.Incarnation = "recovery-incarnation-2" },
	} {
		observed := aborted
		change(&observed)
		if err := e.RestoreSource(t.Context(), observed); !errors.Is(err, ErrConflict) || len(runner.commands) != commands {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if err := os.WriteFile(snapshot, append(original, ' '), 0600); err != nil {
		t.Fatal(err)
	}
	if err := e.RestoreSource(t.Context(), aborted); !errors.Is(err, ErrConflict) || len(runner.commands) != commands {
		t.Fatalf("restored from a snapshot that is not the prepared one: %v", err)
	}
	if err := os.WriteFile(snapshot, original, 0600); err != nil {
		t.Fatal(err)
	}
	other := *e
	other.c.Kind = ECS
	if err := other.RestoreSource(t.Context(), aborted); !errors.Is(err, ErrUnsupported) || len(runner.commands) != commands {
		t.Fatalf("an adapter without a restore path ran one: %v", err)
	}
	if err := e.RestoreSource(t.Context(), aborted); err != nil {
		t.Fatal(err)
	}
	last := runner.commands[len(runner.commands)-1].Args
	if !slices.Equal(runner.events, []string{"pull", "stage", "stop", "replace", "restore"}) || runner.image != runner.sourceImage || slices.Contains(last, filepath.Join(e.c.StateDirectory, "compose-stage.json")) || !slices.Contains(last, snapshot) || !slices.Contains(last, "never") {
		t.Fatalf("the source did not return from the prepared snapshot: %v %v", runner.events, last)
	}
	// The source's own observation, not this call, is what releases the pin.
	if j, err := e.journal(); err != nil || j == nil || j.Phase != "applying" {
		t.Fatalf("a restore released the pin on its own: %+v %v", j, err)
	}
}

func TestComposeObservesPastAnExitedOneOffLeftByACrash(t *testing.T) {
	e, runner, _ := newComposeFixture(t)
	runner.exitedOneOff = true
	if _, err := e.Observe(t.Context()); err != nil {
		t.Fatalf("a dead one-off hid the running service: %v", err)
	}
}

func TestComposeRollsBackInOrderAndWritesTheSourceBack(t *testing.T) {
	for name, test := range map[string]struct {
		setup  func(*composeFixtureRunner)
		events []string
		fails  bool
	}{
		"restored": {events: []string{"rm", "rollback", "restore"}},
		"an exited one-off container left by a crash": {setup: func(r *composeFixtureRunner) { r.exitedOneOff = true }, events: []string{"rm", "rollback", "restore"}},
		"stop proof fails":           {setup: func(r *composeFixtureRunner) { r.stuck = true }, events: []string{"rm"}, fails: true},
		"one-shot fails":             {setup: func(r *composeFixtureRunner) { r.failRollback = true }, events: []string{"rm", "rollback", "restore"}, fails: true},
		"one-shot prints no report":  {setup: func(r *composeFixtureRunner) { r.reportless = true }, events: []string{"rm", "rollback", "restore"}, fails: true},
		"target past its final gate": {setup: func(r *composeFixtureRunner) { r.observation.Phase = "healthy" }, events: []string{}, fails: true},
	} {
		t.Run(name, func(t *testing.T) {
			e, runner, target := newComposeFixture(t)
			if _, err := e.Prepare(t.Context(), target); err != nil {
				t.Fatal(err)
			}
			if _, err := e.RollBack(t.Context()); !errors.Is(err, ErrConflict) {
				t.Fatalf("rolled back a replacement that never began: %v", err)
			}
			if err := e.Apply(t.Context(), target); err != nil {
				t.Fatal(err)
			}
			runner.observation.Phase = "migrating"
			if test.setup != nil {
				test.setup(runner)
			}
			before := len(runner.events)
			_, err := e.RollBack(t.Context())
			if (err != nil) != test.fails {
				t.Fatalf("err=%v, want failure %v", err, test.fails)
			}
			if got := runner.events[before:]; !slices.Equal(got, test.events) {
				t.Fatalf("events %v, want %v", got, test.events)
			}
			if j, err := e.journal(); err != nil || j == nil || j.Phase != "applying" {
				t.Fatalf("a rollback released the pin on its own: %+v %v", j, err)
			}
		})
	}
}

// A source someone started again is removed only while it is not serving, healthy and ready.
func TestComposeRollbackLeavesASourceServingAgain(t *testing.T) {
	for name, health := range map[string]string{"healthy": "healthy", "starting": "starting"} {
		t.Run(name, func(t *testing.T) {
			e, runner := applyOnCompose(t)
			runner.image, runner.observation, runner.health = runner.sourceImage, fixtureObservation(), health
			before := len(runner.events)
			_, err := e.RollBack(t.Context())
			if health == "healthy" && (!errors.Is(err, ErrConflict) || len(runner.events) != before) {
				t.Fatalf("a serving source was removed: %v %v", err, runner.events[before:])
			}
			if health == "starting" && (err != nil || !slices.Equal(runner.events[before:], []string{"rm", "rollback", "restore"})) {
				t.Fatalf("a source that was not ready blocked the rollback: %v %v", err, runner.events[before:])
			}
		})
	}
}

// An operator's third image is neither the target to remove nor the source to restore.
func TestComposeRollbackLeavesAThirdImageRunning(t *testing.T) {
	e, runner := applyOnCompose(t)
	runner.image = fixtureRepository + "@sha256:" + strings.Repeat("e", 64)
	before := len(runner.events)
	if _, err := e.RollBack(t.Context()); !errors.Is(err, ErrConflict) || len(runner.events) != before {
		t.Fatalf("a third image was removed: %v %v", err, runner.events[before:])
	}
}

// A restarting service cannot be exec'd into, so the final-gate proof reads the row through a one-off.
func TestComposeNoFinalGateReadsTheRowThroughAOneOff(t *testing.T) {
	source := fixtureObservation()
	pastGate := source
	pastGate.Identity = fixtureIdentity(2)
	writing := pastGate
	writing.Phase = "restore-required"
	maintenance := pastGate
	maintenance.Maintenance = true
	foreign := source
	foreign.Instance = "ins_another"
	for name, test := range map[string]struct {
		row     *Observation
		proven  bool
		wantErr bool
	}{
		"another installation's row":     {row: &foreign, wantErr: true},
		"restore required at the target": {row: &writing, proven: true},
		"maintenance at the target":      {row: &maintenance, proven: true},
		"healthy at the source":          {row: &source, proven: true},
		"healthy at another release":     {row: &pastGate},
		"unreadable":                     {wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			e, runner, _ := newComposeFixture(t)
			runner.running = false // the service is restarting: only a one-off can read the row
			runner.inspectRow = test.row
			proven, err := e.NoFinalGate(t.Context(), source)
			if proven != test.proven || (err != nil) != test.wantErr {
				t.Fatalf("proven=%t err=%v", proven, err)
			}
			if runner.events[len(runner.events)-1] != "inspect" {
				t.Fatalf("events %v", runner.events)
			}
		})
	}
}

// The one-shot and the stage run get the window the coordinator waits for, so a long restore
// is not killed at fifteen minutes while the coordinator is still waiting.
func TestUnsetTimeoutMatchesWhatTheCoordinatorWaits(t *testing.T) {
	for kind, want := range map[Kind]time.Duration{Compose: DefaultTimeout, Kubernetes: DefaultTimeout, ECS: 900 * time.Second} {
		if got := (&Executor{c: Config{Kind: kind}}).timeout(); got != want {
			t.Fatalf("%s default = %s, want %s", kind, got, want)
		}
	}
	if got := (&Executor{c: Config{Kind: Compose, TimeoutSeconds: 60}}).timeout(); got != time.Minute {
		t.Fatalf("configured timeout = %s", got)
	}
}

func applyOnCompose(t *testing.T) (*Executor, *composeFixtureRunner) {
	t.Helper()
	e, runner, target := newComposeFixture(t)
	if _, err := e.Prepare(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	if err := e.Apply(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	// A target that never reported healthy: one that did is past its final gate and is not rolled back.
	runner.observation.Phase = "migrating"
	return e, runner
}

// The marker vouches that no gate was crossed, so it is never set while the target may still run.
func TestRollbackMarkerIsNotSetWhileTheTargetMayStillRun(t *testing.T) {
	e, runner := applyOnCompose(t)
	runner.stuck = true // the stop proof fails
	if _, err := e.RollBack(t.Context()); err == nil {
		t.Fatal("a target that did not stop was rolled back")
	}
	if j, err := e.journal(); err != nil || j == nil || j.RollingBack || j.Phase != "applying" {
		t.Fatalf("a refused stop left a marker: %+v %v", j, err)
	}
	// With no marker the gate reads the row, and a row past the gate is not proven.
	past := fixtureObservation()
	past.Identity = fixtureIdentity(2)
	runner.inspectRow, runner.running = &past, false
	if proven, err := e.NoFinalGate(t.Context(), fixtureObservation()); err != nil || proven {
		t.Fatalf("a stale refusal vouched for a target past the gate: %t %v", proven, err)
	}
}

// A crash while the database is absent leaves no row to read: the journal says the decision was taken.
func TestRollbackMarkerIsDurableOnceTheTargetIsStoppedAndProvesTheGate(t *testing.T) {
	e, runner := applyOnCompose(t)
	runner.failRollback = true // the one-shot fails after the target was proven stopped
	if _, err := e.RollBack(t.Context()); err == nil {
		t.Fatal("a failed one-shot was reported as success")
	}
	if j, err := e.journal(); err != nil || j == nil || !j.RollingBack || j.Phase != "applying" {
		t.Fatalf("no durable marker: %+v %v", j, err)
	}
	// The row cannot be read and says nothing, so the marker answers.
	runner.inspectRow, runner.running = nil, false
	proven, err := e.NoFinalGate(t.Context(), fixtureObservation())
	if err != nil || !proven || runner.events[len(runner.events)-1] != "inspect" {
		t.Fatalf("a journal that began the rollback did not prove it: %t %v %v", proven, err, runner.events)
	}
	// A row that can be read outranks the marker: a person may have carried the target through the gate since.
	served := fixtureObservation()
	served.Identity = fixtureIdentity(2)
	runner.inspectRow = &served
	if proven, err := e.NoFinalGate(t.Context(), fixtureObservation()); err != nil || proven {
		t.Fatalf("the marker hid a target that served: %t %v", proven, err)
	}
}

// A person may have started the target after a refused rollback. During a database outage the marker must not vouch for it.
func TestRollbackMarkerDoesNotVouchForATargetRunningAgain(t *testing.T) {
	e, runner := applyOnCompose(t)
	runner.failRollback = true
	if _, err := e.RollBack(t.Context()); err == nil {
		t.Fatal("a failed one-shot was reported as success")
	}
	j, err := e.journal()
	if err != nil || j == nil || !j.RollingBack {
		t.Fatalf("no marker: %+v %v", j, err)
	}
	runner.inspectRow, runner.running, runner.image = nil, true, j.Target.Image
	if proven, err := e.NoFinalGate(t.Context(), fixtureObservation()); proven || err == nil {
		t.Fatalf("the marker vouched for a running target: %t %v", proven, err)
	}
	runner.image, runner.containerImage = j.Target.Image, "registry.example/hikyo:security-fix" // a target a person started by tag
	if proven, err := e.NoFinalGate(t.Context(), fixtureObservation()); proven || err == nil {
		t.Fatalf("the marker vouched for an image that is not the source: %t %v", proven, err)
	}
	runner.image, runner.containerImage = runner.sourceImage, "" // the source written back, crash-looping on an absent database
	if proven, err := e.NoFinalGate(t.Context(), fixtureObservation()); !proven || err != nil {
		t.Fatalf("the marker no longer answers for the source: %t %v", proven, err)
	}
	// An install pinned by tag writes the tag back; its RepoDigests still name the journaled source.
	runner.containerImage = "registry.example/hikyo:1.0.0"
	if proven, err := e.NoFinalGate(t.Context(), fixtureObservation()); !proven || err != nil {
		t.Fatalf("the marker refuses a tag-pinned source written back: %t %v", proven, err)
	}
	runner.image = j.Target.Image // the same tag, repointed at the target
	if proven, err := e.NoFinalGate(t.Context(), fixtureObservation()); proven || err == nil {
		t.Fatalf("the marker vouched for a tag that resolves to the target: %t %v", proven, err)
	}
	runner.image, runner.containerImage, runner.secondImage = runner.sourceImage, "", j.Target.Image // the target beside the source
	if proven, err := e.NoFinalGate(t.Context(), fixtureObservation()); proven || err == nil {
		t.Fatalf("the marker vouched while a second container runs the target: %t %v", proven, err)
	}
	// Without a digest there is nothing to resolve the live container against, so the marker cannot answer.
	runner.secondImage = ""
	j.Source.Image = "registry.example/hikyo:1.0.0"
	if err := e.save(j); err != nil {
		t.Fatal(err)
	}
	if proven, err := e.NoFinalGate(t.Context(), fixtureObservation()); proven || !errors.Is(err, ErrInvalid) {
		t.Fatalf("the marker vouched against a source not pinned by digest: %t %v", proven, err)
	}
}

// The coordinator can die after this release and before it records it, so the journal keeps saying so until the next Prepare.
func TestReconcileAfterARollbackKeepsSayingSoUntilTheNextPrepare(t *testing.T) {
	e, runner := applyOnCompose(t)
	runner.failRollback = true
	if _, err := e.RollBack(t.Context()); err == nil {
		t.Fatal("a failed one-shot was reported as success")
	}
	j, err := e.journal()
	if err != nil || j == nil {
		t.Fatal(j, err)
	}
	target := j.Target
	source := fixtureObservation()
	source.Image = runner.sourceImage
	for range 2 {
		if err := e.Reconcile(t.Context(), source); !errors.Is(err, ErrFollowsRollback) {
			t.Fatalf("the release lost the rollback: %v", err)
		}
		if j, err := e.journal(); err != nil || j == nil || j.Phase != "rolled-back" {
			t.Fatalf("journal %+v %v", j, err)
		}
	}
	row := runner.inspectRow
	runner.inspectRow = nil
	if proven, err := e.NoFinalGate(t.Context(), source); proven || err == nil {
		t.Fatalf("the marker answered after the rollback ended: %t %v", proven, err)
	}
	runner.inspectRow = row
	runner.running, runner.observation = true, fixtureObservation()
	if _, err := e.Prepare(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	if err := e.Reconcile(t.Context(), source); err != nil {
		t.Fatalf("a new attempt reported the old rollback: %v", err)
	}
}

// A resumed rollback may find the row already restored; the source may also have consumed the journal that said so.
func TestResumedRollbackReportsRestoredEvenWhenTheOneShotFindsNothingToDo(t *testing.T) {
	e, runner := applyOnCompose(t)
	runner.failRollback = true
	if _, err := e.RollBack(t.Context()); err == nil {
		t.Fatal("a failed one-shot was reported as success")
	}
	runner.failRollback, runner.notNeeded = false, true
	if restored, err := e.RollBack(t.Context()); err != nil || !restored {
		t.Fatalf("resumed: restored=%t err=%v", restored, err)
	}
}

func TestRollBackReportsWhetherTheSnapshotWasRestored(t *testing.T) {
	e, _ := applyOnCompose(t)
	restored, err := e.RollBack(t.Context())
	if err != nil || !restored {
		t.Fatalf("restored=%t err=%v", restored, err)
	}
	for result, want := range map[string]bool{"restored": true, "not-needed": false} {
		got, err := parseRollbackReport([]byte(`{"format":"` + RollbackReportFormat + `","result":"` + result + `","reason":"x"}`))
		if err != nil || got != want {
			t.Fatalf("%s: %t %v", result, got, err)
		}
	}
}

// Pod logs and a terminal merge stderr, so a warning can precede the report and the row.
func TestOneShotOutputIsReadFromItsLastLine(t *testing.T) {
	e, runner := applyOnCompose(t)
	runner.logLine = "warning: HIKYO_ROOT_KEY is set\n"
	if restored, err := e.RollBack(t.Context()); err != nil || !restored {
		t.Fatalf("a leading log line broke the report: %t %v", restored, err)
	}
	row := fixtureObservation()
	runner.inspectRow, runner.running = &row, false
	if proven, err := e.NoFinalGate(t.Context(), row); err != nil || !proven {
		t.Fatalf("a leading log line broke the row: %t %v", proven, err)
	}
}

// The one-shot may use the whole window, and the source must still be written back after it.
func TestWriteBackGetsItsOwnWindowAfterTheOneShot(t *testing.T) {
	e, runner := applyOnCompose(t)
	ctx, cancel := context.WithCancel(t.Context())
	runner.afterRollback = cancel
	if _, err := e.RollBack(ctx); err != nil {
		t.Fatal(err)
	}
	if runner.restoreErr != nil {
		t.Fatalf("the write-back ran on a spent context: %v", runner.restoreErr)
	}
}

func TestTargetImageIsMatchedByDigest(t *testing.T) {
	digest := strings.Repeat("a", 64)
	target := Target{Image: "registry.example/hikyo@sha256:" + digest, RuntimeImageSHA256: strings.Repeat("b", 64)}
	for _, c := range []struct {
		reference, id string
		want          bool
	}{
		{"other.example/mirror@sha256:" + digest, "", true},
		{"registry.example/hikyo:1.2.3", "sha256:" + strings.Repeat("b", 64), true},
		{"registry.example/hikyo:1.2.3", "sha256:" + digest, true},
		{"registry.example/hikyo:1.2.3", "", false},
		{"registry.example/hikyo@sha256:" + strings.Repeat("c", 64), "sha256:" + strings.Repeat("c", 64), false},
	} {
		if got := isTargetImage(c.reference, c.id, target); got != c.want {
			t.Errorf("%s %s: got %t", c.reference, c.id, got)
		}
	}
}

// Generation moves with ordinary writes, so a serving source image is left alone at any generation.
func TestComposeRollbackLeavesTheSourceImageServingAtAnotherGeneration(t *testing.T) {
	e, runner := applyOnCompose(t)
	runner.image, runner.observation = runner.sourceImage, fixtureObservation()
	runner.observation.Generation++
	before := len(runner.events)
	if _, err := e.RollBack(t.Context()); !errors.Is(err, ErrConflict) || len(runner.events) != before {
		t.Fatalf("a serving source was removed: %v %v", err, runner.events[before:])
	}
}

// Ownership is read from the image, not the application, so a container that cannot be observed is
// removed only when it runs the target or the source.
func TestComposeRollbackRemovesOnlyTheTargetOrTheSource(t *testing.T) {
	for name, test := range map[string]struct {
		image   func(target string) string
		removed bool
	}{
		"another repository":               {image: func(string) string { return "docker.io/library/nginx@sha256:" + strings.Repeat("e", 64) }},
		"a third image that is restarting": {image: func(string) string { return fixtureRepository + "@sha256:" + strings.Repeat("e", 64) }},
		"the target restarting":            {image: func(target string) string { return target }, removed: true},
	} {
		t.Run(name, func(t *testing.T) {
			e, runner := applyOnCompose(t)
			runner.image = test.image(runner.target.Image)
			runner.running = name == "another repository"
			before := len(runner.events)
			_, err := e.RollBack(t.Context())
			if test.removed && (err != nil || !slices.Equal(runner.events[before:], []string{"rm", "rollback", "restore"})) {
				t.Fatalf("a restarting target was not rolled back: %v %v", err, runner.events[before:])
			}
			if !test.removed && (!errors.Is(err, ErrConflict) || len(runner.events) != before) {
				t.Fatalf("a container that is neither target nor source was removed: %v %v", err, runner.events[before:])
			}
		})
	}
}

// A rollback ended the attempt; replacing again needs a fresh Prepare, not the old proof and marker.
func TestApplyRefusesARolledBackJournal(t *testing.T) {
	e, runner, target := newComposeFixture(t)
	if _, err := e.Prepare(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	runner.failUp = true
	if err := e.Apply(t.Context(), target); err == nil {
		t.Fatal("a lost reply was reported as success")
	}
	runner.failUp, runner.notNeeded = false, true
	if _, err := e.RollBack(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := e.Reconcile(t.Context(), fixtureObservation()); !errors.Is(err, ErrFollowsRollback) {
		t.Fatalf("reconcile: %v", err)
	}
	before := len(runner.events)
	if err := e.Apply(t.Context(), target); !errors.Is(err, ErrConflict) || len(runner.events) != before {
		t.Fatalf("a rolled-back journal was applied again: %v %v", err, runner.events[before:])
	}
}

// The snapshot names the image as the environment file did; recovery starts the journaled source digest.
func TestComposeRecoveryStartsTheJournaledSourceImage(t *testing.T) {
	for name, selected := range map[string]string{
		"a mismatched digest": fixtureRepository + "@sha256:" + strings.Repeat("c", 64),
		"a moved tag":         fixtureRepository + ":1.0.0",
	} {
		for _, recovery := range []string{"rollback", "restore"} {
			t.Run(name+" "+recovery, func(t *testing.T) {
				e, runner, target := newComposeFixture(t)
				raw, err := os.ReadFile(e.c.Compose.EnvironmentFile)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(e.c.Compose.EnvironmentFile, []byte(strings.ReplaceAll(string(raw), runner.sourceImage, selected)), 0600); err != nil {
					t.Fatal(err)
				}
				runner.renderedImage = selected
				if _, err := e.Prepare(t.Context(), target); err != nil {
					t.Fatal(err)
				}
				if err := e.Apply(t.Context(), target); err != nil {
					t.Fatal(err)
				}
				runner.observation.Phase = "migrating"
				if recovery == "rollback" {
					_, err = e.RollBack(t.Context())
				} else {
					aborted := fixtureObservation()
					aborted.Phase, aborted.Maintenance, aborted.Ready, aborted.AbortedBeforeMutation = "backup-preparing", true, false, true
					err = e.RestoreSource(t.Context(), aborted)
				}
				if err != nil || runner.image != runner.sourceImage {
					t.Fatalf("recovery started %s, not the source %s: %v", runner.image, runner.sourceImage, err)
				}
			})
		}
	}
}

// A timed-out one-shot keeps running in the daemon: the source starts only once it is proven gone.
func TestComposeRollbackRemovesAOneShotThatOutlivedItsCommand(t *testing.T) {
	for name, test := range map[string]struct {
		stuck  bool
		events []string
	}{
		"removed":           {events: []string{"rm", "rollback", "clear", "restore"}},
		"cannot be removed": {stuck: true, events: []string{"rm", "rollback", "clear"}},
	} {
		t.Run(name, func(t *testing.T) {
			e, runner := applyOnCompose(t)
			runner.failRollback, runner.outlive, runner.oneShotStuck = true, true, test.stuck
			before := len(runner.events)
			_, err := e.RollBack(t.Context())
			if err == nil || test.stuck != errors.Is(err, ErrRecoveryRequired) || !slices.Equal(runner.events[before:], test.events) {
				t.Fatalf("err=%v events=%v, want %v", err, runner.events[before:], test.events)
			}
		})
	}
}

// A retried rollback removes the one-shot an earlier attempt left before it runs its own under the same name.
func TestComposeRollbackRetryClearsTheEarlierOneShot(t *testing.T) {
	e, runner := applyOnCompose(t)
	runner.oneShotLeft = true
	before := len(runner.events)
	if _, err := e.RollBack(t.Context()); err != nil || !slices.Equal(runner.events[before:], []string{"rm", "clear", "rollback", "restore"}) {
		t.Fatalf("err=%v events=%v", err, runner.events[before:])
	}
}

// Where the image store records the platform manifest, completion requires the verified one.
func TestComposeObserveVerifiesThePlatformManifest(t *testing.T) {
	for name, test := range map[string]struct {
		manifest string
		healthy  bool
	}{
		"the verified manifest":             {manifest: "sha256:" + strings.Repeat("d", 64), healthy: true},
		"another manifest of the index":     {manifest: "sha256:" + strings.Repeat("e", 64)},
		"a classic store that records none": {healthy: true},
	} {
		t.Run(name, func(t *testing.T) {
			e, runner, target := newComposeFixture(t)
			target.RuntimeImageSHA256 = strings.Repeat("d", 64)
			runner.target = target
			runner.manifest = "sha256:" + strings.Repeat("f", 64) // the source's own platform manifest
			if _, err := e.Prepare(t.Context(), target); err != nil {
				t.Fatal(err)
			}
			if err := e.Apply(t.Context(), target); err != nil {
				t.Fatal(err)
			}
			runner.manifest = test.manifest
			_, err := e.Observe(t.Context())
			j, journalErr := e.journal()
			if journalErr != nil || (j.Phase == "healthy") != test.healthy || (err == nil) != test.healthy || (!test.healthy && !errors.Is(err, ErrConflict)) {
				t.Fatalf("phase=%s err=%v", j.Phase, err)
			}
			environment, readErr := os.ReadFile(e.c.Compose.EnvironmentFile)
			if readErr != nil || strings.Contains(string(environment), target.Image) != test.healthy {
				t.Fatalf("desired state published %t, want %t", strings.Contains(string(environment), target.Image), test.healthy)
			}
		})
	}
}

// The Docker CLI runs its compose plugin and credential helpers with the coordinator's authority, so
// each, and every directory searched before it, is held to the docker executable's rules.
func TestDockerClientIsVettedBeforeCompose(t *testing.T) {
	for name, test := range map[string]struct {
		change func(t *testing.T, root string) (path string)
		named  string
	}{
		"an installed client":                {change: func(*testing.T, string) string { return "" }},
		"a writable plugin directory":        {change: func(t *testing.T, root string) string { mode(t, root+"/docker/cli-plugins", 0775); return "" }, named: "/docker/cli-plugins"},
		"a writable configuration":           {change: func(t *testing.T, root string) string { mode(t, root+"/docker/config.json", 0620); return "" }, named: "/docker/config.json"},
		"a writable configuration directory": {change: func(t *testing.T, root string) string { mode(t, root+"/docker", 0777); return "" }, named: "/docker"},
		"a malformed configuration": {change: func(t *testing.T, root string) string {
			put(t, root+"/docker/config.json", `{"credsStore":1}`, 0600)
			return ""
		}, named: "/docker/config.json"},
		"a writable plugin": {change: func(t *testing.T, root string) string {
			mode(t, root+"/docker/cli-plugins/docker-compose", 0757)
			return ""
		}, named: "docker-compose"},
		"a plugin linked to a writable file": {change: func(t *testing.T, root string) string {
			put(t, root+"/writable-compose", "plugin", 0777)
			if err := os.Remove(root + "/docker/cli-plugins/docker-compose"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(root+"/writable-compose", root+"/docker/cli-plugins/docker-compose"); err != nil {
				t.Fatal(err)
			}
			return ""
		}, named: "/writable-compose"},
		"a plugin linked through a writable directory": {change: func(t *testing.T, root string) string {
			directory(t, root+"/trusted", 0755)
			put(t, root+"/trusted/docker-compose", "plugin", 0755)
			directory(t, root+"/writable", 0777)
			link(t, root+"/trusted/docker-compose", root+"/writable/docker-compose")
			link(t, root+"/writable/docker-compose", root+"/docker/cli-plugins/docker-compose")
			return ""
		}, named: "/writable"},
		"a plugin linked through trusted directories": {change: func(t *testing.T, root string) string {
			directory(t, root+"/trusted/bin", 0755)
			put(t, root+"/trusted/bin/docker-compose", "plugin", 0755)
			link(t, "bin/docker-compose", root+"/trusted/current")
			link(t, root+"/trusted/current", root+"/docker/cli-plugins/docker-compose")
			return ""
		}},
		"a writable extra plugin directory searched first": {change: func(t *testing.T, root string) string {
			directory(t, root+"/extra", 0777)
			put(t, root+"/docker/config.json", `{"cliPluginsExtraDirs":["`+root+`/extra"],"credsStore":"vault"}`, 0600)
			return ""
		}, named: "/extra"},
		"a writable system plugin directory holding compose": {change: func(t *testing.T, root string) string {
			if err := os.Remove(root + "/docker/cli-plugins/docker-compose"); err != nil {
				t.Fatal(err)
			}
			directory(t, root+"/system/cli-plugins", 0777)
			put(t, root+"/system/cli-plugins/docker-compose", "plugin", 0755)
			return ""
		}, named: "/system/cli-plugins"},
		"no compose plugin": {change: func(t *testing.T, root string) string {
			if err := os.Remove(root + "/docker/cli-plugins/docker-compose"); err != nil {
				t.Fatal(err)
			}
			return ""
		}, named: "docker-compose"},
		"a writable PATH directory searched before the helper": {change: func(t *testing.T, root string) string {
			directory(t, root+"/early", 0777)
			return root + "/early:" + root + "/bin"
		}, named: "/early"},
		"a writable credential helper": {change: func(t *testing.T, root string) string { mode(t, root+"/bin/docker-credential-vault", 0775); return "" }, named: "docker-credential-vault"},
		"a missing credential helper": {change: func(t *testing.T, root string) string {
			if err := os.Remove(root + "/bin/docker-credential-vault"); err != nil {
				t.Fatal(err)
			}
			return ""
		}, named: "docker-credential-vault"},
	} {
		t.Run(name, func(t *testing.T) {
			root := privateDirectory(t)
			directory(t, root+"/docker/cli-plugins", 0755)
			mode(t, root+"/docker", 0700)
			put(t, root+"/docker/cli-plugins/docker-compose", "plugin", 0755)
			put(t, root+"/docker/config.json", `{"auths":{"registry.example":{"auth":"c2VjcmV0"}},"credsStore":"vault","credHelpers":{"registry.example":"vault"}}`, 0600)
			directory(t, root+"/bin", 0755)
			put(t, root+"/bin/docker-credential-vault", "helper", 0755)
			system := dockerSystemPluginDirs
			dockerSystemPluginDirs = []string{root + "/system/cli-plugins"}
			t.Cleanup(func() { dockerSystemPluginDirs = system })
			path := cmp.Or(test.change(t, root), root+"/bin")
			err := vetDockerClient(root+"/docker", path)
			if test.named == "" && err != nil {
				t.Fatalf("an installed client was refused: %v", err)
			}
			if test.named != "" && (!errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), test.named)) {
				t.Fatalf("refusal does not name %s: %v", test.named, err)
			}
		})
	}
}

func directory(t *testing.T, path string, perm os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	mode(t, path, perm)
}

// link replaces path with a symbolic link to target.
func link(t *testing.T, target, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

func put(t *testing.T, path, content string, perm os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	mode(t, path, perm)
}

func mode(t *testing.T, path string, perm os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, perm); err != nil {
		t.Fatal(err)
	}
}

// The service command is parsed the way the server parses its flags, as on Kubernetes.
func TestComposeRefusesACommandThatDisablesUnattendedUpgrades(t *testing.T) {
	for _, command := range [][]string{{"server", "--upgrade-unattended=0"}, {"server", "-auto-migrate=false"}, {"server", "--ha"}} {
		e, runner, target := newComposeFixture(t)
		runner.command = command
		if _, err := e.Prepare(t.Context(), target); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("command %q qualified: %v", command, err)
		}
	}
	e, runner, target := newComposeFixture(t)
	runner.command = []string{"server", "--upgrade-unattended"}
	if _, err := e.Prepare(t.Context(), target); err != nil {
		t.Fatalf("an enabling flag was refused: %v", err)
	}
}

// The most specific mount decides: a tmpfs or anonymous volume under a persistent bind shadows it.
func TestComposeStateMustBeOnItsMostSpecificMountPersistently(t *testing.T) {
	for name, test := range map[string]struct {
		mounts    []map[string]string
		tmpfs     []string
		supported bool
	}{
		"nested in the persistent data bind": {supported: true},
		"shadowed by a tmpfs mount":          {mounts: []map[string]string{{"type": "tmpfs", "target": "/var/lib/hikyo/upgrade"}}},
		"shadowed by a service tmpfs":        {tmpfs: []string{"/var/lib/hikyo/upgrade:size=64m"}},
		"shadowed by an anonymous volume":    {mounts: []map[string]string{{"type": "volume", "target": "/var/lib/hikyo/upgrade/"}}},
		"a persistent bind under a tmpfs":    {mounts: []map[string]string{{"type": "tmpfs", "target": "/var/lib/hikyo/upgrade"}, {"type": "bind", "source": "/srv/hikyo/state", "target": "/var/lib/hikyo/upgrade/current"}}, supported: true},
	} {
		t.Run(name, func(t *testing.T) {
			e, runner, target := newComposeFixture(t)
			runner.stateDirectory, runner.mounts, runner.tmpfs = "/var/lib/hikyo/upgrade/current", test.mounts, test.tmpfs
			_, err := e.Prepare(t.Context(), target)
			if test.supported != (err == nil) || !test.supported && !errors.Is(err, ErrUnsupported) {
				t.Fatalf("supported=%t err=%v", test.supported, err)
			}
		})
	}
}
