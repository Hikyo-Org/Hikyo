package securitydeployment

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/multicall"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	appsv1client "k8s.io/client-go/kubernetes/typed/apps/v1"
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
	// command is the subcommand of the last one-shot Pod. keepPods stops the
	// simulated controller from removing Pods at zero replicas, failPod fails the
	// rollback one-shot, and updates records every Deployment write.
	command           string
	keepPods, failPod bool
	updates           []*appsv1.Deployment
	// inspectRow is what an `inspect` one-shot prints, behind logLine; nil fails the Pod.
	inspectRow *Observation
	logLine    string
	stageArgs  []string
	// conflicts fails that many Deployment writes; finalized keeps a deleted stage Pod for that many reads.
	conflicts, finalized int
	// admit is the API server's admission of a stage Pod, which may add to its spec.
	admit func(*corev1.Pod)
}

func owner(kind, name string, uid types.UID) []metav1.OwnerReference {
	yes := true
	return []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: kind, Name: name, UID: uid, Controller: &yes}}
}

func newKubernetesFixture(t *testing.T) *kubernetesFixture {
	t.Helper()
	one, no, yes := int32(1), false, true
	image := fixtureRepository + "@sha256:" + strings.Repeat("a", 64)
	labels := map[string]string{"app": "hikyo"}
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "hikyo", Namespace: "hikyo", UID: "deployment-1", ResourceVersion: "1", Generation: 1}, Spec: appsv1.DeploymentSpec{Replicas: &one, Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}, Selector: &metav1.LabelSelector{MatchLabels: labels}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels}, Spec: corev1.PodSpec{AutomountServiceAccountToken: &no, Containers: []corev1.Container{{Name: "server", Image: image, Args: []string{"server", "--root-key-file=/run/hikyo-root/root-key"}, Env: []corev1.EnvVar{{Name: "HIKYO_UPGRADE_UNATTENDED", Value: "true"}, {Name: "HIKYO_UPGRADE_STATE_DIR", Value: "/var/lib/hikyo-upgrade/private"}, {Name: "HIKYO_DB", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "production-database"}, Key: "HIKYO_DB"}}}, {Name: "HIKYO_UPGRADE_SCRATCH_POSTGRES_DSN", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "scratch-database"}, Key: "HIKYO_UPGRADE_SCRATCH_POSTGRES_DSN"}}}, {Name: "HIKYO_SECURITY_MAINTENANCE_STATUS_FILE", Value: "/run/hikyo-security-status/status.json"}}, VolumeMounts: []corev1.VolumeMount{{Name: "upgrade-state", MountPath: "/var/lib/hikyo-upgrade"}, {Name: "security-status", MountPath: "/run/hikyo-security-status", ReadOnly: true}}}}, InitContainers: []corev1.Container{{Name: "root-key-stage", Image: image, Args: []string{"__hikyo-stage-root-key"}}}, Volumes: []corev1.Volume{{Name: "upgrade-state", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "upgrade-state"}}}, {Name: "security-status", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "hikyo-security-status"}, Optional: &yes}}}}}}}, Status: appsv1.DeploymentStatus{ObservedGeneration: 1, Replicas: 1, UpdatedReplicas: 1, ReadyReplicas: 1, AvailableReplicas: 1}}
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
		if f.admit != nil {
			f.admit(stage)
		}
		stage.Status.Phase = corev1.PodSucceeded
		f.command = stage.Spec.Containers[0].Args[1]
		f.stageArgs = stage.Spec.Containers[0].Args
		if f.command == "rollback" && f.failPod || f.command == "inspect" && f.inspectRow == nil {
			stage.Status.Phase = corev1.PodFailed
		}
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
		if f.conflicts > 0 {
			f.conflicts--
			return true, nil, apierrors.NewConflict(schema.GroupResource{Group: "apps", Resource: "deployments"}, desired.Name, errors.New("resource version changed"))
		}
		if f.forceConflict || current.ResourceVersion != desired.ResourceVersion {
			return true, nil, apierrors.NewConflict(schema.GroupResource{Group: "apps", Resource: "deployments"}, desired.Name, errors.New("resource version changed"))
		}
		version, _ := strconv.Atoi(current.ResourceVersion)
		desired.ResourceVersion = strconv.Itoa(version + 1)
		desired.Generation++
		f.updates = append(f.updates, desired.DeepCopy())
		if desired.Spec.Replicas != nil && *desired.Spec.Replicas == 0 && !f.keepPods {
			// The controller scales the old Pod away and reports it.
			desired.Status.ObservedGeneration, desired.Status.Replicas, desired.Status.ReadyReplicas, desired.Status.AvailableReplicas, desired.Status.UpdatedReplicas = desired.Generation, 0, 0, 0, 0
			if err := client.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("pods"), "hikyo", "hikyo-source-pod"); err != nil && !apierrors.IsNotFound(err) {
				return true, nil, err
			}
		}
		if err := client.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), desired, desired.Namespace); err != nil {
			return true, nil, err
		}
		return true, desired, nil
	})
	client.PrependReactor("delete", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
		name := action.(ktesting.DeleteAction).GetName()
		if name != "hikyo-security-stage" || f.finalized == 0 {
			return false, nil, nil
		}
		object, err := client.Tracker().Get(corev1.SchemeGroupVersion.WithResource("pods"), "hikyo", name)
		if err != nil {
			return true, nil, err
		}
		pod := object.(*corev1.Pod).DeepCopy()
		now := metav1.Now()
		pod.DeletionTimestamp = &now
		return true, nil, client.Tracker().Update(corev1.SchemeGroupVersion.WithResource("pods"), pod, "hikyo")
	})
	client.PrependReactor("get", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
		name := action.(ktesting.GetAction).GetName()
		object, err := client.Tracker().Get(corev1.SchemeGroupVersion.WithResource("pods"), "hikyo", name)
		if name != "hikyo-security-stage" || err != nil || object.(*corev1.Pod).DeletionTimestamp == nil {
			return false, nil, nil
		}
		if f.finalized--; f.finalized == 0 {
			return true, nil, client.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("pods"), "hikyo", name)
		}
		return true, object, nil
	})
	config := Config{Kind: Kubernetes, StateDirectory: privateDirectory(t), ImageRepository: fixtureRepository, Kubernetes: &KubernetesConfig{Kubeconfig: "/etc/hikyo/security-maintenance.kubeconfig", Context: "installed-hikyo", Namespace: "hikyo", Deployment: "hikyo", DeploymentUID: "deployment-1", Container: "server", StagePod: "hikyo-security-stage", Binary: "/usr/local/bin/hikyo"}}
	executor, err := NewWithDependencies(config, CommandRunner{}, client)
	if err != nil {
		t.Fatal(err)
	}
	executor.pods = f
	f.executor = executor
	return f
}

func (f *kubernetesFixture) Exec(_ context.Context, _, _ string, command []string) ([]byte, error) {
	if strings.Join(command, " ") != "/usr/local/bin/hikyo security-maintenance inspect" {
		f.t.Fatalf("unexpected exec command: %v", command)
	}
	return encoded(f.t, f.current), nil
}

func (f *kubernetesFixture) Logs(context.Context, string, string) ([]byte, error) {
	if f.command == "inspect" {
		return append([]byte(f.logLine), encoded(f.t, *f.inspectRow)...), nil
	}
	if f.command == "rollback" {
		return encoded(f.t, RollbackReport{Format: RollbackReportFormat, Result: "restored", Reason: "test"}), nil
	}
	staged := f.current
	staged.Ready = false
	staged.Phase = "staged"
	staged.StagedTarget = f.target.Identity
	staged.RouteSHA256 = fixtureIdentity(2).CompatibilitySHA256
	staged.RequiresMaintenance = true
	return append([]byte(f.logLine), encoded(f.t, staged)...), nil
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

// Containerd reports whichever repo digest it stored first: kind v1.36.1 gave
// the index digest for an index pull, and the child digest once the child was cached.
func TestKubernetesEitherSignedDigestProvesMultiarchTarget(t *testing.T) {
	for name, reported := range map[string]string{"index": strings.Repeat("b", 64), "child": strings.Repeat("c", 64), "foreign": strings.Repeat("d", 64)} {
		t.Run(name, func(t *testing.T) {
			f := newKubernetesFixture(t)
			f.target.RuntimeImageSHA256 = strings.Repeat("c", 64)
			if _, err := f.executor.Prepare(t.Context(), f.target); err != nil {
				t.Fatal(err)
			}
			if err := f.executor.Apply(t.Context(), f.target); err != nil {
				t.Fatal(err)
			}
			f.replace(reported)
			o, err := f.executor.Observe(t.Context())
			if name == "foreign" {
				if !errors.Is(err, ErrConflict) {
					t.Fatalf("unsigned digest accepted: %v", err)
				}
				return
			}
			if err != nil || !o.Ready {
				t.Fatalf("signed digest refused: %+v %v", o, err)
			}
		})
	}
}

func TestKubernetesDefiniteUpdateRefusalStaysRetryable(t *testing.T) {
	deployments := schema.GroupResource{Group: "apps", Resource: "deployments"}
	for name, refusal := range map[string]error{
		"rbac":    apierrors.NewForbidden(deployments, "hikyo", errors.New("not allowed")),
		"expired": apierrors.NewUnauthorized("token expired"),
		"policy":  apierrors.NewInvalid(schema.GroupKind{Group: "apps", Kind: "Deployment"}, "hikyo", field.ErrorList{field.Forbidden(field.NewPath("spec"), "denied")}),
		"webhook": apierrors.NewBadRequest(`admission webhook "x" denied the request`),
		"deleted": apierrors.NewNotFound(deployments, "hikyo"),
	} {
		t.Run(name, func(t *testing.T) {
			f := newKubernetesFixture(t)
			if _, err := f.executor.Prepare(t.Context(), f.target); err != nil {
				t.Fatal(err)
			}
			failure := refusal
			f.client.PrependReactor("update", "deployments", func(ktesting.Action) (bool, runtime.Object, error) {
				return failure != nil, nil, failure
			})
			if err := f.executor.Apply(t.Context(), f.target); !errors.Is(err, ErrPreconditionRefused) {
				t.Fatalf("definite refusal not retryable: %v", err)
			}
			j, err := f.executor.journal()
			if err != nil || j.Phase != "prepared" {
				t.Fatalf("refusal pinned a nonexistent mutation: %+v %v", j, err)
			}
			failure = nil
			if err := f.executor.Apply(t.Context(), f.target); err != nil {
				t.Fatal("definite refusal prevented retry", err)
			}
		})
	}
}

func TestKubernetesUncertainUpdateKeepsApplyingFence(t *testing.T) {
	deployments := schema.GroupResource{Group: "apps", Resource: "deployments"}
	for name, failure := range map[string]error{
		"server timeout": apierrors.NewServerTimeout(deployments, "update", 1),
		"gateway":        apierrors.NewTimeoutError("no reply", 1),
		"internal":       apierrors.NewInternalError(errors.New("failed after storage")),
		"unavailable":    apierrors.NewServiceUnavailable("restarting"),
		"lost reply":     context.DeadlineExceeded,
	} {
		t.Run(name, func(t *testing.T) {
			f := newKubernetesFixture(t)
			if _, err := f.executor.Prepare(t.Context(), f.target); err != nil {
				t.Fatal(err)
			}
			f.client.PrependReactor("update", "deployments", func(ktesting.Action) (bool, runtime.Object, error) {
				return true, nil, failure
			})
			if err := f.executor.Apply(t.Context(), f.target); err == nil || errors.Is(err, ErrPreconditionRefused) {
				t.Fatalf("uncertain update reported as refusal: %v", err)
			}
			j, err := f.executor.journal()
			if err != nil || j.Phase != "applying" {
				t.Fatalf("uncertain update erased fence: %+v %v", j, err)
			}
		})
	}
}

func TestKubernetesAcceptsOnlyOptedInHelmRelease(t *testing.T) {
	f := newKubernetesFixture(t)
	d := f.deployment()
	d.Labels = map[string]string{helmOwnerOptIn: "helm", "app.kubernetes.io/managed-by": "Helm", "app.kubernetes.io/instance": "hikyo"}
	d.Annotations = map[string]string{"meta.helm.sh/release-name": "hikyo", "meta.helm.sh/release-namespace": "hikyo"}
	if err := f.client.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), d, d.Namespace); err != nil {
		t.Fatal(err)
	}
	if _, err := f.executor.Prepare(t.Context(), f.target); err != nil {
		t.Fatalf("opted-in Helm release refused: %v", err)
	}
}

