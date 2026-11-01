package securitydeployment

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/securefile"
)

const inspectContainer = `{"id":{{json .Id}},"image":{{json .Config.Image}},"image_id":{{json .Image}},"running":{{json .State.Running}},"health":{{if .State.Health}}{{json .State.Health.Status}}{{else}}null{{end}},"labels":{{json .Config.Labels}}}`

type composeContainer struct {
	ID      string            `json:"id"`
	Image   string            `json:"image"`
	ImageID string            `json:"image_id"`
	Running bool              `json:"running"`
	Health  string            `json:"health"`
	Labels  map[string]string `json:"labels"`
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

func (e *Executor) currentComposeArgs() ([]string, error) {
	p := e.c.Compose
	args := e.composeArgs(p.File)
	active := filepath.Join(e.c.StateDirectory, "compose-active.json")
	if _, err := os.Lstat(active); err == nil {
		if _, err := privateFile(active); err != nil {
			return nil, err
		}
		args = append(args, "--file", active)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if p.EnvironmentFile != "" {
		args = append(args, "--env-file", p.EnvironmentFile)
	}
	return args, nil
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
	args, err := e.currentComposeArgs()
	if err != nil {
		return nil, err
	}
	raw, err := e.run(ctx, e.c.Compose.Docker, append(args, "config", "--format", "json"), nil)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Services map[string]composeService `json:"services"`
	}
	if len(raw) > maxRecordBytes || json.Unmarshal(raw, &parsed) != nil {
		return nil, ErrInvalid
	}
	s, ok := parsed.Services[e.c.Compose.Service]
	if !ok || !e.ourImage(s.Image) || s.Privileged || s.Scale > 1 || (s.Deploy.Replicas != nil && *s.Deploy.Replicas != 1) || s.Environment["HIKYO_UPGRADE_UNATTENDED"] != "true" || s.Environment["HIKYO_HA"] == "true" || s.Environment["HIKYO_CONFIG_ROLLOUT_ENROLLMENT"] != "" || !strings.HasPrefix(s.Environment["HIKYO_DB"], "sqlite:") {
		return nil, ErrUnsupported
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

func (e *Executor) observeCompose(ctx context.Context) (Observation, error) {
	args, err := e.currentComposeArgs()
	if err != nil {
		return Observation{}, err
	}
	raw, err := e.run(ctx, e.c.Compose.Docker, append(args, "ps", "--all", "--quiet", e.c.Compose.Service), nil)
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
	// The applying journal is durable before either stop or desired-state
	// publication. Failure retains that journal and never selects an old image.
	if _, err = e.run(ctx, e.c.Compose.Docker, args, nil); err != nil {
		return err
	}
	active := filepath.Join(e.c.StateDirectory, "compose-active.json")
	if err := securefile.WriteAtomic(active, expected, 0600); err != nil {
		return err
	}
	args = append(e.composeArgs(snapshot, active), "up", "--detach", "--no-deps", "--force-recreate", "--pull", "never", e.c.Compose.Service)
	_, err = e.run(ctx, e.c.Compose.Docker, args, nil)
	return err
}
