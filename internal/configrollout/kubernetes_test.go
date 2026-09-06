package configrollout

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

type fixture struct {
	executor *Kubernetes
	client   *fake.Clientset
	target   Target
	intent   Intent
	before   *appsv1.Deployment
}

func TestTLSAliasesUpdateExplicitFlagsAndRestore(t *testing.T) {
	f := newFixture(t)
	f.target.Sources[TLSCertificateFile] = map[string]string{"next": "/run/tls-next/tls.crt"}
	f.target.Sources[TLSKeyFile] = map[string]string{"next": "/run/tls-next/tls.key"}
	d := f.deployment(t)
	d.Spec.Template.Spec.Containers[0].Args = append(d.Spec.Template.Spec.Containers[0].Args, "--tls-cert-file=/run/tls/tls.crt", "--tls-key-file", "/run/tls/tls.key")
	before, err := f.client.AppsV1().Deployments(f.target.Namespace).Update(t.Context(), d, metav1.UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	executor, err := NewKubernetes(f.client, f.target)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := executor.Prepare(t.Context(), f.intent, []Change{{Variable: TLSCertificateFile, Value: "next"}, {Variable: TLSKeyFile, Value: "next"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = executor.Submit(t.Context(), f.intent, plan.Digest(), plan); err != nil {
		t.Fatal(err)
	}
	args := strings.Join(f.deployment(t).Spec.Template.Spec.Containers[0].Args, " ")
	if !strings.Contains(args, "--tls-cert-file=/run/tls-next/tls.crt") || !strings.Contains(args, "--tls-key-file /run/tls-next/tls.key") {
		t.Fatal("installed TLS flags still override requested sources")
	}
	if _, err = executor.Restore(t.Context(), f.intent, plan.Digest()); err != nil {
		t.Fatal(err)
	}
	if digest(f.deployment(t).Spec) != digest(before.Spec) {
		t.Fatal("TLS restore changed unrelated deployment inputs")
	}
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	replicas := int32(1)
	probe := func() *corev1.Probe {
		return &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/readyz", Port: intstr.FromString("ops")}}}
	}
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "hikyo", Namespace: "hikyo", UID: "deployment-1", ResourceVersion: "1", Generation: 1}, Spec: appsv1.DeploymentSpec{Replicas: &replicas, Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"unrelated": "keep"}}, Spec: corev1.PodSpec{ServiceAccountName: "no-cluster-power", Containers: []corev1.Container{{Name: "server", Image: "hikyo@sha256:pinned", Args: []string{"server", "--listen=0.0.0.0:8080", "--operational-listen", "0.0.0.0:8081", "--root-key-file=/run/root/key"}, Env: []corev1.EnvVar{{Name: string(NodeID), Value: "hikyo-server"}, {Name: "UNRELATED_SECRET", Value: "UNRELATED_CANARY_DO_NOT_COPY"}, {Name: string(AdmissionBudget), Value: "272"}}, Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: 8080}, {Name: "ops", ContainerPort: 8081}}, ReadinessProbe: probe(), LivenessProbe: probe(), StartupProbe: probe(), VolumeMounts: []corev1.VolumeMount{{Name: "root", MountPath: "/run/root", ReadOnly: true}}}}, Volumes: []corev1.Volume{{Name: "root", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "external-root-key"}}}}}}}}
	target := Target{Namespace: "hikyo", Deployment: "hikyo", DeploymentUID: d.UID, Container: "server", StableNodeID: "hikyo-server", ConfigSecret: "config", RollbackSecret: "rollback", RequestSecret: "request", ReceiptSecret: "receipt", Sources: map[Variable]map[string]string{BackupDirectory: {"primary": "/var/backups/hikyo"}}}
	objects := []runtime.Object{d}
	for _, name := range []string{"config", "rollback", "request", "receipt"} {
		objects = append(objects, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "hikyo", UID: types.UID(name + "-1"), ResourceVersion: "1"}})
	}
	client := fake.NewClientset(objects...)
	// The generated fake omits API-server optimistic concurrency. Install it
	// here so stale-RV retries and lost replies exercise actual CAS behavior.
	var mu sync.Mutex
	client.PrependReactor("update", "*", func(action ktesting.Action) (bool, runtime.Object, error) {
		mu.Lock()
		defer mu.Unlock()
		update := action.(ktesting.UpdateAction)
		obj := update.GetObject().DeepCopyObject()
		accessor, _ := meta.Accessor(obj)
		old, err := client.Tracker().Get(action.GetResource(), action.GetNamespace(), accessor.GetName())
		if err != nil {
			return true, nil, err
		}
		prior, _ := meta.Accessor(old)
		if accessor.GetResourceVersion() != prior.GetResourceVersion() || accessor.GetUID() != prior.GetUID() {
			return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: action.GetResource().Resource}, accessor.GetName(), errors.New("stale resource version"))
		}
		n, _ := strconv.Atoi(prior.GetResourceVersion())
		accessor.SetResourceVersion(strconv.Itoa(n + 1))
		if deployment, ok := obj.(*appsv1.Deployment); ok {
			previous := old.(*appsv1.Deployment)
			if action.GetSubresource() == "status" {
				deployment.Spec = previous.Spec
				deployment.Generation = previous.Generation
			} else if digest(deployment.Spec) != digest(previous.Spec) {
				deployment.Generation = previous.Generation + 1
			}
		}
		if err := client.Tracker().Update(action.GetResource(), obj, action.GetNamespace()); err != nil {
			return true, nil, err
		}
		return true, obj, nil
	})
	executor, err := NewKubernetes(client, target)
	if err != nil {
		t.Fatal(err)
	}
	return fixture{executor: executor, client: client, target: target, intent: Intent{JobID: "job-1", OwnerInstanceID: "owner-1", Incarnation: "incarnation-1", SnapshotID: "snapshot-1", Revision: 2, CatalogueVersion: 2, ExpectedGeneration: 1, Generation: 2}, before: d.DeepCopy()}
}