func TestKubernetesRefusesCompetingOwnersAndUnsupportedTopology(t *testing.T) {
	for _, change := range []string{"flux", "flux-opt-in", "flux-helm-opt-in", "helm", "helm-other-label", "opt-in-rendered-only", "argocd", "argocd-opt-in", "argocd-label-tracking", "replicas", "rolling", "token", "scratch", "persistent", "init-writes-claim", "init-writes-claim-explicitly", "init-attaches-claim-device", "init-unknown", "init-hikyo-migrate", "init-stager-other-image", "init-stager-command", "envfrom-could-enable-ha", "unattended-single-dash", "unattended-zero", "unattended-capital-false", "auto-migrate-f", "wrapper-command", "state-dir-shadowed-by-scratch", "state-claim-read-only"} {
		t.Run(change, func(t *testing.T) {
			f := newKubernetesFixture(t)
			d := f.deployment()
			switch change {
			case "flux":
				d.Labels = map[string]string{"kustomize.toolkit.fluxcd.io/name": "another-owner"}
			case "flux-opt-in":
				d.Labels = map[string]string{helmOwnerOptIn: "helm", "kustomize.toolkit.fluxcd.io/name": "another-owner"}
				d.Annotations = map[string]string{"meta.helm.sh/release-name": "r", "meta.helm.sh/release-namespace": "hikyo"}
			case "flux-helm-opt-in":
				d.Labels = map[string]string{helmOwnerOptIn: "helm", "helm.toolkit.fluxcd.io/name": "another-owner"}
				d.Annotations = map[string]string{"meta.helm.sh/release-name": "r", "meta.helm.sh/release-namespace": "hikyo"}
			case "helm":
				d.Annotations = map[string]string{"meta.helm.sh/release-name": "managed-release"}
			case "helm-other-label":
				d.Labels = map[string]string{helmOwnerOptIn: "true"}
				d.Annotations = map[string]string{"meta.helm.sh/release-name": "r", "meta.helm.sh/release-namespace": "hikyo"}
			case "opt-in-rendered-only":
				d.Labels = map[string]string{helmOwnerOptIn: "helm"}
			case "argocd":
				d.Annotations = map[string]string{"argocd.argoproj.io/tracking-id": "another-owner"}
			case "argocd-opt-in":
				d.Labels = map[string]string{helmOwnerOptIn: "helm"}
				d.Annotations = map[string]string{"meta.helm.sh/release-name": "r", "meta.helm.sh/release-namespace": "hikyo", "argocd.argoproj.io/tracking-id": "another-owner"}
			case "argocd-label-tracking":
				// Argo CD's label tracking adds only this label; Helm would add its annotations too.
				d.Labels = map[string]string{"app.kubernetes.io/instance": "hikyo"}
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
			case "init-writes-claim":
				// The stage Pod would run it again while the source serves.
				d.Spec.Template.Spec.InitContainers[0].VolumeMounts = []corev1.VolumeMount{{Name: "upgrade-state", MountPath: "/var/lib/hikyo-upgrade"}}
			case "init-attaches-claim-device":
				d.Spec.Template.Spec.InitContainers[0].VolumeDevices = []corev1.VolumeDevice{{Name: "upgrade-state", DevicePath: "/dev/hikyo-state"}}
			case "init-unknown":
				// It holds the production credentials and would run again while the source serves.
				d.Spec.Template.Spec.InitContainers = append(d.Spec.Template.Spec.InitContainers, corev1.Container{Name: "wait-for-db", Image: "busybox", Env: d.Spec.Template.Spec.Containers[0].Env})
			case "init-hikyo-migrate":
				d.Spec.Template.Spec.InitContainers = append(d.Spec.Template.Spec.InitContainers, corev1.Container{Name: "migrate", Image: d.Spec.Template.Spec.Containers[0].Image, Args: []string{"migrate"}, Env: d.Spec.Template.Spec.Containers[0].Env})
			case "init-stager-other-image":
				d.Spec.Template.Spec.InitContainers[0].Image = "registry.example/other@sha256:" + strings.Repeat("e", 64)
			case "init-stager-command":
				d.Spec.Template.Spec.InitContainers[0].Command = []string{"/bin/sh", "-c"}
			case "envfrom-could-enable-ha":
				d.Spec.Template.Spec.Containers[0].EnvFrom = []corev1.EnvFromSource{{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "settings"}}}}
			case "unattended-single-dash":
				d.Spec.Template.Spec.Containers[0].Args = append(d.Spec.Template.Spec.Containers[0].Args, "-upgrade-unattended=false")
			case "unattended-zero":
				d.Spec.Template.Spec.Containers[0].Args = append(d.Spec.Template.Spec.Containers[0].Args, "--upgrade-unattended=0")
			case "unattended-capital-false":
				d.Spec.Template.Spec.Containers[0].Args = append(d.Spec.Template.Spec.Containers[0].Args, "--upgrade-unattended=FALSE")
			case "auto-migrate-f":
				d.Spec.Template.Spec.Containers[0].Args = append(d.Spec.Template.Spec.Containers[0].Args, "-auto-migrate=f")
			case "wrapper-command":
				d.Spec.Template.Spec.Containers[0].Command = []string{"/bin/sh", "-c", "exec hikyo server --upgrade-unattended=false"}
			case "state-dir-shadowed-by-scratch":
				// The claim covers /var/lib/hikyo-upgrade, but the state directory itself is an emptyDir.
				d.Spec.Template.Spec.Volumes = append(d.Spec.Template.Spec.Volumes, corev1.Volume{Name: "scratch", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}})
				d.Spec.Template.Spec.Containers[0].VolumeMounts = append(d.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{Name: "scratch", MountPath: "/var/lib/hikyo-upgrade/private"})
			case "state-claim-read-only":
				d.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ReadOnly = true
			case "init-writes-claim-explicitly":
				d.Spec.Template.Spec.InitContainers = append(d.Spec.Template.Spec.InitContainers, corev1.Container{Name: "chown", Image: "busybox", VolumeMounts: []corev1.VolumeMount{{Name: "upgrade-state", MountPath: "/data", ReadOnly: false}}})
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

// Init containers that only read the claim or write scratch volumes, as the chart's do, stay supported.
func TestKubernetesAcceptsInitContainersThatCannotWriteTheClaim(t *testing.T) {
	f := newKubernetesFixture(t)
	d := f.deployment()
	spec := &d.Spec.Template.Spec
	spec.Volumes = append(spec.Volumes, corev1.Volume{Name: "root-key", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory}}}, corev1.Volume{Name: "root-key-source", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "root-key"}}})
	spec.InitContainers[0].VolumeMounts = []corev1.VolumeMount{{Name: "root-key-source", MountPath: "/run/hikyo-root-key-source", ReadOnly: true}, {Name: "root-key", MountPath: "/run/hikyo-root-key"}, {Name: "upgrade-state", MountPath: "/var/lib/hikyo-upgrade", ReadOnly: true}}
	if err := f.client.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), d, d.Namespace); err != nil {
		t.Fatal(err)
	}
	if _, err := f.executor.Prepare(t.Context(), f.target); err != nil {
		t.Fatalf("read-only and scratch init mounts refused: %v", err)
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

// client-go can retry a PUT whose first attempt committed. The retry's conflict must
// not read as a refusal, or the coordinator drops its pin during a real replacement.
func TestKubernetesRefusalNeedsAReadProvingNothingChanged(t *testing.T) {
	forbidden := apierrors.NewForbidden(schema.GroupResource{Group: "apps", Resource: "deployments"}, "hikyo", errors.New("denied"))
	// Each change happens as the write's reply arrives. It goes through the tracker,
	// because the fake client's lock is held while a reactor runs.
	for name, change := range map[string]func(*appsv1.Deployment, Target){
		"read failed": nil,
		"annotation only": func(d *appsv1.Deployment, target Target) {
			d.Spec.Template.Annotations = map[string]string{replacementTarget: string(target.Identity.ManifestSHA256)}
		},
		"image only":       func(d *appsv1.Deployment, target Target) { d.Spec.Template.Spec.Containers[0].Image = target.Image },
		"generation moved": func(d *appsv1.Deployment, _ Target) { d.Generation++ },
	} {
		t.Run(name, func(t *testing.T) {
			f := newKubernetesFixture(t)
			if _, err := f.executor.Prepare(t.Context(), f.target); err != nil {
				t.Fatal(err)
			}
			f.target.EligibilityUntil = f.executor.now().Add(time.Hour)
			written := false
			f.client.PrependReactor("get", "deployments", func(ktesting.Action) (bool, runtime.Object, error) {
				if written && change == nil {
					return true, nil, errors.New("unreachable")
				}
				return false, nil, nil
			})
			f.client.PrependReactor("update", "deployments", func(ktesting.Action) (bool, runtime.Object, error) {
				written = true
				if change != nil {
					resource := appsv1.SchemeGroupVersion.WithResource("deployments")
					object, err := f.client.Tracker().Get(resource, "hikyo", "hikyo")
					if err != nil {
						t.Fatal(err)
					}
					d := object.(*appsv1.Deployment).DeepCopy()
					change(d, f.target)
					if err := f.client.Tracker().Update(resource, d, "hikyo"); err != nil {
						t.Fatal(err)
					}
				}
				return true, nil, forbidden
			})
			err := f.executor.Apply(t.Context(), f.target)
			if err == nil || errors.Is(err, ErrPreconditionRefused) {
				t.Fatalf("unproven refusal: %v", err)
			}
			if j, err := f.executor.journal(); err != nil || j == nil || j.Phase != "applying" {
				t.Fatalf("pin released: %+v %v", j, err)
			}
		})
	}
}

func TestKubernetesConflictAfterCommittedWriteStaysApplying(t *testing.T) {
	conflict := apierrors.NewConflict(schema.GroupResource{Group: "apps", Resource: "deployments"}, "hikyo", errors.New("retried"))
	forbidden := apierrors.NewForbidden(schema.GroupResource{Group: "apps", Resource: "deployments"}, "hikyo", errors.New("retried"))
	for name, tc := range map[string]struct {
		commit bool
		reply  error
	}{"committed conflict": {true, conflict}, "refused conflict": {false, conflict}, "committed forbidden": {true, forbidden}, "refused forbidden": {false, forbidden}} {
		commit := tc.commit
		t.Run(name, func(t *testing.T) {
			f := newKubernetesFixture(t)
			if _, err := f.executor.Prepare(t.Context(), f.target); err != nil {
				t.Fatal(err)
			}
			f.target.EligibilityUntil = f.executor.now().Add(time.Hour)
			f.client.PrependReactor("update", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
				if commit {
					update, ok := action.(ktesting.UpdateAction)
					if !ok {
						t.Fatal("unexpected action")
					}
					if err := f.client.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), update.GetObject(), "hikyo"); err != nil {
						t.Fatal(err)
					}
				}
				return true, nil, tc.reply
			})
			err := f.executor.Apply(t.Context(), f.target)
			if err == nil || errors.Is(err, ErrPreconditionRefused) != !commit {
				t.Fatalf("conflict classified wrongly: %v", err)
			}
			j, err := f.executor.journal()
			want := "prepared"
			if commit {
				want = "applying"
			}
			if err != nil || j == nil || j.Phase != want {
				t.Fatalf("journal phase: %+v %v, want %s", j, err, want)
			}
		})
	}
}

