package operator

import (
	"context"
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	hikyov1 "github.com/Hikyo-Org/hikyo/internal/operator/api/v1alpha1"
)

func withAllKeys(cr *hikyov1.HikyoSecret) {
	cr.Spec.AllKeys = true
	cr.Spec.Mapping = nil
}

func TestAllKeysManifestConvergenceAndCurrent(t *testing.T) {
	cr := makeCR("app", withAllKeys)
	h := newHarness(t, interceptor.Funcs{}, makeInstance(""), makeBootstrapSecret("boot", testInstance, "tok", true), makeOptedInDeployment("web", testTarget), cr)
	var previousStamp string
	for i, values := range []map[string]string{
		{"API_KEY": "initial"},
		{"API_KEY": "initial", "NEW_SETTING": "published"},
		{"NEW_SETTING": "published"},
		{},
	} {
		var keys []deliveredKey
		want := map[string][]byte{}
		for name, value := range values {
			keys = append(keys, configVal(name, value))
			want[name] = []byte(value)
		}
		cursor := fmt.Sprintf("v1:cursor-%d", i)
		h.stub.set(200, deliveryJSON(false, cursor, cursor, keys, nil))
		if _, err := h.reconcile("app"); err != nil {
			t.Fatal(err)
		}
		got, exists := h.getSecret(testNS, testTarget)
		if !exists || !dataEqual(got.Data, want) {
			t.Fatalf("step %d: manifest did not fully replace data", i)
		}
		status := h.getCR("app").Status
		if status.Cursor != cursor || status.Stamp == previousStamp || stampAnnotation(h.getDeployment("web")) != status.Stamp {
			t.Fatalf("step %d: cursor/stamp/rollout did not converge", i)
		}
		previousStamp = status.Stamp
		targetRV := got.ResourceVersion
		workloadRV := h.getDeployment("web").ResourceVersion
		h.stub.set(200, deliveryJSON(true, cursor, cursor, nil, nil))
		if _, err := h.reconcile("app"); err != nil {
			t.Fatal(err)
		}
		got, _ = h.getSecret(testNS, testTarget)
		if h.stub.lastCursor != cursor || got.ResourceVersion != targetRV || h.getDeployment("web").ResourceVersion != workloadRV {
			t.Fatalf("step %d: current fetch was ineligible or wrote target/workload", i)
		}
		requireCond(t, h.getCR("app"), hikyov1.ConditionSynced, metav1.ConditionTrue, hikyov1.ReasonCurrent)
	}
}

func TestSelectionAndAllKeysAcknowledgementsRefuseBeforeCredentials(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate crOpt
		reason string
	}{
		{"neither", withMapping(), hikyov1.ReasonInvalidSecretData},
		{"both", func(cr *hikyov1.HikyoSecret) { cr.Spec.AllKeys = true }, hikyov1.ReasonInvalidSecretData},
		{"typed", func(cr *hikyov1.HikyoSecret) { withAllKeys(cr); cr.Spec.Target.Type = corev1.SecretTypeTLS }, hikyov1.ReasonInvalidSecretData},
		{"invalid source", withMapping([2]string{"BAD-NAME", "VALID"}), hikyov1.ReasonInvalidSecretData},
		{"invalid destination", withMapping([2]string{"VALID", "BAD/NAME"}), hikyov1.ReasonInvalidSecretData},
		{"duplicate destination", withMapping([2]string{"ONE", "SAME"}, [2]string{"TWO", "SAME"}), hikyov1.ReasonInvalidSecretData},
		{"nonbaseline acknowledgement", func(cr *hikyov1.HikyoSecret) {
			withAllKeys(cr)
			cr.Spec.AcknowledgedLoaderKeys = []hikyov1.KeyName{"API_KEY"}
		}, hikyov1.ReasonLoaderControlUnacknowledged},
		{"wildcard acknowledgement", func(cr *hikyov1.HikyoSecret) {
			withAllKeys(cr)
			cr.Spec.AcknowledgedLoaderKeys = []hikyov1.KeyName{"LD_*"}
		}, hikyov1.ReasonLoaderControlUnacknowledged},
		{"duplicate acknowledgement", func(cr *hikyov1.HikyoSecret) {
			withAllKeys(cr)
			cr.Spec.AcknowledgedLoaderKeys = []hikyov1.KeyName{"PATH", "PATH"}
		}, hikyov1.ReasonLoaderControlUnacknowledged},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cr := makeCR("app", tc.mutate)
			cr.Status.Cursor, cr.Status.CursorBinding = "old", "old-binding"
			// No instance or credential exists. Fail if any Secret is read, proving
			// that selection is validated before credential acquisition, not just fetch.
			h := newHarness(t, interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*corev1.Secret); ok {
					t.Fatal("invalid selection attempted credential/Secret read")
				}
				return c.Get(ctx, key, obj, opts...)
			}}, cr)
			if _, err := h.reconcile("app"); err != nil {
				t.Fatal(err)
			}
			got := h.getCR("app")
			requireCond(t, got, hikyov1.ConditionDelivery, metav1.ConditionFalse, tc.reason)
			if h.stub.requests != 0 || got.Status.Cursor != "" || got.Status.CursorBinding != "" {
				t.Fatal("refusal fetched or kept a conditional cursor")
			}
		})
	}
}

