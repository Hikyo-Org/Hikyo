package operator

import (
	"context"
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	hikyov1 "github.com/Hikyo-Org/hikyo/internal/operator/api/v1alpha1"
)

func withSecretType(t corev1.SecretType) crOpt {
	return func(cr *hikyov1.HikyoSecret) { cr.Spec.Target.Type = t }
}

func TestNativeSecretTypes(t *testing.T) {
	for _, tt := range []struct {
		typ  corev1.SecretType
		data map[string]string
	}{
		{"", map[string]string{"API_KEY": "value"}},
		{corev1.SecretTypeOpaque, map[string]string{"API_KEY": "value"}},
		{corev1.SecretTypeDockerConfigJson, map[string]string{corev1.DockerConfigJsonKey: `{"auths":{"registry.example":{"auth":"dXNlcjpwYXNz"}}}`}},
		{corev1.SecretTypeTLS, map[string]string{corev1.TLSCertKey: "certificate", corev1.TLSPrivateKeyKey: "private-key"}},
		{corev1.SecretTypeBasicAuth, map[string]string{corev1.BasicAuthUsernameKey: "user", corev1.BasicAuthPasswordKey: "password"}},
		{corev1.SecretTypeSSHAuth, map[string]string{corev1.SSHAuthPrivateKey: "private-key"}},
	} {
		t.Run(string(tt.typ), func(t *testing.T) {
			cr := makeCR("app", withSecretType(tt.typ))
			cr.Spec.Mapping = nil
			var keys []deliveredKey
			for dest, value := range tt.data {
				source := fmt.Sprintf("KEY_%d", len(keys))
				cr.Spec.Mapping = append(cr.Spec.Mapping, hikyov1.Mapping{Key: hikyov1.KeyName(source), SecretKey: dest})
				keys = append(keys, secretVal(source, value))
			}
			h := newHarness(t, interceptor.Funcs{}, makeInstance(""), makeBootstrapSecret("boot", testInstance, "tok", true), cr)
			h.r.Config.NativeSecretTypes = effectiveSecretType(tt.typ) != corev1.SecretTypeOpaque
			h.stub.set(200, deliveryJSON(false, "v1:cursor", "v1:token", keys, nil))
			if _, err := h.reconcile("app"); err != nil {
				t.Fatal(err)
			}
			sec, exists := h.getSecret(testNS, testTarget)
			if !exists || sec.Type != effectiveSecretType(tt.typ) {
				t.Fatalf("Secret type: %v, exists: %v", sec, exists)
			}
			for dest, value := range tt.data {
				if string(sec.Data[dest]) != value {
					t.Fatalf("incorrect value at %s", dest)
				}
			}
			requireCond(t, h.getCR("app"), hikyov1.ConditionReady, metav1.ConditionTrue, hikyov1.ReasonReconciled)
		})
	}
}

