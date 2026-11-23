package securitydeployment

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/filedurability"
	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
	"github.com/Hikyo-Org/hikyo/internal/multicall"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/securefile"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utiljson "k8s.io/apimachinery/pkg/util/json"
)

const stageOwner = "hikyo.dev/security-stage-owner"
const stageTemplate = "hikyo.dev/security-stage-template"
const replacementTarget = "hikyo.dev/security-release-manifest"
const helmOwnerOptIn = "hikyo.dev/security-maintenance"

const identityFile = "kubernetes-identity.json"

type kubernetesIdentity struct {
	DeploymentUID string `json:"deployment_uid"`
}

// pinnedUID is the Deployment identity this installation trusts: the configured
// one, else the one explicit enrollment recorded. Empty means neither exists yet.
func (e *Executor) pinnedUID() (string, error) {
	if uid := e.c.Kubernetes.DeploymentUID; uid != "" {
		return uid, nil
	}
	raw, err := privateFile(filepath.Join(e.c.StateDirectory, identityFile))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var id kubernetesIdentity
	if decode(raw, &id) != nil || !safeName(id.DeploymentUID) {
		return "", ErrInvalid
	}
	return id.DeploymentUID, nil
}

// requirePinned guards every workload mutation. Observation alone may run before
// enrollment, so that an unenrolled coordinator can say so instead of failing.
func (e *Executor) requirePinned() error {
	uid, err := e.pinnedUID()
	if err != nil {
		return err
	}
	if uid == "" {
		return fmt.Errorf("%w: the Deployment identity is not pinned; enroll first", ErrConflict)
	}
	return nil
}

// IdentityPinned reports, with no cluster access, whether an identity is recorded.
func (e *Executor) IdentityPinned() (bool, error) {
	if e.c.Kind != Kubernetes {
		return true, nil
	}
	uid, err := e.pinnedUID()
	return uid != "", err
}

