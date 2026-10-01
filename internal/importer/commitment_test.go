package importer

import (
	"bytes"
	"strings"
	"testing"
)

func TestValuesCommitmentsBlindIdenticalLowEntropySecrets(t *testing.T) {
	firstKey, err := NewValuesCommitmentKey()
	if err != nil {
		t.Fatal(err)
	}
	secondKey, err := NewValuesCommitmentKey()
	if err != nil {
		t.Fatal(err)
	}
	values := ValuesFile{FormatVersion: RunArtifactFormatVersion, CommitmentKey: firstKey, Project: "prj_test", Environment: "env_test", Entries: []ValuesEntry{{Key: "PASSWORD", Value: "password1"}}}
	first, err := ValuesCommitment(values)
	if err != nil {
		t.Fatal(err)
	}
	values.CommitmentKey = secondKey
	second, err := ValuesCommitment(values)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("independent private keys produced the same public commitment")
	}
	manifest := Manifest{FormatVersion: RunArtifactFormatVersion, ConnectorContractVersion: ConnectorContractVersion, Target: Target{Project: "prj_test", Environments: []string{"env_test"}}, ValuesDigests: []ValuesDigest{{Environment: "env_test", Digest: second}}}
	raw, err := Encode(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{firstKey, secondKey, "password1", "commitment_key"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Error("committable manifest includes private values material")
		}
	}
	values.Entries[0].Value = "password2"
	changed, _ := ValuesCommitment(values)
	if changed == second {
		t.Fatal("changed secret retained the same commitment")
	}
	values.CommitmentKey = ""
	if _, err := ValuesCommitment(values); err == nil {
		t.Fatal("commitment accepted an absent private key")
	}
}

func TestLegacyRunArtifactsRequireRegeneration(t *testing.T) {
	for _, parse := range []func([]byte) error{
		func(raw []byte) error { _, err := ParseManifest(raw); return err },
		func(raw []byte) error { _, err := ParseValuesFile(raw); return err },
	} {
		err := parse([]byte(`{"format_version":1}`))
		if err == nil || !strings.Contains(err.Error(), "regenerate") {
			t.Fatalf("v1 artifact refusal needs regeneration guidance: %v", err)
		}
	}
	bad := Manifest{FormatVersion: RunArtifactFormatVersion, ConnectorContractVersion: ConnectorContractVersion, ValuesDigests: []ValuesDigest{{Digest: "sha256:" + strings.Repeat("a", 64)}}}
	raw, _ := Encode(bad)
	if _, err := ParseManifest(raw); err == nil {
		t.Fatal("v2 manifest accepted an unkeyed digest")
	}
}

func TestSourceFileReferencesDoNotPublishPlaintextDigest(t *testing.T) {
	data := []byte("PASSWORD=password1")
	for _, source := range []string{k8sSource, vaultSource, infisicalSource} {
		ref := SourceFileReference(source, data, nil)
		if ref != "file-export" || ref == Digest(data) {
			t.Fatalf("%s plaintext source published a content oracle", source)
		}
		input := envFrom(t, "k8s-multi.yaml", "env_prod", nil, nil)
		input.FileDigest = ref
		plan, err := BuildProjectPlan(ProjectPlanInput{Source: source, Project: "prj_test", Envs: []EnvInput{input}})
		if err != nil {
			t.Fatal(err)
		}
		for _, artifact := range []any{plan.Template, plan.Manifest} {
			raw, _ := Encode(artifact)
			if bytes.Contains(raw, []byte(Digest(data))) {
				t.Fatal("committable output republished the plaintext source digest")
			}
		}
	}
	if ref := SourceFileReference(sopsSource, data, []Record{{PlaintextHint: true}}); ref != "file-export" {
		t.Fatal("partially encrypted SOPS input published a plaintext oracle")
	}
	encrypted := []byte("PASSWORD: ENC[AES256_GCM,data:ciphertext]")
	if ref := SourceFileReference(sopsSource, encrypted, []Record{{PlaintextHint: false}}); ref != Digest(encrypted) {
		t.Fatal("fully encrypted SOPS provenance lost its ciphertext digest")
	}
}