func TestKubernetesRestoresTheSourceOnlyOverTheStateApplyWrote(t *testing.T) {
	f := newKubernetesFixture(t)
	sourceImage := f.deployment().Spec.Template.Spec.Containers[0].Image
	aborted := f.current
	aborted.AbortedBeforeMutation = true
	if _, err := f.executor.Prepare(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	// A prepared journal has replaced nothing, so there is nothing to restore.
	if err := f.executor.RestoreSource(t.Context(), aborted); !errors.Is(err, ErrConflict) {
		t.Fatalf("restored a source that was never replaced: %v", err)
	}
	if err := f.executor.Apply(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	writes := 0
	f.client.PrependReactor("update", "deployments", func(ktesting.Action) (bool, runtime.Object, error) {
		writes++
		return false, nil, nil
	})
	for name, change := range map[string]func(*Observation){
		"without the target's report": func(o *Observation) { o.AbortedBeforeMutation = false },
		"another generation":          func(o *Observation) { o.Generation++ },
		"another release":             func(o *Observation) { o.Identity = fixtureIdentity(3) },
		"another installation":        func(o *Observation) { o.Instance = "other" },
		"another recovery":            func(o *Observation) { o.Incarnation = "recovery-incarnation-2" },
	} {
		observed := aborted
		change(&observed)
		if err := f.executor.RestoreSource(t.Context(), observed); !errors.Is(err, ErrConflict) || writes != 0 {
			t.Fatalf("%s: %v", name, err)
		}
	}
	// Another writer's change to the Deployment is never written over.
	other := f.deployment()
	other.Spec.Template.Spec.Containers[0].Args = append(other.Spec.Template.Spec.Containers[0].Args, "--other")
	if err := f.client.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), other, "hikyo"); err != nil {
		t.Fatal(err)
	}
	if err := f.executor.RestoreSource(t.Context(), aborted); !errors.Is(err, ErrConflict) || writes != 0 {
		t.Fatalf("overwrote another writer: %v", err)
	}
	other.Spec.Template.Spec.Containers[0].Args = other.Spec.Template.Spec.Containers[0].Args[:2]
	if err := f.client.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), other, "hikyo"); err != nil {
		t.Fatal(err)
	}
	ecs := *f.executor
	ecs.c.Kind = ECS
	if err := ecs.RestoreSource(t.Context(), aborted); !errors.Is(err, ErrUnsupported) || writes != 0 {
		t.Fatalf("an adapter without a restore path ran one: %v", err)
	}
	if err := f.executor.RestoreSource(t.Context(), aborted); err != nil {
		t.Fatal(err)
	}
	restored := f.deployment()
	if restored.Spec.Template.Spec.Containers[0].Image != sourceImage || restored.Spec.Template.Spec.InitContainers[0].Image != sourceImage || restored.Spec.Template.Annotations[replacementTarget] != "" || writes != 1 {
		t.Fatalf("the source did not return: %+v writes=%d", restored.Spec.Template, writes)
	}
	// Repeating it, as after a lost reply, writes nothing more.
	if err := f.executor.RestoreSource(t.Context(), aborted); err != nil || writes != 1 {
		t.Fatalf("a repeat wrote again: %v writes=%d", err, writes)
	}
	// The pin goes only when the source itself is observed healthy again.
	if j, err := f.executor.journal(); err != nil || j == nil || j.Phase != "applying" {
		t.Fatalf("a restore released the pin on its own: %+v %v", j, err)
	}
	if _, err := f.executor.Observe(t.Context()); err != nil {
		t.Fatal(err)
	}
	observed := f.current
	if err := f.executor.Reconcile(t.Context(), observed); err != nil {
		t.Fatal(err)
	}
	if j, err := f.executor.journal(); err != nil || j != nil {
		t.Fatalf("the healthy source kept the pin: %+v %v", j, err)
	}
}

func TestKubernetesRestoreTrustsOnlyAReadAfterAFailedWrite(t *testing.T) {
	deployments := schema.GroupResource{Group: "apps", Resource: "deployments"}
	forbidden := apierrors.NewForbidden(deployments, "hikyo", errors.New("denied"))
	conflict := apierrors.NewConflict(deployments, "hikyo", errors.New("changed"))
	for name, tc := range map[string]struct {
		reply   error
		commit  bool
		repeats bool
		want    bool
	}{
		"refused":                {reply: forbidden},
		"committed then refused": {reply: forbidden, commit: true, want: true},
		"conflict then success":  {reply: conflict, repeats: false, want: true},
		"conflict forever":       {reply: conflict, repeats: true},
	} {
		t.Run(name, func(t *testing.T) {
			f := newKubernetesFixture(t)
			aborted := f.current
			aborted.AbortedBeforeMutation = true
			if _, err := f.executor.Prepare(t.Context(), f.target); err != nil {
				t.Fatal(err)
			}
			if err := f.executor.Apply(t.Context(), f.target); err != nil {
				t.Fatal(err)
			}
			attempts := 0
			f.client.PrependReactor("update", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
				attempts++
				if tc.commit {
					if err := f.client.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), action.(ktesting.UpdateAction).GetObject(), "hikyo"); err != nil {
						t.Fatal(err)
					}
				}
				if attempts == 1 || tc.repeats || !tc.want {
					return true, nil, tc.reply
				}
				return false, nil, nil
			})
			err := f.executor.RestoreSource(t.Context(), aborted)
			if (err == nil) != tc.want {
				t.Fatalf("got %v, want success %v", err, tc.want)
			}
			if restored := f.deployment().Spec.Template.Annotations[replacementTarget] == ""; restored != tc.want {
				t.Fatalf("Deployment restored=%v, want %v", restored, tc.want)
			}
		})
	}
}