// PinIdentity records the live Deployment's UID at explicit enrollment, so a
// replaced Deployment is refused afterwards. Repeating it for the same
// Deployment changes nothing; a different one is never adopted silently.
func (e *Executor) PinIdentity(ctx context.Context) error {
	if e.c.Kind != Kubernetes || e.c.Kubernetes.DeploymentUID != "" {
		return nil
	}
	p := e.c.Kubernetes
	d, err := e.client.AppsV1().Deployments(p.Namespace).Get(ctx, p.Deployment, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if d.UID == "" || d.DeletionTimestamp != nil {
		return ErrConflict
	}
	if err := e.supportedDeployment(d); err != nil {
		return err
	}
	pinned, err := e.pinnedUID()
	if err != nil {
		return err
	}
	if pinned != "" {
		if pinned != string(d.UID) {
			return ErrConflict
		}
		return nil
	}
	raw, err := json.Marshal(kubernetesIdentity{DeploymentUID: string(d.UID)}, jsonwire.Stable)
	if err != nil {
		return err
	}
	return e.publishIdentity(raw, string(d.UID))
}

// publishIdentity writes the pin only where none exists: a link of a complete file never replaces
// one, so of two enrollments that both read no pin, the second compares against the first's.
func (e *Executor) publishIdentity(raw []byte, uid string) error {
	temp, err := os.CreateTemp(e.c.StateDirectory, ".kubernetes-identity-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	_, err = temp.Write(raw)
	if err == nil {
		err = temp.Chmod(0600)
	}
	if err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Link(temp.Name(), filepath.Join(e.c.StateDirectory, identityFile)); errors.Is(err, os.ErrExist) {
		pinned, err := e.pinnedUID()
		if err != nil {
			return err
		}
		if pinned != uid {
			return ErrConflict
		}
		return nil
	} else if err != nil {
		return err
	}
	return filedurability.SyncDirectory(e.c.StateDirectory)
}

func managedDesiredState(d *appsv1.Deployment) bool {
	for _, key := range []string{"kustomize.toolkit.fluxcd.io/name", "helm.toolkit.fluxcd.io/name", "argocd.argoproj.io/instance"} {
		if d.Labels[key] != "" || d.Annotations[key] != "" {
			return true
		}
	}
	if d.Annotations["argocd.argoproj.io/tracking-id"] != "" {
		return true
	}
	release, namespace := d.Annotations["meta.helm.sh/release-name"], d.Annotations["meta.helm.sh/release-namespace"]
	// The chart's opt-in label is honored only on a real Helm release, whose
	// upgrades run its image lookup. A rendered copy of the chart has no such guard.
	if d.Labels[helmOwnerOptIn] == "helm" {
		return release == "" || namespace == ""
	}
	// Argo CD's label tracking marks a resource only with app.kubernetes.io/instance,
	// which Helm also sets, so the label refuses unless a Helm release explains it.
	// shortcut: a custom application.instanceLabelKey is undetectable; the runbook has operators rule Argo out.
	return release != "" || namespace != "" || d.Labels["app.kubernetes.io/instance"] != ""
}

func (e *Executor) deployment(ctx context.Context) (*appsv1.Deployment, error) {
	return e.deploymentAt(ctx, 1)
}

// deploymentAt reads the Deployment with exactly the given replica count. Only the
// rollback path accepts zero, because it scales the stopped target to nothing.
func (e *Executor) deploymentAt(ctx context.Context, replicas int32) (*appsv1.Deployment, error) {
	p := e.c.Kubernetes
	d, err := e.client.AppsV1().Deployments(p.Namespace).Get(ctx, p.Deployment, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	uid, err := e.pinnedUID()
	if err != nil {
		return nil, err
	}
	if uid != "" && string(d.UID) != uid || d.DeletionTimestamp != nil || d.ResourceVersion == "" {
		return nil, ErrConflict
	}
	if err := e.supportedDeploymentAt(d, replicas); err != nil {
		return nil, err
	}
	return d, nil
}

func (e *Executor) supportedDeployment(d *appsv1.Deployment) error {
	return e.supportedDeploymentAt(d, 1)
}

func (e *Executor) supportedDeploymentAt(d *appsv1.Deployment, replicas int32) error {
	p := e.c.Kubernetes
	if managedDesiredState(d) || d.Spec.Replicas == nil || *d.Spec.Replicas != replicas || d.Spec.Strategy.Type != appsv1.RecreateDeploymentStrategyType || d.Spec.Template.Spec.AutomountServiceAccountToken == nil || *d.Spec.Template.Spec.AutomountServiceAccountToken || d.Spec.Template.Spec.HostNetwork || d.Spec.Template.Spec.HostPID || d.Spec.Template.Spec.HostIPC {
		return ErrUnsupported
	}
	var server *corev1.Container
	for i := range d.Spec.Template.Spec.Containers {
		if d.Spec.Template.Spec.Containers[i].Name == p.Container {
			server = &d.Spec.Template.Spec.Containers[i]
		}
	}
	if server == nil || !strings.HasPrefix(server.Image, e.c.ImageRepository+"@sha256:") || imageDigest(server.Image) == "" {
		return ErrUnsupported
	}
	// The stage and rollback Pods run init containers again while the source serves, with its
	// credentials. Only the chart's stagers are known to do nothing there but copy a key or certificate.
	for _, init := range d.Spec.Template.Spec.InitContainers {
		if !e.knownStager(init) {
			return fmt.Errorf("%w: init container %q is not one of the chart's stagers (%s or %s, image %s); staging would run it again while the source serves, so remove it or move its work into the server container", ErrUnsupported, init.Name, multicall.RootKeyStage, multicall.TLSStage, e.c.ImageRepository)
		}
	}
	// A wrapper command could pass the server any flags, so only the binary's own entrypoint qualifies.
	if len(server.Command) != 0 && !slices.Equal(server.Command, []string{p.Binary}) {
		return ErrUnsupported
	}
	unattended, postgres, scratch, persistent := false, false, false, false
	stateDirectory := ""
	seen := map[string]bool{}
	for _, v := range server.Env {
		if seen[v.Name] {
			return ErrUnsupported
		}
		seen[v.Name] = true
		if v.Name == "HIKYO_UPGRADE_UNATTENDED" && v.Value == "true" && v.ValueFrom == nil {
			unattended = true
		}
		if v.Name == "HIKYO_HA" && v.Value != "false" {
			return ErrUnsupported
		}
		if v.Name == "HIKYO_CONFIG_ROLLOUT_ENROLLMENT" {
			return ErrUnsupported
		}
		if v.Name == "HIKYO_DB" && v.ValueFrom != nil && v.ValueFrom.SecretKeyRef != nil {
			postgres = true
		}
		if v.Name == "HIKYO_UPGRADE_SCRATCH_POSTGRES_DSN" && v.ValueFrom != nil && v.ValueFrom.SecretKeyRef != nil {
			scratch = true
		}
		if v.Name == "HIKYO_UPGRADE_STATE_DIR" && safePath(v.Value) && v.ValueFrom == nil {
			stateDirectory = v.Value
		}
	}
	// Kubernetes lets an explicit env entry override EnvFrom, whose sources this adapter cannot read.
	if len(server.EnvFrom) != 0 && !seen["HIKYO_HA"] {
		return fmt.Errorf("%w: the server container imports environment through envFrom, which could enable HIKYO_HA; set HIKYO_HA=false in its env", ErrUnsupported)
	}
	for _, arg := range server.Args {
		if serverFlagDisables(arg) {
			return ErrUnsupported
		}
	}
	// The state directory is on the most specific mount covering it, which must be a writable claim.
	var stateMount *corev1.VolumeMount
	for i := range server.VolumeMounts {
		mount := &server.VolumeMounts[i]
		path := filepath.Clean(mount.MountPath)
		if stateDirectory != "" && (stateDirectory == path || strings.HasPrefix(stateDirectory, strings.TrimSuffix(path, "/")+"/")) && (stateMount == nil || len(path) > len(filepath.Clean(stateMount.MountPath))) {
			stateMount = mount
		}
	}
	for _, v := range d.Spec.Template.Spec.Volumes {
		if stateMount != nil && v.Name == stateMount.Name && v.PersistentVolumeClaim != nil && !v.PersistentVolumeClaim.ReadOnly && !stateMount.ReadOnly {
			persistent = true
		}
		if v.HostPath != nil {
			return ErrUnsupported
		}
		// The stage Pod runs init containers again while the source serves, so they must not write a claim.
		for _, init := range d.Spec.Template.Spec.InitContainers {
			for _, mount := range init.VolumeMounts {
				if v.PersistentVolumeClaim != nil && mount.Name == v.Name && !mount.ReadOnly {
					return ErrUnsupported
				}
			}
			// A block device has no read-only form, so attaching the claim's device can write it.
			for _, device := range init.VolumeDevices {
				if v.PersistentVolumeClaim != nil && device.Name == v.Name {
					return ErrUnsupported
				}
			}
		}
	}
	if !unattended || !postgres || !scratch || !persistent {
		return ErrUnsupported
	}
	return nil
}

// serverFlagDisables reports whether a server argument may turn off unattended upgrades or enable
// what they exclude. The server parses flags with Go's flag package: one or two dashes, a bare
// boolean flag is true, and =value takes strconv.ParseBool. Matching too much only refuses.
func serverFlagDisables(arg string) bool {
	name, value, hasValue := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(arg, "-"), "-"), "=")
	switch {
	case name == "upgrade-unattended" || name == "auto-migrate":
		on, err := strconv.ParseBool(value)
		return hasValue && (err != nil || !on)
	case strings.HasPrefix(name, "config-rollout-enrollment") || strings.HasPrefix(name, "ha"):
		return true
	}
	return false
}

// knownStager is one of the chart's init containers, root-key-stage or tls-stage: this repository's
// image running only its copy, which reads a Secret volume and writes an in-memory one.
func (e *Executor) knownStager(c corev1.Container) bool {
	return e.ourImage(c.Image) && len(c.Command) == 0 &&
		(c.Name == "root-key-stage" && slices.Equal(c.Args, []string{multicall.RootKeyStage}) ||
			c.Name == "tls-stage" && slices.Equal(c.Args, []string{multicall.TLSStage, "--once"}))
}

func (e *Executor) sourcePod(ctx context.Context, d *appsv1.Deployment) (*corev1.Pod, error) {
	selector, err := metav1.LabelSelectorAsSelector(d.Spec.Selector)
	if err != nil || selector.Empty() {
		return nil, ErrUnsupported
	}
	sets, err := e.client.AppsV1().ReplicaSets(d.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, err
	}
	owned := map[types.UID]bool{}
	for _, set := range sets.Items {
		owner := metav1.GetControllerOf(&set)
		if owner != nil && owner.Kind == "Deployment" && owner.UID == d.UID {
			owned[set.UID] = true
		}
	}
	pods, err := e.client.CoreV1().Pods(d.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, err
	}
	var selected *corev1.Pod
	for i := range pods.Items {
		pod := &pods.Items[i]
		owner := metav1.GetControllerOf(pod)
		if owner == nil || owner.Kind != "ReplicaSet" || !owned[owner.UID] || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		if selected != nil {
			return nil, ErrNotReady
		}
		selected = pod
	}
	if selected == nil || selected.DeletionTimestamp != nil || selected.Spec.NodeName == "" || selected.Status.Phase != corev1.PodRunning {
		return nil, ErrNotReady
	}
	return selected, nil
}

func runtimeDigest(imageID string) string {
	index := strings.LastIndex(imageID, "sha256:")
	if index < 0 {
		return ""
	}
	digest := imageID[index+len("sha256:"):]
	if releaseidentity.Digest(digest).Validate() != nil {
		return ""
	}
	return digest
}

func (e *Executor) observeKubernetes(ctx context.Context) (Observation, error) {
	d, err := e.deployment(ctx)
	if err != nil {
		return Observation{}, err
	}
	pod, err := e.sourcePod(ctx, d)
	if err != nil {
		return Observation{}, err
	}
	image := ""
	ready := false
	runtime := ""
	for _, container := range pod.Spec.Containers {
		if container.Name == e.c.Kubernetes.Container {
			image = container.Image
		}
	}
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == e.c.Kubernetes.Container {
			runtime = runtimeDigest(status.ImageID)
			ready = status.Ready && status.State.Running != nil
		}
	}
	podReady := false
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			podReady = true
		}
	}
	ready = ready && podReady
	if !strings.HasPrefix(image, e.c.ImageRepository+"@sha256:") || imageDigest(image) == "" || runtime == "" {
		return Observation{}, ErrConflict
	}
	for _, container := range d.Spec.Template.Spec.Containers {
		if container.Name == e.c.Kubernetes.Container && container.Image != image {
			return Observation{}, ErrNotReady
		}
	}
	child, cancel := context.WithTimeout(ctx, e.timeout())
	defer cancel()
	raw, err := e.pods.Exec(child, pod.Name, e.c.Kubernetes.Container, []string{e.c.Kubernetes.Binary, "security-maintenance", "inspect"})
	if err != nil {
		return Observation{}, err
	}
	var observation Observation
	if decode(raw, &observation) != nil || !validObservation(observation) {
		return Observation{}, ErrInvalid
	}
	if observation.Engine != releaseidentity.Postgres {
		return Observation{}, ErrUnsupported
	}
	observation.Image, observation.RuntimeImageSHA256 = image, runtime
	observation.Ready = observation.Ready && ready && d.Status.ObservedGeneration >= d.Generation && d.Status.Replicas == 1 && d.Status.UpdatedReplicas == 1 && d.Status.ReadyReplicas == 1 && d.Status.AvailableReplicas == 1 && d.Status.UnavailableReplicas == 0 && (d.Status.TerminatingReplicas == nil || *d.Status.TerminatingReplicas == 0)
	j, err := e.journal()
	if err != nil {
		return Observation{}, err
	}
	// A runtime reports the signed index digest or its derived platform child,
	// depending on what it pulled first. Either one proves the exact target.
	if j != nil && image == j.Target.Image && runtime != imageDigest(j.Target.Image) && runtime != j.Target.RuntimeImageSHA256 {
		return Observation{}, ErrConflict
	}
	return observation, nil
}

