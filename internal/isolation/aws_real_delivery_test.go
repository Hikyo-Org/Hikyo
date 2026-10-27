package isolation

import (
	"crypto/x509"
	"encoding/json"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm/awssmtest"
)

func TestAWSRealLabeledVersionDelivery(t *testing.T) {
	remote := awssmtest.New(awsAccount, awsRegion)
	t.Cleanup(remote.Close)
	roots := x509.NewCertPool()
	roots.AddCert(remote.Certificate())
	raw, err := json.Marshal(awssm.Descriptor{Mode: awssm.AuthStatic, Region: awsRegion, AccessKeyID: "AKIAHIKYOTEST0000001", SecretKey: "synthetic-version-observer"})
	if err != nil {
		t.Fatal("cannot encode local test descriptor")
	}
	client, err := awssm.NewClient(awssm.ClientConfig{Origin: remote.URL, Credential: string(raw), Deadline: 5 * time.Second, RootCAs: roots, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}})
	if err != nil {
		t.Fatal("cannot create local wire client")
	}
	t.Cleanup(client.Forget)
	name := "offline/version-observer"
	oldest, previous, current := strings.Repeat("d", 64), strings.Repeat("e", 64), strings.Repeat("a", 64)
	if err := client.CreateSecret(t.Context(), awssm.CreateSecretInput{Name: name, ClientRequestToken: oldest, Tags: map[string]string{adapter.SentinelName: "offline-target"}}); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{oldest, previous} {
		if err := client.PutSecretValue(t.Context(), name, version, "synthetic predecessor"); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.UpdateSecretVersionStage(t.Context(), name, "AWSCURRENT", previous, oldest); err != nil {
		t.Fatal(err)
	}
	before, err := client.DescribeSecret(t.Context(), name)
	if err != nil || len(before.Stages) != 2 {
		t.Fatal("wire fixture did not retain current and previous versions")
	}
	sandbox := &realAWSSandbox{manifest: realAWSManifest{ExpiresAt: time.Now().Add(time.Hour)}, owned: map[string]string{before.ARN: name}}
	writer := &realAWSWriter{API: client, s: sandbox, t: t}
	if err := writer.PutSecretValue(t.Context(), before.ARN, current, "synthetic delivery"); err != nil {
		t.Fatal(err)
	}
	if err := client.UpdateSecretVersionStage(t.Context(), before.ARN, "AWSCURRENT", current, previous); err != nil {
		t.Fatal(err)
	}
	// Adoption records the consent predecessor before promotion. Refusing the
	// subsequent current marker must preserve that consent until replay.
	if err := client.TagSecret(t.Context(), before.ARN, map[string]string{awssm.VersionTag: previous}); err != nil {
		t.Fatal(err)
	}
	partial, err := client.DescribeSecret(t.Context(), before.ARN)
	if err != nil || !realAWSMarkerRefused(before, partial, "offline-target", sandbox.recordedPutTokens(before.ARN)) || realAWSAddedVersion(before, partial, "offline-target", sandbox.recordedPutTokens(before.ARN)) {
		t.Fatal("intentional marker refusal was confused with completed delivery")
	}
	if err := client.UpdateSecretVersionStage(t.Context(), before.ARN, awssm.CurrentStage, current, ""); err != nil {
		t.Fatal(err)
	}
	if err := client.TagSecret(t.Context(), before.ARN, map[string]string{awssm.VersionTag: current}); err != nil {
		t.Fatal(err)
	}
	after, err := client.DescribeSecret(t.Context(), before.ARN)
	if err != nil || remote.Versions(name) != 3 || len(after.Stages) != 2 {
		t.Fatal("wire fixture did not omit the unlabelled deprecated version")
	}
	if realAWSMarkerRefused(before, after, "offline-target", sandbox.recordedPutTokens(before.ARN)) {
		t.Fatal("completed markers were accepted as the intentional refusal state")
	}
	if !realAWSAddedVersion(before, after, "offline-target", sandbox.recordedPutTokens(before.ARN)) {
		t.Fatal("fixture rejected exactly one new version because labeled count stayed constant")
	}
	if value, ok := remote.Value(name); !ok || value != "synthetic delivery" {
		t.Fatal("exact current version did not contain the expected synthetic value")
	}
	if realAWSReplayCompleted(1, before, after, "offline-target", sandbox.recordedPutTokens(before.ARN)) || !realAWSReplayCompleted(3, before, after, "offline-target", sandbox.recordedPutTokens(before.ARN)) {
		t.Fatal("replay load count replaced the exact single-version delivery proof")
	}
	for _, scenario := range []struct {
		name  string
		alter func(*awssm.SecretMetadata)
		puts  []string
	}{
		{"no write", func(*awssm.SecretMetadata) {}, nil},
		{"different-token duplicate", func(*awssm.SecretMetadata) {}, []string{current, strings.Repeat("b", 64)}},
		{"foreign ARN", func(m *awssm.SecretMetadata) { m.ARN += "foreign" }, []string{current}},
		{"foreign name", func(m *awssm.SecretMetadata) { m.Name += "foreign" }, []string{current}},
		{"foreign owner", func(m *awssm.SecretMetadata) { m.Tags[adapter.SentinelName] = "other" }, []string{current}},
		{"missing version tag", func(m *awssm.SecretMetadata) { delete(m.Tags, awssm.VersionTag) }, []string{current}},
		{"missing Hikyo current", func(m *awssm.SecretMetadata) { m.Stages[current] = []string{"AWSCURRENT"} }, []string{current}},
		{"duplicate Hikyo current", func(m *awssm.SecretMetadata) { m.Stages[previous] = append(m.Stages[previous], awssm.CurrentStage) }, []string{current}},
		{"duplicate AWS current", func(m *awssm.SecretMetadata) { m.Stages[previous] = append(m.Stages[previous], "AWSCURRENT") }, []string{current}},
		{"missing predecessor", func(m *awssm.SecretMetadata) { delete(m.Stages, previous) }, []string{current}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			candidate := awssm.SecretMetadata{ARN: after.ARN, Name: after.Name, Tags: map[string]string{}, Stages: map[string][]string{}}
			for key, value := range after.Tags {
				candidate.Tags[key] = value
			}
			for version, stages := range after.Stages {
				candidate.Stages[version] = append([]string(nil), stages...)
			}
			scenario.alter(&candidate)
			if realAWSAddedVersion(before, candidate, "offline-target", scenario.puts) || realAWSReplayCompleted(3, before, candidate, "offline-target", scenario.puts) {
				t.Fatal("delivery proof accepted missing, duplicate or foreign evidence")
			}
		})
	}
	// Observing the same completed delivery keeps the token without a write.
	replayed, err := client.DescribeSecret(t.Context(), before.ARN)
	if err != nil || !realAWSAddedVersion(before, replayed, "offline-target", sandbox.recordedPutTokens(before.ARN)) {
		t.Fatal("metadata-only replay lost exact single-write evidence")
	}
	if err := writer.PutSecretValue(t.Context(), before.ARN, current, "synthetic delivery"); err != nil {
		t.Fatal(err)
	}
	retried, err := client.DescribeSecret(t.Context(), before.ARN)
	if err != nil || remote.Versions(name) != 3 || !realAWSAddedVersion(before, retried, "offline-target", sandbox.recordedPutTokens(before.ARN)) {
		t.Fatal("retrying the same token created or falsely reported another version")
	}
}
