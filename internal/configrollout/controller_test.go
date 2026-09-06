package configrollout

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"

	coordv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ktesting "k8s.io/client-go/testing"
)

func controllerFixture(t *testing.T) (fixture, Enrollment, *Controller, *Mailbox, ed25519.PrivateKey) {
	t.Helper()
	f := newFixture(t)
	e := Enrollment{ID: "enrollment-1", OwnerInstanceID: f.intent.OwnerInstanceID, Incarnation: f.intent.Incarnation, Target: f.target, CommandSecret: "command", CommandSecretUID: "command-1", ResponseSecret: "response", ResponseSecretUID: "response-1", JournalSecret: "journal", JournalSecretUID: "journal-1", LeaseName: "custody", LeaseUID: "custody-1", ExecutorPod: "executor-0"}
	for _, name := range []string{"command", "response", "journal"} {
		if err := f.client.Tracker().Add(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "hikyo", UID: types.UID(name + "-1"), ResourceVersion: "1"}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.client.Tracker().Add(&coordv1.Lease{ObjectMeta: metav1.ObjectMeta{Name: e.LeaseName, Namespace: "hikyo", UID: e.LeaseUID, ResourceVersion: "1"}}); err != nil {
		t.Fatal(err)
	}
	if err := f.client.Tracker().Add(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: e.ExecutorPod, Namespace: "hikyo", UID: "pod-1", ResourceVersion: "1"}, Spec: corev1.PodSpec{NodeName: "node-1"}}); err != nil {
		t.Fatal(err)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewController(f.client, e, public)
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewMailbox(f.client, e)
	if err != nil {
		t.Fatal(err)
	}
	return f, e, c, m, private
}