// stagePod is the one-shot Pod: the server container with its probes and ports removed, running
// args in image on node.
func (e *Executor) stagePod(d *appsv1.Deployment, node, image string, args []string) (*corev1.Pod, error) {
	p := e.c.Kubernetes
	spec := d.Spec.Template.Spec.DeepCopy()
	var server *corev1.Container
	for i := range spec.Containers {
		if spec.Containers[i].Name == p.Container {
			server = spec.Containers[i].DeepCopy()
		}
	}
	if server == nil {
		return nil, ErrUnsupported
	}
	server.Image = image
	server.Command = []string{p.Binary}
	server.Args = args
	server.ReadinessProbe, server.LivenessProbe, server.StartupProbe = nil, nil, nil
	server.Lifecycle = nil
	server.Ports = nil
	spec.Containers = []corev1.Container{*server}
	for i := range spec.InitContainers {
		if e.ourImage(spec.InitContainers[i].Image) {
			spec.InitContainers[i].Image = image
		}
	}
	spec.RestartPolicy = corev1.RestartPolicyNever
	spec.NodeName = node
	spec.Hostname, spec.Subdomain = "", ""
	spec.Affinity = nil
	spec.TopologySpreadConstraints = nil
	no := false
	spec.AutomountServiceAccountToken = &no
	if spec.ServiceAccountName == "" {
		spec.ServiceAccountName = "default"
	}
	// Dedicated labels prevent Services/Deployment selectors from routing to
	// this preparation-only Pod, which never opens a serving listener.
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: p.StagePod, Namespace: p.Namespace, Labels: map[string]string{"app.kubernetes.io/name": "hikyo-security-stage"}, Annotations: map[string]string{stageOwner: string(d.UID)}}, Spec: *spec}
	selector, err := metav1.LabelSelectorAsSelector(d.Spec.Selector)
	if err != nil || selector.Matches(labels.Set(pod.Labels)) {
		return nil, ErrUnsupported
	}
	// Kubernetes types keep the encoding of the client libraries that own their tags.
	raw, err := utiljson.Marshal(pod.Spec)
	if err != nil {
		return nil, err
	}
	pod.Annotations[stageTemplate] = string(releaseidentity.Hash(raw))
	return pod, nil
}

