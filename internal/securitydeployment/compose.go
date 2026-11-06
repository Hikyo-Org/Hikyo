package securitydeployment

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/securefile"
)

const composeImageVariable = "HIKYO_SERVER_IMAGE"

const inspectContainer = `{"id":{{json .Id}},"image":{{json .Config.Image}},"image_id":{{json .Image}},"running":{{json .State.Running}},"health":{{if .State.Health}}{{json .State.Health.Status}}{{else}}null{{end}},"labels":{{json .Config.Labels}}}`

type composeContainer struct {
	ID      string            `json:"id"`
	Image   string            `json:"image"`
	ImageID string            `json:"image_id"`
	Running bool              `json:"running"`
	Health  string            `json:"health"`
	Labels  map[string]string `json:"labels"`
}

// composeConfig is the part of `docker compose config --format json` the adapter reads.
type composeConfig struct {
	Services map[string]composeService `json:"services"`
}

type composeService struct {
	Image       string            `json:"image"`
	Environment map[string]string `json:"environment"`
	Command     []string          `json:"command"`
	Privileged  bool              `json:"privileged"`
	Scale       int               `json:"scale"`
	Deploy      struct {
		Replicas *int `json:"replicas"`
	} `json:"deploy"`
	Volumes []struct {
		Type   string `json:"type"`
		Source string `json:"source"`
		Target string `json:"target"`
	} `json:"volumes"`
}

func (e *Executor) composeArgs(files ...string) []string {
	p := e.c.Compose
	args := []string{"compose", "--project-directory", p.Directory, "--project-name", p.Project}
	for _, file := range files {
		args = append(args, "--file", file)
	}
	return args
}

func (e *Executor) currentComposeArgs() []string {
	return append(e.composeArgs(e.c.Compose.File), "--env-file", e.c.Compose.EnvironmentFile)
}

// composeEnvironmentImage locates the one line that a plain `docker compose up`
// reads the image from. Anything else cannot follow an applied update, so it
// refuses before staging rather than reverting the workload after replacement.
func composeEnvironmentImage(path, repository string) ([]string, int, error) {
	raw, err := privateFile(path)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %s must be a regular file with mode 0600: %w", ErrInvalid, path, err)
	}
	lines, index := strings.SplitAfter(string(raw), "\n"), -1
	for i, line := range lines {
		if strings.HasPrefix(line, composeImageVariable+"=") {
			if index >= 0 {
				index = -1
				break
			}
			index = i
		}
	}
	if index >= 0 {
		value := composeImageValue(lines[index])
		if strings.HasPrefix(value, repository+"@sha256:") || strings.HasPrefix(value, repository+":") {
			return lines, index, nil
		}
	}
	return nil, 0, fmt.Errorf("%w: %s must set the image of %s on exactly one %s= line", ErrUnsupported, path, repository, composeImageVariable)
}

// composeEnvironmentValues lists the values of the protected file Compose reads,
// which is what it can echo in an error. Each value is listed in every form a
// reader might print it: the whole line, which covers a continuation of a multi-line
// value, then as written, unquoted, without an inline comment, and with escapes decoded.
// Values under four bytes are skipped: they are not secrets and would shred the text.
// shortcut: not a complete dotenv parser, the pattern scrubbers remain the second
// layer; adopt compose-go/dotenv if it becomes a dependency for another reason.
func composeEnvironmentValues(path string) ([]string, error) {
	raw, err := privateFile(path)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		value := line
		if index := strings.IndexAny(line, "=:"); index >= 0 {
			value = strings.TrimSpace(line[index+1:])
		}
		uncommented, _, _ := strings.Cut(value, " #")
		for _, form := range []string{line, value, strings.Trim(value, `"'`), strings.TrimSpace(uncommented), strings.Trim(strings.TrimSpace(uncommented), `"'`)} {
			for _, candidate := range []string{form, composeEnvironmentEscapes.Replace(form)} {
				if len(candidate) >= 4 {
					seen[candidate] = true
				}
			}
		}
	}
	// Longest first, so a value containing another one is removed whole.
	values := slices.Collect(maps.Keys(seen))
	slices.SortFunc(values, func(a, b string) int { return cmp.Or(len(b)-len(a), strings.Compare(a, b)) })
	return values, nil
}