func TestTypedSecretInvalidContentRetainsTargetWithoutLeakingPayload(t *testing.T) {
	for _, tt := range []struct {
		typ         corev1.SecretType
		dest, value string
	}{
		{corev1.SecretTypeDockerConfigJson, corev1.DockerConfigJsonKey, "VERY_PRIVATE_INVALID_JSON"},
		{corev1.SecretTypeDockerConfigJson, corev1.DockerConfigJsonKey, `[]`},
		{corev1.SecretTypeDockerConfigJson, corev1.DockerConfigJsonKey, `null`},
		{corev1.SecretTypeSSHAuth, corev1.SSHAuthPrivateKey, ""},
	} {
		t.Run(string(tt.typ)+tt.value, func(t *testing.T) {
			cr := makeCR("app", withSecretType(tt.typ), withMapping([2]string{"KEY", tt.dest}))
			cr.Status.Conditions = []metav1.Condition{{Type: hikyov1.ConditionSynced, Status: metav1.ConditionTrue, Reason: hikyov1.ReasonDelivered, LastTransitionTime: metav1.NewTime(testClock)}}
			cr.Status.Cursor, cr.Status.CursorBinding = "v1:previous", "previous-binding"
			h := newHarness(t, interceptor.Funcs{}, makeInstance(""), makeBootstrapSecret("boot", testInstance, "tok", true), cr)
			sec := makeOwnedSecret(t, h.scheme, cr, map[string][]byte{tt.dest: []byte("old")})
			sec.Type = tt.typ
			if err := h.cl.Create(t.Context(), sec); err != nil {
				t.Fatal(err)
			}
			h.stub.set(200, deliveryJSON(false, "v1:cursor", "v1:token", []deliveredKey{secretVal("KEY", tt.value)}, nil))
			if _, err := h.reconcile("app"); err != nil {
				t.Fatal(err)
			}
			got := h.getCR("app")
			requireCond(t, got, hikyov1.ConditionDelivery, metav1.ConditionFalse, hikyov1.ReasonInvalidSecretData)
			requireCond(t, got, hikyov1.ConditionReady, metav1.ConditionFalse, hikyov1.ReasonBlocked)
			if strings.Contains(fmt.Sprint(got.Status.Conditions, h.drainEvents()), "VERY_PRIVATE_INVALID_JSON") {
				t.Fatal("payload leaked")
			}
			stored, exists := h.getSecret(testNS, testTarget)
			if !exists || string(stored.Data[tt.dest]) != "old" || got.Status.Cursor != "" {
				t.Fatal("invalid content wrote data or cursor")
			}
			valid := "new-private-key"
			if tt.typ == corev1.SecretTypeDockerConfigJson {
				valid = `{"auths":{}}`
			}
			h.stub.set(200, deliveryJSON(false, "v1:recovered", "v1:recovered-token", []deliveredKey{secretVal("KEY", valid)}, nil))
			if _, err := h.reconcile("app"); err != nil {
				t.Fatal(err)
			}
			requireCond(t, h.getCR("app"), hikyov1.ConditionReady, metav1.ConditionTrue, hikyov1.ReasonReconciled)
			if h.stub.lastCursor != "" {
				t.Fatal("recovery skipped full content validation")
			}
		})
	}
}

func TestTypedSecretMappingMissingRequiredKeyRefusesBeforeFetch(t *testing.T) {
	cr := makeCR("app", withSecretType(corev1.SecretTypeTLS), withMapping([2]string{"CERT", corev1.TLSCertKey}))
	cr.Status.Conditions = []metav1.Condition{{Type: hikyov1.ConditionSynced, Status: metav1.ConditionTrue, Reason: hikyov1.ReasonDelivered, LastTransitionTime: metav1.NewTime(testClock)}}
	cr.Status.Cursor, cr.Status.CursorBinding = "v1:previous", "previous-binding"
	h := newHarness(t, interceptor.Funcs{}, makeInstance(""), makeBootstrapSecret("boot", testInstance, "tok", true), cr)
	if _, err := h.reconcile("app"); err != nil {
		t.Fatal(err)
	}
	requireCond(t, h.getCR("app"), hikyov1.ConditionDelivery, metav1.ConditionFalse, hikyov1.ReasonKeysMissing)
	requireCond(t, h.getCR("app"), hikyov1.ConditionReady, metav1.ConditionFalse, hikyov1.ReasonBlocked)
	if h.getCR("app").Status.Cursor != "" {
		t.Fatal("invalid mapping retained conditional cursor")
	}
	if h.stub.requests != 0 {
		t.Fatal("invalid mapping fetched plaintext")
	}
	if _, exists := h.getSecret(testNS, testTarget); exists {
		t.Fatal("partial Secret created")
	}
}

func TestSecretTypeChangeNeverWritesOrDeletes(t *testing.T) {
	cr := makeCR("app", withSecretType(corev1.SecretTypeDockerConfigJson), withMapping([2]string{"CONFIG", corev1.DockerConfigJsonKey}))
	h := newHarness(t, interceptor.Funcs{Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
		t.Fatal("type change attempted deletion")
		return nil
	}}, makeInstance(""), makeBootstrapSecret("boot", testInstance, "tok", true), cr)
	sec := makeOwnedSecret(t, h.scheme, cr, map[string][]byte{"API_KEY": []byte("old")})
	if err := h.cl.Create(t.Context(), sec); err != nil {
		t.Fatal(err)
	}
	if _, err := h.reconcile("app"); err != nil {
		t.Fatal(err)
	}
	requireCond(t, h.getCR("app"), hikyov1.ConditionConflict, metav1.ConditionTrue, hikyov1.ReasonTargetTypeImmutable)
	stored, _ := h.getSecret(testNS, testTarget)
	if stored.Type != corev1.SecretTypeOpaque || string(stored.Data["API_KEY"]) != "old" || h.stub.requests != 0 {
		t.Fatal("type change modified target or fetched")
	}
}