// targetArgs names the target the one-shot acts on, which it checks against its own image.
func targetArgs(command string, target Target) []string {
	return []string{"security-maintenance", command, "--expected-manifest", string(target.Identity.ManifestSHA256)}
}

func stageSpecMatches(expected, actual *corev1.Pod) bool {
	// Ownership annotations are locators, not authentication. A preexisting Pod
	// must also have the exact preparation process and mounted inputs. Extra
	// containers, credentials or command mutations cannot impersonate a stage.
	a, b := expected.Spec, actual.Spec
	return b.AutomountServiceAccountToken != nil && !*b.AutomountServiceAccountToken && !b.HostNetwork && !b.HostPID && !b.HostIPC && b.NodeName == a.NodeName && b.ServiceAccountName == a.ServiceAccountName && b.RestartPolicy == corev1.RestartPolicyNever && len(b.EphemeralContainers) == 0 && apiequality.Semantic.DeepEqual(a.Containers, admittedResources(a.Containers, b.Containers)) && apiequality.Semantic.DeepEqual(a.InitContainers, admittedResources(a.InitContainers, b.InitContainers)) && apiequality.Semantic.DeepEqual(a.Volumes, b.Volumes) && apiequality.Semantic.DeepEqual(a.SecurityContext, b.SecurityContext)
}

// admittedResources is actual without the resource keys expected leaves unset. Admission fills those in
// on a Pod but never on its template: requests from limits, and LimitRange defaults. A key the
// template sets must still match exactly.
func admittedResources(expected, actual []corev1.Container) []corev1.Container {
	if len(expected) != len(actual) {
		return actual
	}
	only := func(set, admitted corev1.ResourceList) corev1.ResourceList {
		kept := corev1.ResourceList{}
		for name, quantity := range admitted {
			if _, ok := set[name]; ok {
				kept[name] = quantity
			}
		}
		return kept
	}
	out := make([]corev1.Container, len(actual))
	for i := range actual {
		out[i] = *actual[i].DeepCopy()
		out[i].Resources.Limits = only(expected[i].Resources.Limits, actual[i].Resources.Limits)
		out[i].Resources.Requests = only(expected[i].Resources.Requests, actual[i].Resources.Requests)
	}
	return out
}

func (e *Executor) prepareKubernetes(ctx context.Context, target Target) (result Observation, returnErr error) {
	if err := e.requirePinned(); err != nil {
		return Observation{}, err
	}
	source, err := e.observeKubernetes(ctx)
	if err != nil {
		return Observation{}, err
	}
	if source.Maintenance || source.Phase != "healthy" || !source.Ready {
		return Observation{}, ErrNotReady
	}
	d, err := e.deployment(ctx)
	if err != nil {
		return Observation{}, err
	}
	pod, err := e.sourcePod(ctx, d)
	if err != nil {
		return Observation{}, err
	}
	stage, err := e.stagePod(d, pod.Spec.NodeName, target.Image, targetArgs("stage", target))
	if err != nil {
		return Observation{}, err
	}
	// A Pod an earlier crashed run left behind would make every later run conflict.
	if err := e.removeStagePod(ctx, d); err != nil {
		return Observation{}, err
	}
	raw, err := e.runStagePod(ctx, d, stage)
	if err != nil {
		return Observation{}, err
	}
	var staged Observation
	if decode(lastLine(raw), &staged) != nil || !validObservation(staged) {
		return Observation{}, ErrInvalid
	}
	staged.Image = source.Image
	staged.RuntimeImageSHA256 = source.RuntimeImageSHA256
	if !sameSource(source, staged) || staged.StagedTarget != target.Identity || staged.Phase != "staged" || staged.Ready || staged.RouteSHA256.Validate() != nil {
		return Observation{}, ErrConflict
	}
	after, err := e.observeKubernetes(ctx)
	if err != nil {
		return Observation{}, err
	}
	if !sameSource(source, after) || after.Maintenance || !after.Ready {
		return Observation{}, ErrConflict
	}
	// Save the exact source resourceVersion. Apply never refreshes a changed
	// desired state and silently overwrites a concurrent operator/controller.
	prepared, err := utiljson.Marshal(d)
	if err != nil {
		return Observation{}, err
	}
	if err := securefile.WriteAtomic(filepath.Join(e.c.StateDirectory, "kubernetes-prepared.json"), prepared, 0600); err != nil {
		return Observation{}, err
	}
	j := &deploymentJournal{Format: journalFormat, Phase: "prepared", Target: target, Source: source, Staged: staged, DesiredSHA256: releaseidentity.Hash(prepared)}
	if err := e.save(j); err != nil {
		return Observation{}, err
	}
	return staged, nil
}

