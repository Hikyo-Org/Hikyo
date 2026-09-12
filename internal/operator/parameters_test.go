package operator

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	hikyov1 "github.com/Hikyo-Org/hikyo/internal/operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestParametersBindStampWithoutChangingMappedValues(t *testing.T) {
	r := &HikyoSecretReconciler{}
	inst := &hikyov1.HikyoInstance{ObjectMeta: metav1.ObjectMeta{UID: "instance"}}
	cr := &hikyov1.HikyoSecret{ObjectMeta: metav1.ObjectMeta{UID: "secret"}}
	cr.Spec.Target.Name = "app-config"
	root := bytes.Repeat([]byte{1}, crypto.KeySize)
	legacy, err := r.computeStamp(inst, cr, nil, root)
	if err != nil {
		t.Fatal(err)
	}
	cr.Spec.Parameters = map[string]hikyov1.ParameterValue{"PR_NUMBER": "123"}
	first, err := r.computeStamp(inst, cr, nil, root)
	if err != nil {
		t.Fatal(err)
	}
	cr.Spec.Parameters["PR_NUMBER"] = "124"
	second, err := r.computeStamp(inst, cr, nil, root)
	if err != nil {
		t.Fatal(err)
	}
	if legacy == first || first == second {
		t.Fatal("parameters failed to move delivery stamp with identical mapped values")
	}
	cr.Spec.Parameters = map[string]hikyov1.ParameterValue{}
	empty, err := r.computeStamp(inst, cr, nil, root)
	if err != nil {
		t.Fatal(err)
	}
	if empty != legacy {
		t.Fatal("empty params changed legacy stamp")
	}
	if bindingDigest(bindingInput{parameters: map[string]string{"PR_NUMBER": "123"}}) == bindingDigest(bindingInput{parameters: map[string]string{"PR_NUMBER": "124"}}) {
		t.Fatal("parameter change reused local cursor binding")
	}
}

func TestParameterizedLegacyResponseRetainsManagedSecret(t *testing.T) {
	for _, current := range []bool{false, true} {
		t.Run(fmt.Sprint(current), func(t *testing.T) {
			cr := makeCR("app", withMapping([2]string{"URL", "URL"}))
			cr.Spec.Parameters = map[string]hikyov1.ParameterValue{"PR_NUMBER": "123"}
			h := newHarness(t, interceptor.Funcs{}, makeInstance(""), makeBootstrapSecret("boot", testInstance, "tok", true), cr)
			sec := makeOwnedSecret(t, h.scheme, cr, map[string][]byte{"URL": []byte("last-good")})
			if err := h.cl.Create(t.Context(), sec); err != nil {
				t.Fatal(err)
			}
			var keys []deliveredKey
			if !current {
				keys = []deliveredKey{configVal("URL", "literal-${PR_NUMBER}")}
			}
			oldResponse := strings.Replace(deliveryJSON(current, "legacy-cursor", "token", keys, nil), `"revision":1,`, "", 1)
			h.stub.set(200, oldResponse)
			if _, err := h.reconcile("app"); err == nil {
				t.Fatal("legacy parameter response accepted")
			}
			got := h.getCR("app")
			requireCond(t, got, hikyov1.ConditionSynced, metav1.ConditionFalse, hikyov1.ReasonFetchFailed)
			if got.Status.Cursor == "legacy-cursor" {
				t.Fatal("unsupported response advanced cursor")
			}
			stored, exists := h.getSecret(testNS, testTarget)
			if !exists || string(stored.Data["URL"]) != "last-good" {
				t.Fatal("unsupported response changed managed data")
			}
		})
	}
}
