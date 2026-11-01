// Package securitydeployment replaces one explicitly enrolled deployment using
// installation-owned authority. Feed metadata supplies an authenticated release
// identity and image digest, never a command, credential, workload or path.
//
// Supported adapters are host-owned Compose/SQLite and directly owned singleton
// Kubernetes/PostgreSQL, plus an explicit Fargate/ALB/EFS/PostgreSQL contract.
// GitOps, Helm-owned releases, HA and other managed services deliberately refuse.
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

	"github.com/Hikyo-Org/hikyo/internal/localsocket"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/securefile"
	"k8s.io/client-go/kubernetes"
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
)

type Config struct {
	Kind            Kind              `json:"kind"`
	StateDirectory  string            `json:"state_directory"`
	ImageRepository string            `json:"image_repository"`
	TimeoutSeconds  int               `json:"timeout_seconds"`
	Compose         *ComposeConfig    `json:"compose,omitempty"`
	Kubernetes      *KubernetesConfig `json:"kubernetes,omitempty"`
	ECS             *ECSConfig        `json:"ecs,omitempty"`
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

type KubernetesConfig struct {
	Kubectl       string `json:"kubectl"`
	Kubeconfig    string `json:"kubeconfig"`
	Context       string `json:"context"`
	Namespace     string `json:"namespace"`
	Deployment    string `json:"deployment"`
	DeploymentUID string `json:"deployment_uid"`
	Container     string `json:"container"`
	StagePod      string `json:"stage_pod"`
	Binary        string `json:"binary"`
}

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
}

type deploymentJournal struct {
	Format        string                 `json:"format"`
	Phase         string                 `json:"phase"`
	Target        Target                 `json:"target"`
	Source        Observation            `json:"source"`
	Staged        Observation            `json:"staged"`
	DesiredSHA256 releaseidentity.Digest `json:"desired_sha256"`
}

// Executor is single-owner. The enclosing maintenance coordinator must hold its
// persistent installation lock across Prepare, policy evaluation, Apply and
// observation. An interrupted applying journal never grants automatic rollback.
type Executor struct {
	c      Config
	runner Runner
	client kubernetes.Interface
	now    func() time.Time
}

var namePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
var repositoryPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:/-]{1,254}$`)

func safeName(s string) bool { return namePattern.MatchString(s) && s != "." && s != ".." }
func safePath(s string) bool {
	return filepath.IsAbs(s) && filepath.Clean(s) == s && !strings.ContainsAny(s, "\x00\r\n")
}

func Validate(c Config) error {
	if !safePath(c.StateDirectory) || !repositoryPattern.MatchString(c.ImageRepository) || !strings.Contains(c.ImageRepository, "/") || strings.Contains(c.ImageRepository, "//") || strings.Contains(c.ImageRepository, "..") || strings.HasSuffix(c.ImageRepository, "/") || c.TimeoutSeconds < 0 || c.TimeoutSeconds > 3600 {
		return ErrInvalid
	}
	switch c.Kind {
	case Compose:
		p := c.Compose
		if p == nil || c.Kubernetes != nil || c.ECS != nil || !safePath(p.Docker) || !safePath(p.Directory) || !safePath(p.File) || (p.EnvironmentFile != "" && !safePath(p.EnvironmentFile)) || !safePath(p.Binary) || !safeName(p.Project) || !safeName(p.Service) {
			return ErrInvalid
		}
	case Kubernetes:
		p := c.Kubernetes
		if p == nil || c.Compose != nil || c.ECS != nil || !safePath(p.Kubectl) || !safePath(p.Kubeconfig) || !safePath(p.Binary) || !safeName(p.Context) || !safeName(p.Namespace) || !safeName(p.Deployment) || !safeName(p.DeploymentUID) || !safeName(p.Container) || !safeName(p.StagePod) {
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

func New(c Config) (*Executor, error) {
	if err := Validate(c); err != nil {
		return nil, err
	}
	if c.Kind == Compose {
		if err := installedAuthority(c.Compose.Docker, true, false); err != nil {
			return nil, err
		}
		if err := installedAuthority(c.Compose.File, false, false); err != nil {
			return nil, err
		}
		if c.Compose.EnvironmentFile != "" {
			if err := installedAuthority(c.Compose.EnvironmentFile, false, true); err != nil {
				return nil, err
			}
		}
	} else if c.Kind == Kubernetes {
		if err := installedAuthority(c.Kubernetes.Kubectl, true, false); err != nil {
			return nil, err
		}
		if err := installedAuthority(c.Kubernetes.Kubeconfig, false, true); err != nil {
			return nil, err
		}
	} else if c.Kind == ECS {
		if err := installedAuthority(c.ECS.AWS, true, false); err != nil {
			return nil, err
		}
	}
	var client kubernetes.Interface
	if c.Kind == Kubernetes {
		loading := &clientcmd.ClientConfigLoadingRules{ExplicitPath: c.Kubernetes.Kubeconfig}
		overrides := &clientcmd.ConfigOverrides{CurrentContext: c.Kubernetes.Context}
		cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loading, overrides).ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("load installed Kubernetes authority: %w", err)
		}
		cfg.Timeout = 30 * time.Second
		client, err = kubernetes.NewForConfig(cfg)
		if err != nil {
			return nil, err
		}
	}
	return NewWithDependencies(c, CommandRunner{}, client)
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
	if err := localsocket.ValidatePath(filepath.Join(c.StateDirectory, "deployment-authority.sock")); err != nil {
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
		j.Phase = "prepared"
		return errors.Join(ErrEligibilityExpired, e.save(j))
	}
	if j.Target.BeforeMutation != nil {
		if err := j.Target.BeforeMutation(); err != nil {
			j.Phase = "prepared"
			return errors.Join(ErrPreconditionRefused, err, e.save(j))
		}
	}
	if !e.now().Before(j.Target.EligibilityUntil) {
		j.Phase = "prepared"
		return errors.Join(ErrEligibilityExpired, e.save(j))
	}
	return nil
}

func decode(raw []byte, out any) error {
	if len(raw) == 0 || len(raw) > maxRecordBytes {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
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
	if decode(raw, &j) != nil || j.Format != journalFormat || e.target(j.Target) != nil || !validObservation(j.Source) || !validObservation(j.Staged) || j.Staged.StagedTarget != j.Target.Identity || j.DesiredSHA256.Validate() != nil || (j.Phase != "prepared" && j.Phase != "applying" && j.Phase != "healthy") {
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

func (e *Executor) run(ctx context.Context, path string, args []string, input []byte) ([]byte, error) {
	seconds := e.c.TimeoutSeconds
	if seconds == 0 {
		seconds = 900
	}
	child, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
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
		j.Phase = "healthy"
		if err := e.save(j); err != nil {
			return Observation{}, err
		}
	}
	return observation, nil
}