// runStagePod runs the one-shot Pod to completion and returns its bounded log.
// The Pod is removed afterwards by exact UID. A Pod left by a crashed run is
// resumed only if it is exactly this one.
func (e *Executor) runStagePod(ctx context.Context, d *appsv1.Deployment, stage *corev1.Pod) (raw []byte, returnErr error) {
	created, err := e.client.CoreV1().Pods(d.Namespace).Create(ctx, stage, metav1.CreateOptions{})
	resumed := false
	if apierrors.IsAlreadyExists(err) {
		resumed = true
		created, err = e.client.CoreV1().Pods(d.Namespace).Get(ctx, stage.Name, metav1.GetOptions{})
		if err == nil && (created.Annotations[stageOwner] != stage.Annotations[stageOwner] || created.Annotations[stageTemplate] != stage.Annotations[stageTemplate] || created.DeletionTimestamp != nil || !stageSpecMatches(stage, created)) {
			return nil, ErrConflict
		}
	}
	if err != nil {
		return nil, err
	}
	if created.UID == "" {
		return nil, ErrConflict
	}
	if resumed && !stageSpecMatches(stage, created) {
		return nil, ErrConflict
	}
	ownedUID := created.UID
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		zero := int64(0)
		err := e.client.CoreV1().Pods(d.Namespace).Delete(cleanup, stage.Name, metav1.DeleteOptions{GracePeriodSeconds: &zero, Preconditions: &metav1.Preconditions{UID: &ownedUID}})
		if err != nil && !apierrors.IsNotFound(err) {
			returnErr = errors.Join(returnErr, err)
		}
	}()
	if !stageSpecMatches(stage, created) {
		return nil, ErrConflict
	}
	deadline, cancel := context.WithTimeout(ctx, e.timeout())
	defer cancel()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		current, err := e.client.CoreV1().Pods(d.Namespace).Get(deadline, stage.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		if current.UID != ownedUID || current.Annotations[stageTemplate] != stage.Annotations[stageTemplate] || !stageSpecMatches(stage, current) {
			return nil, ErrConflict
		}
		if current.Status.Phase == corev1.PodFailed {
			return nil, errors.New("security deployment: target one-shot Pod failed")
		}
		if current.Status.Phase == corev1.PodSucceeded {
			break
		}
		select {
		case <-deadline.Done():
			return nil, deadline.Err()
		case <-ticker.C:
		}
	}
	logs, cancelLogs := context.WithTimeout(ctx, e.timeout())
	defer cancelLogs()
	raw, err = e.pods.Logs(logs, stage.Name, e.c.Kubernetes.Container)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func (e *Executor) applyKubernetes(ctx context.Context, j *deploymentJournal) error {
	if err := e.requirePinned(); err != nil {
		return err
	}
	source, err := e.observeKubernetes(ctx)
	if err != nil {
		return err
	}
	if !sameSource(source, j.Source) || source.Maintenance || !source.Ready {
		return ErrConflict
	}
	raw, err := privateFile(filepath.Join(e.c.StateDirectory, "kubernetes-prepared.json"))
	if err != nil {
		return err
	}
	if releaseidentity.Hash(raw) != j.DesiredSHA256 {
		return ErrConflict
	}
	var prepared appsv1.Deployment
	if json.Unmarshal(raw, &prepared) != nil {
		return ErrInvalid
	}
	current, err := e.deployment(ctx)
	if err != nil {
		return err
	}
	if current.UID != prepared.UID || current.ResourceVersion != prepared.ResourceVersion {
		return ErrConflict
	}
	currentRaw, err := utiljson.Marshal(current)
	if err != nil {
		return err
	}
	if releaseidentity.Hash(currentRaw) != j.DesiredSHA256 {
		return ErrConflict
	}
	// The rollback one-shot must run where the source Pod ran.
	pod, err := e.sourcePod(ctx, current)
	if err != nil {
		return err
	}
	j.Node = pod.Spec.NodeName
	e.withTarget(current, j.Target)
	if err := e.beginReplacement(j); err != nil {
		return err
	}
	// Recreate stops the old ReplicaSet before starting its replacement. The
	// database gate remains the writer-exclusion authority under partitions.
	_, err = e.client.AppsV1().Deployments(current.Namespace).Update(ctx, current, metav1.UpdateOptions{})
	// These replies come from authentication, authorization, admission, lookup or the
	// version check, before storage. Timeouts and 5xx replies stay applying.
	if !apierrors.IsConflict(err) && !apierrors.IsForbidden(err) && !apierrors.IsUnauthorized(err) && !apierrors.IsInvalid(err) && !apierrors.IsBadRequest(err) && !apierrors.IsNotFound(err) {
		return err
	}
	// client-go can retry a write whose first attempt committed, and report the retry's
	// reply. Only a read showing the same generation without the target proves nothing
	// changed: a moved generation could be that write reverted by another writer.
	latest, getErr := e.client.AppsV1().Deployments(current.Namespace).Get(ctx, current.Name, metav1.GetOptions{})
	if getErr != nil || latest.Generation != current.Generation || e.selectsTarget(latest, j) {
		return errors.Join(err, getErr)
	}
	if apierrors.IsConflict(err) {
		err = errors.Join(err, ErrConflict)
	}
	return e.refuseMutation(j, errors.Join(ErrPreconditionRefused, err))
}

// withTarget is the exact desired-state change Apply writes. The source restore
// rebuilds it to prove that nobody else has changed the Deployment since.
func (e *Executor) withTarget(d *appsv1.Deployment, target Target) {
	for i := range d.Spec.Template.Spec.Containers {
		if e.ourImage(d.Spec.Template.Spec.Containers[i].Image) {
			d.Spec.Template.Spec.Containers[i].Image = target.Image
		}
	}
	for i := range d.Spec.Template.Spec.InitContainers {
		if e.ourImage(d.Spec.Template.Spec.InitContainers[i].Image) {
			d.Spec.Template.Spec.InitContainers[i].Image = target.Image
		}
	}
	if d.Spec.Template.Annotations == nil {
		d.Spec.Template.Annotations = map[string]string{}
	}
	d.Spec.Template.Annotations[replacementTarget] = string(target.Identity.ManifestSHA256)
}

