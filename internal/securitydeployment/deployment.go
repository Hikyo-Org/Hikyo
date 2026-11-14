// Package securitydeployment replaces one explicitly enrolled deployment using
// installation-owned authority. Feed metadata supplies an authenticated release
// identity and image digest, never a command, credential, workload or path.
//
// Supported adapters are host-owned Compose/SQLite and directly owned singleton
// Kubernetes/PostgreSQL, plus an explicit Fargate/ALB/EFS/PostgreSQL contract.
// A chart release is accepted only when it opts in through its own values. GitOps,
// other Helm-owned releases, HA and other managed services deliberately refuse.
package securitydeployment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/filedurability"
	"github.com/Hikyo-Org/hikyo/internal/localsocket"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/securefile"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	ErrPreconditionRefused = errors.New("security deployment: precondition refused before workload mutation")
	ErrInvalid             = errors.New("security deployment: invalid installation contract")
	ErrUnsupported         = errors.New("security deployment: deployment requires a dedicated adapter")
	ErrConflict            = errors.New("security deployment: enrolled source or desired state changed")
	ErrNotReady            = errors.New("security deployment: exact running target is not healthy")
	ErrRecoveryRequired    = errors.New("security deployment: replacement outcome requires operator reconciliation")
	ErrEligibilityExpired  = errors.New("security deployment: authenticated eligibility expired before replacement")
)

type Kind string

const (
	Compose        Kind = "compose"
	Kubernetes     Kind = "kubernetes"
	ECS            Kind = "ecs"
	maxRecordBytes      = 1 << 20
	journalFormat       = "hikyo.security-deployment/v1"
	// A target that never reports is rolled back when this elapses, so the bound is a day.
	maxTimeoutSeconds = 86400
	// ECS has no rollback and keeps the bound it always had.
	maxECSTimeoutSeconds = 3600
)

type Config struct {
	Kind            Kind              `json:"kind"`
	StateDirectory  string            `json:"state_directory"`
	ImageRepository string            `json:"image_repository"`
	TimeoutSeconds  int               `json:"timeout_seconds"`
	Compose         *ComposeConfig    `json:"compose,omitzero"`
	Kubernetes      *KubernetesConfig `json:"kubernetes,omitzero"`
	ECS             *ECSConfig        `json:"ecs,omitzero"`
}

type ComposeConfig struct {
	Docker          string `json:"docker"`
	Directory       string `json:"directory"`
	Project         string `json:"project"`
	File            string `json:"file"`
	EnvironmentFile string `json:"environment_file"`
	Service         string `json:"service"`
	Binary          string `json:"binary"`
}

// KubernetesConfig selects the cluster by kubeconfig and context, or, with both
// empty, by the pod's own service account when the coordinator runs in the cluster.
type KubernetesConfig struct {
	Kubeconfig string `json:"kubeconfig,omitempty"`
	Context    string `json:"context,omitempty"`
	Namespace  string `json:"namespace"`
	Deployment string `json:"deployment"`
	// DeploymentUID may be empty: explicit enrollment then pins the live Deployment's
	// UID in private state, because a chart user cannot know it before installing.
	DeploymentUID string `json:"deployment_uid,omitempty"`
	Container     string `json:"container"`
	StagePod      string `json:"stage_pod"`
	Binary        string `json:"binary"`
	// CronJob, when set, is this coordinator's own CronJob; its image follows the
	// proven server image. StatusConfigMap, when set, receives the public status;
	// in-cluster only, because only the chart mounts it into the server.
	CronJob         string `json:"cron_job,omitempty"`
	StatusConfigMap string `json:"status_config_map,omitempty"`
	// VolumeRoot is the mounted volume that holds the coordinator state. Kubelet
	// applies fsGroup to a volume root, making it group-writable, which would
	// otherwise fail the ancestor check. In-cluster only.
	VolumeRoot string `json:"volume_root,omitempty"`
	// AcceptWorldWritableVolumeRoot accepts a 0777 VolumeRoot (local-path), where another user could replace the state.
	AcceptWorldWritableVolumeRoot bool `json:"accept_world_writable_volume_root,omitzero"`
}

func (p *KubernetesConfig) inCluster() bool { return p.Kubeconfig == "" }

