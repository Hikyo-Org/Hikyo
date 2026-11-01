package securitydeployment

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

type kubernetesFixture struct {
	t             *testing.T
	executor      *Executor
	client        *fake.Clientset
	target        Target
	current       Observation
	stage         *corev1.Pod
	forceConflict bool
}

func owner(kind, name string, uid types.UID) []metav1.OwnerReference {
	yes := true
	return []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: kind, Name: name, UID: uid, Controller: &yes}}
}

func newKubernetesFixture(t *testing.T) *kubernetesFixture {
	t.Helper()
	one, no := int32(1), false
	image := fixtureRepository + "@sha256:" + strings.Repeat("a", 64)
	labels := map[string]string{"app": "hikyo"}
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "hikyo", Namespace: "hikyo", UID: "deployment-1", ResourceVersion: "1", Generation: 1}, Spec: appsv1.DeploymentSpec{Replicas: &one, Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}, Selector: &metav1.LabelSelector{MatchLabels: labels}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels}, Spec: corev1.PodSpec{AutomountServiceAccountToken: &no, Containers: []corev1.Container{{Name: "server", Image: image, Args: []string{"server", "--root-key-file=/run/hikyo-root/root-key"}, Env: []corev1.EnvVar{{Name: "HIKYO_UPGRADE_UNATTENDED", Value: "true"}, {Name: "HIKYO_UPGRADE_STATE_DIR", Value: "/var/lib/hikyo-upgrade/private"}, {Name: "HIKYO_DB", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "production-database"}, Key: "HIKYO_DB"}}}, {Name: "HIKYO_UPGRADE_SCRATCH_POSTGRES_DSN", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "scratch-database"}, Key: "HIKYO_UPGRADE_SCRATCH_POSTGRES_DSN"}}}}, VolumeMounts: []corev1.VolumeMount{{Name: "upgrade-state", MountPath: "/var/lib/hikyo-upgrade"}}}}, InitContainers: []corev1.Container{{Name: "root-key-stage", Image: image, Args: []string{"__hikyo-stage-root-key"}}}, Volumes: []corev1.Volume{{Name: "upgrade-state", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "upgrade-state"}}}}}}}, Status: appsv1.DeploymentStatus{ObservedGeneration: 1, Replicas: 1, UpdatedReplicas: 1, ReadyReplicas: 1, AvailableReplicas: 1}}
	set := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "hikyo-source", Namespace: "hikyo", UID: "replica-set-1", Labels: labels, OwnerReferences: owner("Deployment", d.Name, d.UID)}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "hikyo-source-pod", Namespace: "hikyo", UID: "pod-1", Labels: labels, OwnerReferences: owner("ReplicaSet", set.Name, set.UID)}, Spec: *d.Spec.Template.Spec.DeepCopy(), Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Name: "server", ImageID: "containerd://sha256:" + strings.Repeat("a", 64), Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}}}
	pod.Spec.NodeName = "node-1"
	pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
	client := fake.NewClientset(d, set, pod)
	f := &kubernetesFixture{t: t, client: client, target: Target{Identity: fixtureIdentity(2), Image: fixtureRepository + "@sha256:" + strings.Repeat("b", 64), EligibilityUntil: time.Now().Add(time.Hour)}, current: fixtureObservation()}
	f.current.Engine = releaseidentity.Postgres
	client.PrependReactor("create", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
		create := action.(ktesting.CreateAction)
		stage := create.GetObject().(*corev1.Pod).DeepCopy()
		if stage.Name != "hikyo-security-stage" {
			return false, nil, nil
		}
		stage.UID = "stage-pod-1"
		// Match API admission, including a source template which omits the
		// service account field but uses the default account at runtime.
		if stage.Spec.ServiceAccountName == "" {
			stage.Spec.ServiceAccountName = "default"
		}
		stage.Status.Phase = corev1.PodSucceeded
		f.stage = stage.DeepCopy()
		if err := client.Tracker().Create(corev1.SchemeGroupVersion.WithResource("pods"), stage, stage.Namespace); err != nil {
			return true, nil, err
		}
		return true, stage, nil
	})
	client.PrependReactor("update", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
		desired := action.(ktesting.UpdateAction).GetObject().(*appsv1.Deployment).DeepCopy()
		object, err := client.Tracker().Get(appsv1.SchemeGroupVersion.WithResource("deployments"), desired.Namespace, desired.Name)
		if err != nil {
			return true, nil, err
		}
		current := object.(*appsv1.Deployment)
		if f.forceConflict || current.ResourceVersion != desired.ResourceVersion {
			return true, nil, apierrors.NewConflict(schema.GroupResource{Group: "apps", Resource: "deployments"}, desired.Name, errors.New("resource version changed"))
		}
		version, _ := strconv.Atoi(current.ResourceVersion)
		desired.ResourceVersion = strconv.Itoa(version + 1)
		desired.Generation++
		if err := client.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), desired, desired.Namespace); err != nil {
			return true, nil, err
		}
		return true, desired, nil
	})
	config := Config{Kind: Kubernetes, StateDirectory: privateDirectory(t), ImageRepository: fixtureRepository, Kubernetes: &KubernetesConfig{Kubectl: "/usr/bin/kubectl", Kubeconfig: "/etc/hikyo/security-maintenance.kubeconfig", Context: "installed-hikyo", Namespace: "hikyo", Deployment: "hikyo", DeploymentUID: "deployment-1", Container: "server", StagePod: "hikyo-security-stage", Binary: "/usr/local/bin/hikyo"}}
	executor, err := NewWithDependencies(config, f, client)
	if err != nil {
		t.Fatal(err)
	}
	f.executor = executor
	return f
}

