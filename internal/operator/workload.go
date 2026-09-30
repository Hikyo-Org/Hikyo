package operator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	hikyov1 "github.com/Hikyo-Org/hikyo/internal/operator/api/v1alpha1"
)

// workloadHandler maps a changed Deployment/StatefulSet/DaemonSet to the
// HikyoSecrets in its namespace whose target it names via the hikyo.dev/secrets
// opt-in annotation, so a rollout that stalls after a stamp patch is observed
// from the workload controller's own status (§ 0.3) rather than only on resync.
func (r *HikyoSecretReconciler) workloadHandler() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		list, ok := obj.GetAnnotations()[hikyov1.AnnotationWorkloadSecrets]
		if !ok {
			return nil
		}
		named := map[string]bool{}
		for _, n := range strings.Split(list, ",") {
			if n = strings.TrimSpace(n); n != "" {
				named[n] = true
			}
		}
		if len(named) == 0 {
			return nil
		}
		var secrets hikyov1.HikyoSecretList
		if err := r.Client.List(ctx, &secrets, client.InNamespace(obj.GetNamespace())); err != nil {
			if r.Log != nil {
				r.Log.Error("workload handler: list HikyoSecrets failed", "namespace", obj.GetNamespace(), "err", err)
			}
			return nil
		}
		var reqs []reconcile.Request
		for i := range secrets.Items {
			cr := &secrets.Items[i]
			if named[cr.Spec.Target.Name] {
				reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(cr)})
			}
		}
		return reqs
	})
}

// patchWorkloads applies § 0.5 step 2: for each Deployment/StatefulSet/DaemonSet
// in the CR's namespace whose hikyo.dev/secrets annotation names this target and
// whose current pod-template stamp differs, strategic-merge patch the stamp
// annotation. It is gated by TRIGGER_ROLLOUTS.
//
// It returns the names of already-stamped-but-not-progressed workloads
// (Rollout=False/Stalled is informational and does NOT block the cursor), and an
// error only on an actual patch/list failure (which DOES block the cursor, so
// the next reconcile re-attempts).
//
// Rollout status is evaluated only for workloads that did NOT need a patch this
// reconcile: a workload freshly patched here has just had its generation bumped
// and would always read as "not yet progressed", which is why the ADR evaluates
// it on the NEXT reconcile.
func (r *HikyoSecretReconciler) patchWorkloads(ctx context.Context, cr *hikyov1.HikyoSecret, stamp string) ([]string, error) {
	if !r.Config.TriggerRollouts {
		return nil, nil
	}
	return r.walkWorkloads(ctx, cr, stamp, true)
}

// observeRollout is the READ-ONLY rollout evaluation used on the `current` path
// (§ 0.3/decision 8): for each opted-in workload already carrying this target's
// stamp, report whether the workload controller's own status shows it
// progressed. It NEVER patches — a current answer writes nothing. stamp is the
// recorded status.stamp. Gated by TRIGGER_ROLLOUTS.
func (r *HikyoSecretReconciler) observeRollout(ctx context.Context, cr *hikyov1.HikyoSecret, stamp string) ([]string, error) {
	if !r.Config.TriggerRollouts || stamp == "" {
		return nil, nil
	}
	return r.walkWorkloads(ctx, cr, stamp, false)
}

type rolloutWorkloadKind struct {
	name       string
	list       client.ObjectList
	count      func() int
	workloadAt func(int) rolloutWorkload
}

type rolloutWorkload struct {
	object              client.Object
	templateAnnotations map[string]string
	progressed          bool
}

// rolloutWorkloadKinds adapts each Kubernetes workload list to the one walk
// that owns opt-in, stamp, progress, ordering, and patch-error behavior.
func rolloutWorkloadKinds() []rolloutWorkloadKind {
	deployments := &appsv1.DeploymentList{}
	statefulSets := &appsv1.StatefulSetList{}
	daemonSets := &appsv1.DaemonSetList{}

	return []rolloutWorkloadKind{
		{
			name:  "Deployment",
			list:  deployments,
			count: func() int { return len(deployments.Items) },
			workloadAt: func(i int) rolloutWorkload {
				workload := &deployments.Items[i]
				return rolloutWorkload{
					object:              workload,
					templateAnnotations: workload.Spec.Template.Annotations,
					progressed:          deploymentProgressed(workload),
				}
			},
		},
		{
			name:  "StatefulSet",
			list:  statefulSets,
			count: func() int { return len(statefulSets.Items) },
			workloadAt: func(i int) rolloutWorkload {
				workload := &statefulSets.Items[i]
				return rolloutWorkload{
					object:              workload,
					templateAnnotations: workload.Spec.Template.Annotations,
					progressed:          statefulSetProgressed(workload),
				}
			},
		},
		{
			name:  "DaemonSet",
			list:  daemonSets,
			count: func() int { return len(daemonSets.Items) },
			workloadAt: func(i int) rolloutWorkload {
				workload := &daemonSets.Items[i]
				return rolloutWorkload{
					object:              workload,
					templateAnnotations: workload.Spec.Template.Annotations,
					progressed:          daemonSetProgressed(workload),
				}
			},
		},
	}
}