func TestTypedSecretWithdrawal(t *testing.T) {
	for _, missingKey := range []bool{false, true} {
		t.Run(fmt.Sprintf("manifest-key-missing=%t", missingKey), func(t *testing.T) {
			cr := makeCR("app", withSecretType(corev1.SecretTypeTLS), withMapping([2]string{"CERT", corev1.TLSCertKey}, [2]string{"KEY", corev1.TLSPrivateKeyKey}))
			var deletes int
			h := newHarness(t, interceptor.Funcs{Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
				deletes++
				o := &client.DeleteOptions{}
				for _, opt := range opts {
					opt.ApplyToDelete(o)
				}
				if o.Preconditions == nil || o.Preconditions.UID == nil || *o.Preconditions.UID != obj.GetUID() || o.Preconditions.ResourceVersion == nil || *o.Preconditions.ResourceVersion != obj.GetResourceVersion() {
					t.Fatal("delete missing identity/version preconditions")
				}
				return c.Delete(ctx, obj, opts...)
			}}, makeInstance(""), makeBootstrapSecret("boot", testInstance, "tok", true), makeOptedInDeployment("web", testTarget), cr)
			sec := makeOwnedSecret(t, h.scheme, cr, map[string][]byte{corev1.TLSCertKey: []byte("old-cert"), corev1.TLSPrivateKeyKey: []byte("old-key")})
			sec.Type = corev1.SecretTypeTLS
			if err := h.cl.Create(t.Context(), sec); err != nil {
				t.Fatal(err)
			}
			if missingKey {
				h.stub.set(200, deliveryJSON(false, "v1:new", "v1:token", []deliveredKey{secretVal("CERT", "new-cert")}, nil))
			} else {
				h.stub.set(404, "")
			}
			for range 2 {
				if _, err := h.reconcile("app"); err != nil {
					t.Fatal(err)
				}
				if _, exists := h.getSecret(testNS, testTarget); exists {
					t.Fatal("withdrawn credentials remain")
				}
			}
			got := h.getCR("app")
			if missingKey {
				requireCond(t, got, hikyov1.ConditionDelivery, metav1.ConditionFalse, hikyov1.ReasonKeysMissing)
			} else {
				requireCond(t, got, hikyov1.ConditionScrubbed, metav1.ConditionTrue, hikyov1.ReasonAuthorizationWithdrawn)
			}
			if deletes != 1 || got.Status.Cursor != "" || got.Status.ManagedSecretUID != "" || got.Status.ManagedSecretResourceVersion != "" || stampAnnotation(h.getDeployment("web")) == "" {
				t.Fatal("withdrawal status/rollout incorrect")
			}
		})
	}
}

func TestTypedWithdrawalReplacementRacesFailClosed(t *testing.T) {
	for _, afterDelete := range []bool{false, true} {
		t.Run(fmt.Sprintf("after-delete=%t", afterDelete), func(t *testing.T) {
			cr := makeCR("app", withSecretType(corev1.SecretTypeSSHAuth), withMapping([2]string{"KEY", corev1.SSHAuthPrivateKey}))
			h := newHarness(t, interceptor.Funcs{Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
				if !afterDelete {
					return apierrors.NewConflict(schema.GroupResource{Resource: "secrets"}, obj.GetName(), fmt.Errorf("UID precondition failed"))
				}
				if err := c.Delete(ctx, obj, opts...); err != nil {
					return err
				}
				replacement := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: obj.GetName(), Namespace: obj.GetNamespace(), UID: types.UID("replacement")}, Data: map[string][]byte{"KEY": []byte("replacement-value")}}
				return c.Create(ctx, replacement)
			}}, makeInstance(""), makeBootstrapSecret("boot", testInstance, "tok", true), cr)
			sec := makeOwnedSecret(t, h.scheme, cr, map[string][]byte{corev1.SSHAuthPrivateKey: []byte("old")})
			sec.Type = corev1.SecretTypeSSHAuth
			if err := h.cl.Create(t.Context(), sec); err != nil {
				t.Fatal(err)
			}
			h.stub.set(404, "")
			if _, err := h.reconcile("app"); err == nil {
				t.Fatal("race incorrectly reported success")
			}
			got := h.getCR("app")
			requireCond(t, got, hikyov1.ConditionSynced, metav1.ConditionFalse, hikyov1.ReasonFetchFailed)
			if got.Status.Cursor != "" {
				t.Fatal("cursor retained after failed withdrawal")
			}
			if _, exists := h.getSecret(testNS, testTarget); !exists {
				t.Fatal("race deleted target")
			}
		})
	}
}