var composeEnvironmentEscapes = strings.NewReplacer(`\"`, `"`, `\\`, `\`, `\n`, "\n", `\$`, `$`)

func composeImageValue(line string) string {
	return strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, composeImageVariable+"=")), `"'`)
}

// persistComposeImage is idempotent and runs only after target health is proven.
// WriteAtomic keeps the required mode; the desired-state hash is recomputed from
// these same files by the next Prepare. An operator edit since Prepare is refused, never overwritten.
func (e *Executor) persistComposeImage(j *deploymentJournal) error {
	path, image := e.c.Compose.EnvironmentFile, j.Target.Image
	lines, index, err := composeEnvironmentImage(path, e.c.ImageRepository)
	if err != nil {
		return err
	}
	current := composeImageValue(lines[index])
	if current == image {
		return nil
	}
	prepared, err := privateFile(filepath.Join(e.c.StateDirectory, "compose-prepared.json"))
	if err != nil {
		return err
	}
	var snapshot composeConfig
	if releaseidentity.Hash(prepared) != j.DesiredSHA256 {
		return ErrConflict
	}
	if err := json.Unmarshal(prepared, &snapshot); err != nil {
		return errors.Join(ErrConflict, err)
	}
	if current != snapshot.Services[e.c.Compose.Service].Image {
		return fmt.Errorf("%w: %s in %s changed during the update; set it to %s, the healthy release now running, or back to %s, for the update to finish", ErrConflict, composeImageVariable, path, image, snapshot.Services[e.c.Compose.Service].Image)
	}
	lines[index] = composeImageVariable + "=" + image + "\n"
	return securefile.WriteAtomic(path, []byte(strings.Join(lines, "")), 0600)
}

func privateFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > maxRecordBytes {
		return nil, ErrInvalid
	}
	return os.ReadFile(path)
}

func (e *Executor) composeRendered(ctx context.Context) ([]byte, error) {
	raw, err := e.run(ctx, e.c.Compose.Docker, append(e.currentComposeArgs(), "config", "--format", "json"), nil)
	if err != nil {
		return nil, err
	}
	var parsed composeConfig
	if len(raw) > maxRecordBytes || json.Unmarshal(raw, &parsed) != nil {
		return nil, ErrInvalid
	}
	s, ok := parsed.Services[e.c.Compose.Service]
	if !ok || !e.ourImage(s.Image) || s.Privileged || s.Scale > 1 || (s.Deploy.Replicas != nil && *s.Deploy.Replicas != 1) || s.Environment["HIKYO_UPGRADE_UNATTENDED"] != "true" || s.Environment["HIKYO_HA"] == "true" || s.Environment["HIKYO_CONFIG_ROLLOUT_ENROLLMENT"] != "" || !strings.HasPrefix(s.Environment["HIKYO_DB"], "sqlite:") {
		return nil, ErrUnsupported
	}
	lines, index, err := composeEnvironmentImage(e.c.Compose.EnvironmentFile, e.c.ImageRepository)
	if err != nil {
		return nil, err
	}
	// Equal values do not prove a hard-coded image follows the file; the shell overrides --env-file.
	probe := e.c.ImageRepository + ":hikyo-image-variable-probe"
	child, cancel := context.WithTimeout(ctx, e.timeout())
	probed, err := e.runner.Run(child, Command{Path: e.c.Compose.Docker, Args: append(e.currentComposeArgs(), "config", "--format", "json"), Env: []string{composeImageVariable + "=" + probe}})
	cancel()
	if err != nil {
		return nil, err
	}
	var probedConfig composeConfig
	if len(probed) > maxRecordBytes || json.Unmarshal(probed, &probedConfig) != nil {
		return nil, ErrInvalid
	}
	if composeImageValue(lines[index]) != s.Image || probedConfig.Services[e.c.Compose.Service].Image != probe {
		return nil, fmt.Errorf("%w: service %s does not take its image from %s in %s", ErrUnsupported, e.c.Compose.Service, composeImageVariable, e.c.Compose.EnvironmentFile)
	}
	state := s.Environment["HIKYO_UPGRADE_STATE_DIR"]
	data := strings.TrimPrefix(s.Environment["HIKYO_DB"], "sqlite:")
	if !safePath(state) || !safePath(data) {
		return nil, ErrUnsupported
	}
	statePersistent, dataPersistent := false, false
	for _, v := range s.Volumes {
		if strings.Contains(v.Source, "docker.sock") || strings.Contains(v.Target, "docker.sock") {
			return nil, ErrUnsupported
		}
		if (v.Type == "bind" || v.Type == "volume") && v.Source != "" {
			if state == v.Target || strings.HasPrefix(state, v.Target+"/") {
				statePersistent = true
			}
			if data == v.Target || strings.HasPrefix(data, v.Target+"/") {
				dataPersistent = true
			}
		}
	}
	if !statePersistent || !dataPersistent {
		return nil, ErrUnsupported
	}
	// Keep a canonical resolved configuration: later Compose operations must
	// consume these staged bytes rather than reread changing env/config inputs.
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil, ErrInvalid
	}
	return json.Marshal(value)
}

