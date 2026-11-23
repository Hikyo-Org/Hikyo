//go:build k8se2e

package isolation

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/dynamic"
	hikyov1 "github.com/Hikyo-Org/hikyo/internal/operator/api/v1alpha1"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// testCredentialRotationRollout (#148): a scheduled credential rotation runs
// through the real executor and publish path (only the external PostgreSQL
// provider is a double), and the real operator then re-delivers the managed
// Secret and rolls the opted-in Deployment. Rotation is a publish, so the
// operator has no rotation-specific path: the changed delivery content moves the
// stamp and the existing rollout trigger fires.
func testCredentialRotationRollout(t *testing.T, restCfg *rest.Config, sch *runtime.Scheme) {
	e := newOpEnv(t, restCfg, sch, false)
	e.createInstance(instanceName, "")

	sa, cred := e.newWorkloadCredential("wl-rotation")
	grantE2ERead(t, e.db, sa.Principal)
	seedE2EReveal(t, e.db, "g_rotation_reveal", sa.Principal, domain.CapReveal)
	e.createBootstrapSecret("boot-rotation", cred.Value, instanceName, true)
	e.createPauseDeployment("rotapp", "rotation-secret")
	e.createPauseDeployment("rotapp-bystander")

	// The rotation authority is a signed-in human with a publish ceremony window;
	// it rotates SECRET_ONE (password) and CONFIG_ONE (username).
	execRaw(t, e.db, "INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_rotation_authority_reveal', '"+
		string(identAdmin)+"', 'reveal', '"+e2eOrg+"', '"+e2ePrj+"', NULL, "+ts+")")
	f := &rotationFixture{t: t, db: e.db, env: domain.EnvID(e2eEnv), scope: e2eScopeEnv(), now: time.Now().UTC().Truncate(time.Second)}
	f.dyn = &service.Dynamic{
		DB: e.db, Keyring: probeKeyring(t, e.db), Runtime: store.NewDynamicRuntime(e.db),
		LeaseDeadline: 5 * time.Second,
		ProviderFactory: func(dynamic.Kind, string, string, string) (dynamic.Provider, error) {
			return fakeDynamicProvider{ctl: &fakeDynamicControl{}}, nil
		},
	}
	provider, err := f.dyn.Configure(e.ctx, service.LocalPrincipal(identAdmin), e2eScopePrj(), service.CreateDynamicProviderRequest{
		Kind: string(dynamic.KindPostgres), Origin: "postgres://admin@db.rotation.example:5432/app",
		GrantRole: "app_rw", Credential: []byte(rotAdminSecret),
	})
	must(t, err)
	f.provider = provider.ID
	auth := authService(t, e.db)
	auth.ReauthWindow = 15 * time.Minute
	f.svc = &service.CredentialRotations{DB: e.db, Auth: auth, Now: func() time.Time { return f.now }}
	f.actor = f.session(identAdmin, true)
	x := newRotExecOn(t, e.db, f)
	rot := x.create("key_e2e_sec1", "key_e2e_cfg1")

	// 30s is the operator's minimum resync, which bounds how soon it sees the publish.
	cr := e.createCR(crSpec{name: "cr-rotation", target: "rotation-secret", secretRef: "boot-rotation", mapping: allFourMapping()})
	must(t, e.cl.Get(e.ctx, types.NamespacedName{Namespace: e.ns, Name: cr.Name}, cr))
	cr.Spec.ResyncInterval = "30s"
	must(t, e.cl.Update(e.ctx, cr))
	e.startManager()

	// Baseline: the original values are delivered and the Deployment is stamped.
	e.waitCondition("cr-rotation", hikyov1.ConditionReady, metav1.ConditionTrue, hikyov1.ReasonReconciled)
	waitRolled(t, e, "rotapp", "rotation-secret", "")
	before := e.getDeployment("rotapp")
	baselineStamp := stampOf(before, "rotation-secret")
	baselineGen := before.Generation
	baselineRS := e.replicaSetCount("rotapp")
	bystander := e.getDeployment("rotapp-bystander").Generation
	sec, ok := e.getSecret("rotation-secret")
	if !ok || string(sec.Data[secKeyOne]) != secValOne || string(sec.Data[cfgKeyOne]) != cfgValOne {
		t.Fatal("baseline Secret does not carry the original credential")
	}

	// Rotate through the real executor: due, mint, publish. The publish moves the
	// delivery cursor; nothing else touches the operator.
	x.advance(24*time.Hour + time.Minute)
	if did, err := x.sweep(x.worker); err != nil || !did {
		t.Fatalf("rotation sweep = %v, %v", did, err)
	}
	candidate := x.lastRun(rot.ID, "candidate_handle")
	role, minted := x.ctl.roles[candidate]
	if !minted || role.password == "" || role.password == secValOne {
		t.Fatalf("rotation did not mint a new credential for %q", candidate)
	}

	// The operator-managed Secret now carries the rotated credential...
	poll(t, e.ctx, func(context.Context) (bool, error) {
		got, ok := e.getSecret("rotation-secret")
		return ok && string(got.Data[secKeyOne]) == role.password && string(got.Data[cfgKeyOne]) == candidate, nil
	})
	got, _ := e.getSecret("rotation-secret")
	if string(got.Data[cfgKeyTwo]) != cfgValTwo || string(got.Data[secKeyTwo]) != secValTwo {
		t.Fatal("rotation changed keys it does not own")
	}

	// ...and the consuming Deployment rolled: new pod-template stamp, a new
	// generation and ReplicaSet, a ready replacement pod; the bystander is untouched.
	waitRolled(t, e, "rotapp", "rotation-secret", baselineStamp)
	if after := e.getDeployment("rotapp"); after.Generation <= baselineGen {
		t.Fatalf("Deployment generation did not advance: %d -> %d", baselineGen, after.Generation)
	}
	if rs := e.replicaSetCount("rotapp"); rs <= baselineRS {
		t.Fatalf("no new ReplicaSet after rotation: %d -> %d", baselineRS, rs)
	}
	if g := e.getDeployment("rotapp-bystander").Generation; g != bystander {
		t.Fatalf("non-opted-in Deployment rolled: generation %d -> %d", bystander, g)
	}
}

// waitRolled waits for a fully rolled-out, ready Deployment whose stamp for the
// secret is set and differs from notStamp.
func waitRolled(t *testing.T, e *opEnv, deployment, secret, notStamp string) {
	t.Helper()
	poll(t, e.ctx, func(ctx context.Context) (bool, error) {
		var d appsv1.Deployment
		if err := e.cl.Get(ctx, types.NamespacedName{Namespace: e.ns, Name: deployment}, &d); err != nil {
			return false, err
		}
		stamp := stampOf(&d, secret)
		return stamp != "" && stamp != notStamp && d.Status.ObservedGeneration >= d.Generation &&
			d.Status.Replicas == 1 && d.Status.UpdatedReplicas == 1 && d.Status.AvailableReplicas == 1, nil
	})
}