func TestVerifyManagedSecretRejectsWrongType(t *testing.T) {
	cr := makeCR("app")
	h := newHarness(t, interceptor.Funcs{}, cr)
	sec := makeOwnedSecret(t, h.scheme, cr, map[string][]byte{"API_KEY": []byte("value")})
	sec.Type = corev1.SecretTypeSSHAuth
	if err := h.cl.Create(t.Context(), sec); err != nil {
		t.Fatal(err)
	}
	if _, err := h.r.verifyManagedSecret(t.Context(), cr, sec.Data, sec.UID); err == nil {
		t.Fatal("wrong type passed write verification")
	}
}

func TestSecretTypeBindsCursor(t *testing.T) {
	cr := makeCR("app")
	inst := makeInstance("")
	cred := credential{uid: "auth-uid", resourceVersion: "10"}
	defaultBinding := bindingDigest(bindingInputFor(cr, inst, cred))
	cr.Spec.Target.Type = corev1.SecretTypeOpaque
	if bindingDigest(bindingInputFor(cr, inst, cred)) != defaultBinding {
		t.Fatal("explicit Opaque changed default binding")
	}
	cr.Spec.Target.Type = corev1.SecretTypeTLS
	if bindingDigest(bindingInputFor(cr, inst, cred)) == defaultBinding {
		t.Fatal("type change did not invalidate cursor binding")
	}
}

func TestTypedWithdrawalWaitsForFinalizers(t *testing.T) {
	cr := makeCR("app", withSecretType(corev1.SecretTypeSSHAuth), withMapping([2]string{"KEY", corev1.SSHAuthPrivateKey}))
	h := newHarness(t, interceptor.Funcs{}, makeInstance(""), makeBootstrapSecret("boot", testInstance, "tok", true), cr)
	sec := makeOwnedSecret(t, h.scheme, cr, map[string][]byte{corev1.SSHAuthPrivateKey: []byte("old")})
	sec.Type = corev1.SecretTypeSSHAuth
	sec.Finalizers = []string{"example.com/hold"}
	if err := h.cl.Create(t.Context(), sec); err != nil {
		t.Fatal(err)
	}
	h.stub.set(404, "")
	if _, err := h.reconcile("app"); err == nil {
		t.Fatal("pending deletion reported successful scrub")
	}
	got := h.getCR("app")
	requireCond(t, got, hikyov1.ConditionSynced, metav1.ConditionFalse, hikyov1.ReasonFetchFailed)
	if got.Status.Cursor != "" || got.Status.Stamp != "" {
		t.Fatal("failed withdrawal persisted success state")
	}
	stored, exists := h.getSecret(testNS, testTarget)
	if !exists || stored.DeletionTimestamp == nil {
		t.Fatal("finalizer fixture was not pending deletion")
	}
}

func TestNativeSecretTypesDisabledRefusesBeforeFetchAndRetainsTarget(t *testing.T) {
	cr := makeCR("app", withSecretType(corev1.SecretTypeSSHAuth), withMapping([2]string{"KEY", corev1.SSHAuthPrivateKey}))
	cr.Status.Cursor, cr.Status.CursorBinding = "previous", "binding"
	h := newHarness(t, interceptor.Funcs{Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
		t.Fatal("disabled native support attempted deletion")
		return nil
	}}, cr)
	h.r.Config.NativeSecretTypes = false
	sec := makeOwnedSecret(t, h.scheme, cr, map[string][]byte{corev1.SSHAuthPrivateKey: []byte("old-key")})
	sec.Type = corev1.SecretTypeSSHAuth
	if err := h.cl.Create(t.Context(), sec); err != nil {
		t.Fatal(err)
	}
	if _, err := h.reconcile("app"); err != nil {
		t.Fatal(err)
	}
	got := h.getCR("app")
	requireCond(t, got, hikyov1.ConditionDelivery, metav1.ConditionFalse, hikyov1.ReasonBlocked)
	requireCond(t, got, hikyov1.ConditionReady, metav1.ConditionFalse, hikyov1.ReasonBlocked)
	if h.stub.requests != 0 || got.Status.Cursor != "" {
		t.Fatal("disabled native support fetched data or retained cursor")
	}
	stored, exists := h.getSecret(testNS, testTarget)
	if !exists || string(stored.Data[corev1.SSHAuthPrivateKey]) != "old-key" {
		t.Fatal("disabling native support modified target")
	}
}
