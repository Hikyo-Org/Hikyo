package testfixture

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
)

func StableProvenance(t testing.TB, policy releasetrust.StablePolicy, manifest releasetrust.Manifest) []byte {
	t.Helper()
	var statement releasetrust.StableProvenance
	statement.Type = "https://in-toto.io/Statement/v1"
	statement.PredicateType = "https://slsa.dev/provenance/v1"
	for _, artifact := range manifest.Artifacts {
		if artifact.Kind != "build-provenance" {
			statement.Subject = append(statement.Subject, releasetrust.ProvenanceSubject{Name: artifact.Name, Digest: map[string]string{"sha256": artifact.SHA256}})
		}
	}
	statement.Predicate.BuildDefinition.BuildType = "https://hikyo.dev/build/github-actions/v1"
	statement.Predicate.BuildDefinition.ExternalParameters.Repository = policy.RepositoryURI
	statement.Predicate.BuildDefinition.ExternalParameters.Ref = "refs/tags/" + manifest.Tag
	return JSON(t, map[string]any{"_type": statement.Type, "subject": statement.Subject, "predicateType": statement.PredicateType, "predicate": map[string]any{
		"buildDefinition": map[string]any{"buildType": statement.Predicate.BuildDefinition.BuildType, "externalParameters": statement.Predicate.BuildDefinition.ExternalParameters, "resolvedDependencies": []any{map[string]any{"uri": "git+" + policy.RepositoryURI + "@refs/tags/" + manifest.Tag, "digest": map[string]string{"gitCommit": manifest.SourceCommit}}}},
		"runDetails":      map[string]any{"builder": map[string]string{"id": policy.RepositoryURI + "/" + policy.WorkflowPath + "@refs/tags/" + manifest.Tag}, "metadata": map[string]string{"invocationId": policy.RepositoryURI + "/actions/runs/123/attempts/1"}},
	}})
}
