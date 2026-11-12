package isolation

import (
	"context"
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm"
)

// A negative credential must be authentic, previously issued temporary static
// material retained through its natural expiry. This fixture does not forge or
// alter a session. File shape alone does not prove expiry; the durable job's
// auth refusal does.
func (s *realAWSSandbox) expiredStaticDescriptor(t *testing.T, path, freshPath string) string {
	t.Helper()
	raw := s.descriptor(t, realAWSActor{CredentialFile: path})
	fresh := s.descriptor(t, realAWSActor{CredentialFile: freshPath})
	d, err := awssm.ParseDescriptor(raw)
	current, currentErr := awssm.ParseDescriptor(fresh)
	if err != nil || currentErr != nil || d.Mode != awssm.AuthStatic || current.Mode != awssm.AuthStatic || d.SessionToken == "" || current.SessionToken == "" || d.AccessKeyID == current.AccessKeyID {
		t.Fatal("expired fixture requires a separate protected temporary static session")
	}
	return raw
}

// This emulator-only group consumes no CreateSecret reservations. The live
// smoke skips it; workload modes and expiry are owned by the controlled tests.
func runRealAWSAuthBoundaries(t *testing.T, s *realAWSSandbox) {
	t.Helper()
	s.mu.Lock()
	creations, mutations := len(s.creations), len(s.ops)
	s.mu.Unlock()
	t.Log("expired-* skipped on the emulator: STS refuses sessions under 900 s, see docs/testing/aws-acceptance-matrix.md")
	t.Run("configured-identity-refusal", func(t *testing.T) {
		_, client := s.writer(t, "static")
		api := &realAWSIdentityOnly{API: client}
		module := &awssm.Module{API: api}
		otherAccount := "111122223333"
		if otherAccount == s.manifest.Account {
			otherAccount = "444455556666"
		}
		_, err := module.TestConnection(t.Context(), adapter.ConnectionRequest{Destination: adapter.Destination{Kind: adapter.JSONObject, Owner: otherAccount, Name: s.manifest.Prefix + "static/identity-refusal"}, Gate: func(context.Context) error { return nil }})
		if !errors.Is(err, adapter.ErrDestinationID) || api.otherCalls != 0 {
			t.Fatal("configured account mismatch reached provider metadata or was accepted")
		}
		t.Log("region mismatch skipped on the emulator: it needs an AWS regional origin, see docs/testing/aws-acceptance-matrix.md")
	})
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.creations) != creations || len(s.ops) != mutations {
		t.Fatal("STS auth boundaries consumed a creation or mutation reservation")
	}
}

type realAWSIdentityOnly struct {
	awssm.API
	otherCalls int
}

func (a *realAWSIdentityOnly) DescribeSecret(context.Context, string) (awssm.SecretMetadata, error) {
	return awssm.SecretMetadata{}, a.unexpected()
}

func (a *realAWSIdentityOnly) ListSecretNames(context.Context, string, int) ([]string, error) {
	return nil, a.unexpected()
}

func (a *realAWSIdentityOnly) unexpected() error {
	a.otherCalls++
	return errors.New("identity refusal unexpectedly attempted a provider operation")
}

func (a *realAWSIdentityOnly) CreateSecret(context.Context, awssm.CreateSecretInput) error {
	return a.unexpected()
}

func (a *realAWSIdentityOnly) PutSecretValue(context.Context, string, string, string) error {
	return a.unexpected()
}

func (a *realAWSIdentityOnly) UpdateSecretVersionStage(context.Context, string, string, string, string) error {
	return a.unexpected()
}

func (a *realAWSIdentityOnly) TagSecret(context.Context, string, map[string]string) error {
	return a.unexpected()
}

func (a *realAWSIdentityOnly) RestoreSecret(context.Context, string) error {
	return a.unexpected()
}

func (a *realAWSIdentityOnly) DeleteSecret(context.Context, string) error {
	return a.unexpected()
}
