package configrollout

import (
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSingletonTopologyReplacementAndRestore(t *testing.T) {
	for _, rename := range []bool{false, true} {
		t.Run(map[bool]string{false: "ha", true: "identity"}[rename], func(t *testing.T) {
			f := newFixture(t)
			f.target.TopologyNodeIDs = []string{"hikyo-server", "renamed-server"}
			var err error
			f.executor, err = NewKubernetes(f.client, f.target)
			if err != nil {
				t.Fatal(err)
			}
			after := domain.SingletonTopology{HA: true, NodeID: "hikyo-server"}
			if rename {
				after.NodeID = "renamed-server"
			}
			change := domain.SingletonTopologyChange{Before: domain.SingletonTopology{NodeID: "hikyo-server"}, After: after}
			plan, err := f.executor.PrepareTopology(t.Context(), f.intent, change)
			if err != nil {
				t.Fatal(err)
			}
			if digest(f.deployment(t).Spec) != digest(f.before.Spec) {
				t.Fatal("preparation mutated deployment")
			}
			if !f.executor.validPlan(plan.data) {
				t.Fatal("own plan invalid")
			}
			if _, err := f.executor.Submit(t.Context(), f.intent, plan.Digest(), plan); err != nil {
				t.Fatal(err)
			}
			d := f.deployment(t)
			for _, want := range topologyEnvironment(after) {
				found := false
				for _, e := range d.Spec.Template.Spec.Containers[0].Env {
					found = found || digest(e) == digest(want)
				}
				if !found {
					t.Fatalf("missing installed %s", want.Name)
				}
			}
			if _, err := f.executor.Restore(t.Context(), f.intent, plan.Digest()); err != nil {
				t.Fatal(err)
			}
			if digest(f.deployment(t).Spec) != digest(f.before.Spec) {
				t.Fatal("restore did not recover exact original spec")
			}
		})
	}
}
func TestSingletonTopologyRefusesAuthorityAndCorrespondenceDrift(t *testing.T) {
	f := newFixture(t)
	change := domain.SingletonTopologyChange{Before: domain.SingletonTopology{NodeID: "hikyo-server"}, After: domain.SingletonTopology{HA: true, NodeID: "renamed-server"}}
	if _, err := f.executor.PrepareTopology(t.Context(), f.intent, change); err == nil {
		t.Fatal("unenrolled topology accepted")
	}
	f.target.TopologyNodeIDs = []string{"hikyo-server", "renamed-server"}
	var err error
	f.executor, err = NewKubernetes(f.client, f.target)
	if err != nil {
		t.Fatal(err)
	}
	wrong := change
	wrong.After.NodeID = "foreign"
	if _, err := f.executor.PrepareTopology(t.Context(), f.intent, wrong); err == nil {
		t.Fatal("foreign identity accepted")
	}
	wrong = change
	wrong.Before.HA = true
	if _, err := f.executor.PrepareTopology(t.Context(), f.intent, wrong); err == nil {
		t.Fatal("invented prior mode accepted")
	}
	plan, err := f.executor.PrepareTopology(t.Context(), f.intent, change)
	if err != nil {
		t.Fatal(err)
	}
	forged := plan.data
	forged.Topology = &domain.SingletonTopologyChange{Before: change.Before, After: domain.SingletonTopology{NodeID: "foreign"}}
	if f.executor.validPlan(forged) {
		t.Fatal("forged stored plan accepted")
	}
	d := f.deployment(t)
	two := int32(2)
	d.Spec.Replicas = &two
	if _, err := f.client.AppsV1().Deployments(f.target.Namespace).Update(t.Context(), d, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.executor.Submit(t.Context(), f.intent, plan.Digest(), plan); err == nil {
		t.Fatal("scale-out after preparation accepted")
	}
}

func TestInitialHASourceCorrespondenceCannotChangeTopology(t *testing.T) {
	f := newFixture(t)
	f.target.TopologyNodeIDs = []string{"hikyo-server", "renamed-server"}
	f.target.DatabaseSources = map[string]SecretSource{"next": {Name: "database-next", Key: "dsn"}}
	d := f.deployment(t)
	d.Spec.Template.Spec.Containers[0].Env = append(d.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{Name: string(HA), Value: "true"}, databaseEnv(SecretSource{Name: "database-current", Key: "dsn"}))
	before, err := f.client.AppsV1().Deployments(f.target.Namespace).Update(t.Context(), d, metav1.UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	k, err := NewKubernetes(f.client, f.target)
	if err != nil {
		t.Fatal(err)
	}
	actual := domain.SingletonTopology{HA: true, NodeID: "hikyo-server"}
	same := domain.SingletonTopologyChange{Before: actual, After: actual}
	source := BootstrapChanges{Database: &SourceProof{Alias: "next", SourceDigest: SourceDigest(f.target.DatabaseSources["next"]), ProofDigest: strings.Repeat("a", 64)}}
	plan, err := k.PrepareBootstrapWithTopology(t.Context(), f.intent, source, same)
	if err != nil {
		t.Fatal(err)
	}
	if !k.validPlan(plan.data) || len(plan.data.Delta.Environment) != 1 || plan.data.Delta.Environment[0].Name != "HIKYO_DB" {
		t.Fatal("correspondence emitted topology mutation")
	}
	wrong := same
	wrong.After.NodeID = "renamed-server"
	if _, err := k.PrepareBootstrapWithTopology(t.Context(), f.intent, source, wrong); err == nil {
		t.Fatal("source plan changed node identity")
	}
	wrong = same
	wrong.Before.HA, wrong.After.HA = false, false
	if _, err := k.PrepareBootstrapWithTopology(t.Context(), f.intent, source, wrong); err == nil {
		t.Fatal("source plan invented installed mode")
	}
	wrong = same
	wrong.Before.NodeID, wrong.After.NodeID = "renamed-server", "renamed-server"
	if _, err := k.PrepareBootstrapWithTopology(t.Context(), f.intent, source, wrong); err == nil {
		t.Fatal("source plan invented installed identity")
	}
	forged := plan.data
	forged.Delta.Environment = append(append([]envDelta(nil), forged.Delta.Environment...), envDelta{Name: string(HA), After: corev1.EnvVar{Name: string(HA), Value: "false"}})
	if k.validPlan(forged) {
		t.Fatal("source plan concealed topology mutation")
	}
	if _, err := k.Submit(t.Context(), f.intent, plan.Digest(), plan); err != nil {
		t.Fatal(err)
	}
	for _, e := range f.deployment(t).Spec.Template.Spec.Containers[0].Env {
		if e.Name == string(HA) && e.Value != "true" || e.Name == string(NodeID) && e.Value != "hikyo-server" {
			t.Fatal("source rollout changed topology")
		}
	}
	if _, err := k.Restore(t.Context(), f.intent, plan.Digest()); err != nil {
		t.Fatal(err)
	}
	if digest(f.deployment(t).Spec) != digest(before.Spec) {
		t.Fatal("source Restore changed installed topology")
	}
}
