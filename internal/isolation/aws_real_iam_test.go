package isolation

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/aws/aws-sdk-go-v2/aws"
)

// This external negative oracle can attempt GetSecretValue only on an exact
// synthetic ARN recorded by this run. It never broadens the production client.
// A second STS identity check binds the probe's credentials to the same
// approved principal before the negative request.
func (s *realAWSSandbox) denialActor(t *testing.T, raw string) externalAWS {
	t.Helper()
	d, err := awssm.ParseDescriptor(raw)
	if err != nil || d.Mode != awssm.AuthStatic {
		t.Fatal("invalid IAM negative descriptor")
	}
	creds := aws.Credentials{AccessKeyID: d.AccessKeyID, SecretAccessKey: d.SecretKey, SessionToken: d.SessionToken}
	encoded, err := json.Marshal(awssm.Descriptor{Mode: awssm.AuthStatic, Region: s.manifest.Region, AccessKeyID: creds.AccessKeyID, SecretKey: creds.SecretAccessKey, SessionToken: creds.SessionToken, STSOrigin: s.stsOrigin})
	if err != nil {
		t.Fatal("cannot bind IAM negative identity")
	}
	s.client(t, string(encoded), s.manifest.Writers["static"].PrincipalARN)
	crypto.Zero(encoded)
	return externalAWS{origin: s.origin, region: s.manifest.Region, account: s.manifest.Account, roots: s.roots, allowed: s.allowed, creds: creds}
}

func runRealAWSIAMDenial(t *testing.T, s *realAWSSandbox) {
	t.Helper()
	// One additional cleanup-owned synthetic canary stays live in a namespace
	// no writer owns. It is inside the approved run but outside the static writer.
	name := s.manifest.Prefix + "ambient/iam-canary"
	var seeded struct {
		ARN string `json:"ARN"`
	}
	if err := s.call(t.Context(), false, "CreateSecret", name, map[string]any{"SecretString": "iam-synthetic-canary", "KmsKeyId": s.manifest.KMSKeyARN, "ClientRequestToken": strings.Repeat("c", 64)}, &seeded); err != nil {
		t.Fatal("IAM synthetic seed failed")
	}
	if err := s.remember(name, seeded.ARN); err != nil {
		t.Fatal(err)
	}
	if s.value(t, name) != "iam-synthetic-canary" {
		t.Fatal("positive synthetic oracle control failed")
	}
	own := s.manifest.Prefix + "static/sqlite/document"
	if s.emulated() {
		// The lifecycle group that normally delivers this value is skipped on
		// the emulator, so the cleanup actor seeds the writer's own namespace.
		var ownSeed struct {
			ARN string `json:"ARN"`
		}
		if err := s.call(t.Context(), false, "CreateSecret", own, map[string]any{"SecretString": "iam-synthetic-own", "KmsKeyId": s.manifest.KMSKeyARN, "ClientRequestToken": strings.Repeat("d", 64)}, &ownSeed); err != nil {
			t.Fatal("IAM synthetic own-namespace seed failed")
		}
		if err := s.remember(own, ownSeed.ARN); err != nil {
			t.Fatal(err)
		}
	}
	s.checkWriterDenials(t, own, name)
}

func (s *realAWSSandbox) checkWriterDenials(t *testing.T, ownName, foreignName string) {
	t.Helper()
	raw, writer := s.writer(t, "static")
	probe := s.denialActor(t, raw)
	defer func() { probe.creds = aws.Credentials{} }()
	if !s.manifest.ExpiresAt.After(time.Now()) {
		t.Fatal("IAM negative run window expired")
	}
	ownARN, foreignARN := s.arn(t, ownName), s.arn(t, foreignName)
	if !strings.HasPrefix(ownName, s.manifest.Prefix+"static/") || strings.HasPrefix(foreignName, s.manifest.Prefix+"static/") {
		t.Fatal("IAM negative namespaces are not distinct")
	}
	// A broad policy causes only a harmless synthetic tag, followed by a hard
	// failure. No ownership/version marker or value is overwritten.
	err := writer.TagSecret(t.Context(), foreignARN, map[string]string{"HIKYO_ACCEPTANCE_DENY_PROBE": "synthetic"})
	var response *awssm.ResponseError
	if !errors.Is(err, adapter.ErrProviderAuth) || !errors.As(err, &response) || response.Code != "AccessDeniedException" {
		t.Fatal("AWS did not enforce the cross-writer namespace mutation deny")
	}
	// Use the writer's OWN namespace after a successful value delivery, so a
	// namespace or missing-secret error cannot masquerade as the no-read deny.
	err = externalAWSCall(t.Context(), probe, "GetSecretValue", map[string]any{"SecretId": ownARN, "VersionStage": "AWSCURRENT"}, nil)
	if !realAWSCode(err, "AccessDeniedException") {
		t.Fatal("AWS did not deny a writer's synthetic value read")
	}
	if s.emulated() {
		t.Log("explicit versus implicit deny skipped on the emulator: it grants no read, so it cannot tell NeverReadValues from an implicit deny, see docs/testing/aws-acceptance-matrix.md")
		t.Log("NeverForceDelete probe skipped on the emulator: it evaluates no condition keys, so the conditional deny never matches and the probe would delete the secret, see docs/testing/aws-acceptance-matrix.md")
		return
	}
	// An implicit deny also yields AccessDenied. Only AWS's own statement that
	// an identity-based policy explicitly denied proves NeverReadValues applied.
	switch realAWSDeny(err) {
	case realAWSDenyExplicitIdentity:
	case realAWSDenyImplicit:
		t.Fatal("AWS denied the writer's value read only implicitly; NeverReadValues did not apply")
	case realAWSDenyExplicitOther:
		t.Fatal("AWS denied the writer's value read by a policy other than the identity-based NeverReadValues")
	default:
		t.Fatal("AWS did not state the deny kind for the writer's value read")
	}
	// NeverForceDelete is a conditional deny. The secret must be live before
	// the probe, so surviving it proves the deny and not an earlier deletion.
	s.requireLive(t, ownARN, "before the force-delete probe")
	err = externalAWSCall(t.Context(), probe, "DeleteSecret", map[string]any{"SecretId": ownARN, "ForceDeleteWithoutRecovery": true}, nil)
	if !realAWSCode(err, "AccessDeniedException") || realAWSDeny(err) != realAWSDenyExplicitIdentity {
		t.Fatal("AWS did not explicitly deny the writer's force delete")
	}
	s.requireLive(t, ownARN, "after the denied force delete")
}