func TestKubernetesConfigAcceptsKubeconfigOrInClusterOnly(t *testing.T) {
	state := privateDirectory(t)
	base := func() KubernetesConfig {
		return KubernetesConfig{Namespace: "hikyo", Deployment: "hikyo", Container: "server", StagePod: "hikyo-security-stage", Binary: "/usr/local/bin/hikyo"}
	}
	for name, tc := range map[string]struct {
		change func(*KubernetesConfig)
		ok     bool
	}{
		"in cluster, no UID": {func(*KubernetesConfig) {}, true},
		"in cluster, named objects, volume root": {func(p *KubernetesConfig) {
			p.CronJob, p.StatusConfigMap, p.VolumeRoot = "coordinator", "status", filepath.Dir(state)
		}, true},
		"kubeconfig":                 {func(p *KubernetesConfig) { p.Kubeconfig, p.Context = "/etc/hikyo/kube", "installed" }, true},
		"kubeconfig without context": {func(p *KubernetesConfig) { p.Kubeconfig = "/etc/hikyo/kube" }, false},
		"context without kubeconfig": {func(p *KubernetesConfig) { p.Context = "installed" }, false},
		"relative binary":            {func(p *KubernetesConfig) { p.Binary = "hikyo" }, false},
		"status ConfigMap with kubeconfig": {func(p *KubernetesConfig) {
			p.Kubeconfig, p.Context, p.StatusConfigMap = "/etc/hikyo/kube", "installed", "status"
		}, false},
		"unsafe UID":                {func(p *KubernetesConfig) { p.DeploymentUID = "../uid" }, false},
		"unsafe cron job":           {func(p *KubernetesConfig) { p.CronJob = "a/b" }, false},
		"volume root off the state": {func(p *KubernetesConfig) { p.VolumeRoot = "/var/lib/elsewhere" }, false},
		"volume root is the state":  {func(p *KubernetesConfig) { p.VolumeRoot = state }, false},
		"volume root with kubeconfig": {func(p *KubernetesConfig) {
			p.Kubeconfig, p.Context, p.VolumeRoot = "/etc/hikyo/kube", "installed", filepath.Dir(state)
		}, false},
		"world-writable volume root accepted in cluster": {func(p *KubernetesConfig) {
			p.VolumeRoot, p.AcceptWorldWritableVolumeRoot = filepath.Dir(state), true
		}, true},
		"world-writable acceptance without a volume root": {func(p *KubernetesConfig) { p.AcceptWorldWritableVolumeRoot = true }, false},
		"world-writable acceptance with kubeconfig": {func(p *KubernetesConfig) {
			p.Kubeconfig, p.Context, p.AcceptWorldWritableVolumeRoot = "/etc/hikyo/kube", "installed", true
		}, false},
	} {
		t.Run(name, func(t *testing.T) {
			p := base()
			tc.change(&p)
			err := Validate(Config{Kind: Kubernetes, StateDirectory: state, ImageRepository: fixtureRepository, Kubernetes: &p})
			if (err == nil) != tc.ok {
				t.Fatalf("Validate = %v, want ok=%v", err, tc.ok)
			}
		})
	}
}

// A volume root any local user can write, as a local-path provisioner leaves it, needs the operator's acceptance.
func TestKubernetesWorldWritableVolumeRootNeedsAcceptance(t *testing.T) {
	volume := privateDirectory(t)
	state := filepath.Join(volume, "state")
	if err := os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(volume, 0777); err != nil {
		t.Fatal(err)
	}
	for _, accept := range []bool{false, true} {
		p := KubernetesConfig{Namespace: "hikyo", Deployment: "hikyo", Container: "server", StagePod: "hikyo-security-stage", Binary: "/usr/local/bin/hikyo", VolumeRoot: volume, AcceptWorldWritableVolumeRoot: accept}
		_, err := NewWithDependencies(Config{Kind: Kubernetes, StateDirectory: state, ImageRepository: fixtureRepository, Kubernetes: &p}, CommandRunner{}, fake.NewClientset())
		if (err == nil) != accept {
			t.Fatalf("accept=%v: %v", accept, err)
		}
	}
}

// Observation may run before enrollment so that an unenrolled coordinator can say
// so; every mutation needs the pinned identity.
func TestKubernetesEnrollmentPinsDeploymentIdentity(t *testing.T) {
	f := newKubernetesFixture(t)
	f.executor.c.Kubernetes.DeploymentUID = ""
	if _, err := f.executor.Observe(t.Context()); err != nil {
		t.Fatalf("unenrolled observation refused: %v", err)
	}
	if _, err := f.executor.Prepare(t.Context(), f.target); !errors.Is(err, ErrConflict) || f.stage != nil {
		t.Fatalf("unpinned installation was prepared: %v", err)
	}
	if err := f.executor.PinIdentity(t.Context()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.executor.c.StateDirectory, identityFile)
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("identity not private: %v %v", info, err)
	}
	if err := f.executor.PinIdentity(t.Context()); err != nil {
		t.Fatalf("repeating enrollment for the same Deployment failed: %v", err)
	}
	if _, err := f.executor.Prepare(t.Context(), f.target); err != nil {
		t.Fatalf("pinned installation refused: %v", err)
	}
	replace := func(uid types.UID) {
		d := f.deployment()
		d.UID = uid
		if err := f.client.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), d, d.Namespace); err != nil {
			t.Fatal(err)
		}
	}
	replace("replacement-deployment")
	if _, err := f.executor.Observe(t.Context()); !errors.Is(err, ErrConflict) {
		t.Fatalf("replaced Deployment observed as the enrolled one: %v", err)
	}
	if _, err := f.executor.Prepare(t.Context(), f.target); !errors.Is(err, ErrConflict) {
		t.Fatalf("replaced Deployment prepared: %v", err)
	}
	if err := f.executor.Apply(t.Context(), f.target); !errors.Is(err, ErrConflict) {
		t.Fatalf("replaced Deployment applied: %v", err)
	}
	before, _ := os.ReadFile(path)
	if err := f.executor.PinIdentity(t.Context()); !errors.Is(err, ErrConflict) {
		t.Fatalf("a replaced Deployment was re-pinned silently: %v", err)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatal("refused enrollment rewrote the pinned identity")
	}
}

func TestKubernetesPinRefusesWhatTheAdapterCannotUpdate(t *testing.T) {
	f := newKubernetesFixture(t)
	f.executor.c.Kubernetes.DeploymentUID = ""
	d := f.deployment()
	d.Labels = map[string]string{"kustomize.toolkit.fluxcd.io/name": "another-owner"}
	if err := f.client.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), d, d.Namespace); err != nil {
		t.Fatal(err)
	}
	if err := f.executor.PinIdentity(t.Context()); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("GitOps-managed Deployment was pinned: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.executor.c.StateDirectory, identityFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused enrollment left an identity: %v", err)
	}
}

func TestKubernetesConfiguredUIDWinsOverAPin(t *testing.T) {
	f := newKubernetesFixture(t)
	if err := f.executor.PinIdentity(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(f.executor.c.StateDirectory, identityFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("an explicit deployment_uid still wrote a second identity")
	}
}

func TestKubernetesCorruptIdentityIsNotTreatedAsUnpinned(t *testing.T) {
	for name, content := range map[string]string{"garbage": "{", "unknown member": `{"deployment_uid":"deployment-1","extra":1}`, "unsafe": `{"deployment_uid":"../x"}`, "empty": `{"deployment_uid":""}`} {
		t.Run(name, func(t *testing.T) {
			f := newKubernetesFixture(t)
			f.executor.c.Kubernetes.DeploymentUID = ""
			if err := os.WriteFile(filepath.Join(f.executor.c.StateDirectory, identityFile), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := f.executor.Observe(t.Context()); !errors.Is(err, ErrInvalid) {
				t.Fatalf("corrupt identity accepted: %v", err)
			}
		})
	}
}

func (f *kubernetesFixture) addCoordinator(image string) {
	f.t.Helper()
	f.executor.c.Kubernetes.CronJob = "hikyo-security-maintenance"
	cron := &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "hikyo-security-maintenance", Namespace: "hikyo", ResourceVersion: "1"}, Spec: batchv1.CronJobSpec{JobTemplate: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "coordinator", Image: image}, {Name: "sidecar", Image: "registry.example/other@sha256:" + strings.Repeat("e", 64)}}}}}}}}
	if _, err := f.client.BatchV1().CronJobs("hikyo").Create(f.t.Context(), cron, metav1.CreateOptions{}); err != nil {
		f.t.Fatal(err)
	}
}

func (f *kubernetesFixture) coordinatorImage() string {
	f.t.Helper()
	cron, err := f.client.BatchV1().CronJobs("hikyo").Get(f.t.Context(), "hikyo-security-maintenance", metav1.GetOptions{})
	if err != nil {
		f.t.Fatal(err)
	}
	return cron.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Image
}

func (f *kubernetesFixture) update() {
	f.t.Helper()
	if _, err := f.executor.Prepare(f.t.Context(), f.target); err != nil {
		f.t.Fatal(err)
	}
	if err := f.executor.Apply(f.t.Context(), f.target); err != nil {
		f.t.Fatal(err)
	}
	f.replace(imageDigest(f.target.Image))
}

func (f *kubernetesFixture) cronJobPatches() int {
	f.t.Helper()
	n := 0
	for _, action := range f.client.Actions() {
		if action.GetVerb() == "patch" && action.GetResource().Resource == "cronjobs" {
			n++
		}
	}
	return n
}