// restoreKubernetes puts the prepared Deployment template back, which returns the
// server container to the journal's source image and removes the target
// annotation. It writes only over the exact state Apply produced, so any other
// writer's change refuses. A conflict re-reads, and any failed write is judged
// by a read: only a Deployment that is back at the source counts as done.
func (e *Executor) restoreKubernetes(ctx context.Context, j *deploymentJournal) error {
	if err := e.requirePinned(); err != nil {
		return err
	}
	prepared, err := e.preparedDeployment(j)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(prepared.Spec.Template.Spec.Containers, func(c corev1.Container) bool {
		return c.Name == e.c.Kubernetes.Container && c.Image == j.Source.Image
	}) {
		return ErrConflict
	}
	applied, appliedZero := e.appliedSpecs(&prepared, j.Target)
	for range 3 {
		current, getErr := e.deploymentEither(ctx)
		if getErr != nil {
			return errors.Join(err, getErr)
		}
		if current.UID != prepared.UID {
			return ErrConflict
		}
		if apiequality.Semantic.DeepEqual(current.Spec, prepared.Spec) {
			return nil
		}
		if !apiequality.Semantic.DeepEqual(current.Spec, applied.Spec) && !apiequality.Semantic.DeepEqual(current.Spec, appliedZero.Spec) {
			return ErrConflict
		}
		// Template and replica count return in this one write, so no Pod starts
		// from the source template at zero replicas or the target at one.
		current.Spec = *prepared.Spec.DeepCopy()
		_, err = e.client.AppsV1().Deployments(current.Namespace).Update(ctx, current, metav1.UpdateOptions{})
		if err == nil {
			return nil
		}
		if !apierrors.IsConflict(err) {
			break
		}
	}
	// A reply is never trusted, whatever its status: one more read decides.
	latest, getErr := e.deploymentEither(ctx)
	if getErr == nil && apiequality.Semantic.DeepEqual(latest.Spec, prepared.Spec) {
		return nil
	}
	return errors.Join(err, getErr)
}

// appliedSpecs are the two states Apply and the rollback's stop can leave: the
// target template at one replica, and the same template scaled to zero.
func (e *Executor) appliedSpecs(prepared *appsv1.Deployment, target Target) (applied, zero *appsv1.Deployment) {
	applied = prepared.DeepCopy()
	e.withTarget(applied, target)
	zero = applied.DeepCopy()
	none := int32(0)
	zero.Spec.Replicas = &none
	return applied, zero
}

// isTargetImage compares by digest: the reference's own, or the runtime image ID when one is known.
func isTargetImage(reference, runtimeID string, target Target) bool {
	digest := imageDigest(target.Image)
	if digest == "" {
		return false
	}
	if imageDigest(reference) == digest {
		return true
	}
	id := runtimeDigest(runtimeID)
	return id != "" && (id == digest || id == target.RuntimeImageSHA256)
}

// kubernetesTargetDeployed reports whether the Deployment is scaled up with the target as its server image,
// or any of its Pods runs the target, whatever reference started it.
func (e *Executor) kubernetesTargetDeployed(ctx context.Context, target Target) (bool, error) {
	d, err := e.deploymentEither(ctx)
	if err != nil {
		return false, err
	}
	if d.Spec.Replicas != nil && *d.Spec.Replicas > 0 && slices.ContainsFunc(d.Spec.Template.Spec.Containers, func(c corev1.Container) bool {
		return c.Name == e.c.Kubernetes.Container && isTargetImage(c.Image, "", target)
	}) {
		return true, nil
	}
	selector, err := metav1.LabelSelectorAsSelector(d.Spec.Selector)
	if err != nil || selector.Empty() {
		return false, ErrUnsupported
	}
	pods, err := e.client.CoreV1().Pods(d.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return false, err
	}
	for _, pod := range pods.Items {
		// Stage Pods have no ReplicaSet; finished Pods run nothing.
		if owner := metav1.GetControllerOf(&pod); owner == nil || owner.Kind != "ReplicaSet" || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		for _, c := range pod.Spec.Containers {
			if c.Name == e.c.Kubernetes.Container && isTargetImage(c.Image, "", target) {
				return true, nil
			}
		}
		for _, status := range pod.Status.ContainerStatuses {
			if status.Name == e.c.Kubernetes.Container && isTargetImage("", status.ImageID, target) {
				return true, nil
			}
		}
	}
	return false, nil
}

// deploymentEither reads the Deployment at one replica or at the zero a rollback leaves.
func (e *Executor) deploymentEither(ctx context.Context) (*appsv1.Deployment, error) {
	d, err := e.deploymentAt(ctx, 1)
	if errors.Is(err, ErrUnsupported) {
		return e.deploymentAt(ctx, 0)
	}
	return d, err
}

func (e *Executor) preparedDeployment(j *deploymentJournal) (appsv1.Deployment, error) {
	var prepared appsv1.Deployment
	raw, err := privateFile(filepath.Join(e.c.StateDirectory, "kubernetes-prepared.json"))
	if err != nil {
		return prepared, err
	}
	if releaseidentity.Hash(raw) != j.DesiredSHA256 {
		return prepared, ErrConflict
	}
	if json.Unmarshal(raw, &prepared) != nil {
		return prepared, ErrInvalid
	}
	return prepared, nil
}

