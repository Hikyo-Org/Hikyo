package operator

import (
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	hikyov1 "github.com/Hikyo-Org/hikyo/internal/operator/api/v1alpha1"
)

func TestPinExpiryConditionAcrossSuccessfulFetches(t *testing.T) {
	for _, current := range []bool{false, true} {
		name := "full delivery"
		if current {
			name = "current without Secret write"
		}
		t.Run(name, func(t *testing.T) {
			cr := makeCR("app")
			cr.Generation = 7
			h := newHarness(t, interceptor.Funcs{},
				makeInstance(""), makeBootstrapSecret("boot", testInstance, "tok", true), cr)
			soon := testClock.Add(3 * 24 * time.Hour)
			keys := []deliveredKey{secretVal("API_KEY", "delivered-value")}
			h.stub.set(200, deliveryJSON(false, "v1:c", "v1:t", keys, &soon))
			if _, err := h.reconcile("app"); err != nil {
				t.Fatal(err)
			}
			before, ok := h.getSecret(testNS, testTarget)
			if !ok {
				t.Fatal("initial delivery did not create Secret")
			}
			if current {
				keys = nil
			}

			// A fresh pin, expiry, then renewal/re-pinning/unpinning all arrive
			// through the real TLS client and reconciler. The server's false bit
			// is the same authoritative result for each way of clearing expiry.
			for _, expired := range []bool{false, true, false} {
				body := deliveryJSON(current, "v1:c", "v1:t", keys, &soon)
				if expired {
					body = strings.Replace(body, `"pin_expired":false`, `"pin_expired":true`, 1)
				}
				h.stub.set(200, body)
				if _, err := h.reconcile("app"); err != nil {
					t.Fatal(err)
				}
				got := h.getCR("app")
				requireCond(t, got, hikyov1.ConditionReady, metav1.ConditionTrue, hikyov1.ReasonReconciled)
				requireCond(t, got, hikyov1.ConditionCredentialExpiry, metav1.ConditionTrue, hikyov1.ReasonExpiresSoon)
				if got.Status.CredentialExpiresAt == nil || !got.Status.CredentialExpiresAt.Time.Equal(soon) {
					t.Fatal("pin condition changed credential expiry")
				}
				if expired {
					requireCond(t, got, hikyov1.ConditionPinExpired, metav1.ConditionTrue, hikyov1.ReasonPinExpired)
					condition := meta.FindStatusCondition(got.Status.Conditions, hikyov1.ConditionPinExpired)
					if condition.ObservedGeneration != got.Generation || !strings.Contains(condition.Message, "retention may collect") {
						t.Fatalf("expiry condition lacks current generation or actionable warning: %+v", condition)
					}
					foundEvent := false
					for len(h.events) > 0 {
						if strings.Contains(<-h.events, "Warning PinExpired") {
							foundEvent = true
						}
					}
					if !foundEvent {
						t.Fatal("expired pin did not emit Warning event")
					}
				} else if meta.FindStatusCondition(got.Status.Conditions, hikyov1.ConditionPinExpired) != nil {
					t.Fatal("fresh or unpinned response left stale pin condition")
				}
				after, ok := h.getSecret(testNS, testTarget)
				if !ok || string(after.Data["API_KEY"]) != "delivered-value" {
					t.Fatal("pin expiry interrupted or altered delivery")
				}
				if current {
					requireCond(t, got, hikyov1.ConditionSynced, metav1.ConditionTrue, hikyov1.ReasonCurrent)
					if h.stub.lastCursor != "v1:c" || after.ResourceVersion != before.ResourceVersion {
						t.Fatal("current response lost cursor eligibility or rewrote Secret")
					}
				} else {
					requireCond(t, got, hikyov1.ConditionSynced, metav1.ConditionTrue, hikyov1.ReasonDelivered)
				}

				if expired {
					// A failed fetch provides no evidence of renewal. Keep the
					// warning until the next successful false response above.
					h.stub.set(503, "")
					if _, err := h.reconcile("app"); err == nil {
						t.Fatal("failed fetch should return an error")
					}
					requireCond(t, h.getCR("app"), hikyov1.ConditionPinExpired, metav1.ConditionTrue, hikyov1.ReasonPinExpired)
				}
			}
		})
	}
}