func (e *Executor) ourImage(image string) bool {
	return strings.HasPrefix(image, e.c.ImageRepository+"@sha256:") || strings.HasPrefix(image, e.c.ImageRepository+":") || image == e.c.ImageRepository
}

func (e *Executor) imageReferences(ctx context.Context, image string) ([]string, error) {
	raw, err := e.run(ctx, e.c.Compose.Docker, []string{"image", "inspect", "--format", "{{json .RepoDigests}}", image}, nil)
	if err != nil {
		return nil, err
	}
	var references []string
	if decode(raw, &references) != nil || len(references) > 64 {
		return nil, ErrInvalid
	}
	return references, nil
}

// composeLiveContainers lists a service's containers, without "exited": `run --rm` leaves an
// exited one-off when the host dies under it and `rm` does not remove one-offs, so counting it
// would refuse every later observation and rollback. It holds nothing.
func composeLiveContainers(compose []string, service string) []string {
	return append(compose, "ps", "--all", "--quiet", "--status", "running", "--status", "paused", "--status", "restarting", "--status", "removing", "--status", "dead", "--status", "created", service)
}

func (e *Executor) observeCompose(ctx context.Context) (Observation, error) {
	raw, err := e.run(ctx, e.c.Compose.Docker, composeLiveContainers(e.currentComposeArgs(), e.c.Compose.Service), nil)
	if err != nil {
		return Observation{}, err
	}
	ids := strings.Fields(string(raw))
	if len(ids) != 1 || len(ids[0]) != 64 || releaseidentity.Digest(ids[0]).Validate() != nil {
		return Observation{}, ErrNotReady
	}
	raw, err = e.run(ctx, e.c.Compose.Docker, []string{"inspect", "--type", "container", "--format", inspectContainer, ids[0]}, nil)
	if err != nil {
		return Observation{}, err
	}
	var container composeContainer
	if decode(raw, &container) != nil || container.ID != ids[0] || !container.Running || !e.ourImage(container.Image) || container.Labels["com.docker.compose.project"] != e.c.Compose.Project || container.Labels["com.docker.compose.service"] != e.c.Compose.Service {
		return Observation{}, ErrConflict
	}
	references, err := e.imageReferences(ctx, container.ImageID)
	if err != nil {
		return Observation{}, err
	}
	image := ""
	for _, reference := range references {
		if strings.HasPrefix(reference, e.c.ImageRepository+"@sha256:") && imageDigest(reference) != "" {
			if image != "" && image != reference {
				return Observation{}, ErrConflict
			}
			image = reference
		}
	}
	if image == "" || (imageDigest(container.Image) != "" && container.Image != image) {
		return Observation{}, ErrConflict
	}
	raw, err = e.run(ctx, e.c.Compose.Docker, []string{"exec", container.ID, e.c.Compose.Binary, "security-maintenance", "inspect"}, nil)
	if err != nil {
		return Observation{}, err
	}
	var observation Observation
	if decode(raw, &observation) != nil || !validObservation(observation) {
		return Observation{}, ErrInvalid
	}
	if observation.Engine != releaseidentity.SQLite {
		return Observation{}, ErrUnsupported
	}
	observation.Image = image
	observation.Ready = observation.Ready && container.Health == "healthy"
	// Docker RepoDigests binds its local platform image to a requested index;
	// .Image is a config digest and must never be reported as a manifest digest.
	observation.RuntimeImageSHA256 = ""
	return observation, nil
}

