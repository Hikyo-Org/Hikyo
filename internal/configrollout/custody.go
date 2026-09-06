package configrollout

import (
	"context"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	typedapps "k8s.io/client-go/kubernetes/typed/apps/v1"
	typedcore "k8s.io/client-go/kubernetes/typed/core/v1"
)

const custodyAnnotation = "hikyo.dev/rollout-custody"

// custody never expires. A replacement may acquire only when the same fixed
// StatefulSet Pod name now has its own distinct UID. Normal StatefulSet
// termination ordering is required: force deletion while the old process runs
// invalidates that Kubernetes guarantee and is unsupported.
type custody struct {
	client     kubernetes.Interface
	enrollment Enrollment
	podUID     types.UID
	epoch      int32
}

func (c *custody) identity() string { return c.enrollment.ExecutorPod + ":" + string(c.podUID) }

func (c *custody) ownPod(ctx context.Context) error {
	pod, err := c.client.CoreV1().Pods(c.enrollment.Target.Namespace).Get(ctx, c.enrollment.ExecutorPod, metav1.GetOptions{})
	if err != nil {
		return apiError(err)
	}
	if pod.UID != c.podUID || pod.DeletionTimestamp != nil || pod.Spec.NodeName == "" {
		return ErrConflict
	}
	return nil
}

func (c *custody) acquire(ctx context.Context) error {
	if c.podUID == "" {
		return ErrInvalid
	}
	if err := c.ownPod(ctx); err != nil {
		return err
	}
	e := c.enrollment
	lease, err := c.client.CoordinationV1().Leases(e.Target.Namespace).Get(ctx, e.LeaseName, metav1.GetOptions{})
	if err != nil {
		return apiError(err)
	}
	if lease.UID != e.LeaseUID {
		return ErrConflict
	}
	if lease.Spec.LeaseDurationSeconds != nil {
		return ErrConflict
	}
	identity := c.identity()
	if lease.Spec.HolderIdentity != nil && *lease.Spec.HolderIdentity != "" && *lease.Spec.HolderIdentity != identity {
		// No other Pod name can gain custody. ownPod proved the previous exact
		// UID is absent, even if its lease says it renewed many months ago.
		if !strings.HasPrefix(*lease.Spec.HolderIdentity, e.ExecutorPod+":") {
			return ErrConflict
		}
	}
	epoch := int32(0)
	if lease.Spec.LeaseTransitions != nil {
		epoch = *lease.Spec.LeaseTransitions
	}
	if lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity != identity {
		if epoch < 0 || epoch == 2147483647 {
			return ErrConflict
		}
		epoch++
		lease.Spec.HolderIdentity = &identity
		lease.Spec.LeaseTransitions = &epoch
		now := metav1.NewMicroTime(time.Now().UTC())
		lease.Spec.AcquireTime = &now
		lease.Spec.RenewTime = &now
		if _, err = c.client.CoordinationV1().Leases(e.Target.Namespace).Update(ctx, lease, metav1.UpdateOptions{}); err != nil {
			return apiError(err)
		}
	}
	c.epoch = epoch
	return c.verify(ctx)
}

func (c *custody) verify(ctx context.Context) error {
	if err := c.ownPod(ctx); err != nil {
		return err
	}
	e := c.enrollment
	lease, err := c.client.CoordinationV1().Leases(e.Target.Namespace).Get(ctx, e.LeaseName, metav1.GetOptions{})
	if err != nil {
		return apiError(err)
	}
	if lease.UID != e.LeaseUID || lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity != c.identity() || lease.Spec.LeaseTransitions == nil || *lease.Spec.LeaseTransitions != c.epoch || lease.Spec.LeaseDurationSeconds != nil {
		return ErrConflict
	}
	return nil
}