func TestAllKeysModeTransitionsForceFullFetch(t *testing.T) {
	cr := makeCR("app")
	h := newHarness(t, interceptor.Funcs{}, makeInstance(""), makeBootstrapSecret("boot", testInstance, "tok", true), cr)
	keys := []deliveredKey{configVal("API_KEY", "original"), configVal("EXTRA", "new")}
	for i, allKeys := range []bool{false, true, false} {
		fresh := h.getCR("app")
		fresh.Spec.AllKeys = allKeys
		fresh.Spec.Mapping = []hikyov1.Mapping{{Key: "API_KEY"}}
		if allKeys {
			fresh.Spec.Mapping = nil
		}
		if err := h.cl.Update(t.Context(), fresh); err != nil {
			t.Fatal(err)
		}
		h.stub.set(200, deliveryJSON(false, fmt.Sprintf("v1:cursor-%d", i), "unchanged", keys, nil))
		if _, err := h.reconcile("app"); err != nil {
			t.Fatal(err)
		}
		if h.stub.lastCursor != "" {
			t.Fatal("selection change reused cursor")
		}
		stored, _ := h.getSecret(testNS, testTarget)
		_, extra := stored.Data["EXTRA"]
		if extra != allKeys {
			t.Fatal("selection did not control delivered data")
		}
	}
}

func TestAllKeysLoaderControlProspectiveAcknowledgementAndRemoval(t *testing.T) {
	cr := makeCR("app", withAllKeys)
	cr.Spec.AcknowledgedLoaderKeys = []hikyov1.KeyName{"PATH", "LD_PRELOAD"}
	h := newHarness(t, interceptor.Funcs{}, makeInstance(""), makeBootstrapSecret("boot", testInstance, "tok", true), cr)
	for i, keys := range [][]deliveredKey{
		{configVal("API_KEY", "safe")},
		{configVal("API_KEY", "safe"), configVal("PATH", "/opt/bin"), configVal("LD_PRELOAD", "library")},
		{configVal("API_KEY", "safe")},
	} {
		h.stub.set(200, deliveryJSON(false, fmt.Sprintf("v1:cursor-%d", i), "token", keys, nil))
		if _, err := h.reconcile("app"); err != nil {
			t.Fatal(err)
		}
		requireCond(t, h.getCR("app"), hikyov1.ConditionReady, metav1.ConditionTrue, hikyov1.ReasonReconciled)
		stored, _ := h.getSecret(testNS, testTarget)
		if len(stored.Data) != len(keys) || h.stub.lastAck != "PATH,LD_PRELOAD" {
			t.Fatal("prospective acknowledgements did not allow manifest additions/removals")
		}
	}
	before, _ := h.getSecret(testNS, testTarget)
	fresh := h.getCR("app")
	fresh.Spec.AcknowledgedLoaderKeys = nil
	if err := h.cl.Update(t.Context(), fresh); err != nil {
		t.Fatal(err)
	}
	h.stub.set(200, deliveryJSON(false, "refused", "new-token", []deliveredKey{configVal("PATH", "PRIVATE_VALUE")}, nil))
	if _, err := h.reconcile("app"); err != nil {
		t.Fatal(err)
	}
	got := h.getCR("app")
	requireCond(t, got, hikyov1.ConditionDelivery, metav1.ConditionFalse, hikyov1.ReasonLoaderControlUnacknowledged)
	after, _ := h.getSecret(testNS, testTarget)
	if h.stub.lastCursor != "" || got.Status.Cursor != "" || !dataEqual(before.Data, after.Data) || before.ResourceVersion != after.ResourceVersion {
		t.Fatal("removed acknowledgement reused cursor or wrote unacknowledged data")
	}
	if strings.Contains(fmt.Sprint(got.Status.Conditions, h.drainEvents()), "PRIVATE_VALUE") {
		t.Fatal("refusal leaked payload")
	}
}

