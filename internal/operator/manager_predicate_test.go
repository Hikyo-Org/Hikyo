package operator

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	hikyov1 "github.com/Hikyo-Org/hikyo/internal/operator/api/v1alpha1"
)

func TestHikyoSecretPredicateIgnoresStatusOnlyUpdates(t *testing.T) {
	oldCR := &hikyov1.HikyoSecret{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "default", Generation: 3}}
	newCR := oldCR.DeepCopy()
	newCR.ResourceVersion = "2"
	now := metav1.NewTime(time.Now().UTC())
	newCR.Status.LastFetch = &now
	if hikyoSecretPredicate().Update(event.UpdateEvent{ObjectOld: oldCR, ObjectNew: newCR}) {
		t.Fatal("status-only update enqueued an immediate reconcile")
	}
}

func TestHikyoSecretPredicateKeepsLifecycleUpdates(t *testing.T) {
	base := &hikyov1.HikyoSecret{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "default", Generation: 3}}
	for name, mutate := range map[string]func(*hikyov1.HikyoSecret){
		"generation": func(cr *hikyov1.HikyoSecret) { cr.Generation++ },
		"label":      func(cr *hikyov1.HikyoSecret) { cr.Labels = map[string]string{"owner": "platform"} },
		"annotation": func(cr *hikyov1.HikyoSecret) { cr.Annotations = map[string]string{"floor.hikyo.dev/phase": "1"} },
		"finalizer":  func(cr *hikyov1.HikyoSecret) { cr.Finalizers = []string{hikyov1.OrphanFinalizer} },
		"deletion": func(cr *hikyov1.HikyoSecret) {
			now := metav1.NewTime(time.Now().UTC())
			cr.DeletionTimestamp = &now
		},
	} {
		t.Run(name, func(t *testing.T) {
			changed := base.DeepCopy()
			mutate(changed)
			if !hikyoSecretPredicate().Update(event.UpdateEvent{ObjectOld: base, ObjectNew: changed}) {
				t.Fatal("lifecycle update was suppressed")
			}
		})
	}
}