// Run owns the singleton's non-expiring custody until process termination. It
// never releases on cancellation: the Pod UID must disappear before takeover.
func (c *Controller) Run(ctx context.Context, podUID types.UID) error {
	owner := &custody{client: c.client, enrollment: c.enrollment, podUID: podUID}
	if err := owner.acquire(ctx); err != nil {
		return err
	}
	for {
		if err := owner.verify(ctx); err != nil {
			return err
		}
		if c.proveAdmission(ctx, owner) == nil {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	guarded := custodyClient{Interface: c.client, owner: owner}
	executor, err := NewKubernetes(guarded, c.enrollment.Target)
	if err != nil {
		return err
	}
	c.client = guarded
	c.executor = executor
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := owner.verify(ctx); err != nil {
			return err
		}
		// A malformed mailbox cannot stop the executor or widen its authority.
		// The server observes its own bounded timeout and can issue a new command.
		_ = c.reconcile(ctx)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// API registration and policy-cache activation are separate. Probe denial of
// forbidden dry-run updates before accepting any real mutation. This uses the
// same resourceNames-limited client, without policy-listing authority.
func (c *Controller) proveAdmission(ctx context.Context, owner *custody) error {
	d, secrets, err := c.executor.get(ctx)
	if err != nil {
		return err
	}
	if d.Annotations == nil {
		d.Annotations = map[string]string{}
	}
	d.Annotations[custodyAnnotation] = owner.identity() + ":" + strconv.Itoa(int(owner.epoch))
	container(d, c.enrollment.Target.Container).Image = "invalid.example/hikyo-admission-probe:v1"
	_, err = c.client.AppsV1().Deployments(c.enrollment.Target.Namespace).Update(ctx, d, metav1.UpdateOptions{DryRun: []string{metav1.DryRunAll}})
	if !apierrors.IsInvalid(err) || !strings.Contains(err.Error(), "Container authority outside configuration fields is immutable.") {
		return ErrUnavailable
	}
	s := secrets[c.enrollment.Target.ConfigSecret]
	if s.Annotations == nil {
		s.Annotations = map[string]string{}
	}
	s.Annotations[custodyAnnotation] = "invalid-admission-probe"
	_, err = c.client.CoreV1().Secrets(c.enrollment.Target.Namespace).Update(ctx, s, metav1.UpdateOptions{DryRun: []string{metav1.DryRunAll}})
	if !apierrors.IsInvalid(err) || !strings.Contains(err.Error(), "Executor write must carry the current custody epoch.") {
		return ErrUnavailable
	}
	return nil
}

type custodyClient struct {
	kubernetes.Interface
	owner *custody
}

func (c custodyClient) AppsV1() typedapps.AppsV1Interface {
	return custodyApps{AppsV1Interface: c.Interface.AppsV1(), owner: c.owner}
}
func (c custodyClient) CoreV1() typedcore.CoreV1Interface {
	return custodyCore{CoreV1Interface: c.Interface.CoreV1(), owner: c.owner}
}

type custodyApps struct {
	typedapps.AppsV1Interface
	owner *custody
}

func (c custodyApps) Deployments(ns string) typedapps.DeploymentInterface {
	return custodyDeployments{DeploymentInterface: c.AppsV1Interface.Deployments(ns), owner: c.owner}
}

type custodyDeployments struct {
	typedapps.DeploymentInterface
	owner *custody
}

func (c custodyDeployments) Update(ctx context.Context, d *appsv1.Deployment, options metav1.UpdateOptions) (*appsv1.Deployment, error) {
	if err := c.owner.verify(ctx); err != nil {
		return nil, err
	}
	d = d.DeepCopy()
	if d.Annotations == nil {
		d.Annotations = map[string]string{}
	}
	d.Annotations[custodyAnnotation] = c.owner.identity() + ":" + strconv.Itoa(int(c.owner.epoch))
	return c.DeploymentInterface.Update(ctx, d, options)
}

type custodyCore struct {
	typedcore.CoreV1Interface
	owner *custody
}

func (c custodyCore) Secrets(ns string) typedcore.SecretInterface {
	return custodySecrets{SecretInterface: c.CoreV1Interface.Secrets(ns), owner: c.owner}
}

type custodySecrets struct {
	typedcore.SecretInterface
	owner *custody
}

func (c custodySecrets) Update(ctx context.Context, s *corev1.Secret, options metav1.UpdateOptions) (*corev1.Secret, error) {
	if err := c.owner.verify(ctx); err != nil {
		return nil, err
	}
	s = s.DeepCopy()
	if s.Annotations == nil {
		s.Annotations = map[string]string{}
	}
	s.Annotations[custodyAnnotation] = c.owner.identity() + ":" + strconv.Itoa(int(c.owner.epoch))
	return c.SecretInterface.Update(ctx, s, options)
}