// walkWorkloads owns the common workload invariant. patch requests rollouts for
// stale stamps; observe-only walks report stalls without writing.
func (r *HikyoSecretReconciler) walkWorkloads(ctx context.Context, cr *hikyov1.HikyoSecret, stamp string, patch bool) ([]string, error) {
	annKey := hikyov1.StampAnnotationPrefix + cr.Spec.Target.Name
	var stalled []string

	for _, kind := range rolloutWorkloadKinds() {
		if err := r.List(ctx, kind.list, client.InNamespace(cr.Namespace)); err != nil {
			return nil, err
		}
		for i := 0; i < kind.count(); i++ {
			workload := kind.workloadAt(i)
			if !consumesTarget(workload.object.GetAnnotations(), cr.Spec.Target.Name) {
				continue
			}
			if podAnnotation(workload.templateAnnotations, annKey) == stamp {
				if !workload.progressed {
					stalled = append(stalled, kind.name+"/"+workload.object.GetName())
				}
				continue
			}
			if !patch {
				continue
			}
			if err := r.patchPodTemplateAnnotation(ctx, workload.object, annKey, stamp); err != nil {
				return nil, fmt.Errorf("patch %s %q: %w", kind.name, workload.object.GetName(), err)
			}
		}
	}

	return stalled, nil
}

// patchPodTemplateAnnotation writes the stamp into the pod template annotation
// with a strategic-merge patch — the minimal mutation that requests a rollout
// under the workload's own update strategy.
func (r *HikyoSecretReconciler) patchPodTemplateAnnotation(ctx context.Context, obj client.Object, annKey, stamp string) error {
	fresh, ok := obj.DeepCopyObject().(client.Object)
	if !ok {
		return errors.New("workload object cannot be copied for an authoritative consent check")
	}
	reader := r.Reader
	if reader == nil {
		reader = r.Client
	}
	if err := reader.Get(ctx, client.ObjectKeyFromObject(obj), fresh); err != nil {
		return err
	}
	targets := fresh.GetAnnotations()[hikyov1.AnnotationWorkloadSecrets]
	if fresh.GetUID() != obj.GetUID() || !consumesTarget(fresh.GetAnnotations(), strings.TrimPrefix(annKey, hikyov1.StampAnnotationPrefix)) {
		return errors.New("workload identity or rollout consent changed before patch")
	}
	type operation struct {
		Op    string `json:"op"`
		Path  string `json:"path"`
		Value any    `json:"value"`
	}
	operations := []operation{
		{Op: "test", Path: "/metadata/uid", Value: string(fresh.GetUID())},
		{Op: "test", Path: "/metadata/resourceVersion", Value: fresh.GetResourceVersion()},
		{Op: "test", Path: "/metadata/annotations/hikyo.dev~1secrets", Value: targets},
	}
	annotations := podTemplateAnnotations(fresh)
	if annotations == nil {
		operations = append(operations, operation{Op: "add", Path: "/spec/template/metadata/annotations", Value: map[string]string{annKey: stamp}})
	} else {
		escaped := strings.NewReplacer("~", "~0", "/", "~1").Replace(annKey)
		operations = append(operations, operation{Op: "add", Path: "/spec/template/metadata/annotations/" + escaped, Value: stamp})
	}
	patch, err := json.Marshal(operations)
	if err != nil {
		return err
	}
	return r.Patch(ctx, fresh, client.RawPatch(types.JSONPatchType, patch))
}

func podTemplateAnnotations(obj client.Object) map[string]string {
	switch workload := obj.(type) {
	case *appsv1.Deployment:
		return workload.Spec.Template.Annotations
	case *appsv1.StatefulSet:
		return workload.Spec.Template.Annotations
	case *appsv1.DaemonSet:
		return workload.Spec.Template.Annotations
	default:
		return nil
	}
}

// consumesTarget reports whether a workload's hikyo.dev/secrets annotation names
// this managed Secret (the workload's opt-in consent to be rolled).
func consumesTarget(annotations map[string]string, target string) bool {
	list, ok := annotations[hikyov1.AnnotationWorkloadSecrets]
	if !ok {
		return false
	}
	for _, name := range strings.Split(list, ",") {
		if strings.TrimSpace(name) == target {
			return true
		}
	}
	return false
}

func podAnnotation(annotations map[string]string, key string) string {
	if annotations == nil {
		return ""
	}
	return annotations[key]
}

// deploymentProgressed reports whether the Deployment has observed its latest
// generation and every desired replica is updated and available.
func deploymentProgressed(d *appsv1.Deployment) bool {
	desired := int32(1)
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	if desired == 0 {
		return d.Status.ObservedGeneration >= d.Generation && d.Status.Replicas == 0
	}
	return !d.Spec.Paused && d.Status.ObservedGeneration >= d.Generation &&
		d.Status.Replicas == desired && d.Status.UpdatedReplicas == desired &&
		d.Status.AvailableReplicas == desired && d.Status.UnavailableReplicas == 0
}

func statefulSetProgressed(s *appsv1.StatefulSet) bool {
	desired := int32(1)
	if s.Spec.Replicas != nil {
		desired = *s.Spec.Replicas
	}
	if desired == 0 {
		return s.Status.ObservedGeneration >= s.Generation && s.Status.Replicas == 0
	}
	return s.Status.ObservedGeneration >= s.Generation && s.Status.Replicas == desired &&
		s.Status.UpdatedReplicas == desired && s.Status.ReadyReplicas == desired &&
		s.Status.CurrentRevision != "" && s.Status.CurrentRevision == s.Status.UpdateRevision
}

func daemonSetProgressed(d *appsv1.DaemonSet) bool {
	desired := d.Status.DesiredNumberScheduled
	return d.Status.ObservedGeneration >= d.Generation &&
		d.Status.UpdatedNumberScheduled == desired && d.Status.NumberAvailable == desired &&
		d.Status.NumberUnavailable == 0
}
