package isolation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// This external negative oracle can attempt GetSecretValue only on an exact
// synthetic ARN recorded by this run. It never broadens the production client.
// SDK acquisition is independent of awssm.Client; a second STS check binds the
// probe's credentials to the same approved role before the negative request.
func (s *realAWSSandbox) denialActor(t *testing.T, mode, raw string) externalAWS {
	t.Helper()
	d, err := awssm.ParseDescriptor(raw)
	if err != nil {
		t.Fatal("invalid IAM negative descriptor")
	}
	var creds aws.Credentials
	if d.Mode == awssm.AuthStatic {
		creds = aws.Credentials{AccessKeyID: d.AccessKeyID, SecretAccessKey: d.SecretKey, SessionToken: d.SessionToken}
	} else {
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(s.manifest.Region), config.WithHTTPClient(&http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}))
		if err != nil {
			t.Fatal("IAM negative credential source unavailable")
		}
		provider := cfg.Credentials
		if d.Mode == awssm.AuthAssumeRole {
			provider = stscreds.NewAssumeRoleProvider(sts.NewFromConfig(cfg), d.RoleARN, func(o *stscreds.AssumeRoleOptions) {
				o.ExternalID = aws.String(d.ExternalID)
				o.RoleSessionName = "hikyo-acceptance-denial"
				o.Duration = 15 * time.Minute
			})
		}
		// writer() isolated the ambient chain and bound both workload modes
		// to the approved projected token and exact role ARN. No profile,
		// container source or instance metadata fallback is available.
		creds, err = provider.Retrieve(ctx)
		if err != nil || creds.SessionToken == "" {
			t.Fatal("IAM negative temporary credentials unavailable")
		}
	}
	encoded, err := json.Marshal(awssm.Descriptor{Mode: awssm.AuthStatic, Region: s.manifest.Region, AccessKeyID: creds.AccessKeyID, SecretKey: creds.SecretAccessKey, SessionToken: creds.SessionToken})
	if err != nil {
		t.Fatal("cannot bind IAM negative identity")
	}
	s.client(t, string(encoded), false, s.manifest.Writers[mode].PrincipalARN)
	crypto.Zero(encoded)
	return externalAWS{origin: "https://secretsmanager." + s.manifest.Region + ".amazonaws.com", region: s.manifest.Region, account: s.manifest.Account, creds: creds}
}

func runRealAWSIAMDenial(t *testing.T, s *realAWSSandbox) {
	t.Helper()
	// One additional cleanup-owned synthetic canary stays live in the ambient
	// namespace. It is in the full approved run but outside the static writer.
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
	s.checkWriterDenials(t, "static", s.manifest.Prefix+"static/sqlite/document", name)
}

func (s *realAWSSandbox) checkWriterDenials(t *testing.T, mode, ownName, foreignName string) {
	t.Helper()
	raw, writer := s.writer(t, mode)
	probe := s.denialActor(t, mode, raw)
	defer func() { probe.creds = aws.Credentials{} }()
	if !s.manifest.ExpiresAt.After(time.Now()) {
		t.Fatal("IAM negative run window expired")
	}
	ownARN, foreignARN := s.arn(t, ownName), s.arn(t, foreignName)
	if !strings.HasPrefix(ownName, s.manifest.Prefix+mode+"/") || strings.HasPrefix(foreignName, s.manifest.Prefix+mode+"/") {
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
	if err := externalAWSCall(t.Context(), probe, "GetSecretValue", map[string]any{"SecretId": ownARN, "VersionStage": "AWSCURRENT"}, nil); !realAWSCode(err, "AccessDeniedException") {
		t.Fatal("AWS did not deny a writer's synthetic value read")
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
	err = module.API.TagSecret(t.Context(), meta.ARN, map[string]string{awssm.VersionTag: current})
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