func TestAllKeysPresenceOnlyAndAuthorizationWithdrawal(t *testing.T) {
	cr := makeCR("app", withAllKeys)
	h := newHarness(t, interceptor.Funcs{}, makeInstance(""), makeBootstrapSecret("boot", testInstance, "tok", true), makeOptedInDeployment("web", testTarget), cr)
	h.stub.set(200, deliveryJSON(false, "initial", "token", []deliveredKey{configVal("API_KEY", "old")}, nil))
	if _, err := h.reconcile("app"); err != nil {
		t.Fatal(err)
	}
	before, _ := h.getSecret(testNS, testTarget)
	stamp := h.getCR("app").Status.Stamp
	h.stub.set(200, deliveryJSON(false, "undelivered", "token2", []deliveredKey{configVal("API_KEY", "new"), secretPresenceOnly("DB_PASSWORD")}, nil))
	if _, err := h.reconcile("app"); err != nil {
		t.Fatal(err)
	}
	requireCond(t, h.getCR("app"), hikyov1.ConditionDelivery, metav1.ConditionFalse, hikyov1.ReasonUndeliveredSecrets)
	after, _ := h.getSecret(testNS, testTarget)
	if !dataEqual(before.Data, after.Data) || h.getCR("app").Status.Cursor != "initial" || stampAnnotation(h.getDeployment("web")) != stamp {
		t.Fatal("presence-only manifest partially wrote or advanced cursor/rollout")
	}
	h.stub.set(404, `{"error":{"code":"not_found"}}`)
	if _, err := h.reconcile("app"); err != nil {
		t.Fatal(err)
	}
	requireCond(t, h.getCR("app"), hikyov1.ConditionScrubbed, metav1.ConditionTrue, hikyov1.ReasonAuthorizationWithdrawn)
	after, _ = h.getSecret(testNS, testTarget)
	if len(after.Data) != 0 || h.getCR("app").Status.Cursor != "" || stampAnnotation(h.getDeployment("web")) == stamp {
		t.Fatal("authoritative withdrawal did not scrub and roll")
	}
}

func TestAllKeysInvalidManifestRetainsLastDelivery(t *testing.T) {
	for _, keys := range [][]deliveredKey{
		{configVal("API_KEY", "new"), configVal("API_KEY", "duplicate")},
		{configVal("INVALID-NAME", "PRIVATE_VALUE")},
		{configVal(strings.Repeat("A", 129), "PRIVATE_VALUE")},
	} {
		t.Run(keys[len(keys)-1].name, func(t *testing.T) {
			cr := makeCR("app", withAllKeys)
			h := newHarness(t, interceptor.Funcs{}, makeInstance(""), makeBootstrapSecret("boot", testInstance, "tok", true), cr)
			h.stub.set(200, deliveryJSON(false, "initial", "token", []deliveredKey{configVal("API_KEY", "old")}, nil))
			if _, err := h.reconcile("app"); err != nil {
				t.Fatal(err)
			}
			before, _ := h.getSecret(testNS, testTarget)
			h.stub.set(200, deliveryJSON(false, "bad", "token2", keys, nil))
			if _, err := h.reconcile("app"); err == nil {
				t.Fatal("malformed manifest accepted")
			}
			after, _ := h.getSecret(testNS, testTarget)
			if !dataEqual(before.Data, after.Data) || before.ResourceVersion != after.ResourceVersion || h.getCR("app").Status.Cursor != "initial" {
				t.Fatal("malformed manifest wrote or advanced cursor")
			}
		})
	}
}

func TestAllKeysAcknowledgementOrderDoesNotInvalidateCursor(t *testing.T) {
	cr := makeCR("app", withAllKeys)
	cr.Spec.AcknowledgedLoaderKeys = []hikyov1.KeyName{"PATH", "LD_PRELOAD"}
	inst := makeInstance("")
	cred := credential{uid: "auth-uid", resourceVersion: "10"}
	before := bindingDigest(bindingInputFor(cr, inst, cred))
	cr.Spec.AcknowledgedLoaderKeys = []hikyov1.KeyName{"LD_PRELOAD", "PATH"}
	if bindingDigest(bindingInputFor(cr, inst, cred)) != before {
		t.Fatal("reordered equivalent acknowledgements invalidated cursor")
	}
	cr.Spec.AcknowledgedLoaderKeys = []hikyov1.KeyName{"PATH"}
	if bindingDigest(bindingInputFor(cr, inst, cred)) == before {
		t.Fatal("changed acknowledgement grants did not invalidate cursor")
	}
}