func (f fixture) changes() []Change {
	return []Change{{Variable: AdmissionBudget, Value: "512"}, {Variable: Listen, Value: "0.0.0.0:9080"}, {Variable: OperationalListen, Value: "0.0.0.0:9081"}, {Variable: BackupDirectory, Value: "primary"}}
}
func (f fixture) deployment(t *testing.T) *appsv1.Deployment {
	t.Helper()
	d, err := f.client.AppsV1().Deployments(f.target.Namespace).Get(t.Context(), f.target.Deployment, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func (f fixture) secret(t *testing.T, name string) *corev1.Secret {
	t.Helper()
	s, err := f.client.CoreV1().Secrets(f.target.Namespace).Get(t.Context(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func (f fixture) prepare(t *testing.T) *Plan {
	t.Helper()
	p, err := f.executor.Prepare(t.Context(), f.intent, f.changes())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestKubernetesRolloutRequiresExactApplicationAcknowledgement(t *testing.T) {
	f := newFixture(t)
	p := f.prepare(t)
	for _, action := range f.client.Actions() {
		if action.GetVerb() != "get" {
			t.Fatal("preparation mutated deployment")
		}
	}
	receipt, err := f.executor.Submit(t.Context(), f.intent, p.Digest(), p)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Phase != RolloutRequested || receipt.ApplicationAcknowledged {
		t.Fatalf("premature completion: %+v", receipt)
	}
	d := f.deployment(t)
	c := container(d, "server")
	if c.Image != container(f.before, "server").Image || d.Spec.Template.Spec.ServiceAccountName != f.before.Spec.Template.Spec.ServiceAccountName || digest(d.Spec.Template.Spec.Volumes) != digest(f.before.Spec.Template.Spec.Volumes) {
		t.Fatal("rollout changed unrelated privileged fields")
	}
	if c.Args[1] != "--listen=0.0.0.0:9080" || c.Args[3] != "0.0.0.0:9081" || c.Ports[0].ContainerPort != 9080 || c.Ports[1].ContainerPort != 9081 {
		t.Fatal("listener args and named ports did not move together")
	}
	for _, name := range []string{"request", "rollback", "receipt"} {
		raw := mustJSON(f.secret(t, name).Data)
		if strings.Contains(string(raw), "UNRELATED_CANARY_DO_NOT_COPY") {
			t.Fatalf("copied unrelated secret to %s", name)
		}
	}
	observed, err := f.executor.Observe(t.Context(), f.intent, p.Digest(), nil)
	if err != nil || observed.Phase != RolloutRequested {
		t.Fatalf("zero unavailable replicas falsely proved rollout: %+v %v", observed, err)
	}
	d.Status = appsv1.DeploymentStatus{ObservedGeneration: d.Generation, Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1, ReadyReplicas: 1}
	if _, err := f.client.AppsV1().Deployments("hikyo").UpdateStatus(t.Context(), d, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	observed, err = f.executor.Observe(t.Context(), f.intent, p.Digest(), nil)
	if err != nil || observed.Phase != RolloutReady || observed.ApplicationAcknowledged {
		t.Fatalf("availability became application ack: %+v %v", observed, err)
	}
	ack := &ApplicationAcknowledgement{Intent: f.intent, PlanDigest: p.Digest(), DeploymentUID: f.target.DeploymentUID, ReadyReplicas: 1}
	ack.Intent.Generation++
	if _, err := f.executor.Observe(t.Context(), f.intent, p.Digest(), ack); !errors.Is(err, ErrConflict) {
		t.Fatalf("wrong generation ack: %v", err)
	}
	ack.Intent = f.intent
	observed, err = f.executor.Observe(t.Context(), f.intent, p.Digest(), ack)
	if err != nil || observed.Phase != Applied || !observed.ApplicationAcknowledged {
		t.Fatalf("exact ack: %+v %v", observed, err)
	}
	for range 2 {
		observed, err = f.executor.Submit(t.Context(), f.intent, p.Digest(), nil)
		if err != nil || observed.Phase != Applied {
			t.Fatalf("completed retry regressed: %+v %v", observed, err)
		}
	}
	observed, err = f.executor.Observe(t.Context(), f.intent, p.Digest(), nil)
	if err != nil || observed.Phase != Applied {
		t.Fatalf("observing without new ack regressed completion: %+v %v", observed, err)
	}
}

func TestKubernetesRolloutPersistsBeforeMutationAndResumesLostReplies(t *testing.T) {
	for _, failTarget := range []string{"request", "rollback", "receipt", "config", "hikyo"} {
		t.Run(failTarget, func(t *testing.T) {
			f := newFixture(t)
			p := f.prepare(t)
			failed := false
			// Commit the selected write but lose its response. A new executor must
			// use persisted identity and before-images, not replay stale RV writes.
			f.client.PrependReactor("update", "*", func(action ktesting.Action) (bool, runtime.Object, error) {
				update := action.(ktesting.UpdateAction)
				obj := update.GetObject()
				a, _ := meta.Accessor(obj)
				if a.GetName() == "hikyo" {
					for name, key := range map[string]string{"request": requestKey, "rollback": rollbackKey, "receipt": receiptKey} {
						s, err := f.client.Tracker().Get(corev1.SchemeGroupVersion.WithResource("secrets"), "hikyo", name)
						if err != nil || len(s.(*corev1.Secret).Data[key]) == 0 {
							t.Fatalf("deployment changed before durable %s", name)
						}
					}
				}
				if failed || a.GetName() != failTarget {
					return false, nil, nil
				}
				failed = true
				copy := obj.DeepCopyObject()
				acc, _ := meta.Accessor(copy)
				n, _ := strconv.Atoi(acc.GetResourceVersion())
				acc.SetResourceVersion(strconv.Itoa(n + 1))
				if d, ok := copy.(*appsv1.Deployment); ok {
					d.Generation++
				}
				if err := f.client.Tracker().Update(action.GetResource(), copy, action.GetNamespace()); err != nil {
					t.Fatal(err)
				}
				return true, nil, errors.New("backend error contains SECRET_CANARY")
			})
			if _, err := f.executor.Submit(t.Context(), f.intent, p.Digest(), p); !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "SECRET_CANARY") {
				t.Fatalf("lost reply not safely reported: %v", err)
			}
			restarted, err := NewKubernetes(f.client, f.target)
			if err != nil {
				t.Fatal(err)
			}
			out, err := restarted.Submit(t.Context(), f.intent, p.Digest(), nil)
			if err != nil || out.Phase != RolloutRequested {
				t.Fatalf("restart failed: %+v %v", out, err)
			}
			if digest(f.deployment(t).Spec) != p.data.AfterSpecDigest {
				t.Fatal("restart did not converge exact plan")
			}
		})
	}
}

func TestKubernetesSubmitAllowsOnlyStatusResourceVersionDrift(t *testing.T) {
	f := newFixture(t)
	plan := f.prepare(t)
	deployment := f.deployment(t)
	deployment.Status = appsv1.DeploymentStatus{ObservedGeneration: deployment.Generation, Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1, ReadyReplicas: 1}
	if _, err := f.client.AppsV1().Deployments("hikyo").UpdateStatus(t.Context(), deployment, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.executor.Submit(t.Context(), f.intent, plan.Digest(), plan); err != nil {
		t.Fatalf("status-only resource version churn stranded an authorized plan: %v", err)
	}
	if digest(f.deployment(t).Spec) != plan.data.AfterSpecDigest {
		t.Fatal("status-only retry did not install the exact authorized specification")
	}
}

func TestKubernetesSubmitStillPinsDeploymentAuthorityAndModuleVersions(t *testing.T) {
	metadataChanges := map[string]func(*appsv1.Deployment){
		"labels":  func(d *appsv1.Deployment) { d.Labels = map[string]string{"owner": "different"} },
		"custody": func(d *appsv1.Deployment) { d.Annotations = map[string]string{custodyAnnotation: "different-epoch"} },
		"owner-reference": func(d *appsv1.Deployment) {
			d.OwnerReferences = []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: "foreign", UID: "foreign"}}
		},
		"finalizer":     func(d *appsv1.Deployment) { d.Finalizers = []string{"foreign.example/retained"} },
		"deletion":      func(d *appsv1.Deployment) { at := metav1.Now(); d.DeletionTimestamp = &at },
		"specification": func(d *appsv1.Deployment) { d.Spec.Template.Spec.Containers[0].Image = "foreign.example/image" },
	}
	for name, mutate := range metadataChanges {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			plan := f.prepare(t)
			deployment := f.deployment(t)
			mutate(deployment)
			if _, err := f.client.AppsV1().Deployments("hikyo").Update(t.Context(), deployment, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			if _, err := f.executor.Submit(t.Context(), f.intent, plan.Digest(), plan); !errors.Is(err, ErrConflict) {
				t.Fatalf("changed deployment authority accepted: %v", err)
			}
			for _, module := range []string{"request", "rollback", "receipt", "config"} {
				if len(f.secret(t, module).Data) != 0 {
					t.Fatalf("refused plan modified %s custody", module)
				}
			}
		})
	}
	for _, module := range []string{"request", "rollback", "receipt", "config"} {
		t.Run(module+"-version", func(t *testing.T) {
			f := newFixture(t)
			plan := f.prepare(t)
			secret := f.secret(t, module)
			secret.Annotations = map[string]string{"changed": "outside-plan"}
			if _, err := f.client.CoreV1().Secrets("hikyo").Update(t.Context(), secret, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			if _, err := f.executor.Submit(t.Context(), f.intent, plan.Digest(), plan); !errors.Is(err, ErrConflict) {
				t.Fatalf("changed module version accepted: %v", err)
			}
			if digest(f.deployment(t).Spec) != plan.data.BeforeSpecDigest {
				t.Fatal("refused module drift changed deployment inputs")
			}
		})
	}
}

func TestKubernetesRolloutRefusesDriftAndUnsafeValues(t *testing.T) {
	for _, change := range []Change{{Variable: "HIKYO_ROOT_KEY", Value: "SECRET_CANARY"}, {Variable: "HIKYO_DB", Value: "postgres://SECRET_CANARY"}, {Variable: "HIKYO_UPGRADE_LEGACY_WRITERS_STOPPED", Value: "true"}, {Variable: "HIKYO_DEV_SERVICE_BUDGETS_DISABLED", Value: "true"}, {Variable: BackupDirectory, Value: "/arbitrary/path"}, {Variable: Listen, Value: "https://arbitrary.example"}, {Variable: AdmissionBudget, Value: "1\nSECRET_CANARY"}} {
		t.Run(string(change.Variable)+change.Value[:1], func(t *testing.T) {
			f := newFixture(t)
			if _, err := f.executor.Prepare(t.Context(), f.intent, []Change{change}); err == nil || strings.Contains(err.Error(), "SECRET_CANARY") {
				t.Fatalf("unsafe change: %v", err)
			}
		})
	}
	t.Run("resource-version", func(t *testing.T) {
		f := newFixture(t)
		p := f.prepare(t)
		d := f.deployment(t)
		d.Annotations = map[string]string{"external": "changed"}
		if _, err := f.client.AppsV1().Deployments("hikyo").Update(t.Context(), d, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.executor.Submit(t.Context(), f.intent, p.Digest(), p); !errors.Is(err, ErrConflict) {
			t.Fatalf("stale preparation accepted: %v", err)
		}
		if len(f.secret(t, "config").Data) != 0 {
			t.Fatal("stale preparation changed config")
		}
	})
	t.Run("uid", func(t *testing.T) {
		f := newFixture(t)
		d := f.deployment(t)
		d.UID = "replacement-deployment"
		if err := f.client.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), d, "hikyo"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.executor.Prepare(t.Context(), f.intent, f.changes()); !errors.Is(err, ErrConflict) {
			t.Fatalf("replacement UID accepted: %v", err)
		}
	})
	t.Run("immutable", func(t *testing.T) {
		f := newFixture(t)
		s := f.secret(t, "config")
		immutable := true
		s.Immutable = &immutable
		if _, err := f.client.CoreV1().Secrets("hikyo").Update(t.Context(), s, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.executor.Prepare(t.Context(), f.intent, f.changes()); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("immutable Secret accepted: %v", err)
		}
	})
}

func TestKubernetesRestoreUsesExactBeforeImagesAndRefusesUnrelatedChanges(t *testing.T) {
	f := newFixture(t)
	p := f.prepare(t)
	if _, err := f.executor.Submit(t.Context(), f.intent, p.Digest(), p); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		out, err := f.executor.Restore(t.Context(), f.intent, p.Digest())
		if err != nil || out.Phase != Restored || out.ApplicationAcknowledged {
			t.Fatalf("restore: %+v %v", out, err)
		}
	}
	if digest(f.deployment(t).Spec) != digest(f.before.Spec) || len(f.secret(t, "config").Data) != 0 {
		t.Fatal("before-images not restored exactly")
	}
	if _, err := f.executor.Submit(t.Context(), f.intent, p.Digest(), nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("restored job reapplied: %v", err)
	}
	f = newFixture(t)
	p = f.prepare(t)
	if _, err := f.executor.Submit(t.Context(), f.intent, p.Digest(), p); err != nil {
		t.Fatal(err)
	}
	d := f.deployment(t)
	container(d, "server").Image = "operator-changed-image"
	if _, err := f.client.AppsV1().Deployments("hikyo").Update(t.Context(), d, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.executor.Restore(t.Context(), f.intent, p.Digest()); !errors.Is(err, ErrConflict) {
		t.Fatalf("restore overwrote unrelated drift: %v", err)
	}
}

func TestKubernetesStoredPlanCannotWidenAuthority(t *testing.T) {
	f := newFixture(t)
	p := f.prepare(t)
	f.client.PrependReactor("update", "secrets", func(action ktesting.Action) (bool, runtime.Object, error) {
		if action.(ktesting.UpdateAction).GetObject().(*corev1.Secret).Name == "rollback" {
			return true, nil, errors.New("injected stop")
		}
		return false, nil, nil
	})
	if _, err := f.executor.Submit(t.Context(), f.intent, p.Digest(), p); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	s := f.secret(t, "request")
	var r record
	if err := decode(s.Data[requestKey], &r); err != nil {
		t.Fatal(err)
	}
	r.Plan.Delta.Environment[0].After.Name = "HIKYO_ROOT_KEY"
	r.Digest = digest(r.Plan)
	putRecord(s, requestKey, r)
	if _, err := f.client.CoreV1().Secrets("hikyo").Update(t.Context(), s, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.executor.Submit(t.Context(), f.intent, p.Digest(), nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("persisted plan widened: %v", err)
	}
	if len(f.secret(t, "config").Data) != 0 {
		t.Fatal("tampered plan changed target")
	}
}

func TestKubernetesRequiresDurablyPinnedPlanDigest(t *testing.T) {
	f := newFixture(t)
	p := f.prepare(t)
	if _, err := f.executor.Submit(t.Context(), f.intent, p.Digest(), p); err != nil {
		t.Fatal(err)
	}
	s := f.secret(t, "request")
	var r record
	if err := decode(s.Data[requestKey], &r); err != nil {
		t.Fatal(err)
	}
	// Still a well-typed plan with a valid self-hash. The durable decision,
	// rather than request-object write access, must authorize rollback material.
	stamp := "changed-before-image"
	r.Plan.Delta.BeforeStamp = &stamp
	r.Digest = digest(r.Plan)
	if !f.executor.validPlan(r.Plan) {
		t.Fatal("test requires structurally valid tampering")
	}
	putRecord(s, requestKey, r)
	if _, err := f.client.CoreV1().Secrets("hikyo").Update(t.Context(), s, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	f.client.ClearActions()
	for _, call := range []func(string) (Receipt, error){
		func(expected string) (Receipt, error) { return f.executor.Submit(t.Context(), f.intent, expected, nil) },
		func(expected string) (Receipt, error) {
			return f.executor.Observe(t.Context(), f.intent, expected, nil)
		},
		func(expected string) (Receipt, error) { return f.executor.Restore(t.Context(), f.intent, expected) },
	} {
		if _, err := call(p.Digest()); !errors.Is(err, ErrConflict) {
			t.Fatalf("editable self-hash substituted for committed digest: %v", err)
		}
		if _, err := call(""); !errors.Is(err, ErrInvalid) {
			t.Fatalf("missing durable digest accepted: %v", err)
		}
	}
	for _, action := range f.client.Actions() {
		if action.GetVerb() != "get" {
			t.Fatalf("digest refusal mutated deployment inputs: %s", action.GetVerb())
		}
	}
}

func TestKubernetesPendingRequestCannotBeReplacedBeforeReceipt(t *testing.T) {
	f := newFixture(t)
	p := f.prepare(t)
	f.client.PrependReactor("update", "secrets", func(action ktesting.Action) (bool, runtime.Object, error) {
		if action.(ktesting.UpdateAction).GetObject().(*corev1.Secret).Name == "rollback" {
			return true, nil, errors.New("stop before receipt")
		}
		return false, nil, nil
	})
	if _, err := f.executor.Submit(t.Context(), f.intent, p.Digest(), p); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	other := f.intent
	other.JobID = "job-2"
	other.SnapshotID = "snapshot-2"
	p2, err := f.executor.Prepare(t.Context(), other, f.changes())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.executor.Submit(t.Context(), other, p2.Digest(), p2); !errors.Is(err, ErrConflict) {
		t.Fatalf("pending request replaced: %v", err)
	}
}

func TestKubernetesClientUsesOnlyInstalledNamedResources(t *testing.T) {
	f := newFixture(t)
	p := f.prepare(t)
	if _, err := f.executor.Submit(context.Background(), f.intent, p.Digest(), p); err != nil {
		t.Fatal(err)
	}
	for _, action := range f.client.Actions() {
		if action.GetNamespace() != f.target.Namespace || action.GetVerb() != "get" && action.GetVerb() != "update" {
			t.Fatalf("broad Kubernetes action: %s %s", action.GetVerb(), action.GetResource().Resource)
		}
	}
}