func (f *kubernetesFixture) Run(_ context.Context, command Command) ([]byte, error) {
	joined := strings.Join(command.Args, " ")
	if strings.Contains(joined, " exec ") {
		return encoded(f.t, f.current), nil
	}
	if strings.Contains(joined, " logs ") {
		staged := f.current
		staged.Ready = false
		staged.Phase = "staged"
		staged.StagedTarget = f.target.Identity
		staged.RouteSHA256 = fixtureIdentity(2).CompatibilitySHA256
		staged.RequiresMaintenance = true
		return encoded(f.t, staged), nil
	}
	f.t.Fatalf("unexpected kubectl command: %v", command.Args)
	return nil, nil
}

func (f *kubernetesFixture) deployment() *appsv1.Deployment {
	f.t.Helper()
	d, err := f.client.AppsV1().Deployments("hikyo").Get(f.t.Context(), "hikyo", metav1.GetOptions{})
	if err != nil {
		f.t.Fatal(err)
	}
	return d
}

func (f *kubernetesFixture) replace(runtimeSHA string) {
	f.t.Helper()
	d := f.deployment()
	d.Status.ObservedGeneration = d.Generation
	if err := f.client.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), d, d.Namespace); err != nil {
		f.t.Fatal(err)
	}
	pod, err := f.client.CoreV1().Pods("hikyo").Get(f.t.Context(), "hikyo-source-pod", metav1.GetOptions{})
	if err != nil {
		f.t.Fatal(err)
	}
	pod.Spec.Containers[0].Image = f.target.Image
	pod.Status.ContainerStatuses[0].ImageID = "containerd://sha256:" + runtimeSHA
	if _, err := f.client.CoreV1().Pods("hikyo").Update(f.t.Context(), pod, metav1.UpdateOptions{}); err != nil {
		f.t.Fatal(err)
	}
	f.current.Identity = f.target.Identity
	f.current.Generation++
}