// rollBackKubernetes scales the target to nothing and waits until no Pod of the
// Deployment exists, terminating ones included. Only then does the one-shot run
// in the target image on the recorded node, and only then is the source written
// back. A one-shot failure still writes the source back: its gate decides.
func (e *Executor) rollBackKubernetes(ctx context.Context, j *deploymentJournal) (restored bool, err error) {
	if err := e.requirePinned(); err != nil {
		return false, err
	}
	if j.Node == "" {
		return false, fmt.Errorf("%w: the journal predates the rollback and records no node", ErrRecoveryRequired)
	}
	prepared, err := e.preparedDeployment(j)
	if err != nil {
		return false, err
	}
	if err := e.refuseKubernetesRollback(ctx, j); err != nil {
		return false, err
	}
	if err := e.stopKubernetesTarget(ctx, &prepared, j.Target); err != nil {
		return false, err
	}
	if err := e.markRollingBack(j); err != nil {
		return false, err
	}
	_, appliedZero := e.appliedSpecs(&prepared, j.Target)
	oneShot := func() (bool, error) {
		// Read here, after the stop is proven, so a failed read still gets the source write-back.
		d, err := e.deploymentAt(ctx, 0)
		if err != nil {
			return false, err
		}
		// The one-shot is built from this read, so it must still be the stop that was proven.
		if !apiequality.Semantic.DeepEqual(d.Spec, appliedZero.Spec) {
			return false, fmt.Errorf("%w: the Deployment changed after the target was stopped", ErrConflict)
		}
		stage, err := e.stagePod(d, j.Node, j.Target.Image, targetArgs("rollback", j.Target))
		if err != nil {
			return false, err
		}
		if err := e.removeStagePod(ctx, d); err != nil {
			return false, err
		}
		raw, err := e.runStagePod(ctx, d, stage)
		if err != nil {
			return false, err
		}
		return parseRollbackReport(raw)
	}
	restored, oneShotErr := oneShot()
	writeBack, cancel := e.writeBackContext(ctx)
	defer cancel()
	return restored, errors.Join(oneShotErr, e.restoreKubernetes(writeBack, j))
}

// refuseKubernetesRollback leaves alone what is not a stopped or failing target: a source image
// serving again has nothing to roll back, whatever its generation; a target past its final gate is a
// person's decision; and a Pod running neither the target nor the source is an operator's. A
// workload that cannot be observed, such as a crashed or stopped target, proceeds.
func (e *Executor) refuseKubernetesRollback(ctx context.Context, j *deploymentJournal) error {
	if live, err := e.observeKubernetes(ctx); err == nil {
		if live.Image == j.Source.Image && live.Phase == "healthy" && !live.Maintenance && live.Ready {
			return fmt.Errorf("%w: the source image is serving again; nothing was stopped", ErrConflict)
		}
		if live.PastFinalGate(j.Source.Identity) {
			return fmt.Errorf("%w: the Deployment is healthy at a release past its final gate; nothing was stopped", ErrConflict)
		}
	}
	d, err := e.deploymentEither(ctx)
	if err != nil {
		return err
	}
	selector, err := metav1.LabelSelectorAsSelector(d.Spec.Selector)
	if err != nil || selector.Empty() {
		return ErrUnsupported
	}
	pods, err := e.client.CoreV1().Pods(d.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return err
	}
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		for _, c := range pod.Spec.Containers {
			if c.Name == e.c.Kubernetes.Container && c.Image != j.Source.Image && !isTargetImage(c.Image, "", j.Target) {
				return fmt.Errorf("%w: Pod %s runs %s, neither the target nor the source; nothing was stopped", ErrConflict, pod.Name, c.Image)
			}
		}
	}
	return nil
}

// inspectKubernetes reads the control row through a stage Pod of the source image on the node the
// source ran on, for a target that cannot be exec'd into: scaled to zero, or crash-looping. It runs
// inspect --control-row, which prints the row with no binary or schema checks, so the source image
// can read a row the target or a hop wrote. The Pod is always removed.
func (e *Executor) inspectKubernetes(ctx context.Context, j *deploymentJournal) ([]byte, error) {
	if err := e.requirePinned(); err != nil {
		return nil, err
	}
	if j == nil || j.Phase != "applying" || j.Node == "" {
		return nil, fmt.Errorf("%w: no applying journal records the node to read the control row on", ErrRecoveryRequired)
	}
	d, err := e.deploymentEither(ctx)
	if err != nil {
		return nil, err
	}
	stage, err := e.stagePod(d, j.Node, j.Source.Image, []string{"security-maintenance", "inspect", "--control-row"})
	if err != nil {
		return nil, err
	}
	if err := e.removeStagePod(ctx, d); err != nil {
		return nil, err
	}
	return e.runStagePod(ctx, d, stage)
}

func (e *Executor) stopKubernetesTarget(ctx context.Context, prepared *appsv1.Deployment, target Target) error {
	applied, appliedZero := e.appliedSpecs(prepared, target)
	// Three writes, each followed by a read, so the last successful one is also proven.
	for attempt := range 4 {
		current, err := e.deploymentEither(ctx)
		if err != nil {
			return err
		}
		if current.UID != prepared.UID {
			return ErrConflict
		}
		if apiequality.Semantic.DeepEqual(current.Spec, appliedZero.Spec) {
			return e.awaitNoPods(ctx, current)
		}
		if attempt == 3 {
			break
		}
		// A failed one-shot leaves the source written back; the retry stops it the way it stops the target.
		retry := apiequality.Semantic.DeepEqual(current.Spec, prepared.Spec)
		if !retry && !apiequality.Semantic.DeepEqual(current.Spec, applied.Spec) {
			return ErrConflict
		}
		if retry {
			current.Spec = *appliedZero.Spec.DeepCopy()
		}
		current.Spec.Replicas = appliedZero.Spec.Replicas
		_, err = e.client.AppsV1().Deployments(current.Namespace).Update(ctx, current, metav1.UpdateOptions{})
		if err != nil && !apierrors.IsConflict(err) {
			return err
		}
	}
	return ErrConflict
}