// NoFinalGate proves no gate was crossed unless the control row says the target is healthy at a
// release other than the source, or belongs to another installation. Only when no row can be read
// does the rollback marker answer, and only while the journal is still applying: the target was stopped after the gate decision, the one-shot
// re-checks rule 2 under its lock, and a crash while the database is absent leaves no row. The marker
// vouches only for that stop: a target running again was started by a person, who may have taken it
// through the gate. Compose reads the row through a one-off container, which runs while the
// service is restarting and cannot be exec'd into. Kubernetes reads it through a stage Pod on the
// node the source ran on.
func (e *Executor) NoFinalGate(ctx context.Context, source Observation) (bool, error) {
	if e.c.Kind != Compose && e.c.Kind != Kubernetes {
		return false, ErrUnsupported
	}
	j, err := e.journal()
	if err != nil {
		return false, err
	}
	var raw []byte
	if e.c.Kind == Kubernetes {
		raw, err = e.inspectKubernetes(ctx, j)
	} else {
		args := append(e.currentComposeArgs(), "run", "--rm", "--no-deps", "--pull", "never", "--entrypoint", e.c.Compose.Binary, e.c.Compose.Service, "security-maintenance", "inspect", "--control-row")
		raw, err = e.run(ctx, e.c.Compose.Docker, args, nil)
	}
	var row Observation
	if err == nil && (decode(lastLine(raw), &row) != nil || !validObservation(row)) {
		err = ErrInvalid
	}
	if err != nil {
		if j == nil || !j.RollingBack || j.Phase != "applying" {
			return false, err
		}
		// Its own window: a read that hung may have spent ctx.
		check, cancel := context.WithTimeout(context.WithoutCancel(ctx), e.timeout())
		defer cancel()
		deployed, deployedErr := e.targetDeployed(check, j)
		if deployedErr != nil || deployed {
			return false, errors.Join(err, deployedErr)
		}
		return true, nil
	}
	if row.Instance != source.Instance || row.Incarnation != source.Incarnation || row.RestoreEpoch != source.RestoreEpoch {
		return false, fmt.Errorf("%w: the control row belongs to another installation", ErrConflict)
	}
	return !row.PastFinalGate(source.Identity), nil
}

// targetDeployed reports whether the workload may run the target, whatever its health. Compose fails
// safe: a live container counts unless its image resolves, through RepoDigests as observeCompose
// resolves it, to the journaled source reference. Its own reference may be a tag, and Docker's image
// ID is the config digest under classic storage.
func (e *Executor) targetDeployed(ctx context.Context, j *deploymentJournal) (bool, error) {
	if e.c.Kind == Kubernetes {
		return e.kubernetesTargetDeployed(ctx, j.Target)
	}
	if imageDigest(j.Source.Image) == "" {
		return false, fmt.Errorf("%w: the journal's source image is not pinned by digest", ErrInvalid)
	}
	ids, err := e.run(ctx, e.c.Compose.Docker, composeLiveContainers(e.currentComposeArgs(), e.c.Compose.Service), nil)
	if err != nil {
		return false, err
	}
	for _, id := range strings.Fields(string(ids)) {
		raw, err := e.run(ctx, e.c.Compose.Docker, []string{"inspect", "--type", "container", "--format", inspectContainer, id}, nil)
		if err != nil {
			return false, err
		}
		var container composeContainer
		if decode(raw, &container) != nil {
			return false, ErrInvalid
		}
		if container.Image == j.Source.Image {
			continue
		}
		references, err := e.imageReferences(ctx, container.ImageID)
		if err != nil {
			return false, err
		}
		if !slices.Contains(references, j.Source.Image) {
			return true, nil
		}
	}
	return false, nil
}