// check, enroll and reconcile reach Observe, and apply=false never changes a workload.
func TestKubernetesObserveNeverWritesTheCoordinatorCronJob(t *testing.T) {
	f := newKubernetesFixture(t)
	source := f.deployment().Spec.Template.Spec.Containers[0].Image
	f.addCoordinator(source)
	f.update()
	before := len(f.client.Actions())
	for range 2 {
		if _, err := f.executor.Observe(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if j, err := f.executor.journal(); err != nil || j.Phase != "healthy" {
		t.Fatalf("update not recorded healthy: %+v %v", j, err)
	}
	for _, action := range f.client.Actions()[before:] {
		if action.GetResource().Resource == "cronjobs" {
			t.Fatalf("Observe touched the CronJob: %s", action.GetVerb())
		}
	}
	if f.coordinatorImage() != source {
		t.Fatal("coordinator image moved without an applying run")
	}
}

func TestKubernetesCoordinatorImageFollowsTheGivenImage(t *testing.T) {
	f := newKubernetesFixture(t)
	source := f.deployment().Spec.Template.Spec.Containers[0].Image
	f.addCoordinator(source)
	if err := f.executor.FollowCoordinator(t.Context(), f.target.Image); err != nil {
		t.Fatal(err)
	}
	if f.coordinatorImage() != f.target.Image {
		t.Fatalf("coordinator image = %s", f.coordinatorImage())
	}
	cron, _ := f.client.BatchV1().CronJobs("hikyo").Get(t.Context(), "hikyo-security-maintenance", metav1.GetOptions{})
	if cron.Spec.JobTemplate.Spec.Template.Spec.Containers[1].Image == f.target.Image {
		t.Fatal("a container that is not this image was moved")
	}
	patches := f.cronJobPatches()
	if err := f.executor.FollowCoordinator(t.Context(), f.target.Image); err != nil || f.cronJobPatches() != patches {
		t.Fatalf("an already-current coordinator was patched again: %v", err)
	}
}

func TestKubernetesCoordinatorRefusesAnImageThatIsNotItsRepositoryDigest(t *testing.T) {
	f := newKubernetesFixture(t)
	source := f.deployment().Spec.Template.Spec.Containers[0].Image
	f.addCoordinator(source)
	for name, image := range map[string]string{
		"another repository": "registry.example/other@sha256:" + strings.Repeat("e", 64),
		"a tag":              fixtureRepository + ":latest",
		"short digest":       fixtureRepository + "@sha256:abc",
	} {
		if err := f.executor.FollowCoordinator(t.Context(), image); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
	if f.cronJobPatches() != 0 || f.coordinatorImage() != source {
		t.Fatal("a refused image changed the CronJob")
	}
	f.executor.c.Kubernetes.CronJob = ""
	if err := f.executor.FollowCoordinator(t.Context(), f.target.Image); err != nil || f.cronJobPatches() != 0 {
		t.Fatalf("an unconfigured CronJob was written: %v", err)
	}
}

func TestKubernetesRefusedCoordinatorPatchIsReportedAndRetried(t *testing.T) {
	f := newKubernetesFixture(t)
	source := f.deployment().Spec.Template.Spec.Containers[0].Image
	f.addCoordinator(source)
	refuse := true
	f.client.PrependReactor("patch", "cronjobs", func(ktesting.Action) (bool, runtime.Object, error) {
		if refuse {
			return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "batch", Resource: "cronjobs"}, "hikyo-security-maintenance", errors.New("denied"))
		}
		return false, nil, nil
	})
	f.update()
	if err := f.executor.FollowCoordinator(t.Context(), f.target.Image); !apierrors.IsForbidden(err) {
		t.Fatalf("a refused patch was not reported to the caller that logs it: %v", err)
	}
	o, err := f.executor.Observe(t.Context())
	if err != nil || !o.Ready || o.Image != f.target.Image {
		t.Fatalf("a refused self-image patch failed the proven update: %+v %v", o, err)
	}
	if f.coordinatorImage() != source {
		t.Fatal("refused patch changed the image")
	}
	refuse = false
	if err := f.executor.FollowCoordinator(t.Context(), f.target.Image); err != nil || f.coordinatorImage() != f.target.Image {
		t.Fatalf("a later run did not finish a refused follow: %v", err)
	}
}

func TestKubernetesStatusIsCopiedToItsNamedConfigMap(t *testing.T) {
	f := newKubernetesFixture(t)
	f.executor.c.Kubernetes.StatusConfigMap = "hikyo-security-status"
	if _, err := f.client.CoreV1().ConfigMaps("hikyo").Create(t.Context(), &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "hikyo-security-status", Namespace: "hikyo", Labels: map[string]string{"owner": "chart"}}}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := f.executor.PublishStatus(t.Context(), []byte(`{"phase":"current"}`)); err != nil {
		t.Fatal(err)
	}
	m, err := f.client.CoreV1().ConfigMaps("hikyo").Get(t.Context(), "hikyo-security-status", metav1.GetOptions{})
	if err != nil || m.Data["status.json"] != `{"phase":"current"}` || m.Labels["owner"] != "chart" {
		t.Fatalf("status ConfigMap = %+v %v", m, err)
	}
	f.client.PrependReactor("update", "configmaps", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "configmaps"}, "hikyo-security-status", errors.New("denied"))
	})
	if err := f.executor.PublishStatus(t.Context(), []byte(`{}`)); !apierrors.IsForbidden(err) {
		t.Fatalf("refusal not reported to the caller that logs it: %v", err)
	}
	f.executor.c.Kubernetes.StatusConfigMap = ""
	if err := f.executor.PublishStatus(t.Context(), []byte(`{}`)); err != nil {
		t.Fatalf("an unconfigured status ConfigMap was written: %v", err)
	}
}