func TestKubernetesStagesIsolatedPodAndReplacesExactImages(t *testing.T) {
	f := newKubernetesFixture(t)
	source := f.deployment()
	staged, err := f.executor.Prepare(t.Context(), f.target)
	if err != nil {
		t.Fatal(err)
	}
	if staged.StagedTarget != f.target.Identity || f.stage == nil {
		t.Fatal("target route was not staged")
	}
	if f.stage.Labels["app"] != "" || f.stage.Spec.ServiceAccountName != "default" || f.stage.Spec.NodeName != "node-1" || len(f.stage.Spec.Containers) != 1 || f.stage.Spec.Containers[0].Image != f.target.Image || f.stage.Spec.InitContainers[0].Image != f.target.Image || *f.stage.Spec.AutomountServiceAccountToken {
		t.Fatal("staging Pod escaped isolated enrolled source contract")
	}
	if f.deployment().Spec.Template.Spec.Containers[0].Image != source.Spec.Template.Spec.Containers[0].Image {
		t.Fatal("prepare replaced long-lived source")
	}
	if _, err := f.client.CoreV1().Pods("hikyo").Get(t.Context(), "hikyo-security-stage", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("owned stage Pod not removed: %v", err)
	}
	if err := f.executor.Apply(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	next := f.deployment()
	if next.Spec.Template.Spec.Containers[0].Image != f.target.Image || next.Spec.Template.Spec.InitContainers[0].Image != f.target.Image || next.Spec.Strategy.Type != appsv1.RecreateDeploymentStrategyType || *next.Spec.Replicas != 1 {
		t.Fatal("replacement did not preserve exact singleton target contract")
	}
	if _, err := f.executor.Observe(t.Context()); !errors.Is(err, ErrNotReady) {
		t.Fatalf("desired-state update falsely counted as running target: %v", err)
	}
	f.replace(imageDigest(f.target.Image))
	observation, err := f.executor.Observe(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if observation.Identity != f.target.Identity || observation.Image != f.target.Image || !observation.Ready {
		t.Fatalf("exact target not observed: %+v", observation)
	}
	j, err := f.executor.journal()
	if err != nil || j.Phase != "healthy" {
		t.Fatalf("actual target not committed: %+v %v", j, err)
	}
}

func TestKubernetesRejectsUIDAndDesiredStateConflicts(t *testing.T) {
	for _, change := range []string{"uid", "cas", "desired"} {
		t.Run(change, func(t *testing.T) {
			f := newKubernetesFixture(t)
			if _, err := f.executor.Prepare(t.Context(), f.target); err != nil {
				t.Fatal(err)
			}
			if change == "cas" {
				f.forceConflict = true
			} else {
				d := f.deployment()
				if change == "uid" {
					d.UID = "replacement-deployment"
				} else {
					d.ResourceVersion = "2"
					d.Spec.Template.Spec.ServiceAccountName = "operator-change"
				}
				if err := f.client.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), d, d.Namespace); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.executor.Apply(t.Context(), f.target); !errors.Is(err, ErrConflict) || !errors.Is(err, ErrPreconditionRefused) {
				t.Fatalf("conflicting deployment applied: %v", err)
			}
			if f.deployment().Spec.Template.Spec.Containers[0].Image == f.target.Image {
				t.Fatal("conflict overwrote source")
			}
			if change == "cas" {
				j, err := f.executor.journal()
				if err != nil || j.Phase != "prepared" {
					t.Fatalf("CAS refusal pinned a nonexistent mutation: %+v %v", j, err)
				}
				f.forceConflict = false
				if _, err := f.executor.Prepare(t.Context(), f.target); err != nil {
					t.Fatal("conflict prevented authenticated re-preparation", err)
				}
				if err := f.executor.Apply(t.Context(), f.target); err != nil {
					t.Fatal("definite CAS refusal prevented safe retry", err)
				}
			}
		})
	}
}

func TestKubernetesPolicyRevocationBeforeFirstPatchRemainsRetryable(t *testing.T) {
	for _, refuseAt := range []int{1, 2} {
		t.Run(strconv.Itoa(refuseAt), func(t *testing.T) {
			f := newKubernetesFixture(t)
			if _, err := f.executor.Prepare(t.Context(), f.target); err != nil {
				t.Fatal(err)
			}
			calls := 0
			target := f.target
			target.BeforeMutation = func() error {
				calls++
				if calls == refuseAt {
					return errors.New("local operator revoked automatic updates")
				}
				return nil
			}
			if err := f.executor.Apply(t.Context(), target); !errors.Is(err, ErrPreconditionRefused) {
				t.Fatal("revocation did not refuse mutation", err)
			}
			if f.deployment().Spec.Template.Spec.Containers[0].Image == target.Image {
				t.Fatal("revocation changed the workload")
			}
			j, err := f.executor.journal()
			if err != nil || j.Phase != "prepared" {
				t.Fatalf("pre-mutation refusal left a recovery obligation: %+v %v", j, err)
			}
			if err := f.executor.Apply(t.Context(), f.target); err != nil {
				t.Fatal("newly authorized retry failed", err)
			}
		})
	}
}

func TestKubernetesFinalPolicyCheckCannotCrossAdmissionDeadline(t *testing.T) {
	f := newKubernetesFixture(t)
	if _, err := f.executor.Prepare(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	calls := 0
	target := f.target
	target.BeforeMutation = func() error {
		calls++
		if calls == 2 {
			f.executor.now = func() time.Time { return target.EligibilityUntil }
		}
		return nil
	}
	if err := f.executor.Apply(t.Context(), target); !errors.Is(err, ErrEligibilityExpired) {
		t.Fatal("final authorization crossed window/metadata expiry but patched workload", err)
	}
	if f.deployment().Spec.Template.Spec.Containers[0].Image == target.Image {
		t.Fatal("expired callback changed workload")
	}
	j, err := f.executor.journal()
	if err != nil || j.Phase != "prepared" {
		t.Fatal("definite admission refusal was not retryable", err)
	}
}

func TestKubernetesWrongRuntimeDigestCannotBecomeHealthy(t *testing.T) {
	f := newKubernetesFixture(t)
	if _, err := f.executor.Prepare(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	if err := f.executor.Apply(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	f.replace(strings.Repeat("c", 64))
	if _, err := f.executor.Observe(t.Context()); !errors.Is(err, ErrConflict) {
		t.Fatalf("wrong running image accepted: %v", err)
	}
	j, err := f.executor.journal()
	if err != nil || j.Phase != "applying" {
		t.Fatal("mismatching runtime erased applying fence")
	}
}

func TestKubernetesMultiarchIndexNeedsVerifiedPlatformDigest(t *testing.T) {
	f := newKubernetesFixture(t)
	f.target.RuntimeImageSHA256 = strings.Repeat("c", 64)
	if _, err := f.executor.Prepare(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	if err := f.executor.Apply(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	f.replace(f.target.RuntimeImageSHA256)
	if o, err := f.executor.Observe(t.Context()); err != nil || !o.Ready {
		t.Fatalf("verified index/platform mapping refused: %+v %v", o, err)
	}
}

func TestKubernetesRefusesCompetingOwnersAndUnsupportedTopology(t *testing.T) {
	for _, change := range []string{"flux", "helm", "argocd", "replicas", "rolling", "token", "scratch", "persistent"} {
		t.Run(change, func(t *testing.T) {
			f := newKubernetesFixture(t)
			d := f.deployment()
			switch change {
			case "flux":
				d.Labels = map[string]string{"kustomize.toolkit.fluxcd.io/name": "another-owner"}
			case "helm":
				d.Annotations = map[string]string{"meta.helm.sh/release-name": "managed-release"}
			case "argocd":
				d.Annotations = map[string]string{"argocd.argoproj.io/tracking-id": "another-owner"}
			case "replicas":
				n := int32(2)
				d.Spec.Replicas = &n
			case "rolling":
				d.Spec.Strategy.Type = appsv1.RollingUpdateDeploymentStrategyType
			case "token":
				yes := true
				d.Spec.Template.Spec.AutomountServiceAccountToken = &yes
			case "scratch":
				d.Spec.Template.Spec.Containers[0].Env = d.Spec.Template.Spec.Containers[0].Env[:3]
			case "persistent":
				d.Spec.Template.Spec.Volumes = nil
			}
			if err := f.client.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), d, d.Namespace); err != nil {
				t.Fatal(err)
			}
			if _, err := f.executor.Prepare(t.Context(), f.target); !errors.Is(err, ErrUnsupported) {
				t.Fatalf("unsupported owner/topology admitted: %v", err)
			}
			if f.stage != nil {
				t.Fatal("unsupported deployment obtained staging authority")
			}
		})
	}
}

func TestKubernetesRefusesSQLiteHiddenInDatabaseSecret(t *testing.T) {
	f := newKubernetesFixture(t)
	f.current.Engine = releaseidentity.SQLite
	if _, err := f.executor.Prepare(t.Context(), f.target); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("SQLite secret incorrectly qualified as supported PostgreSQL: %v", err)
	}
	if f.stage != nil {
		t.Fatal("unsupported source obtained staging authority")
	}
}

func TestKubernetesStageAnnotationsCannotAuthorizeDifferentPodSpec(t *testing.T) {
	f := newKubernetesFixture(t)
	f.client.PrependReactor("create", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
		pod := action.(ktesting.CreateAction).GetObject().(*corev1.Pod).DeepCopy()
		pod.UID = "admission-mutated-stage"
		pod.Spec.Containers[0].Command = []string{"unapproved-command"}
		if err := f.client.Tracker().Create(corev1.SchemeGroupVersion.WithResource("pods"), pod, pod.Namespace); err != nil {
			return true, nil, err
		}
		return true, pod, nil
	})
	if _, err := f.executor.Prepare(t.Context(), f.target); !errors.Is(err, ErrConflict) {
		t.Fatalf("mutated stage pod admitted: %v", err)
	}
	if _, err := f.client.CoreV1().Pods("hikyo").Get(t.Context(), "hikyo-security-stage", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("owned mutated staging Pod not cleaned up: %v", err)
	}
}

func TestKubernetesExpiryAfterLongValidationCannotPatchDeployment(t *testing.T) {
	f := newKubernetesFixture(t)
	prepared := f.target
	prepared.EligibilityUntil = time.Time{}
	if _, err := f.executor.Prepare(t.Context(), prepared); err != nil {
		t.Fatal(err)
	}
	gets := 0
	f.client.PrependReactor("get", "deployments", func(ktesting.Action) (bool, runtime.Object, error) {
		gets++
		if gets >= 2 {
			f.executor.now = func() time.Time { return f.target.EligibilityUntil }
		}
		return false, nil, nil
	})
	actions := len(f.client.Actions())
	if err := f.executor.Apply(t.Context(), f.target); !errors.Is(err, ErrEligibilityExpired) {
		t.Fatalf("expired eligibility patched desired state: %v", err)
	}
	for _, action := range f.client.Actions()[actions:] {
		if action.GetVerb() == "update" && action.GetResource().Resource == "deployments" {
			t.Fatal("expired metadata changed deployment")
		}
	}
	if f.deployment().Spec.Template.Spec.Containers[0].Image == f.target.Image {
		t.Fatal("expiry replaced source image")
	}
	j, err := f.executor.journal()
	if err != nil || j.Phase != "prepared" {
		t.Fatalf("expiry incorrectly retained applying state: %+v %v", j, err)
	}
}

func TestKubernetesRepreparesSameExactRouteBeforeApply(t *testing.T) {
	f := newKubernetesFixture(t)
	prepared := f.target
	prepared.EligibilityUntil = time.Time{}
	first, err := f.executor.Prepare(t.Context(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.executor.Prepare(t.Context(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	if first.RouteSHA256 != second.RouteSHA256 || !sameSource(first, second) {
		t.Fatal("re-preparation changed exact source/route")
	}
	if err := f.executor.Apply(t.Context(), f.target); err != nil {
		t.Fatalf("deadline refresh changed immutable target: %v", err)
	}
}
