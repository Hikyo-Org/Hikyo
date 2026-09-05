package main

import (
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	hikyov1 "github.com/Hikyo-Org/hikyo/internal/operator/api/v1alpha1"
)

// This tests the evidence checker, not operator behavior. The acceptance script
// must still run the actual pod against a real API server before claiming fit.
func TestSettledCannotAcceptStaleOrChangedRetainedData(t *testing.T) {
	for _, fault := range []string{"none", "old-fetch", "wrong-value", "rewritten", "not-owned", "missing-cr"} {
		t.Run(fault, func(t *testing.T) {
			sch := runtime.NewScheme()
			if err := scheme.AddToScheme(sch); err != nil {
				t.Fatal(err)
			}
			if err := hikyov1.AddToScheme(sch); err != nil {
				t.Fatal(err)
			}
			barrier := time.Now().UTC().Truncate(time.Second)
			versions := map[string]string{}
			var objs []client.Object
			for i := range objects {
				name := fmt.Sprintf("cr-%d", i)
				cr := &hikyov1.HikyoSecret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: types.UID(name)},
					Spec:   hikyov1.HikyoSecretSpec{Target: hikyov1.Target{Name: name}},
					Status: hikyov1.HikyoSecretStatus{LastFetch: &metav1.Time{Time: barrier}, Conditions: []metav1.Condition{{Type: hikyov1.ConditionSynced, Status: metav1.ConditionFalse, Reason: hikyov1.ReasonFetchFailed}}}}
				controller := true
				secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, ResourceVersion: "1", OwnerReferences: []metav1.OwnerReference{{UID: cr.UID, Controller: &controller}}}, Data: map[string][]byte{}}
				for k := range keys {
					secret.Data[fmt.Sprintf("KEY_%03d", k)] = []byte(value(3))
				}
				versions[name] = "1"
				if i == 0 {
					switch fault {
					case "old-fetch":
						cr.Status.LastFetch = &metav1.Time{Time: barrier.Add(-time.Second)}
					case "wrong-value":
						secret.Data["KEY_000"] = []byte("changed")
					case "rewritten":
						versions[name] = "0"
					case "not-owned":
						secret.OwnerReferences = nil
					case "missing-cr":
						continue
					}
				}
				objs = append(objs, cr, secret)
			}
			cl := fake.NewClientBuilder().WithScheme(sch).WithObjects(objs...).Build()
			ok, err := settled(t.Context(), cl, 3, barrier, versions)
			if fault == "none" && (!ok || err != nil) {
				t.Fatalf("valid evidence: %v, %v", ok, err)
			}
			if fault != "none" && ok {
				t.Fatalf("accepted %s evidence", fault)
			}
		})
	}
}
