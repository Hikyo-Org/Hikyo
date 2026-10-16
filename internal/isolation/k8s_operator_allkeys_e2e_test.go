//go:build k8se2e

package isolation

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	hikyov1 "github.com/Hikyo-Org/hikyo/internal/operator/api/v1alpha1"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// Exercise the complete publication -> manager -> Secret -> rollout -> process
// environment path. A new application setting needs no CR or Deployment edit.
func testAllKeys(t *testing.T, restCfg *rest.Config, sch *runtime.Scheme) {
	e := newOpEnv(t, restCfg, sch, false)
	e.createInstance(instanceName, "")
	sa, cred := e.newWorkloadCredential("allkeys")
	grantE2ERead(t, e.db, sa.Principal)
	seedE2EReveal(t, e.db, "g_allkeys_reveal", sa.Principal, domain.CapReveal)
	e.createBootstrapSecret("boot", cred.Value, instanceName, true)

	newCR := func(name string) *hikyov1.HikyoSecret {
		return &hikyov1.HikyoSecret{
			ObjectMeta: metav1.ObjectMeta{Namespace: e.ns, Name: name},
			Spec: hikyov1.HikyoSecretSpec{
				InstanceRef: hikyov1.InstanceRef{Name: instanceName},
				Auth:        hikyov1.AuthRef{SecretRef: &hikyov1.LocalObjectRef{Name: "boot"}},
				Scope:       hikyov1.Scope{Org: e2eOrg, Project: e2ePrj, Environment: e2eEnv},
				AllKeys:     true, Target: hikyov1.Target{Name: name}, ResyncInterval: "30s",
			},
		}
	}
	for _, tc := range []struct {
		name   string
		mutate func(*hikyov1.HikyoSecret)
	}{
		{"both-selectors", func(cr *hikyov1.HikyoSecret) { cr.Spec.Mapping = []hikyov1.Mapping{{Key: cfgKeyOne}} }},
		{"neither-selector", func(cr *hikyov1.HikyoSecret) { cr.Spec.AllKeys = false }},
		{"native-type", func(cr *hikyov1.HikyoSecret) { cr.Spec.Target.Type = corev1.SecretTypeTLS }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cr := newCR(tc.name)
			tc.mutate(cr)
			if err := e.cl.Create(t.Context(), cr); !apierrors.IsInvalid(err) {
				t.Fatalf("API admission error = %v, want Invalid", err)
			}
		})
	}
	cr := newCR("allkeys-secret")
	must(t, e.cl.Create(t.Context(), cr))
	initialCRGeneration := cr.Generation
	want := map[string]string{cfgKeyOne: cfgValOne, cfgKeyTwo: cfgValTwo, secKeyOne: secValOne, secKeyTwo: secValTwo}

	one := int32(1)
	labels := map[string]string{"app": "allkeys-consumer"}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "allkeys-consumer", Namespace: e.ns,
			Annotations: map[string]string{hikyov1.AnnotationWorkloadSecrets: cr.Name}},
		Spec: appsv1.DeploymentSpec{
			Replicas: &one, Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{
					Name: "consumer", Image: "busybox:1.37.0", ImagePullPolicy: corev1.PullIfNotPresent,
					Command: []string{"/bin/sh", "-c", "env; exec sleep 86400"},
					EnvFrom: []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: cr.Name}}}},
				}}},
			},
		},
	}
	must(t, e.cl.Create(t.Context(), deployment))
	e.startManager()
	e.waitCondition(cr.Name, hikyov1.ConditionReady, metav1.ConditionTrue, hikyov1.ReasonReconciled)
	waitAllKeysSecret(t, e, cr.Name, want)
	initialPod := waitAllKeysConsumer(t, e, deployment.Name, cr.Name, want)

	const newKey = "CATALOG_UPDATE_URL"
	_, err := keySvc(t, e.db).Create(t.Context(), service.LocalPrincipal(identAdmin), e2eScopePrj(), service.KeySpec{
		Name: newKey, Classification: string(schema.Config),
		Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString}}, Presence: schema.DefaultPresenceRules(),
	}, nil)
	must(t, err)
	publishE2EValues(t, e.db, map[string]string{newKey: "https://updates.example.test/catalog"})
	want[newKey] = "https://updates.example.test/catalog"
	waitAllKeysSecret(t, e, cr.Name, want)
	addedPod := waitAllKeysConsumer(t, e, deployment.Name, cr.Name, want)
	if addedPod == initialPod {
		t.Fatal("new key did not produce a replacement consumer pod")
	}

	staged, err := valueSvc(t, e.db).Unset(t.Context(), service.LocalPrincipal(identAdmin), e2eScopeEnv(), newKey)
	must(t, err)
	_, err = revisionSvc(t, e.db).PublishPlanned(t.Context(), service.LocalPrincipal(identAdmin), e2eScopeEnv(), service.PublishRequest{VersionIDs: []string{staged.VersionID}})
	must(t, err)
	delete(want, newKey)
	waitAllKeysSecret(t, e, cr.Name, want)
	removedPod := waitAllKeysConsumer(t, e, deployment.Name, cr.Name, want, newKey)
	if removedPod == addedPod {
		t.Fatal("removed key did not produce a replacement consumer pod")
	}
	if got := e.getCR(cr.Name).Generation; got != initialCRGeneration {
		t.Fatalf("CR generation changed %d -> %d without a user edit", initialCRGeneration, got)
	}

	// Observe further successful conditional fetches, instead of sleeping and
	// assuming the manager performed a no-op reconciliation.
	generation := e.getDeployment(deployment.Name).Generation
	currentFetches := e.countCurrentFetch()
	poll(t, e.ctx, func(context.Context) (bool, error) { return e.countCurrentFetch() >= currentFetches+1, nil })
	if got := e.getDeployment(deployment.Name).Generation; got != generation {
		t.Fatalf("unchanged data triggered another rollout: generation %d -> %d", generation, got)
	}
	if got := waitAllKeysConsumer(t, e, deployment.Name, cr.Name, want, newKey); got != removedPod {
		t.Fatal("unchanged data replaced the consumer pod")
	}

	readSA, readCred := e.newWorkloadCredential("allkeys-readonly")
	grantE2ERead(t, e.db, readSA.Principal)
	e.createBootstrapSecret("boot-readonly", readCred.Value, instanceName, true)
	readOnly := newCR("readonly-secret")
	readOnly.Spec.Auth.SecretRef.Name = "boot-readonly"
	must(t, e.cl.Create(t.Context(), readOnly))
	e.waitCondition(readOnly.Name, hikyov1.ConditionDelivery, metav1.ConditionFalse, hikyov1.ReasonUndeliveredSecrets)
	if _, exists := e.getSecret(readOnly.Name); exists {
		t.Fatal("allKeys delivered a partial Secret without reveal authority")
	}

	configOnly := newCR("config-only-secret")
	configOnly.Spec.Auth.SecretRef.Name = "boot-readonly"
	configOnly.Spec.Projection = hikyov1.ProjectionConfigOnly
	must(t, e.cl.Create(t.Context(), configOnly))
	e.waitCondition(configOnly.Name, hikyov1.ConditionReady, metav1.ConditionTrue, hikyov1.ReasonReconciled)
	waitAllKeysSecret(t, e, configOnly.Name, map[string]string{cfgKeyOne: cfgValOne, cfgKeyTwo: cfgValTwo})

	// New loader controls remain an explicit consent boundary even when the
	// application opted into automatic ordinary-key delivery.
	_, err = keySvc(t, e.db).Create(t.Context(), service.LocalPrincipal(identAdmin), e2eScopePrj(), service.KeySpec{
		Name: "NODE_OPTIONS", Classification: string(schema.Config),
		Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString}}, Presence: schema.DefaultPresenceRules(),
	}, nil)
	must(t, err)
	publishE2EValues(t, e.db, map[string]string{"NODE_OPTIONS": "--trace-warnings"})
	e.waitCondition(cr.Name, hikyov1.ConditionDelivery, metav1.ConditionFalse, hikyov1.ReasonLoaderControlUnacknowledged)
	retained, exists := e.getSecret(cr.Name)
	if !exists {
		t.Fatal("loader-control refusal removed the last good Secret")
	}
	assertSecretData(t, retained, want)
	if got := e.getDeployment(deployment.Name).Generation; got != generation {
		t.Fatal("unacknowledged loader control triggered a consumer rollout")
	}
	t.Log("published key addition/removal reached real envFrom process environments through automatic rollouts; unchanged delivery kept the same pod; reveal and loader-control refusals remained enforced")
}