func (e *Executor) prepareCompose(ctx context.Context, target Target) (Observation, error) {
	source, err := e.observeCompose(ctx)
	if err != nil {
		return Observation{}, err
	}
	if source.Maintenance || source.Phase != "healthy" || !source.Ready {
		return Observation{}, ErrNotReady
	}
	rendered, err := e.composeRendered(ctx)
	if err != nil {
		return Observation{}, err
	}
	if _, err = e.run(ctx, e.c.Compose.Docker, []string{"pull", target.Image}, nil); err != nil {
		return Observation{}, err
	}
	references, err := e.imageReferences(ctx, target.Image)
	if err != nil {
		return Observation{}, err
	}
	found := false
	for _, reference := range references {
		if reference == target.Image {
			found = true
		}
	}
	if !found {
		return Observation{}, ErrConflict
	}
	snapshot := filepath.Join(e.c.StateDirectory, "compose-prepared.json")
	override := filepath.Join(e.c.StateDirectory, "compose-stage.json")
	if err := securefile.WriteAtomic(snapshot, rendered, 0600); err != nil {
		return Observation{}, err
	}
	imageOverride, err := e.composeOverride(target.Image)
	if err != nil {
		return Observation{}, err
	}
	if err := securefile.WriteAtomic(override, imageOverride, 0600); err != nil {
		return Observation{}, err
	}
	args := append(e.composeArgs(snapshot, override), "run", "--rm", "--no-deps", "--pull", "never", "--entrypoint", e.c.Compose.Binary, e.c.Compose.Service, "security-maintenance", "stage", "--expected-manifest", string(target.Identity.ManifestSHA256))
	raw, err := e.run(ctx, e.c.Compose.Docker, args, nil)
	if err != nil {
		return Observation{}, err
	}
	var staged Observation
	if decode(raw, &staged) != nil || !validObservation(staged) {
		return Observation{}, ErrInvalid
	}
	staged.Image = source.Image
	if !sameSource(source, staged) || staged.StagedTarget != target.Identity || staged.Phase != "staged" || staged.Ready || staged.RouteSHA256.Validate() != nil {
		return Observation{}, ErrConflict
	}
	after, err := e.observeCompose(ctx)
	if err != nil {
		return Observation{}, err
	}
	if !sameSource(source, after) || after.Maintenance || !after.Ready {
		return Observation{}, ErrConflict
	}
	j := &deploymentJournal{Format: journalFormat, Phase: "prepared", Target: target, Source: source, Staged: staged, DesiredSHA256: releaseidentity.Hash(rendered)}
	if err := e.save(j); err != nil {
		return Observation{}, err
	}
	return staged, nil
}

func (e *Executor) composeOverride(image string) ([]byte, error) {
	return json.Marshal(map[string]any{"services": map[string]any{e.c.Compose.Service: map[string]string{"image": image}}})
}

func (e *Executor) applyCompose(ctx context.Context, j *deploymentJournal) error {
	source, err := e.observeCompose(ctx)
	if err != nil {
		return err
	}
	if !sameSource(source, j.Source) || source.Maintenance || !source.Ready {
		return ErrConflict
	}
	rendered, err := e.composeRendered(ctx)
	if err != nil {
		return err
	}
	if releaseidentity.Hash(rendered) != j.DesiredSHA256 {
		return ErrConflict
	}
	snapshot := filepath.Join(e.c.StateDirectory, "compose-prepared.json")
	prepared, err := privateFile(snapshot)
	if err != nil {
		return err
	}
	if releaseidentity.Hash(prepared) != j.DesiredSHA256 {
		return ErrConflict
	}
	stagePath := filepath.Join(e.c.StateDirectory, "compose-stage.json")
	staged, err := privateFile(stagePath)
	if err != nil {
		return err
	}
	expected, err := e.composeOverride(j.Target.Image)
	if err != nil {
		return err
	}
	if string(staged) != string(expected) {
		return ErrConflict
	}
	args := append(e.composeArgs(snapshot), "stop", "--timeout", "30", e.c.Compose.Service)
	if err := e.beginReplacement(j); err != nil {
		return err
	}
	// The applying journal is durable before the stop. Failure retains it and
	// never selects an old image; Observe publishes desired state after health.
	if _, err = e.run(ctx, e.c.Compose.Docker, args, nil); err != nil {
		return err
	}
	args = append(e.composeArgs(snapshot, stagePath), "up", "--detach", "--no-deps", "--force-recreate", "--pull", "never", e.c.Compose.Service)
	_, err = e.run(ctx, e.c.Compose.Docker, args, nil)
	return err
}

