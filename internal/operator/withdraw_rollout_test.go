package operator

import (
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	hikyov1 "github.com/Hikyo-Org/hikyo/internal/operator/api/v1alpha1"
)

func TestWithdrawalRetainsStalledRolloutUntilProgress(t *testing.T) {
	cr := makeCR("app")
	owned := makeOwnedSecret(t, testScheme(t), cr, map[string][]byte{"API_KEY": []byte("withdraw-me")})
	web := makeOptedInDeployment("web", testTarget)
	web.Spec.Paused = true
	database := makeOptedInStatefulSet("database", testTarget)
	database.Spec.UpdateStrategy.Type = appsv1.OnDeleteStatefulSetStrategyType
	h := newHarness(t, interceptor.Funcs{}, makeInstance(""), makeBootstrapSecret("boot", testInstance, "tok", true), owned, web, database, cr)
	h.stub.set(404, `{"error":{"code":"not_found"}}`)
	// The first withdrawal requests the rollout. Later observations must not
	// erase the stall merely because both templates already carry empty stamps.
	for i := 0; i < 3; i++ {
		if _, err := h.reconcile("app"); err != nil {
			t.Fatalf("withdrawal %d: %v", i, err)
		}
		got := h.getCR("app")
		requireCond(t, got, hikyov1.ConditionScrubbed, metav1.ConditionTrue, hikyov1.ReasonAuthorizationWithdrawn)
		if i > 0 {
			requireCond(t, got, hikyov1.ConditionRollout, metav1.ConditionFalse, hikyov1.ReasonStalled)
			cond := meta.FindStatusCondition(got.Status.Conditions, hikyov1.ConditionRollout)
			if !strings.Contains(cond.Message, "Deployment/web") || !strings.Contains(cond.Message, "StatefulSet/database") {
				t.Fatalf("stalled consumers missing: %s", cond.Message)
			}
		}
		sec, exists := h.getSecret(testNS, testTarget)
		if !exists || len(sec.Data) != 0 || got.Status.Cursor != "" {
			t.Fatal("withdrawal did not preserve empty Secret and cleared cursor")
		}
	}
	web = h.getDeployment("web")
	web.Spec.Paused = false
	if err := h.cl.Update(t.Context(), web); err != nil {
		t.Fatal(err)
	}
	web = h.getDeployment("web")
	web.Status = appsv1.DeploymentStatus{ObservedGeneration: web.Generation, Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1}
	if err := h.cl.Status().Update(t.Context(), web); err != nil {
		t.Fatal(err)
	}
	database = h.getStatefulSet("database")
	database.Status = appsv1.StatefulSetStatus{ObservedGeneration: database.Generation, Replicas: 1, UpdatedReplicas: 1, ReadyReplicas: 1, CurrentRevision: "empty", UpdateRevision: "empty"}
	if err := h.cl.Status().Update(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if _, err := h.reconcile("app"); err != nil {
		t.Fatal(err)
	}
	if cond := meta.FindStatusCondition(h.getCR("app").Status.Conditions, hikyov1.ConditionRollout); cond != nil {
		t.Fatalf("progressed withdrawal retained stale rollout condition: %+v", cond)
	}
}