// awaitNoPods is the stop proof: the controller has seen the zero, reports no
// replicas, and no Pod that could still write exists, including terminating ones.
func (e *Executor) awaitNoPods(ctx context.Context, d *appsv1.Deployment) error {
	selector, err := metav1.LabelSelectorAsSelector(d.Spec.Selector)
	if err != nil || selector.Empty() {
		return ErrUnsupported
	}
	deadline, cancel := context.WithTimeout(ctx, e.timeout())
	defer cancel()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		current, err := e.deploymentAt(deadline, 0)
		if err != nil {
			return err
		}
		pods, err := e.client.CoreV1().Pods(d.Namespace).List(deadline, metav1.ListOptions{LabelSelector: selector.String()})
		if err != nil {
			return err
		}
		running := slices.ContainsFunc(pods.Items, func(p corev1.Pod) bool {
			return p.Status.Phase != corev1.PodSucceeded && p.Status.Phase != corev1.PodFailed
		})
		if !running && current.Status.ObservedGeneration >= current.Generation && current.Status.Replicas == 0 && (current.Status.TerminatingReplicas == nil || *current.Status.TerminatingReplicas == 0) {
			return nil
		}
		select {
		case <-deadline.Done():
			return fmt.Errorf("%w: the target Pods did not stop: %w", ErrRecoveryRequired, deadline.Err())
		case <-ticker.C:
		}
	}
}

// removeStagePod clears an earlier one-shot of this Deployment, so a finished
// stage or rollback Pod cannot be mistaken for this run's. Another owner's Pod refuses.
func (e *Executor) removeStagePod(ctx context.Context, d *appsv1.Deployment) error {
	name := e.c.Kubernetes.StagePod
	pods := e.client.CoreV1().Pods(d.Namespace)
	existing, err := pods.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if existing.Annotations[stageOwner] != string(d.UID) {
		return ErrConflict
	}
	zero := int64(0)
	if err := pods.Delete(ctx, name, metav1.DeleteOptions{GracePeriodSeconds: &zero, Preconditions: &metav1.Preconditions{UID: &existing.UID}}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	// A finalizer keeps an accepted deletion visible, and the next create would conflict with it.
	wait, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		current, err := pods.Get(wait, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) || err == nil && current.UID != existing.UID {
			return nil
		}
		// A read cut short by the wait is the same timeout as one between reads.
		if err != nil && wait.Err() == nil {
			return err
		}
		select {
		case <-wait.Done():
			return fmt.Errorf("%w: the earlier one-shot Pod %s is still being removed: %w", ErrConflict, name, wait.Err())
		case <-ticker.C:
		}
	}
}

func (e *Executor) selectsTarget(d *appsv1.Deployment, j *deploymentJournal) bool {
	if d.Spec.Template.Annotations[replacementTarget] == string(j.Target.Identity.ManifestSHA256) {
		return true
	}
	return slices.ContainsFunc(d.Spec.Template.Spec.Containers, func(c corev1.Container) bool { return c.Image == j.Target.Image })
}

// FollowCoordinator moves this coordinator's own CronJob to image, the running image
// the owner observed. It is a workload write, so only an applying run may call it,
// never Observe, which check, enroll and reconcile also reach. The caller has
// already authenticated image as the signed inventory of the release it names.
// Comparing first makes a repeat free.
func (e *Executor) FollowCoordinator(ctx context.Context, image string) error {
	p := e.c.Kubernetes
	if e.c.Kind != Kubernetes || p.CronJob == "" {
		return nil
	}
	if !strings.HasPrefix(image, e.c.ImageRepository+"@sha256:") || imageDigest(image) == "" {
		return ErrInvalid
	}
	cron, err := e.client.BatchV1().CronJobs(p.Namespace).Get(ctx, p.CronJob, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if cron.ResourceVersion == "" {
		return ErrConflict
	}
	stale := func(containers []corev1.Container) []map[string]string {
		var out []map[string]string
		for _, c := range containers {
			if e.ourImage(c.Image) && c.Image != image {
				out = append(out, map[string]string{"name": c.Name, "image": image})
			}
		}
		return out
	}
	spec := map[string]any{}
	if containers := stale(cron.Spec.JobTemplate.Spec.Template.Spec.Containers); containers != nil {
		spec["containers"] = containers
	}
	if containers := stale(cron.Spec.JobTemplate.Spec.Template.Spec.InitContainers); containers != nil {
		spec["initContainers"] = containers
	}
	if len(spec) == 0 {
		return nil
	}
	// The resourceVersion makes the patch conditional on the read: a concurrent change refuses it.
	patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"resourceVersion": cron.ResourceVersion}, "spec": map[string]any{"jobTemplate": map[string]any{"spec": map[string]any{"template": map[string]any{"spec": spec}}}}}, jsonwire.Stable)
	if err != nil {
		return err
	}
	_, err = e.client.BatchV1().CronJobs(p.Namespace).Patch(ctx, p.CronJob, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
	if apierrors.IsConflict(err) {
		return errors.Join(ErrConflict, err)
	}
	return err
}

// PublishStatus copies the coordinator's public status into the ConfigMap the
// serving Pod mounts, since the coordinator's volume is not visible to it. The
// caller treats a failure as non-fatal; the ConfigMap is created by the chart,
// so the coordinator's Role needs only get and update on that one name.
func (e *Executor) PublishStatus(ctx context.Context, status []byte) error {
	p := e.c.Kubernetes
	if e.c.Kind != Kubernetes || p.StatusConfigMap == "" {
		return nil
	}
	maps := e.client.CoreV1().ConfigMaps(p.Namespace)
	current, err := maps.Get(ctx, p.StatusConfigMap, metav1.GetOptions{})
	if err != nil {
		return err
	}
	current.Data = map[string]string{"status.json": string(status)}
	_, err = maps.Update(ctx, current, metav1.UpdateOptions{})
	return err
}