// rollBackCompose removes the target's container, proves no container of the
// service remains, runs the rollback one-shot in the target image with the stage
// run's shape, and starts the source from the prepared snapshot. The source start
// is authorized by the one-shot's report, because the target is gone and cannot
// be observed; a failed one-shot still starts it, and the source gate decides.
func (e *Executor) rollBackCompose(ctx context.Context, j *deploymentJournal) (restored bool, err error) {
	snapshot := filepath.Join(e.c.StateDirectory, "compose-prepared.json")
	prepared, err := privateFile(snapshot)
	if err != nil {
		return false, err
	}
	stagePath := filepath.Join(e.c.StateDirectory, "compose-stage.json")
	staged, err := privateFile(stagePath)
	if err != nil {
		return false, err
	}
	expected, err := e.composeOverride(j.Target.Image)
	if err != nil {
		return false, err
	}
	if releaseidentity.Hash(prepared) != j.DesiredSHA256 || string(staged) != string(expected) {
		return false, ErrConflict
	}
	// A source serving again has nothing to roll back, and a third image is an operator's; an unreadable service (a crashed target) proceeds.
	if live, err := e.observeCompose(ctx); err == nil {
		if sameSource(live, j.Source) && live.Phase == "healthy" && !live.Maintenance && live.Ready {
			return false, fmt.Errorf("%w: the source is serving again, healthy and unchanged; nothing was removed", ErrConflict)
		}
		if live.Image != j.Target.Image && live.Image != j.Source.Image {
			return false, fmt.Errorf("%w: %s runs %s, neither the target nor the source; nothing was removed", ErrConflict, e.c.Compose.Service, live.Image)
		}
	}
	if _, err := e.run(ctx, e.c.Compose.Docker, append(e.composeArgs(snapshot), "rm", "--stop", "--force", e.c.Compose.Service), nil); err != nil {
		return false, err
	}
	remaining, err := e.run(ctx, e.c.Compose.Docker, composeLiveContainers(e.composeArgs(snapshot), e.c.Compose.Service), nil)
	if err != nil {
		return false, err
	}
	if len(strings.Fields(string(remaining))) != 0 {
		return false, fmt.Errorf("%w: a container of %s remains after it was removed", ErrRecoveryRequired, e.c.Compose.Service)
	}
	if err := e.markRollingBack(j); err != nil {
		return false, err
	}
	args := append(e.composeArgs(snapshot, stagePath), "run", "--rm", "--no-deps", "--pull", "never", "--entrypoint", e.c.Compose.Binary, e.c.Compose.Service, "security-maintenance", "rollback", "--expected-manifest", string(j.Target.Identity.ManifestSHA256))
	report, oneShotErr := e.run(ctx, e.c.Compose.Docker, args, nil)
	if oneShotErr == nil {
		restored, oneShotErr = parseRollbackReport(report)
	}
	up := append(e.composeArgs(snapshot), "up", "--detach", "--no-deps", "--force-recreate", "--pull", "never", e.c.Compose.Service)
	writeBack, cancel := e.writeBackContext(ctx)
	defer cancel()
	_, restoreErr := e.run(writeBack, e.c.Compose.Docker, up, nil)
	return restored, errors.Join(oneShotErr, restoreErr)
}
