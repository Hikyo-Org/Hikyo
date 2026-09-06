package configrollout

import (
	"encoding/json"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func upgradeSourceFixture() UpgradeCustodySource {
	return UpgradeCustodySource{BundleDirectory: "/run/hikyo-upgrade/bundle", StateDirectory: "/var/lib/hikyo-upgrade/operator-custody", OperatorPublicKeyFile: "/run/hikyo-upgrade/operator.pub"}
}

func TestUpgradeSourceEnrollmentRejectsArbitraryInputs(t *testing.T) {
	valid := upgradeSourceFixture()
	if !valid.Valid() {
		t.Fatal("valid source refused")
	}
	raw, _ := json.Marshal(valid)
	var parsed UpgradeCustodySource
	if err := json.Unmarshal(raw, &parsed); err != nil || parsed != valid {
		t.Fatalf("roundtrip: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for key := range fields {
		saved := fields[key]
		fields[key] = json.RawMessage("null")
		invalid, _ := json.Marshal(fields)
		if err := json.Unmarshal(invalid, &parsed); err == nil {
			t.Fatalf("null %s accepted", key)
		}
		fields[key] = saved
	}
	for _, change := range []func(*UpgradeCustodySource){
		func(s *UpgradeCustodySource) { s.StateDirectory = "/tmp/new-custody" },
		func(s *UpgradeCustodySource) { s.BundleDirectory = "/run/hikyo-upgrade/../private" },
		func(s *UpgradeCustodySource) { s.BundleDirectory = "/run/hikyo-upgrade/bundle/" },
		func(s *UpgradeCustodySource) { s.OperatorPublicKeyFile = "/run/hikyo-upgrade/pub\tkey" },
		func(s *UpgradeCustodySource) { s.EvidenceDirectory = "/run/hikyo-upgrade/evidence" },
		func(s *UpgradeCustodySource) { s.LegacyWritersStopped = true },
		func(s *UpgradeCustodySource) { s.TargetManifestSHA256 = strings.Repeat("G", 64) },
	} {
		source := valid
		change(&source)
		if source.Valid() {
			t.Fatalf("invalid source accepted: %+v", source)
		}
	}
	if err := json.Unmarshal([]byte(`{"bundle_directory":"/run/hikyo-upgrade/bundle"}`), &parsed); err == nil {
		t.Fatal("partial tuple accepted")
	}
}

func TestUpgradeBootstrapRolloutBindsCompleteTupleAndRestores(t *testing.T) {
	f := newFixture(t)
	initial := upgradeSourceFixture()
	next := initial
	next.BundleDirectory = "/run/hikyo-upgrade/next/bundle"
	next.StateDirectory = "/var/lib/hikyo-upgrade/aliases/next"
	next.OperatorPublicKeyFile = "/run/hikyo-upgrade/next/operator.pub"
	next.EvidenceDirectory = "/run/hikyo-upgrade/next/evidence"
	next.CiphertextPath = "/run/hikyo-upgrade/next/backup.age"
	next.TargetManifestSHA256 = strings.Repeat("a", 64)
	next.LegacyWritersStopped = true
	f.target.UpgradeSources = map[string]UpgradeCustodySource{"initial": initial, "next": next}
	f.target.InitialUpgradeSource = "initial"
	var err error
	f.executor, err = NewKubernetes(f.client, f.target)
	if err != nil {
		t.Fatal(err)
	}
	before := f.deployment(t)
	before.Spec.Template.Annotations[upgradeAliasAnnotation] = "initial"
	before.Spec.Template.Annotations[upgradeProofAnnotation] = ""
	container(before, "server").Env = append(container(before, "server").Env, initial.Environment()...)
	before, err = f.client.AppsV1().Deployments(f.target.Namespace).Update(t.Context(), before, metav1.UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	f.client.ClearActions()
	proof := &SourceProof{Alias: "next", SourceDigest: UpgradeSourceDigest(next), ProofDigest: strings.Repeat("b", 64)}
	plan, err := f.executor.PrepareBootstrap(t.Context(), f.intent, nil, BootstrapChanges{Upgrade: proof})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range f.client.Actions() {
		if action.GetVerb() != "get" {
			t.Fatal("prepare mutated cluster")
		}
	}
	if _, err := f.executor.Submit(t.Context(), f.intent, plan.Digest(), plan); err != nil {
		t.Fatal(err)
	}
	after := f.deployment(t)
	c := container(after, "server")
	for _, expected := range next.Environment() {
		found := false
		for _, env := range c.Env {
			if env.Name == expected.Name {
				found = true
				if digest(env) != digest(expected) {
					t.Fatal("mixed upgrade tuple")
				}
			}
		}
		if !found {
			t.Fatal("missing upgrade input")
		}
	}
	if after.Spec.Template.Annotations[upgradeProofAnnotation] != proof.ProofDigest || after.Spec.Template.Annotations[upgradeAliasAnnotation] != "next" {
		t.Fatal("missing source/material binding")
	}
	if c.Image != container(before, "server").Image || digest(after.Spec.Template.Spec.Volumes) != digest(before.Spec.Template.Spec.Volumes) || digest(c.VolumeMounts) != digest(container(before, "server").VolumeMounts) {
		t.Fatal("changed execution or custody authority")
	}
	if _, err := f.executor.Restore(t.Context(), f.intent, plan.Digest()); err != nil {
		t.Fatal(err)
	}
	restored := f.deployment(t)
	if digest(container(restored, "server").Env) != digest(container(before, "server").Env) || restored.Spec.Template.Annotations[upgradeAliasAnnotation] != "initial" || restored.Spec.Template.Annotations[upgradeProofAnnotation] != "" {
		t.Fatal("restore lost initial tuple/proof")
	}
	// Substituting an enrolled descriptor changes the source digest and cannot
	// reuse the old command even when the alias remains the same.
	changed := next
	changed.TargetManifestSHA256 = strings.Repeat("c", 64)
	f.executor.target.UpgradeSources["next"] = changed
	if f.executor.validBootstrap(&BootstrapChanges{Upgrade: proof}) {
		t.Fatal("accepted changed enrolled descriptor")
	}
}