func signedFixture(t *testing.T, private ed25519.PrivateKey, e Enrollment, command Command) SignedCommand {
	t.Helper()
	command.EnrollmentID = e.ID
	command.IssuedAt = time.Now().UTC().Add(-time.Second)
	command.ExpiresAt = command.IssuedAt.Add(5 * time.Minute)
	signed, err := SignCommand(t.Context(), private, command)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func TestControllerSignedPrepareCommitAndRecovery(t *testing.T) {
	f, e, c, m, private := controllerFixture(t)
	prepare := signedFixture(t, private, e, Command{Sequence: 1, Action: ActionPrepare, Intent: f.intent, Changes: f.changes()})
	if err := m.Send(t.Context(), prepare); err != nil {
		t.Fatal(err)
	}
	if err := c.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	response, err := m.Response(t.Context(), prepare)
	if err != nil || !validDigest(response.PlanDigest) {
		t.Fatal(response, err)
	}
	if digest(f.deployment(t).Spec) != digest(f.before.Spec) {
		t.Fatal("planning mutated deployment")
	}
	submit := signedFixture(t, private, e, Command{Sequence: 2, Action: ActionSubmit, Intent: f.intent, PlanDigest: response.PlanDigest})
	if err := m.Send(t.Context(), submit); err != nil {
		t.Fatal(err)
	}
	fail := true
	f.client.PrependReactor("update", "secrets", func(action ktesting.Action) (bool, runtime.Object, error) {
		if fail && action.(ktesting.UpdateAction).GetObject().(*corev1.Secret).Name == "response" {
			fail = false
			return true, nil, errors.New("injected response outage")
		}
		return false, nil, nil
	})
	if err := c.reconcile(t.Context()); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	restarted, err := NewController(f.client, e, private.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	restarted.now = func() time.Time { return time.Now().Add(time.Hour) }
	if err := restarted.reconcile(t.Context()); err != nil {
		t.Fatalf("accepted expired command could not resume: %v", err)
	}
	response, err = m.Response(t.Context(), submit)
	if err != nil || response.Receipt == nil || response.Receipt.Phase != RolloutRequested {
		t.Fatal(response, err)
	}
	if response.Receipt.ApplicationAcknowledged {
		t.Fatal("deployment submission forged application proof")
	}
	if err := m.Send(t.Context(), prepare); !errors.Is(err, ErrConflict) {
		t.Fatal("mailbox replay accepted", err)
	}
	for _, name := range []string{"command", "response", "journal"} {
		secret := f.secret(t, name)
		if strings.Contains(string(mustJSON(secret.Data)), "UNRELATED_CANARY_DO_NOT_COPY") {
			t.Fatal("controller copied unrelated deployment secret")
		}
	}
}

func TestControllerRejectsUntrustedAndExpiredCommands(t *testing.T) {
	for _, mode := range []string{"signature", "owner", "incarnation", "enrollment", "expired", "future", "changed-value"} {
		t.Run(mode, func(t *testing.T) {
			f, e, c, _, private := controllerFixture(t)
			command := Command{Sequence: 1, Action: ActionPrepare, Intent: f.intent, Changes: f.changes()}
			if mode == "owner" {
				command.Intent.OwnerInstanceID = "other"
			}
			if mode == "incarnation" {
				command.Intent.Incarnation = "other"
			}
			signed := signedFixture(t, private, e, command)
			switch mode {
			case "signature":
				signed.Signature[0] ^= 1
			case "enrollment":
				signed.Command.EnrollmentID = "other"
			case "changed-value":
				signed.Command.Changes[0].Value = "999"
			case "expired":
				signed.Command.IssuedAt = time.Now().Add(-time.Hour)
				signed.Command.ExpiresAt = time.Now().Add(-time.Hour + time.Minute)
				signed, _ = SignCommand(t.Context(), private, signed.Command)
			case "future":
				signed.Command.IssuedAt = time.Now().Add(time.Hour)
				signed.Command.ExpiresAt = time.Now().Add(time.Hour + time.Minute)
				signed, _ = SignCommand(t.Context(), private, signed.Command)
			}
			s := f.secret(t, "command")
			putRecord(s, commandKey, signed)
			if _, err := f.client.CoreV1().Secrets("hikyo").Update(t.Context(), s, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			f.client.ClearActions()
			if err := c.reconcile(t.Context()); !errors.Is(err, ErrInvalid) {
				t.Fatalf("accepted %s: %v", mode, err)
			}
			for _, action := range f.client.Actions() {
				if action.GetVerb() != "get" {
					t.Fatal("untrusted command caused mutation")
				}
			}
		})
	}
}

func TestCustodyNeverExpiresAndPinsExactPod(t *testing.T) {
	f, e, _, _, _ := controllerFixture(t)
	old := &custody{client: f.client, enrollment: e, podUID: "pod-1"}
	if err := old.acquire(t.Context()); err != nil {
		t.Fatal(err)
	}
	lease, err := f.client.CoordinationV1().Leases("hikyo").Get(t.Context(), e.LeaseName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ancient := metav1.NewMicroTime(time.Now().Add(-365 * 24 * time.Hour))
	lease.Spec.RenewTime = &ancient
	if _, err := f.client.CoordinationV1().Leases("hikyo").Update(t.Context(), lease, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	other := &custody{client: f.client, enrollment: e, podUID: "pod-2"}
	if err := other.acquire(t.Context()); !errors.Is(err, ErrConflict) {
		t.Fatal("elapsed time transferred live Pod custody", err)
	}
	pod, err := f.client.CoreV1().Pods("hikyo").Get(t.Context(), e.ExecutorPod, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Model the StatefulSet's normal ordered replacement after old Pod removal.
	pod.UID = "pod-2"
	if err := f.client.Tracker().Update(corev1.SchemeGroupVersion.WithResource("pods"), pod, "hikyo"); err != nil {
		t.Fatal(err)
	}
	if err := other.acquire(t.Context()); err != nil {
		t.Fatal(err)
	}
	if other.epoch != old.epoch+1 {
		t.Fatal("custody epoch not advanced")
	}
	if err := old.verify(t.Context()); !errors.Is(err, ErrConflict) {
		t.Fatal("previous Pod retained custody", err)
	}
	guarded := custodyClient{Interface: f.client, owner: old}
	if _, err := guarded.AppsV1().Deployments("hikyo").Update(t.Context(), f.deployment(t), metav1.UpdateOptions{}); !errors.Is(err, ErrConflict) {
		t.Fatal("stale executor mutated deployment", err)
	}
	if _, err := guarded.CoreV1().Secrets("hikyo").Update(t.Context(), f.secret(t, "config"), metav1.UpdateOptions{}); !errors.Is(err, ErrConflict) {
		t.Fatal("stale executor mutated config", err)
	}
}

func TestControllerWaitsForActualAdmissionDenial(t *testing.T) {
	f, e, c, _, _ := controllerFixture(t)
	owner := &custody{client: f.client, enrollment: e, podUID: "pod-1"}
	if err := owner.acquire(t.Context()); err != nil {
		t.Fatal(err)
	}
	active := false
	f.client.PrependReactor("update", "*", func(action ktesting.Action) (bool, runtime.Object, error) {
		update := action.(ktesting.UpdateActionImpl)
		if len(update.GetUpdateOptions().DryRun) != 1 || update.GetUpdateOptions().DryRun[0] != metav1.DryRunAll {
			return false, nil, nil
		}
		if !active {
			return true, update.GetObject().DeepCopyObject(), nil
		}
		message := "Container authority outside configuration fields is immutable."
		kind := "Deployment"
		if action.GetResource().Resource == "secrets" {
			message = "Executor write must carry the current custody epoch."
			kind = "Secret"
		}
		return true, nil, apierrors.NewInvalid(schema.GroupKind{Kind: kind}, "fixture", field.ErrorList{field.Forbidden(field.NewPath("spec"), message)})
	})
	if err := c.proveAdmission(t.Context(), owner); !errors.Is(err, ErrUnavailable) {
		t.Fatal("accepted probe treated as active admission", err)
	}
	active = true
	if err := c.proveAdmission(t.Context(), owner); err != nil {
		t.Fatal(err)
	}
	if digest(f.deployment(t).Spec) != digest(f.before.Spec) {
		t.Fatal("dry-run probe mutated target")
	}
}

func TestBootstrapAliasesNeverCarrySourceContents(t *testing.T) {
	f := newFixture(t)
	f.target.DatabaseSources = map[string]SecretSource{"next": {Name: "database-next", Key: "HIKYO_DB"}}
	f.target.RootSources = map[string]SecretSource{"next": {Name: "root-next", Key: "root-key"}}
	d := f.deployment(t)
	server := &d.Spec.Template.Spec.Containers[0]
	server.Env = append(server.Env, databaseEnv(SecretSource{Name: "database-current", Key: "HIKYO_DB"}))
	d.Spec.Template.Spec.Volumes = append(d.Spec.Template.Spec.Volumes, corev1.Volume{Name: "root-key-source", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "root-current", Items: []corev1.KeyToPath{{Key: "root-key", Path: "root-key"}}}}})
	d.Spec.Template.Spec.InitContainers = []corev1.Container{{Name: "root-key-stage", Args: []string{"__hikyo-stage-root-key"}, VolumeMounts: []corev1.VolumeMount{{Name: "root-key-source", MountPath: "/run/hikyo-root-key-source", ReadOnly: true}}}}
	before, err := f.client.AppsV1().Deployments("hikyo").Update(t.Context(), d, metav1.UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	k, err := NewKubernetes(f.client, f.target)
	if err != nil {
		t.Fatal(err)
	}
	proof := BootstrapChanges{Database: &SourceProof{Alias: "next", SourceDigest: SourceDigest(f.target.DatabaseSources["next"]), ProofDigest: strings.Repeat("a", 64)}, Root: &SourceProof{Alias: "next", SourceDigest: SourceDigest(f.target.RootSources["next"]), ProofDigest: strings.Repeat("b", 64), RootEpoch: 2}}
	p, err := k.PrepareBootstrap(t.Context(), f.intent, nil, proof)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k.Submit(t.Context(), f.intent, p.Digest(), p); err != nil {
		t.Fatal(err)
	}
	after := f.deployment(t)
	if after.Spec.Template.Spec.Volumes[1].Secret.SecretName != "root-next" {
		t.Fatal("root source alias not installed")
	}
	if _, err := k.Restore(t.Context(), f.intent, p.Digest()); err != nil {
		t.Fatal(err)
	}
	if digest(f.deployment(t).Spec) != digest(before.Spec) {
		t.Fatal("bootstrap restore changed unrelated deployment inputs")
	}
	proof.Database.ProofDigest = ""
	if _, err := k.PrepareBootstrap(t.Context(), f.intent, nil, proof); !errors.Is(err, ErrInvalid) {
		t.Fatal("missing application proof accepted", err)
	}
	proof.Database.ProofDigest = strings.Repeat("a", 64)
	proof.Root.RootEpoch = 1
	if _, err := k.PrepareBootstrap(t.Context(), f.intent, nil, proof); !errors.Is(err, ErrInvalid) {
		t.Fatal("root without prepared dual-wrap epoch accepted", err)
	}
}
