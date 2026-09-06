package configrollout

import (
	"crypto/ed25519"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"
)

func TestControllerCancelsUnseenSubmitWithoutMutatingTarget(t *testing.T) {
	f, e, c, m, private := controllerFixture(t)
	prepare := signedFixture(t, private, e, Command{Sequence: 1, Action: ActionPrepare, Intent: f.intent, Changes: f.changes()})
	if err := m.Send(t.Context(), prepare); err != nil {
		t.Fatal(err)
	}
	if err := c.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	prepared, err := m.Response(t.Context(), prepare)
	if err != nil {
		t.Fatal(err)
	}
	submit := signedFixture(t, private, e, Command{Sequence: 2, Action: ActionSubmit, Intent: f.intent, PlanDigest: prepared.PlanDigest})
	if err := m.Send(t.Context(), submit); err != nil {
		t.Fatal(err)
	}
	// The executor was absent until the committed command expired.
	c.now = func() time.Time { return time.Now().Add(time.Hour) }
	if err := c.reconcile(t.Context()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expired first submit: %v", err)
	}
	c.now = time.Now
	before := map[string]string{}
	for _, name := range []string{f.target.ConfigSecret, f.target.RequestSecret, f.target.RollbackSecret, f.target.ReceiptSecret} {
		before[name] = digest(f.secret(t, name))
	}
	restore := signedFixture(t, private, e, Command{Sequence: 3, Action: ActionRestore, Intent: f.intent, PlanDigest: prepared.PlanDigest})
	if err := m.Send(t.Context(), restore); err != nil {
		t.Fatal(err)
	}
	if err := c.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	response, err := m.Response(t.Context(), restore)
	if err != nil || response.Outcome != "complete" || response.Receipt == nil || response.Receipt.Phase != Restored || response.Receipt.ApplicationAcknowledged {
		t.Fatalf("cancel response: %v", err)
	}
	if digest(f.deployment(t).Spec) != digest(f.before.Spec) {
		t.Fatal("cancel applied target")
	}
	for name, want := range before {
		if digest(f.secret(t, name)) != want {
			t.Fatal("cancel manufactured module request/rollback/configuration")
		}
	}
	// Terminal controller state survives expiry/restart and releases preparation.
	restarted, err := NewController(f.client, e, private.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	restarted.now = func() time.Time { return time.Now().Add(time.Hour) }
	if err := restarted.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	next := f.intent
	next.JobID = "next-job"
	newPrepare := signedFixture(t, private, e, Command{Sequence: 4, Action: ActionPrepare, Intent: next, Changes: f.changes()})
	if err := m.Send(t.Context(), newPrepare); err != nil {
		t.Fatal(err)
	}
	if err := c.reconcile(t.Context()); err != nil {
		t.Fatalf("cancel stranded later plan: %v", err)
	}
}

func TestControllerCancelsInterruptedSubmitBookkeeping(t *testing.T) {
	for _, failAt := range []string{"rollback", "receipt", "config"} {
		t.Run(failAt, func(t *testing.T) {
			f, e, c, m, private := controllerFixture(t)
			prepare := signedFixture(t, private, e, Command{Sequence: 1, Action: ActionPrepare, Intent: f.intent, Changes: f.changes()})
			if err := m.Send(t.Context(), prepare); err != nil {
				t.Fatal(err)
			}
			if err := c.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			prepared, err := m.Response(t.Context(), prepare)
			if err != nil {
				t.Fatal(err)
			}
			fail := true
			f.client.PrependReactor("update", "secrets", func(action ktesting.Action) (bool, runtime.Object, error) {
				if fail && action.(ktesting.UpdateAction).GetObject().(*corev1.Secret).Name == failAt {
					fail = false
					return true, nil, errors.New("interrupted bookkeeping")
				}
				return false, nil, nil
			})
			submit := signedFixture(t, private, e, Command{Sequence: 2, Action: ActionSubmit, Intent: f.intent, PlanDigest: prepared.PlanDigest})
			if err := m.Send(t.Context(), submit); err != nil {
				t.Fatal(err)
			}
			if err := c.reconcile(t.Context()); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("failure not reached: %v", err)
			}
			restore := signedFixture(t, private, e, Command{Sequence: 3, Action: ActionRestore, Intent: f.intent, PlanDigest: prepared.PlanDigest})
			if err := m.Send(t.Context(), restore); err != nil {
				t.Fatal(err)
			}
			if err := c.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			response, err := m.Response(t.Context(), restore)
			if err != nil || response.Receipt == nil || response.Receipt.Phase != Restored {
				t.Fatalf("interrupted preparation stranded: %v", err)
			}
			if digest(f.deployment(t).Spec) != digest(f.before.Spec) {
				t.Fatal("cancellation applied deployment")
			}
			next := f.intent
			next.JobID = "next-job"
			prepare = signedFixture(t, private, e, Command{Sequence: 4, Action: ActionPrepare, Intent: next, Changes: f.changes()})
			if err := m.Send(t.Context(), prepare); err != nil {
				t.Fatal(err)
			}
			if err := c.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			prepared, err = m.Response(t.Context(), prepare)
			if err != nil {
				t.Fatal(err)
			}
			submit = signedFixture(t, private, e, Command{Sequence: 5, Action: ActionSubmit, Intent: next, PlanDigest: prepared.PlanDigest})
			if err := m.Send(t.Context(), submit); err != nil {
				t.Fatal(err)
			}
			if err := c.reconcile(t.Context()); err != nil {
				t.Fatalf("cancel left module storage unusable: %v", err)
			}
		})
	}
}

func TestCancelPreparedAllowsDeploymentStatusOnlyUpdates(t *testing.T) {
	f := newFixture(t)
	plan, err := f.executor.Prepare(t.Context(), f.intent, f.changes())
	if err != nil {
		t.Fatal(err)
	}
	d := f.deployment(t)
	d.Status.ObservedGeneration++
	if _, err := f.client.AppsV1().Deployments(f.target.Namespace).UpdateStatus(t.Context(), d, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if receipt, err := f.executor.cancelPrepared(t.Context(), f.intent, plan.Digest(), plan); err != nil || receipt.Phase != Restored {
		t.Fatalf("status-only update stranded cancellation: %v", err)
	}
}

func TestCancelPreparedRejectsChangedBaseline(t *testing.T) {
	for _, target := range []string{"deployment", "config", "request", "receipt", "rollback"} {
		t.Run(target, func(t *testing.T) {
			f := newFixture(t)
			plan, err := f.executor.Prepare(t.Context(), f.intent, f.changes())
			if err != nil {
				t.Fatal(err)
			}
			if target == "deployment" {
				d := f.deployment(t)
				d.Spec.Template.Spec.Containers[0].Image = "other:image"
				if _, err := f.client.AppsV1().Deployments(f.target.Namespace).Update(t.Context(), d, metav1.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
			} else {
				s := f.secret(t, target)
				s.Data = map[string][]byte{"changed": []byte("value")}
				if _, err := f.client.CoreV1().Secrets(f.target.Namespace).Update(t.Context(), s, metav1.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.executor.cancelPrepared(t.Context(), f.intent, plan.Digest(), plan); !errors.Is(err, ErrConflict) {
				t.Fatalf("changed baseline cancelled: %v", err)
			}
		})
	}
}
