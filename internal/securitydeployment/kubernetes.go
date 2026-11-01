package securitydeployment

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/securefile"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
)

const stageOwner = "hikyo.dev/security-stage-owner"
const stageTemplate = "hikyo.dev/security-stage-template"
const replacementTarget = "hikyo.dev/security-release-manifest"

func (e *Executor) kubectlArgs() []string {
	p := e.c.Kubernetes
	return []string{"--kubeconfig", p.Kubeconfig, "--context", p.Context, "--namespace", p.Namespace, "--request-timeout=30s"}
}

func managedDesiredState(d *appsv1.Deployment) bool {
	for _, key := range []string{"kustomize.toolkit.fluxcd.io/name", "helm.toolkit.fluxcd.io/name", "argocd.argoproj.io/instance"} {
		if d.Labels[key] != "" || d.Annotations[key] != "" {
			return true
		}
	}
	for _, key := range []string{"meta.helm.sh/release-name", "meta.helm.sh/release-namespace", "argocd.argoproj.io/tracking-id"} {
		if d.Annotations[key] != "" {
			return true
		}
	}
	return false
}

func (e *Executor) deployment(ctx context.Context) (*appsv1.Deployment, error) {
	p := e.c.Kubernetes
	d, err := e.client.AppsV1().Deployments(p.Namespace).Get(ctx, p.Deployment, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if string(d.UID) != p.DeploymentUID || d.DeletionTimestamp != nil || d.ResourceVersion == "" {
		return nil, ErrConflict
	}
	if managedDesiredState(d) || d.Spec.Replicas == nil || *d.Spec.Replicas != 1 || d.Spec.Strategy.Type != appsv1.RecreateDeploymentStrategyType || d.Spec.Template.Spec.AutomountServiceAccountToken == nil || *d.Spec.Template.Spec.AutomountServiceAccountToken || d.Spec.Template.Spec.HostNetwork || d.Spec.Template.Spec.HostPID || d.Spec.Template.Spec.HostIPC {
		return nil, ErrUnsupported
	}
	var server *corev1.Container
	for i := range d.Spec.Template.Spec.Containers {
		if d.Spec.Template.Spec.Containers[i].Name == p.Container {
			server = &d.Spec.Template.Spec.Containers[i]
		}
	}
	if server == nil || !strings.HasPrefix(server.Image, e.c.ImageRepository+"@sha256:") || imageDigest(server.Image) == "" {
		return nil, ErrUnsupported
	}
	unattended, postgres, scratch, persistent := false, false, false, false
	stateDirectory := ""
	seen := map[string]bool{}
	for _, v := range server.Env {
		if seen[v.Name] {
			return nil, ErrUnsupported
		}
		seen[v.Name] = true
		if v.Name == "HIKYO_UPGRADE_UNATTENDED" && v.Value == "true" && v.ValueFrom == nil {
			unattended = true
		}
		if v.Name == "HIKYO_HA" && v.Value != "false" {
			return nil, ErrUnsupported
		}
		if v.Name == "HIKYO_CONFIG_ROLLOUT_ENROLLMENT" {
			return nil, ErrUnsupported
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
	for _, arg := range server.Args {
		if strings.HasPrefix(arg, "--config-rollout-enrollment") || strings.HasPrefix(arg, "--ha") || arg == "--upgrade-unattended=false" || arg == "--auto-migrate=false" {
			return nil, ErrUnsupported
		}
	}
	for _, v := range d.Spec.Template.Spec.Volumes {
		if v.PersistentVolumeClaim != nil {
			for _, mount := range server.VolumeMounts {
				if mount.Name == v.Name && !mount.ReadOnly && stateDirectory != "" && (stateDirectory == mount.MountPath || strings.HasPrefix(stateDirectory, mount.MountPath+"/")) {
					persistent = true
				}
			}
		}
		if v.HostPath != nil {
			return nil, ErrUnsupported
		}
	}
	if !unattended || !postgres || !scratch || !persistent {
		return nil, ErrUnsupported
	}
	return d, nil
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
	args := append(e.kubectlArgs(), "exec", pod.Name, "--container", e.c.Kubernetes.Container, "--", e.c.Kubernetes.Binary, "security-maintenance", "inspect")
	raw, err := e.run(ctx, e.c.Kubernetes.Kubectl, args, nil)
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
	if j != nil && image == j.Target.Image {
		expected := j.Target.RuntimeImageSHA256
		if expected == "" {
			expected = imageDigest(j.Target.Image)
		}
		if runtime != expected {
			return Observation{}, ErrConflict
		}
	}
	return observation, nil
}

func (e *Executor) stagePod(d *appsv1.Deployment, source *corev1.Pod, target Target) (*corev1.Pod, error) {
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
	server.Image = target.Image
	server.Command = []string{p.Binary}
	server.Args = []string{"security-maintenance", "stage", "--expected-manifest", string(target.Identity.ManifestSHA256)}
	server.ReadinessProbe, server.LivenessProbe, server.StartupProbe = nil, nil, nil
	server.Lifecycle = nil
	server.Ports = nil
	spec.Containers = []corev1.Container{*server}
	for i := range spec.InitContainers {
		if e.ourImage(spec.InitContainers[i].Image) {
			spec.InitContainers[i].Image = target.Image
		}
	}
	spec.RestartPolicy = corev1.RestartPolicyNever
	spec.NodeName = source.Spec.NodeName
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
	raw, err := json.Marshal(pod.Spec)
	if err != nil {
		return nil, err
	}
	pod.Annotations[stageTemplate] = string(releaseidentity.Hash(raw))
	return pod, nil
}

func stageSpecMatches(expected, actual *corev1.Pod) bool {
	// Ownership annotations are locators, not authentication. A preexisting Pod
	// must also have the exact preparation process and mounted inputs. Extra
	// containers, credentials or command mutations cannot impersonate a stage.
	a, b := expected.Spec, actual.Spec
	return b.AutomountServiceAccountToken != nil && !*b.AutomountServiceAccountToken && !b.HostNetwork && !b.HostPID && !b.HostIPC && b.NodeName == a.NodeName && b.ServiceAccountName == a.ServiceAccountName && b.RestartPolicy == corev1.RestartPolicyNever && len(b.EphemeralContainers) == 0 && apiequality.Semantic.DeepEqual(a.Containers, b.Containers) && apiequality.Semantic.DeepEqual(a.InitContainers, b.InitContainers) && apiequality.Semantic.DeepEqual(a.Volumes, b.Volumes) && apiequality.Semantic.DeepEqual(a.SecurityContext, b.SecurityContext)
}

func (e *Executor) prepareKubernetes(ctx context.Context, target Target) (result Observation, returnErr error) {
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
	stage, err := e.stagePod(d, pod, target)
	if err != nil {
		return Observation{}, err
	}
	created, err := e.client.CoreV1().Pods(d.Namespace).Create(ctx, stage, metav1.CreateOptions{})
	resumed := false
	if apierrors.IsAlreadyExists(err) {
		resumed = true
		created, err = e.client.CoreV1().Pods(d.Namespace).Get(ctx, stage.Name, metav1.GetOptions{})
		if err == nil && (created.Annotations[stageOwner] != stage.Annotations[stageOwner] || created.Annotations[stageTemplate] != stage.Annotations[stageTemplate] || created.DeletionTimestamp != nil || !stageSpecMatches(stage, created)) {
			return Observation{}, ErrConflict
		}
	}
	if err != nil {
		return Observation{}, err
	}
	if created.UID == "" {
		return Observation{}, ErrConflict
	}
	if resumed && !stageSpecMatches(stage, created) {
		return Observation{}, ErrConflict
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
		return Observation{}, ErrConflict
	}
	seconds := e.c.TimeoutSeconds
	if seconds == 0 {
		seconds = 900
	}
	deadline, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		current, err := e.client.CoreV1().Pods(d.Namespace).Get(deadline, stage.Name, metav1.GetOptions{})
		if err != nil {
			return Observation{}, err
		}
		if current.UID != ownedUID || current.Annotations[stageTemplate] != stage.Annotations[stageTemplate] || !stageSpecMatches(stage, current) {
			return Observation{}, ErrConflict
		}
		if current.Status.Phase == corev1.PodFailed {
			return Observation{}, errors.New("security deployment: target artifact staging failed")
		}
		if current.Status.Phase == corev1.PodSucceeded {
			break
		}
		select {
		case <-deadline.Done():
			return Observation{}, deadline.Err()
		case <-ticker.C:
		}
	}
	args := append(e.kubectlArgs(), "logs", stage.Name, "--container", e.c.Kubernetes.Container, "--limit-bytes=1048576")
	raw, err := e.run(ctx, e.c.Kubernetes.Kubectl, args, nil)
	if err != nil {
		return Observation{}, err
	}
	var staged Observation
	if decode(raw, &staged) != nil || !validObservation(staged) {
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
	prepared, err := json.Marshal(d)
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

func (e *Executor) applyKubernetes(ctx context.Context, j *deploymentJournal) error {
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
	currentRaw, err := json.Marshal(current)
	if err != nil {
		return err
	}
	if releaseidentity.Hash(currentRaw) != j.DesiredSHA256 {
		return ErrConflict
	}
	for i := range current.Spec.Template.Spec.Containers {
		if e.ourImage(current.Spec.Template.Spec.Containers[i].Image) {
			current.Spec.Template.Spec.Containers[i].Image = j.Target.Image
		}
	}
	for i := range current.Spec.Template.Spec.InitContainers {
		if e.ourImage(current.Spec.Template.Spec.InitContainers[i].Image) {
			current.Spec.Template.Spec.InitContainers[i].Image = j.Target.Image
		}
	}
	if current.Spec.Template.Annotations == nil {
		current.Spec.Template.Annotations = map[string]string{}
	}
	current.Spec.Template.Annotations[replacementTarget] = string(j.Target.Identity.ManifestSHA256)
	if err := e.beginReplacement(j); err != nil {
		return err
	}
	// Recreate stops the old ReplicaSet before starting its replacement. The
	// database gate remains the writer-exclusion authority under partitions.
	_, err = e.client.AppsV1().Deployments(current.Namespace).Update(ctx, current, metav1.UpdateOptions{})
	if apierrors.IsConflict(err) {
		j.Phase = "prepared"
		return errors.Join(ErrPreconditionRefused, ErrConflict, e.save(j))
	}
	return err
}