// Target is already independently release-verified by the caller. This package
// checks exact platform replacement and does not substitute for release trust.
type Target struct {
	BeforeMutation func() error             `json:"-"`
	Identity       releaseidentity.Identity `json:"identity"`
	Image          string                   `json:"image"`
	// A multiarchitecture index requires an independently verified platform
	// manifest digest. Omit only when Image itself names the platform manifest.
	RuntimeImageSHA256 string `json:"runtime_image_sha256,omitempty"`
	// EligibilityUntil is local authorization context supplied from freshly
	// authenticated metadata, never an image selector or a migration timeout.
	// Prepare permits zero; Apply requires it before starting replacement.
	EligibilityUntil time.Time `json:"eligibility_until,omitzero"`
}

// Observation is local operator evidence, never a project heartbeat. Identity
// ALWAYS names the actual applied database release, including during staging.
type Observation struct {
	Platform            string                   `json:"platform"`
	Engine              releaseidentity.Engine   `json:"engine"`
	Identity            releaseidentity.Identity `json:"identity"`
	StagedTarget        releaseidentity.Identity `json:"staged_target,omitzero"`
	Image               string                   `json:"image"`
	RuntimeImageSHA256  string                   `json:"runtime_image_sha256,omitempty"`
	Instance            string                   `json:"instance"`
	Generation          int64                    `json:"generation"`
	RestoreEpoch        int64                    `json:"restore_epoch"`
	Incarnation         string                   `json:"incarnation"`
	RouteSHA256         releaseidentity.Digest   `json:"route_sha256,omitempty"`
	RequiresMaintenance bool                     `json:"requires_maintenance"`
	SchemaChanges       bool                     `json:"schema_changes"`
	Phase               string                   `json:"phase"`
	Maintenance         bool                     `json:"maintenance"`
	Ready               bool                     `json:"ready"`
	// AbortedBeforeMutation is reported by the target image after its own
	// preparation failed before the live database was written. The other fields
	// then describe the untouched source release.
	AbortedBeforeMutation bool `json:"aborted_before_mutation,omitzero"`
}

// PastFinalGate is whether this row shows a target that may have served: healthy at a release
// other than source, ready or not. From there a stop is a person's decision.
func (o Observation) PastFinalGate(source releaseidentity.Identity) bool {
	return o.Phase == "healthy" && !o.Maintenance && o.Identity != source
}

type deploymentJournal struct {
	Format        string                 `json:"format"`
	Phase         string                 `json:"phase"`
	Target        Target                 `json:"target"`
	Source        Observation            `json:"source"`
	Staged        Observation            `json:"staged"`
	DesiredSHA256 releaseidentity.Digest `json:"desired_sha256"`
	// Node is where Apply found the source Pod. The rollback one-shot runs there,
	// because the database volume may be attached to that node alone. Empty in an
	// earlier journal, which refuses the rollback.
	Node string `json:"node,omitempty"`
	// RollingBack is written once the rollback has proven the target stopped, before the one-shot:
	// the gate decision was then taken, and a crash while the database is absent resumes from it,
	// because the row cannot be read. It is never set while the target may still run, since it
	// vouches that no gate was crossed. Observe clears it only when the target itself completes; Reconcile clears it with the journal.
	RollingBack bool `json:"rolling_back,omitzero"`
}

// Executor is single-owner. The enclosing maintenance coordinator must hold its
// persistent installation lock across Prepare, policy evaluation, Apply and
// observation. An interrupted applying journal never grants automatic rollback.
type Executor struct {
	c      Config
	runner Runner
	client kubernetes.Interface
	pods   podIO
	now    func() time.Time
}

var namePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
var repositoryPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:/-]{1,254}$`)

func safeName(s string) bool { return namePattern.MatchString(s) && s != "." && s != ".." }
func safePath(s string) bool {
	return filepath.IsAbs(s) && filepath.Clean(s) == s && !strings.ContainsAny(s, "\x00\r\n")
}

func Validate(c Config) error {
	if !safePath(c.StateDirectory) || !repositoryPattern.MatchString(c.ImageRepository) || !strings.Contains(c.ImageRepository, "/") || strings.Contains(c.ImageRepository, "//") || strings.Contains(c.ImageRepository, "..") || strings.HasSuffix(c.ImageRepository, "/") || c.TimeoutSeconds < 0 || c.TimeoutSeconds > maxTimeoutSeconds || c.Kind == ECS && c.TimeoutSeconds > maxECSTimeoutSeconds {
		return ErrInvalid
	}
	switch c.Kind {
	case Compose:
		p := c.Compose
		if p == nil || c.Kubernetes != nil || c.ECS != nil || !safePath(p.Docker) || !safePath(p.Directory) || !safePath(p.File) || !safePath(p.EnvironmentFile) || !safePath(p.Binary) || !safeName(p.Project) || !safeName(p.Service) {
			return ErrInvalid
		}
	case Kubernetes:
		p := c.Kubernetes
		if p == nil || c.Compose != nil || c.ECS != nil || !safePath(p.Binary) || !safeName(p.Namespace) || !safeName(p.Deployment) || !safeName(p.Container) || !safeName(p.StagePod) {
			return ErrInvalid
		}
		for _, optional := range []string{p.DeploymentUID, p.CronJob, p.StatusConfigMap} {
			if optional != "" && !safeName(optional) {
				return ErrInvalid
			}
		}
		if p.inCluster() && p.Context != "" || !p.inCluster() && (!safePath(p.Kubeconfig) || !safeName(p.Context)) {
			return ErrInvalid
		}
		// Only the chart mounts the ConfigMap into the server, and the published
		// external-host Role has no configmaps rule.
		if p.StatusConfigMap != "" && !p.inCluster() {
			return ErrInvalid
		}
		if p.VolumeRoot != "" && (!p.inCluster() || !safePath(p.VolumeRoot) || !strings.HasPrefix(c.StateDirectory, p.VolumeRoot+"/")) {
			return ErrInvalid
		}
		if p.AcceptWorldWritableVolumeRoot && p.VolumeRoot == "" {
			return ErrInvalid
		}
	case ECS:
		if c.ECS == nil || c.Compose != nil || c.Kubernetes != nil {
			return ErrInvalid
		}
		if err := c.ECS.validate(); err != nil {
			return err
		}
	default:
		return ErrUnsupported
	}
	return nil
}

// RequireInstalledExecutable applies the same root-installed test to the
// coordinator's own executable that the adapters apply to their host programs.
func RequireInstalledExecutable(path string) error { return installedAuthority(path, true, false) }

func New(c Config) (*Executor, error) {
	if err := Validate(c); err != nil {
		return nil, err
	}
	if c.Kind == Compose {
		if err := installedAuthority(c.Compose.Docker, true, false); err != nil {
			return nil, err
		}
		directory, err := dockerConfigDirectory()
		if err != nil {
			return nil, err
		}
		if err := vetDockerClient(directory, os.Getenv("PATH")); err != nil {
			return nil, err
		}
		if err := installedAuthority(c.Compose.File, false, false); err != nil {
			return nil, err
		}
		if err := installedAuthority(c.Compose.EnvironmentFile, false, true); err != nil {
			return nil, err
		}
		if _, _, err := composeEnvironmentImage(c.Compose.EnvironmentFile, c.ImageRepository); err != nil {
			return nil, err
		}
	} else if c.Kind == Kubernetes && !c.Kubernetes.inCluster() {
		if err := installedAuthority(c.Kubernetes.Kubeconfig, false, true); err != nil {
			return nil, err
		}
	} else if c.Kind == ECS {
		if err := installedAuthority(c.ECS.AWS, true, false); err != nil {
			return nil, err
		}
	}
	var client kubernetes.Interface
	var pods podIO
	if c.Kind == Kubernetes {
		var cfg *rest.Config
		var err error
		if c.Kubernetes.inCluster() {
			cfg, err = rest.InClusterConfig()
		} else {
			loading := &clientcmd.ClientConfigLoadingRules{ExplicitPath: c.Kubernetes.Kubeconfig}
			overrides := &clientcmd.ConfigOverrides{CurrentContext: c.Kubernetes.Context}
			cfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loading, overrides).ClientConfig()
		}
		if err != nil {
			return nil, fmt.Errorf("load Kubernetes authority: %w", err)
		}
		// A client timeout would cut a long exec stream; the caller's context bounds it.
		streaming := rest.CopyConfig(cfg)
		cfg.Timeout = 30 * time.Second
		client, err = kubernetes.NewForConfig(cfg)
		if err != nil {
			return nil, err
		}
		pods = clusterPods{config: streaming, client: client, namespace: c.Kubernetes.Namespace}
	}
	runner := CommandRunner{}
	if c.Compose != nil {
		runner.Secrets = func() ([]string, error) {
			return composeProtectedValues(c.Compose.EnvironmentFile, filepath.Join(c.StateDirectory, "compose-prepared.json"))
		}
	}
	executor, err := NewWithDependencies(c, runner, client)
	if err != nil {
		return nil, err
	}
	executor.pods = pods
	return executor, nil
}

// NewWithDependencies provides the command and Kubernetes seams used by the
// same adapter code in production and adversarial tests.
func NewWithDependencies(c Config, runner Runner, client kubernetes.Interface) (*Executor, error) {
	if err := Validate(c); err != nil {
		return nil, err
	}
	if runner == nil || (c.Kind == Kubernetes && client == nil) {
		return nil, ErrInvalid
	}
	// Reuse the canonical local-authority path policy. It verifies current-UID
	// ownership and every ancestor, including sticky directories, without
	// creating a socket or duplicating platform-specific ownership syscalls.
	volume, validate := "", localsocket.ValidatePathOnVolume
	if c.Kubernetes != nil {
		volume = c.Kubernetes.VolumeRoot
		if c.Kubernetes.AcceptWorldWritableVolumeRoot {
			validate = localsocket.ValidatePathOnWorldWritableVolume
		}
	}
	if err := validate(filepath.Join(c.StateDirectory, "deployment-authority.sock"), volume); err != nil {
		return nil, fmt.Errorf("security deployment: unsafe local authority: %w", err)
	}
	info, err := os.Lstat(c.StateDirectory)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return nil, errors.New("security deployment: state requires an existing private real directory")
	}
	resolved, err := filepath.EvalSymlinks(c.StateDirectory)
	if err != nil || resolved != c.StateDirectory {
		return nil, errors.New("security deployment: state ancestry must not contain symbolic links")
	}
	return &Executor{c: c, runner: runner, client: client, now: time.Now}, nil
}

func (e *Executor) target(t Target) error {
	if t.Identity.Validate() != nil || !strings.HasPrefix(t.Image, e.c.ImageRepository+"@sha256:") || imageDigest(t.Image) == "" || (t.RuntimeImageSHA256 != "" && releaseidentity.Digest(t.RuntimeImageSHA256).Validate() != nil) {
		return ErrInvalid
	}
	return nil
}

func imageDigest(image string) string {
	_, digest, ok := strings.Cut(image, "@sha256:")
	if !ok || releaseidentity.Digest(digest).Validate() != nil {
		return ""
	}
	return digest
}

func sameSource(a, b Observation) bool {
	return a.Platform == b.Platform && a.Engine == b.Engine && a.Identity == b.Identity && a.Instance == b.Instance && a.Generation == b.Generation && a.RestoreEpoch == b.RestoreEpoch && a.Incarnation == b.Incarnation && a.Image == b.Image && a.RuntimeImageSHA256 == b.RuntimeImageSHA256
}

func sameTarget(a, b Target) bool {
	return a.Identity == b.Identity && a.Image == b.Image && a.RuntimeImageSHA256 == b.RuntimeImageSHA256
}

// beginReplacement is the final freshness gate after potentially long source
// and desired-state validation. Once admitted, metadata expiry must not abort
// this exact operation between stopping writers and completing migration.
func (e *Executor) beginReplacement(j *deploymentJournal) error {
	if j.Target.BeforeMutation != nil {
		if err := j.Target.BeforeMutation(); err != nil {
			return errors.Join(ErrPreconditionRefused, err)
		}
	}
	if j.Target.EligibilityUntil.IsZero() || !e.now().Before(j.Target.EligibilityUntil) {
		return ErrEligibilityExpired
	}
	j.Phase = "applying"
	if err := e.save(j); err != nil {
		return err
	}
	// Disk publication itself can consume the remaining eligibility. No
	// platform mutation has started, so restore prepared state on that refusal.
	if !e.now().Before(j.Target.EligibilityUntil) {
		return e.refuseMutation(j, ErrEligibilityExpired)
	}
	if j.Target.BeforeMutation != nil {
		if err := j.Target.BeforeMutation(); err != nil {
			return e.refuseMutation(j, errors.Join(ErrPreconditionRefused, err))
		}
	}
	if !e.now().Before(j.Target.EligibilityUntil) {
		return e.refuseMutation(j, ErrEligibilityExpired)
	}
	return nil
}

// refuseMutation restores the prepared journal after a proven refusal. If the
// journal cannot be restored it still says applying, so the failure must not be
// reported as a refusal: the coordinator would drop its pin and Prepare would then
// refuse forever.
func (e *Executor) refuseMutation(j *deploymentJournal, cause error) error {
	j.Phase = "prepared"
	if err := e.save(j); err != nil {
		j.Phase = "applying"
		return fmt.Errorf("security deployment: restoring the journal after a refusal (%v): %s", cause, err)
	}
	return cause
}

func decode(raw []byte, out any) error {
	if len(raw) == 0 || len(raw) > maxRecordBytes {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	// A target image may be newer than this coordinator and add observation
	// fields. Everything this installation wrote itself stays strictly decoded.
	if _, observation := out.(*Observation); !observation {
		d.DisallowUnknownFields()
	}
	if err := d.Decode(out); err != nil {
		return ErrInvalid
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return ErrInvalid
	}
	return nil
}

func validObservation(o Observation) bool {
	return o.Platform == runtime.GOOS+"/"+runtime.GOARCH && o.Engine.Validate() == nil && o.Identity.Validate() == nil && o.Instance != "" && len(o.Instance) <= 256 && o.Generation > 0 && o.RestoreEpoch >= 0 && o.Incarnation != "" && len(o.Incarnation) <= 256 && o.Phase != "" && len(o.Phase) <= 64
}

func (e *Executor) journal() (*deploymentJournal, error) {
	path := filepath.Join(e.c.StateDirectory, "deployment.json")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > maxRecordBytes {
		return nil, ErrInvalid
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var j deploymentJournal
	if decode(raw, &j) != nil || j.Format != journalFormat || e.target(j.Target) != nil || !validObservation(j.Source) || !validObservation(j.Staged) || j.Staged.StagedTarget != j.Target.Identity || j.DesiredSHA256.Validate() != nil || (j.Phase != "prepared" && j.Phase != "applying" && j.Phase != "healthy" && j.Phase != "rolled-back") {
		return nil, ErrInvalid
	}
	return &j, nil
}

func (e *Executor) save(j *deploymentJournal) error {
	raw, err := json.Marshal(j)
	if err != nil {
		return err
	}
	return securefile.WriteAtomic(filepath.Join(e.c.StateDirectory, "deployment.json"), raw, 0600)
}

// DefaultTimeout bounds a Compose or Kubernetes replacement or rollback when none is configured.
// The coordinator waits that long, so a one-shot must be allowed the same: a large restore ends in it.
// ECS keeps its own 900 seconds, which its health grace contract is built on.
const DefaultTimeout = 30 * time.Minute

func (e *Executor) timeout() time.Duration {
	if e.c.TimeoutSeconds == 0 {
		if e.c.Kind == ECS {
			return 900 * time.Second
		}
		return DefaultTimeout
	}
	return time.Duration(e.c.TimeoutSeconds) * time.Second
}

func (e *Executor) run(ctx context.Context, path string, args []string, input []byte) ([]byte, error) {
	child, cancel := context.WithTimeout(ctx, e.timeout())
	defer cancel()
	return e.runner.Run(child, Command{Path: path, Args: args, Input: input})
}

// Prepare stages the complete target route while the enrolled source still
// serves. It returns the authenticated local route impact for policy evaluation.
func (e *Executor) Prepare(ctx context.Context, target Target) (Observation, error) {
	if err := e.target(target); err != nil {
		return Observation{}, err
	}
	previous, err := e.journal()
	if err != nil {
		return Observation{}, err
	}
	if previous != nil && previous.Phase == "applying" {
		return Observation{}, ErrRecoveryRequired
	}
	if e.c.Kind == Compose {
		return e.prepareCompose(ctx, target)
	}
	if e.c.Kind == ECS {
		return e.prepareECS(ctx, target)
	}
	return e.prepareKubernetes(ctx, target)
}

// Apply changes only the locally enrolled workload. It never restores an old
// image or clears database maintenance. A lost reply requires Observe/recovery.
func (e *Executor) Apply(ctx context.Context, target Target) error {
	if err := e.target(target); err != nil {
		return err
	}
	j, err := e.journal()
	if err != nil {
		return err
	}
	if j == nil || !sameTarget(j.Target, target) {
		return ErrConflict
	}
	if j.Phase == "applying" {
		return ErrRecoveryRequired
	}
	if j.Phase == "healthy" {
		return nil
	}
	// A rollback ended this attempt; only a fresh Prepare starts another.
	if j.Phase == "rolled-back" {
		return ErrConflict
	}
	j.Target.EligibilityUntil = target.EligibilityUntil
	j.Target.BeforeMutation = target.BeforeMutation
	if e.c.Kind == Compose {
		err = e.applyCompose(ctx, j)
	} else if e.c.Kind == ECS {
		err = e.applyECS(ctx, j)
	} else {
		err = e.applyKubernetes(ctx, j)
	}
	// Every adapter marks applying before its first workload mutation. A
	// journal still prepared proves that a local validation refusal did not
	// stop, patch or scale the workload. Lost replies retain applying.
	if err != nil && j.Phase == "prepared" {
		return errors.Join(ErrPreconditionRefused, err)
	}
	return err
}

func (e *Executor) Observe(ctx context.Context) (Observation, error) {
	var observation Observation
	var err error
	if e.c.Kind == Compose {
		observation, err = e.observeCompose(ctx)
	} else if e.c.Kind == ECS {
		observation, err = e.observeECS(ctx)
	} else {
		observation, err = e.observeKubernetes(ctx)
	}
	if err != nil {
		return Observation{}, err
	}
	j, err := e.journal()
	if err != nil {
		return Observation{}, err
	}
	if j != nil && j.Phase == "applying" && observation.Identity == j.Target.Identity && observation.Image == j.Target.Image && observation.Instance == j.Source.Instance && observation.RestoreEpoch == j.Source.RestoreEpoch && observation.Incarnation == j.Source.Incarnation && observation.Generation >= j.Source.Generation && observation.Phase == "healthy" && !observation.Maintenance && observation.Ready {
		if e.c.Kind == Compose {
			// Before the journal commit, so a failed or interrupted write retries.
			if err := e.persistComposeImage(j); err != nil {
				return Observation{}, err
			}
		}
		j.Phase, j.RollingBack = "healthy", false
		if err := e.save(j); err != nil {
			return Observation{}, err
		}
	}
	return observation, nil
}

// RollBack is the owner's answer to a target that wrote nothing it should keep
// or never reported: stop the target with proof, restore the fenced snapshot in a
// one-shot of the target's own image, and write the source back. The source image
// stays the authority, so a failed one-shot still writes it back. ECS stays pinned.
// restored is whether the one-shot restored the snapshot, as opposed to finding the
// source untouched. The marker is durable once the target is proven stopped and before the one-shot.
func (e *Executor) RollBack(ctx context.Context) (restored bool, err error) {
	if e.c.Kind != Compose && e.c.Kind != Kubernetes {
		return false, ErrUnsupported
	}
	j, err := e.journal()
	if err != nil {
		return false, err
	}
	if j == nil || j.Phase != "applying" {
		return false, ErrConflict
	}
	resumed := j.RollingBack
	if e.c.Kind == Kubernetes {
		restored, err = e.rollBackKubernetes(ctx, j)
	} else {
		restored, err = e.rollBackCompose(ctx, j)
	}
	// ErrUnsupported means "no rollback path here" to the caller. A deployment that drifted from
	// what was prepared or applied is a refusal, and must keep its own cause and be reported.
	if errors.Is(err, ErrUnsupported) {
		return false, fmt.Errorf("%w: the deployment is neither the prepared nor the applied state: %v", ErrConflict, err)
	}
	// shortcut: a resumed rollback, or a release after a failed one-shot, reports the snapshot restored even when the source was only stopped and restarted, because the source may already have consumed the journal that said so; upgrade if the one-shot's report is journaled.
	return restored || resumed && err == nil, err
}

// markRollingBack makes the proof that the target is stopped durable before the one-shot runs.
func (e *Executor) markRollingBack(j *deploymentJournal) error {
	if j.RollingBack {
		return nil
	}
	j.RollingBack = true
	return e.save(j)
}

// writeBackContext is the window of the source write-back alone: the one the one-shot consumed
// may be spent, and an unwritten source leaves the service down.
func (e *Executor) writeBackContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), e.timeout())
}

// RollbackReportFormat names the one line the target's one-shot prints.
const RollbackReportFormat = "hikyo.security-rollback/v1"

// RollbackReport is the one line the target's one-shot prints. It is the only
// thing that authorizes writing the source back while the target is gone.
type RollbackReport struct {
	Format string `json:"format"`
	Result string `json:"result"`
	Reason string `json:"reason"`
}

// lastLine is the one line a one-shot printed: pod logs merge stderr, so a warning can precede it.
func lastLine(raw []byte) []byte {
	lines := bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
	return bytes.TrimSpace(lines[len(lines)-1])
}

// parseRollbackReport returns whether the snapshot was restored, not merely whether the report is valid.
func parseRollbackReport(raw []byte) (restored bool, err error) {
	var report RollbackReport
	if decode(lastLine(raw), &report) != nil || report.Format != RollbackReportFormat || (report.Result != "restored" && report.Result != "not-needed") {
		return false, fmt.Errorf("%w: the rollback one-shot returned no valid report", ErrRecoveryRequired)
	}
	return report.Result == "restored", nil
}

// Reconcile releases a recovery pin once the coordinator has established that
// the installation is healthy and the pin no longer describes live work: the
// source after a failed stop or refused update, or a restored installation. A
// fresh Prepare is then required. An unhealthy observation keeps the pin; an
// unpinned journal has nothing to clear. If a rollback marker was set it reports ErrFollowsRollback once
// the pin is released, and leaves the journal at "rolled-back" so that a coordinator dying before it records
// that is told again; the next Prepare replaces it. Compose publishes desired state only
// after target health, so no file needs restoring.
func (e *Executor) Reconcile(_ context.Context, observed Observation) error {
	j, err := e.journal()
	if err != nil {
		return err
	}
	if j != nil && j.Phase == "rolled-back" {
		return ErrFollowsRollback
	}
	if j == nil || j.Phase != "applying" {
		return nil
	}
	if observed.Phase != "healthy" || observed.Maintenance || !observed.Ready {
		return ErrConflict
	}
	if j.RollingBack {
		j.Phase = "rolled-back"
		if err := e.save(j); err != nil {
			return err
		}
		return ErrFollowsRollback
	}
	if err := os.Remove(filepath.Join(e.c.StateDirectory, "deployment.json")); err != nil {
		return err
	}
	return filedurability.SyncDirectory(e.c.StateDirectory)
}

// ErrFollowsRollback is what Reconcile returns, after it released the pin, when the journal says a
// rollback had begun: the source is serving because that rollback put it back, not because it never stopped.
var ErrFollowsRollback = errors.New("the release follows an earlier rollback")

// RestoreSource puts the enrolled source image back after its target reported that
// it failed before the live database was written. The source starts again from the
// prepared snapshot, never from anything the target controls, and refuses on its
// own if the database was written after all. Compose and Kubernetes have this path;
// ECS reports ErrUnsupported and keeps the replacement pinned.
func (e *Executor) RestoreSource(ctx context.Context, aborted Observation) error {
	if e.c.Kind != Compose && e.c.Kind != Kubernetes {
		return ErrUnsupported
	}
	j, err := e.journal()
	if err != nil {
		return err
	}
	if j == nil || j.Phase != "applying" || !aborted.AbortedBeforeMutation || aborted.Identity != j.Source.Identity || aborted.Instance != j.Source.Instance || aborted.Incarnation != j.Source.Incarnation || aborted.RestoreEpoch != j.Source.RestoreEpoch || aborted.Generation != j.Source.Generation {
		return ErrConflict
	}
	if e.c.Kind == Kubernetes {
		return e.restoreKubernetes(ctx, j)
	}
	snapshot := filepath.Join(e.c.StateDirectory, "compose-prepared.json")
	prepared, err := privateFile(snapshot)
	if err != nil {
		return err
	}
	if releaseidentity.Hash(prepared) != j.DesiredSHA256 {
		return ErrConflict
	}
	args, err := e.composeSourceArgs(j, snapshot)
	if err != nil {
		return err
	}
	args = append(args, "up", "--detach", "--no-deps", "--force-recreate", "--pull", "never", e.c.Compose.Service)
	_, err = e.run(ctx, e.c.Compose.Docker, args, nil)
	return err
}