func waitAllKeysSecret(t *testing.T, e *opEnv, name string, want map[string]string) {
	t.Helper()
	poll(t, e.ctx, func(context.Context) (bool, error) {
		secret, exists := e.getSecret(name)
		if !exists || secret.Type != corev1.SecretTypeOpaque || len(secret.Data) != len(want) {
			return false, nil
		}
		for key, value := range want {
			if string(secret.Data[key]) != value {
				return false, nil
			}
		}
		return true, nil
	})
}

func waitAllKeysConsumer(t *testing.T, e *opEnv, deployment, secret string, want map[string]string, absent ...string) types.UID {
	t.Helper()
	var uid types.UID
	lastObservation := "no observation"
	defer func() {
		if t.Failed() {
			t.Logf("consumer wait: %s; CR conditions: %s", lastObservation, dumpConds(e.getCR(secret)))
		}
	}()
	poll(t, e.ctx, func(ctx context.Context) (bool, error) {
		d := e.getDeployment(deployment)
		lastObservation = fmt.Sprintf("deployment generation=%d observed=%d replicas=%d updated=%d available=%d stamp=%q", d.Generation, d.Status.ObservedGeneration, d.Status.Replicas, d.Status.UpdatedReplicas, d.Status.AvailableReplicas, stampOf(d, secret))
		if stampOf(d, secret) == "" || d.Status.ObservedGeneration < d.Generation || d.Status.UpdatedReplicas != 1 || d.Status.AvailableReplicas != 1 || d.Status.Replicas != 1 {
			return false, nil
		}
		var pods corev1.PodList
		if err := e.cl.List(ctx, &pods, client.InNamespace(e.ns), client.MatchingLabels{"app": deployment}); err != nil {
			return false, err
		}
		for _, pod := range pods.Items {
			lastObservation += fmt.Sprintf("; pod=%s uid=%s phase=%s deleting=%t stamp=%q", pod.Name, pod.UID, pod.Status.Phase, pod.DeletionTimestamp != nil, pod.Annotations[hikyov1.StampAnnotationPrefix+secret])
			if pod.Namespace != e.ns || pod.Labels["app"] != deployment || pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning || pod.Annotations[hikyov1.StampAnnotationPrefix+secret] != stampOf(d, secret) {
				continue
			}
			logs, err := e.cs.CoreV1().Pods(e.ns).GetLogs(pod.Name, &corev1.PodLogOptions{Container: "consumer"}).DoRaw(ctx)
			if err != nil {
				return false, fmt.Errorf("read consumer environment: %w", err)
			}
			environment := make(map[string]string)
			for _, line := range strings.Split(string(logs), "\n") {
				key, value, ok := strings.Cut(line, "=")
				if ok {
					environment[key] = value
				}
			}
			environmentKeys := make([]string, 0, len(environment))
			for key := range environment {
				environmentKeys = append(environmentKeys, key)
			}
			slices.Sort(environmentKeys)
			lastObservation += fmt.Sprintf(" environment keys=%v", environmentKeys)
			for key, value := range want {
				if environment[key] != value {
					lastObservation += fmt.Sprintf("; wrong/missing environment key=%s", key)
					return false, nil
				}
			}
			for _, key := range absent {
				if _, present := environment[key]; present {
					lastObservation += fmt.Sprintf("; unexpected environment key=%s", key)
					return false, nil
				}
			}
			uid = pod.UID
			return true, nil
		}
		return false, nil
	})
	return uid
}