// requireLive reads metadata through the cleanup actor, never a value.
func (s *realAWSSandbox) requireLive(t *testing.T, arn, when string) {
	t.Helper()
	var meta struct {
		DeletedDate *float64 `json:"DeletedDate"`
	}
	if err := s.call(t.Context(), false, "DescribeSecret", arn, map[string]any{}, &meta); err != nil || meta.DeletedDate != nil {
		t.Fatal("writer's own secret is not live " + when)
	}
}

func (h *realAWSDurable) partialConflict(targetID, prefix string) {
	t := h.t
	name := prefix + "ALPHA"
	alphaBefore, sharedBefore := h.metadata(name), h.metadata(prefix+"SHARED_KEY")
	alphaPuts, sharedPuts := len(h.s.recordedPutTokens(alphaBefore.ARN)), len(h.s.recordedPutTokens(sharedBefore.ARN))
	if err := h.s.call(t.Context(), false, "PutSecretValue", h.s.arn(t, name), map[string]any{"SecretString": "partial external synthetic", "ClientRequestToken": strings.Repeat("b", 64)}, nil); err != nil {
		t.Fatal("partial conflict seed failed")
	}
	external := h.metadata(name)
	if !realAWSPromotedVersion(alphaBefore, external, targetID, []string{strings.Repeat("b", 64)}) || external.Tags[awssm.VersionTag] != alphaBefore.Tags[awssm.VersionTag] {
		t.Fatal("external conflict seed changed identity or approved version consent")
	}
	h.publish("partial sibling synthetic", "partial desired alpha")
	h.drain()
	state := h.inspect(targetID)
	if state.Health() == adapter.HealthConverged || state.LastErrorClass != adapter.ErrorClassConflict || h.s.value(t, name) != "partial external synthetic" || h.s.value(t, prefix+"SHARED_KEY") != "partial sibling synthetic" {
		t.Fatal("same-target partial failure hid conflict or blocked its sibling")
	}
	refused, sharedDelivered := h.metadata(name), h.metadata(prefix+"SHARED_KEY")
	sharedAttempts := h.s.recordedPutTokens(sharedBefore.ARN)
	if !realAWSSameCurrent(external, refused) || len(h.s.recordedPutTokens(alphaBefore.ARN)) != alphaPuts || !realAWSAddedVersion(sharedBefore, sharedDelivered, targetID, sharedAttempts[sharedPuts:]) {
		t.Fatal("partial conflict wrote an unapproved or duplicate version")
	}
	// Explicit metadata consent names the actual external version. This is
	// the documented manual drift-consent path, not a new Plan adoption.
	meta := h.metadata(name)
	current := realAWSCurrentVersion(meta)
	if current == "" {
		t.Fatal("external current version unavailable")
	}
	lease, err := h.build(adapter.Config{Origin: h.origin}, h.descriptor)
	if err != nil {
		t.Fatal("manual consent client unavailable")
	}
	module, ok := lease.Module.(*awssm.Module)
	if !ok {
		lease.Release()
		t.Fatal("unexpected consent module")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	err = module.API.TagSecret(ctx, meta.ARN, map[string]string{awssm.VersionTag: current})
	if err == nil {
		_, err = readRealAWSConsentMetadata(ctx, module.API, meta, current)
	}
	cancel()
	lease.Release()
	if err != nil {
		t.Fatal("explicit version consent failed")
	}
	h.clock = h.clock.Add(adapter.RetryCap + time.Second)
	h.drain()
	h.converged(targetID)
	alphaAttempts := h.s.recordedPutTokens(alphaBefore.ARN)
	if h.s.value(t, name) != "partial desired alpha" || !realAWSAddedVersion(refused, h.metadata(name), targetID, alphaAttempts[alphaPuts:]) || !realAWSSameCurrent(sharedDelivered, h.metadata(prefix+"SHARED_KEY")) || len(h.s.recordedPutTokens(sharedBefore.ARN)) != len(sharedAttempts) {
		t.Fatal("partial retry duplicated the successful sibling or failed the consented key")
	}
}

// Observe the one acknowledged consent tag before draining its queued retry.
// Only exact-ARN metadata reads repeat, within the caller's existing deadline.
// A changed owner, current version, marker, key or deletion state stops at once.
func readRealAWSConsentMetadata(ctx context.Context, api awssm.API, before awssm.SecretMetadata, approved string) (awssm.SecretMetadata, error) {
	marker, unique := realAWSStageVersion(before, awssm.CurrentStage)
	if before.ARN == "" || before.Name == "" || approved == "" || realAWSCurrentVersion(before) != approved || before.Tags[adapter.SentinelName] == "" || before.Deleted || !unique || marker == "" {
		return awssm.SecretMetadata{}, errors.New("invalid explicit consent witness")
	}
	sameWitness := func(meta awssm.SecretMetadata) bool {
		actualMarker, actualUnique := realAWSStageVersion(meta, awssm.CurrentStage)
		return meta.Deleted == before.Deleted && meta.KMSKeyID == before.KMSKeyID &&
			meta.Tags[adapter.SentinelName] == before.Tags[adapter.SentinelName] &&
			realAWSCurrentVersion(meta) == approved && actualUnique && actualMarker == marker
	}
	meta, err := readRealAWSMetadataWhere(ctx, api, before.Name, before.ARN, func(meta awssm.SecretMetadata) bool {
		return !sameWitness(meta) || meta.Tags[awssm.VersionTag] == approved
	})
	if err != nil {
		return awssm.SecretMetadata{}, err
	}
	if !sameWitness(meta) || meta.Tags[awssm.VersionTag] != approved {
		return awssm.SecretMetadata{}, errors.New("explicit consent metadata did not settle")
	}
	return meta, nil
}

func TestAWSRealFixtureDenyClassification(t *testing.T) {
	const prefix = "User: arn:aws:iam::123456789012:user/w is not authorized to perform: secretsmanager:GetSecretValue on resource: arn:aws:secretsmanager:eu-west-1:123456789012:secret:x-AbCdEf "
	for name, c := range map[string]struct {
		message string
		want    realAWSDenyKind
	}{
		"explicit-identity": {prefix + "with an explicit deny in an identity-based policy", realAWSDenyExplicitIdentity},
		"explicit-trailing": {prefix + "with an explicit deny in an identity-based policy.", realAWSDenyExplicitIdentity},
		"implicit":          {prefix + "because no identity-based policy allows the secretsmanager:GetSecretValue action", realAWSDenyImplicit},
		"scp":               {prefix + "with an explicit deny in a service control policy", realAWSDenyExplicitOther},
		"resource-based":    {prefix + "with an explicit deny in a resource-based policy", realAWSDenyExplicitOther},
		"empty":             {"", realAWSDenyUnknown},
		"unrelated":         {"Access to the resource is denied", realAWSDenyUnknown},
		"lookalike-without": {prefix + "without an explicit deny in an identity-based policy", realAWSDenyUnknown},
		"lookalike-no":      {prefix + "no explicit deny in an identity-based policy", realAWSDenyUnknown},
		"lookalike-hyphen":  {prefix + "with an explicit deny in an identity-based-policy", realAWSDenyExplicitOther},
	} {
		t.Run(name, func(t *testing.T) {
			if got := classifyAWSDeny(c.message); got != c.want {
				t.Fatalf("classified %q, want %q", got, c.want)
			}
		})
	}
	// The fixture keeps the enum only: neither the message nor its ARNs reach
	// the error, and a non-AccessDenied failure carries no classification.
	for code, want := range map[string]realAWSDenyKind{"AccessDeniedException": realAWSDenyExplicitIdentity, "ThrottlingException": realAWSDenyUnknown} {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"__type":"` + code + `","Message":"` + prefix + `with an explicit deny in an identity-based policy"}`))
		}))
		roots := x509.NewCertPool()
		roots.AddCert(server.Certificate())
		err := externalAWSCall(t.Context(), externalAWS{origin: server.URL, region: "eu-west-1", roots: roots, creds: aws.Credentials{AccessKeyID: "AKIAEXAMPLE", SecretAccessKey: "x"}}, "GetSecretValue", map[string]any{}, nil)
		server.Close()
		if !realAWSCode(err, code) || realAWSDeny(err) != want || strings.Contains(err.Error(), "arn:") {
			t.Fatalf("%s: wrong deny classification or leaked message", code)
		}
	}
}