func TestKubernetesRollsBackInOrderAndRestoresTemplateAndReplicasTogether(t *testing.T) {
	f := newKubernetesFixture(t)
	sourceImage := f.deployment().Spec.Template.Spec.Containers[0].Image
	if _, err := f.executor.Prepare(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	if _, err := f.executor.RollBack(t.Context()); !errors.Is(err, ErrConflict) {
		t.Fatalf("rolled back a replacement that never began: %v", err)
	}
	if err := f.executor.Apply(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	f.client.ClearActions()
	f.updates = nil
	if _, err := f.executor.RollBack(t.Context()); err != nil {
		t.Fatal(err)
	}
	// Order: the stop, then the one-shot Pod, then the source.
	var order []string
	for _, action := range f.client.Actions() {
		switch {
		case action.Matches("update", "deployments"):
			order = append(order, "update")
		case action.Matches("create", "pods"):
			order = append(order, "pod")
		}
	}
	if strings.Join(order, ",") != "update,pod,update" || len(f.updates) != 2 {
		t.Fatalf("order %v with %d writes", order, len(f.updates))
	}
	if stop := f.updates[0]; *stop.Spec.Replicas != 0 || stop.Spec.Template.Spec.Containers[0].Image != f.target.Image {
		t.Fatal("the target was not scaled to zero first")
	}
	// Template and replicas return in the same write, never one without the other.
	if back := f.updates[1]; *back.Spec.Replicas != 1 || back.Spec.Template.Spec.Containers[0].Image != sourceImage || back.Spec.Template.Annotations[replacementTarget] != "" {
		t.Fatalf("the source did not return in one write: %+v", back.Spec)
	}
	if f.command != "rollback" || f.stage.Spec.NodeName != "node-1" || f.stage.Spec.Containers[0].Image != f.target.Image || f.stage.Spec.Containers[0].Args[3] != string(f.target.Identity.ManifestSHA256) {
		t.Fatalf("the one-shot is not the target's rollback on the recorded node: %v on %s", f.stage.Spec.Containers[0].Args, f.stage.Spec.NodeName)
	}
	if _, err := f.client.CoreV1().Pods("hikyo").Get(t.Context(), "hikyo-security-stage", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("the one-shot Pod was left behind: %v", err)
	}
	if j, err := f.executor.journal(); err != nil || j == nil || j.Phase != "applying" {
		t.Fatalf("a rollback released the pin on its own: %+v %v", j, err)
	}
}

// A stop written on the last attempt still gets its proof, and the rollback goes on.
func TestKubernetesRollbackProvesAStopWrittenOnTheLastAttempt(t *testing.T) {
	f := newKubernetesFixture(t)
	if _, err := f.executor.Prepare(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	if err := f.executor.Apply(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	f.conflicts = 2
	if _, err := f.executor.RollBack(t.Context()); err != nil {
		t.Fatalf("a stop that landed was not proven: %v", err)
	}
	if d := f.deployment(); f.command != "rollback" || *d.Spec.Replicas != 1 || d.Spec.Template.Annotations[replacementTarget] != "" {
		t.Fatalf("the rollback did not run and write the source back: %s %+v", f.command, d.Spec)
	}
}

func TestKubernetesRollbackNeedsProofTheTargetStopped(t *testing.T) {
	f := newKubernetesFixture(t)
	if _, err := f.executor.Prepare(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	if err := f.executor.Apply(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	// A terminating Pod still counts: it may be writing.
	f.keepPods = true
	f.executor.c.TimeoutSeconds = 1
	f.client.ClearActions()
	if _, err := f.executor.RollBack(t.Context()); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("a target that did not stop was rolled back: %v", err)
	}
	for _, action := range f.client.Actions() {
		if action.Matches("create", "pods") {
			t.Fatal("the one-shot ran beside a Pod that had not stopped")
		}
	}
	if *f.deployment().Spec.Replicas != 0 {
		t.Fatal("the source was written back before the target stopped")
	}
}

func TestKubernetesRollbackWritesTheSourceBackWhenTheOneShotFails(t *testing.T) {
	f := newKubernetesFixture(t)
	f.failPod = true
	if _, err := f.executor.Prepare(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	if err := f.executor.Apply(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	if _, err := f.executor.RollBack(t.Context()); err == nil {
		t.Fatal("a failed one-shot was reported as success")
	}
	if d := f.deployment(); *d.Spec.Replicas != 1 || d.Spec.Template.Annotations[replacementTarget] != "" {
		t.Fatalf("the source gate was not given the source back: %+v", d.Spec)
	}
}

func TestKubernetesRollbackRetriesTheOneShotFromTheDurableState(t *testing.T) {
	f := newKubernetesFixture(t)
	sourceImage := f.deployment().Spec.Template.Spec.Containers[0].Image
	f.failPod = true
	if _, err := f.executor.Prepare(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	if err := f.executor.Apply(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	if _, err := f.executor.RollBack(t.Context()); err == nil {
		t.Fatal("a failed one-shot was reported as success")
	}
	// The source is back; a drifted Deployment is still refused.
	drifted := f.deployment()
	drifted.Spec.Template.Spec.Containers[0].Image = "registry.example/other:1"
	if _, err := f.client.AppsV1().Deployments("hikyo").Update(t.Context(), drifted, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	// Refused as a conflict, never as "no rollback path": the caller would then report only its timeout.
	if _, err := f.executor.RollBack(t.Context()); !errors.Is(err, ErrConflict) || errors.Is(err, ErrUnsupported) {
		t.Fatalf("a drifted Deployment was not refused as a conflict: %v", err)
	}
	drifted = f.deployment()
	drifted.Spec.Template.Spec.Containers[0].Image = sourceImage
	if _, err := f.client.AppsV1().Deployments("hikyo").Update(t.Context(), drifted, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	f.failPod = false
	if _, err := f.executor.RollBack(t.Context()); err != nil {
		t.Fatalf("the retry did not run the one-shot from the durable state: %v", err)
	}
	if f.command != "rollback" {
		t.Fatalf("the one-shot did not run: %q", f.command)
	}
	if d := f.deployment(); *d.Spec.Replicas != 1 || d.Spec.Template.Annotations[replacementTarget] != "" {
		t.Fatalf("the source is not back after the retry: %+v", d.Spec)
	}
}

func TestKubernetesRollbackRefusesAJournalThatRecordsNoNode(t *testing.T) {
	f := newKubernetesFixture(t)
	if _, err := f.executor.Prepare(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	if err := f.executor.Apply(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	j, err := f.executor.journal()
	if err != nil || j.Node != "node-1" {
		t.Fatalf("apply did not record the node: %+v %v", j, err)
	}
	j.Node = ""
	if err := f.executor.save(j); err != nil {
		t.Fatal(err)
	}
	f.updates = nil
	if _, err := f.executor.RollBack(t.Context()); !errors.Is(err, ErrRecoveryRequired) || len(f.updates) != 0 {
		t.Fatalf("a journal without a node was rolled back: %v writes=%d", err, len(f.updates))
	}
}

func TestDeploymentTimeoutReachesADay(t *testing.T) {
	f := newKubernetesFixture(t)
	config := f.executor.c
	config.TimeoutSeconds = 86400
	if err := Validate(config); err != nil {
		t.Fatalf("a day was refused: %v", err)
	}
	config.TimeoutSeconds++
	if err := Validate(config); !errors.Is(err, ErrInvalid) {
		t.Fatalf("more than a day was accepted: %v", err)
	}
}

func appliedKubernetesFixture(t *testing.T) *kubernetesFixture {
	t.Helper()
	f := newKubernetesFixture(t)
	if _, err := f.executor.Prepare(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	if err := f.executor.Apply(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	f.command = ""
	return f
}

// A target at zero replicas or crash-looping cannot be exec'd into, so a later run reads the row
// through a stage Pod of the source image on the node the source ran on.
func TestKubernetesNoFinalGateReadsTheRowThroughAStagePod(t *testing.T) {
	source := fixtureObservation()
	pastGate := source
	pastGate.Identity = fixtureIdentity(2)
	writing := pastGate
	writing.Phase = "restore-required"
	foreign := source
	foreign.Instance = "ins_another"
	for name, test := range map[string]struct {
		row     *Observation
		zero    bool
		proven  bool
		wantErr bool
		logLine string
	}{
		"healthy at the source":      {row: &source, proven: true},
		"writing at the target":      {row: &writing, proven: true},
		"healthy at another release": {row: &pastGate},
		"unreadable":                 {wantErr: true},
		"another installation's row": {row: &foreign, wantErr: true},
		"at zero replicas":           {row: &source, zero: true, proven: true},
		"behind a log line":          {row: &source, logLine: "warning: HIKYO_ROOT_KEY is set\n", proven: true},
	} {
		t.Run(name, func(t *testing.T) {
			f := appliedKubernetesFixture(t)
			f.inspectRow, f.logLine = test.row, test.logLine
			if test.zero {
				d := f.deployment()
				none := int32(0)
				d.Spec.Replicas = &none
				if _, err := f.client.AppsV1().Deployments("hikyo").Update(t.Context(), d, metav1.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			proven, err := f.executor.NoFinalGate(t.Context(), source)
			if proven != test.proven || (err != nil) != test.wantErr {
				t.Fatalf("proven=%t err=%v", proven, err)
			}
			if f.command != "inspect" || f.stage.Spec.NodeName != "node-1" || f.stage.Spec.Containers[0].Image != fixtureRepository+"@sha256:"+strings.Repeat("a", 64) {
				t.Fatalf("not an inspect of the source image on the recorded node: %v on %s image %s", f.stageArgs, f.stage.Spec.NodeName, f.stage.Spec.Containers[0].Image)
			}
			if strings.Join(f.stageArgs, " ") != "security-maintenance inspect --control-row" {
				t.Fatalf("inspect was given %v", f.stageArgs)
			}
			if _, err := f.client.CoreV1().Pods("hikyo").Get(t.Context(), "hikyo-security-stage", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
				t.Fatalf("the stage Pod was left behind: %v", err)
			}
		})
	}
}

func TestKubernetesRollbackMarkerIsNotSetWhileThePodsMayStillRun(t *testing.T) {
	f := appliedKubernetesFixture(t)
	f.keepPods, f.executor.c.TimeoutSeconds = true, 1
	if _, err := f.executor.RollBack(t.Context()); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("a target that did not stop was rolled back: %v", err)
	}
	if j, err := f.executor.journal(); err != nil || j == nil || j.RollingBack {
		t.Fatalf("a Pod that may still run left a marker: %+v %v", j, err)
	}
	past := fixtureObservation()
	past.Identity = fixtureIdentity(2)
	f.inspectRow = &past
	if proven, err := f.executor.NoFinalGate(t.Context(), fixtureObservation()); err != nil || proven || f.command != "inspect" {
		t.Fatalf("the row was not read: proven=%t err=%v command=%q", proven, err, f.command)
	}
}

func TestKubernetesRollbackMarkerAnswersOnlyWhenNoRowCanBeRead(t *testing.T) {
	f := appliedKubernetesFixture(t)
	f.failPod = true // the target is proven stopped, then the one-shot fails
	if _, err := f.executor.RollBack(t.Context()); err == nil {
		t.Fatal("a failed one-shot was reported as success")
	}
	if j, err := f.executor.journal(); err != nil || j == nil || !j.RollingBack {
		t.Fatalf("no marker after the stop was proven: %+v %v", j, err)
	}
	proven, err := f.executor.NoFinalGate(t.Context(), fixtureObservation())
	if err != nil || !proven {
		t.Fatalf("a journal that began the rollback did not prove it: %t %v", proven, err)
	}
	f.failPod = false
	served := fixtureObservation()
	served.Identity = fixtureIdentity(2)
	f.inspectRow = &served
	if proven, err := f.executor.NoFinalGate(t.Context(), fixtureObservation()); err != nil || proven {
		t.Fatalf("the marker hid a target that served: %t %v", proven, err)
	}
}

// A person may have started the target after a refused rollback. During a database outage the marker must not vouch for it.
func TestKubernetesRollbackMarkerDoesNotVouchForATargetRunningAgain(t *testing.T) {
	f := appliedKubernetesFixture(t)
	f.failPod = true
	if _, err := f.executor.RollBack(t.Context()); err == nil {
		t.Fatal("a failed one-shot was reported as success")
	}
	if proven, err := f.executor.NoFinalGate(t.Context(), fixtureObservation()); !proven || err != nil {
		t.Fatalf("the marker no longer answers for the source written back: %t %v", proven, err)
	}
	// A person started the target by tag: only the Pod's runtime image ID shows it.
	deployments := f.client.AppsV1().Deployments("hikyo")
	d, err := deployments.Get(t.Context(), "hikyo", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	spec := *d.Spec.Template.Spec.DeepCopy()
	spec.Containers[0].Image = "ghcr.io/hikyo-org/hikyo:security-fix"
	pods := f.client.CoreV1().Pods("hikyo")
	tagged := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "hikyo-by-tag", Namespace: "hikyo", Labels: d.Spec.Selector.MatchLabels, OwnerReferences: owner("ReplicaSet", "hikyo-source", "replica-set-1")}, Spec: spec,
		Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Name: "server", ImageID: "containerd://sha256:" + imageDigest(f.target.Image)}}}}
	if _, err := pods.Create(t.Context(), tagged, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if proven, err := f.executor.NoFinalGate(t.Context(), fixtureObservation()); proven || err == nil {
		t.Fatalf("the marker vouched for a target started by tag: %t %v", proven, err)
	}
	if err := pods.Delete(t.Context(), tagged.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if proven, err := f.executor.NoFinalGate(t.Context(), fixtureObservation()); !proven || err != nil {
		t.Fatalf("the marker no longer answers once the tagged target is gone: %t %v", proven, err)
	}
	d, err = deployments.Get(t.Context(), "hikyo", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for i := range d.Spec.Template.Spec.Containers {
		d.Spec.Template.Spec.Containers[i].Image = f.target.Image
	}
	if _, err := deployments.Update(t.Context(), d, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if proven, err := f.executor.NoFinalGate(t.Context(), fixtureObservation()); proven || err == nil {
		t.Fatalf("the marker vouched for a running target: %t %v", proven, err)
	}
}

// spentContextClient records the state of the context every Deployment write got.
type spentContextClient struct {
	kubernetes.Interface
	seen *[]error
}

func (c spentContextClient) AppsV1() appsv1client.AppsV1Interface {
	return spentContextApps{c.Interface.AppsV1(), c.seen}
}

type spentContextApps struct {
	appsv1client.AppsV1Interface
	seen *[]error
}

func (a spentContextApps) Deployments(namespace string) appsv1client.DeploymentInterface {
	return spentContextDeployments{a.AppsV1Interface.Deployments(namespace), a.seen}
}

type spentContextDeployments struct {
	appsv1client.DeploymentInterface
	seen *[]error
}

func (d spentContextDeployments) Update(ctx context.Context, deployment *appsv1.Deployment, options metav1.UpdateOptions) (*appsv1.Deployment, error) {
	*d.seen = append(*d.seen, ctx.Err())
	return d.DeploymentInterface.Update(ctx, deployment, options)
}

func TestKubernetesRollBackReportsRestoredAndWritesBackOnAFreshWindow(t *testing.T) {
	f := appliedKubernetesFixture(t)
	var seen []error
	f.executor.client = spentContextClient{f.client, &seen}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	// The one-shot consumes the caller's window: it is spent when the source is written back.
	f.client.PrependReactor("create", "pods", func(ktesting.Action) (bool, runtime.Object, error) {
		cancel()
		return false, nil, nil
	})
	restored, err := f.executor.RollBack(ctx)
	if err != nil || !restored {
		t.Fatalf("restored=%t err=%v", restored, err)
	}
	if len(seen) == 0 || seen[len(seen)-1] != nil {
		t.Fatalf("the write-back ran on a spent context: %v", seen)
	}
}

// A wait that expires inside the read reports the same conflict as one that expires between reads.
func TestKubernetesStagePodWaitTimesOutAsAConflict(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newKubernetesFixture(t)
		d := f.deployment()
		stage, err := f.executor.stagePod(d, "node-1", fixtureRepository+"@sha256:"+strings.Repeat("a", 64), []string{"security-maintenance", "inspect", "--control-row"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.client.CoreV1().Pods("hikyo").Create(t.Context(), stage, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		f.finalized = 1 << 20
		f.client.PrependReactor("get", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
			object, err := f.client.Tracker().Get(corev1.SchemeGroupVersion.WithResource("pods"), "hikyo", action.(ktesting.GetAction).GetName())
			if err != nil || object.(*corev1.Pod).DeletionTimestamp == nil {
				return false, nil, nil
			}
			// The API call outlives the wait, as a slow server's would.
			time.Sleep(time.Minute)
			return true, nil, context.DeadlineExceeded
		})
		if err := f.executor.removeStagePod(t.Context(), d); !errors.Is(err, ErrConflict) {
			t.Fatalf("a wait that expired in the read was not a conflict: %v", err)
		}
	})
}

// A stage Pod an earlier crashed run left behind, whatever it was for, must not make every later run conflict.
func TestKubernetesLeftoverStagePodDoesNotBlockPrepareOrInspect(t *testing.T) {
	leave := func(f *kubernetesFixture) {
		t.Helper()
		d := f.deployment()
		stage, err := f.executor.stagePod(d, "node-1", fixtureRepository+"@sha256:"+strings.Repeat("a", 64), []string{"security-maintenance", "inspect", "--control-row"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.client.CoreV1().Pods("hikyo").Create(t.Context(), stage, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	f := newKubernetesFixture(t)
	leave(f)
	// A finalizer keeps the deleted Pod for a few reads; the create must wait for it.
	f.finalized = 3
	if _, err := f.executor.Prepare(t.Context(), f.target); err != nil {
		t.Fatalf("a leftover Pod blocked the update: %v", err)
	}
	if err := f.executor.Apply(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	leave(f)
	row := fixtureObservation()
	f.inspectRow = &row
	if _, err := f.executor.NoFinalGate(t.Context(), row); err != nil {
		t.Fatalf("a leftover Pod blocked the inspect: %v", err)
	}
}

// A Pod is admitted with what its template omits: requests default to limits, and a
// LimitRange adds defaults. A resource the template sets must still match exactly.
func TestKubernetesStageAcceptsAdmissionAddedResources(t *testing.T) {
	for name, test := range map[string]struct {
		requests corev1.ResourceList
		admit    func(*corev1.Container)
		wantErr  error
	}{
		"requests from limits": {admit: func(c *corev1.Container) { c.Resources.Requests = c.Resources.Limits.DeepCopy() }},
		"limit range default": {admit: func(c *corev1.Container) {
			c.Resources.Requests = corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")}
		}},
		"a set request changed": {requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")}, admit: func(c *corev1.Container) {
			c.Resources.Requests[corev1.ResourceCPU] = resource.MustParse("200m")
		}, wantErr: ErrConflict},
		"a set limit changed": {admit: func(c *corev1.Container) {
			c.Resources.Limits[corev1.ResourceMemory] = resource.MustParse("2Gi")
		}, wantErr: ErrConflict},
	} {
		t.Run(name, func(t *testing.T) {
			f := newKubernetesFixture(t)
			d := f.deployment()
			for _, containers := range [][]corev1.Container{d.Spec.Template.Spec.Containers, d.Spec.Template.Spec.InitContainers} {
				containers[0].Resources = corev1.ResourceRequirements{Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")}, Requests: test.requests.DeepCopy()}
			}
			if err := f.client.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), d, d.Namespace); err != nil {
				t.Fatal(err)
			}
			f.admit = func(p *corev1.Pod) {
				for i := range p.Spec.Containers {
					test.admit(&p.Spec.Containers[i])
				}
				for i := range p.Spec.InitContainers {
					test.admit(&p.Spec.InitContainers[i])
				}
			}
			_, err := f.executor.Prepare(t.Context(), f.target)
			if test.wantErr == nil && err != nil || test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Fatalf("err = %v, want %v", err, test.wantErr)
			}
		})
	}
}

// Pod logs merge stderr, so a warning can precede the stage report.
func TestKubernetesStageReportBehindALogLine(t *testing.T) {
	f := newKubernetesFixture(t)
	f.logLine = "warning: HIKYO_ROOT_KEY is set\n"
	staged, err := f.executor.Prepare(t.Context(), f.target)
	if err != nil || staged.Phase != "staged" {
		t.Fatalf("a report behind a log line was refused: %+v %v", staged, err)
	}
}

// A read that fails after the stop is proven still gives the source gate the source back.
func TestKubernetesRollbackWritesTheSourceBackWhenTheReadAfterTheStopFails(t *testing.T) {
	f := newKubernetesFixture(t)
	if _, err := f.executor.Prepare(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	if err := f.executor.Apply(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	failed := false
	f.client.PrependReactor("get", "deployments", func(ktesting.Action) (bool, runtime.Object, error) {
		if j, err := f.executor.journal(); !failed && err == nil && j != nil && j.RollingBack {
			failed = true
			return true, nil, errors.New("transient read failure")
		}
		return false, nil, nil
	})
	if _, err := f.executor.RollBack(t.Context()); err == nil || !failed {
		t.Fatalf("a failed read was not reported: failed=%t err=%v", failed, err)
	}
	if d := f.deployment(); *d.Spec.Replicas != 1 || d.Spec.Template.Annotations[replacementTarget] != "" {
		t.Fatalf("the source gate was not given the source back: %+v", d.Spec)
	}
}

func TestKubernetesCoordinatorInitImagesFollowTheGivenImage(t *testing.T) {
	f := newKubernetesFixture(t)
	source := f.deployment().Spec.Template.Spec.Containers[0].Image
	f.addCoordinator(source)
	other := "registry.example/other@sha256:" + strings.Repeat("e", 64)
	cron, err := f.client.BatchV1().CronJobs("hikyo").Get(t.Context(), "hikyo-security-maintenance", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cron.Spec.JobTemplate.Spec.Template.Spec.InitContainers = []corev1.Container{{Name: "root-key-stage", Image: source}, {Name: "other", Image: other}}
	if _, err := f.client.BatchV1().CronJobs("hikyo").Update(t.Context(), cron, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := f.executor.FollowCoordinator(t.Context(), f.target.Image); err != nil {
		t.Fatal(err)
	}
	cron, _ = f.client.BatchV1().CronJobs("hikyo").Get(t.Context(), "hikyo-security-maintenance", metav1.GetOptions{})
	inits := cron.Spec.JobTemplate.Spec.Template.Spec.InitContainers
	if len(inits) != 2 || inits[0].Image != f.target.Image || inits[1].Image != other || f.coordinatorImage() != f.target.Image {
		t.Fatalf("init images = %+v", inits)
	}
}

// The patch carries the read's resourceVersion, so the API server refuses it after a concurrent change.
func TestKubernetesCoordinatorPatchRefusesAConcurrentChange(t *testing.T) {
	f := newKubernetesFixture(t)
	source := f.deployment().Spec.Template.Spec.Containers[0].Image
	f.addCoordinator(source)
	f.client.PrependReactor("patch", "cronjobs", func(action ktesting.Action) (bool, runtime.Object, error) {
		var body struct {
			Metadata struct {
				ResourceVersion string `json:"resourceVersion"`
			} `json:"metadata"`
		}
		if err := json.Unmarshal(action.(ktesting.PatchAction).GetPatch(), &body); err != nil {
			return true, nil, err
		}
		// An operator wrote the CronJob after the read, moving it to version 2.
		if body.Metadata.ResourceVersion != "" && body.Metadata.ResourceVersion != "2" {
			return true, nil, apierrors.NewConflict(schema.GroupResource{Group: "batch", Resource: "cronjobs"}, "hikyo-security-maintenance", errors.New("the object has been modified"))
		}
		return true, nil, errors.New("an unconditional patch would overwrite the concurrent change")
	})
	if err := f.executor.FollowCoordinator(t.Context(), f.target.Image); !errors.Is(err, ErrConflict) {
		t.Fatalf("a concurrent change was not refused as a conflict: %v", err)
	}
	if f.cronJobPatches() != 1 || f.coordinatorImage() != source {
		t.Fatal("the refused patch was retried or changed the image")
	}
}

func TestKubernetesUnknownInitContainerRefusalNamesItAndTheRemedy(t *testing.T) {
	f := newKubernetesFixture(t)
	d := f.deployment()
	d.Spec.Template.Spec.InitContainers = append(d.Spec.Template.Spec.InitContainers, corev1.Container{Name: "wait-for-db", Image: "busybox"})
	if err := f.client.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), d, d.Namespace); err != nil {
		t.Fatal(err)
	}
	_, err := f.executor.Prepare(t.Context(), f.target)
	if !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), `"wait-for-db"`) || !strings.Contains(err.Error(), "remove it") {
		t.Fatalf("refusal does not name the init container and the remedy: %v", err)
	}
	if f.stage != nil {
		t.Fatal("an unknown init container obtained staging authority")
	}
}

// The chart's TLS stager qualifies only in its exact form.
func TestKubernetesAcceptsOnlyTheChartsTLSStager(t *testing.T) {
	for name, args := range map[string][]string{"chart form": {multicall.TLSStage, "--once"}, "watching form": {multicall.TLSStage}, "extra argument": {multicall.TLSStage, "--once", "--x"}} {
		t.Run(name, func(t *testing.T) {
			f := newKubernetesFixture(t)
			d := f.deployment()
			image := d.Spec.Template.Spec.InitContainers[0].Image
			d.Spec.Template.Spec.InitContainers = append(d.Spec.Template.Spec.InitContainers, corev1.Container{Name: "tls-stage", Image: image, Args: args})
			if err := f.client.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), d, d.Namespace); err != nil {
				t.Fatal(err)
			}
			_, err := f.executor.Prepare(t.Context(), f.target)
			if refused := errors.Is(err, ErrUnsupported); refused != (name != "chart form") {
				t.Fatalf("Prepare = %v", err)
			}
		})
	}
}

// Explicit env wins over envFrom, and a bare or true unattended flag keeps it on.
func TestKubernetesAcceptsExplicitOverridesAndEnablingFlagForms(t *testing.T) {
	f := newKubernetesFixture(t)
	d := f.deployment()
	server := &d.Spec.Template.Spec.Containers[0]
	server.EnvFrom = []corev1.EnvFromSource{{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "settings"}}}}
	server.Env = append(server.Env, corev1.EnvVar{Name: "HIKYO_HA", Value: "false"})
	server.Args = append(server.Args, "--upgrade-unattended", "-upgrade-unattended=1", "--auto-migrate=true")
	server.Command = []string{"/usr/local/bin/hikyo"}
	// A claim mounted more specifically than a scratch volume is the state directory's.
	d.Spec.Template.Spec.Volumes = append(d.Spec.Template.Spec.Volumes, corev1.Volume{Name: "scratch", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}})
	server.VolumeMounts = []corev1.VolumeMount{{Name: "scratch", MountPath: "/var/lib"}, {Name: "upgrade-state", MountPath: "/var/lib/hikyo-upgrade/"}, {Name: "security-status", MountPath: "/run/hikyo-security-status", ReadOnly: true}}
	if err := f.client.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), d, d.Namespace); err != nil {
		t.Fatal(err)
	}
	if _, err := f.executor.Prepare(t.Context(), f.target); err != nil {
		t.Fatalf("a qualifying template was refused: %v", err)
	}
}

// Of two enrollments that both read no pin, the second compares against the first instead of replacing it.
func TestKubernetesIdentityPinIsFirstWriterWins(t *testing.T) {
	f := newKubernetesFixture(t)
	f.executor.c.Kubernetes.DeploymentUID = ""
	if err := f.executor.publishIdentity([]byte(`{"deployment_uid":"deployment-0"}`), "deployment-0"); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(kubernetesIdentity{DeploymentUID: "deployment-1"})
	if err := f.executor.publishIdentity(raw, "deployment-1"); !errors.Is(err, ErrConflict) {
		t.Fatalf("a second enrollment replaced or accepted another pin: %v", err)
	}
	if uid, err := f.executor.pinnedUID(); err != nil || uid != "deployment-0" {
		t.Fatalf("pin = %q %v", uid, err)
	}
	if err := f.executor.publishIdentity([]byte(`{"deployment_uid":"deployment-0"}`), "deployment-0"); err != nil {
		t.Fatalf("repeating the same pin failed: %v", err)
	}
	entries, _ := os.ReadDir(f.executor.c.StateDirectory)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".kubernetes-identity-") {
			t.Fatalf("temporary pin left behind: %s", entry.Name())
		}
	}
}

// canceledLogs is Pod I/O whose log read is cut off by a canceled context.
type canceledLogs struct{ podIO }

func (canceledLogs) Logs(context.Context, string, string) ([]byte, error) {
	return nil, context.Canceled
}

func TestKubernetesRollbackWritesTheSourceBackWhenOneShotIOIsCanceled(t *testing.T) {
	f := appliedKubernetesFixture(t)
	var seen []error
	f.executor.client = spentContextClient{f.client, &seen}
	f.executor.pods = canceledLogs{f}
	restored, err := f.executor.RollBack(t.Context())
	if !errors.Is(err, context.Canceled) || restored {
		t.Fatalf("restored=%t err=%v", restored, err)
	}
	if len(seen) == 0 || seen[len(seen)-1] != nil {
		t.Fatalf("the write-back ran on a spent context: %v", seen)
	}
	if d := f.deployment(); *d.Spec.Replicas != 1 || d.Spec.Template.Annotations[replacementTarget] != "" {
		t.Fatalf("the source gate was not given the source back: %+v", d.Spec)
	}
}

// The one-shot is built from a read after the stop; a Deployment changed in between is refused, and the
// write-back still decides by the state it finds.
func TestKubernetesRollbackRefusesADeploymentChangedAfterTheStop(t *testing.T) {
	f := appliedKubernetesFixture(t)
	changed := false
	f.client.PrependReactor("get", "deployments", func(ktesting.Action) (bool, runtime.Object, error) {
		if j, err := f.executor.journal(); changed || err != nil || j == nil || !j.RollingBack {
			return false, nil, nil
		}
		changed = true
		object, err := f.client.Tracker().Get(appsv1.SchemeGroupVersion.WithResource("deployments"), "hikyo", "hikyo")
		if err != nil {
			return true, nil, err
		}
		d := object.(*appsv1.Deployment).DeepCopy()
		d.Spec.Template.Spec.Containers[0].Env = append(d.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{Name: "HIKYO_LOG_LEVEL", Value: "debug"})
		return true, d, nil
	})
	if _, err := f.executor.RollBack(t.Context()); !errors.Is(err, ErrConflict) || !changed {
		t.Fatalf("a changed Deployment was used for the one-shot: changed=%t err=%v", changed, err)
	}
	if f.command == "rollback" {
		t.Fatal("the rollback one-shot ran from a changed Deployment")
	}
	if d := f.deployment(); *d.Spec.Replicas != 1 || d.Spec.Template.Annotations[replacementTarget] != "" {
		t.Fatalf("the source was not written back: %+v", d.Spec)
	}
}

// As in Compose: a source image serving again, a target past its final gate, or a Pod running a third
// image is refused before anything is stopped.
func TestKubernetesRollbackRefusesWhatIsNotAStoppedOrFailingTarget(t *testing.T) {
	for _, name := range []string{"source serving again", "past the final gate", "third image"} {
		t.Run(name, func(t *testing.T) {
			f := appliedKubernetesFixture(t)
			switch name {
			case "source serving again":
				sourcePod, err := f.client.CoreV1().Pods("hikyo").Get(t.Context(), "hikyo-source-pod", metav1.GetOptions{})
				if err != nil {
					t.Fatal(err)
				}
				f.failPod = true
				if _, err := f.executor.RollBack(t.Context()); err == nil {
					t.Fatal("a failed one-shot was reported as success")
				}
				// The source gate admitted the written-back source, which serves at a later generation.
				d := f.deployment()
				d.Status = appsv1.DeploymentStatus{ObservedGeneration: d.Generation, Replicas: 1, UpdatedReplicas: 1, ReadyReplicas: 1, AvailableReplicas: 1}
				if err := f.client.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), d, d.Namespace); err != nil {
					t.Fatal(err)
				}
				if err := f.client.Tracker().Create(corev1.SchemeGroupVersion.WithResource("pods"), sourcePod, "hikyo"); err != nil {
					t.Fatal(err)
				}
				f.current.Generation++
				f.failPod = false
			case "past the final gate":
				f.replace(imageDigest(f.target.Image))
			case "third image":
				pod, err := f.client.CoreV1().Pods("hikyo").Get(t.Context(), "hikyo-source-pod", metav1.GetOptions{})
				if err != nil {
					t.Fatal(err)
				}
				pod.Spec.Containers[0].Image = fixtureRepository + "@sha256:" + strings.Repeat("c", 64)
				if _, err := f.client.CoreV1().Pods("hikyo").Update(t.Context(), pod, metav1.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			f.command, f.updates = "", nil
			if _, err := f.executor.RollBack(t.Context()); !errors.Is(err, ErrConflict) {
				t.Fatalf("rollback was not refused: %v", err)
			}
			if len(f.updates) != 0 || f.command != "" {
				t.Fatalf("a refused rollback stopped or ran something: %d writes, one-shot %q", len(f.updates), f.command)
			}
		})
	}
}
