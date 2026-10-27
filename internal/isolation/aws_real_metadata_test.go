package isolation

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm/awssmtest"
)

type realAWSMetadataProbe struct {
	awssm.API
	describe func(context.Context, string) (awssm.SecretMetadata, error)
}

func (p realAWSMetadataProbe) DescribeSecret(ctx context.Context, arn string) (awssm.SecretMetadata, error) {
	return p.describe(ctx, arn)
}

func TestAWSRealMetadataObservationBounds(t *testing.T) {
	name := "hikyo-e2e-abcdefgh/recorded-metadata"
	arn := "arn:aws:secretsmanager:eu-west-1:123456789012:secret:" + name + "-AbCd12"
	for _, scenario := range []struct {
		name            string
		missing         int
		code            string
		wrongARN        bool
		wrongName       bool
		cancelBefore    bool
		cancelAfterRead bool
		wantReads       int
		wantError       bool
	}{
		{name: "visible", wantReads: 1},
		{name: "one missing observation", missing: 1, wantReads: 2},
		{name: "two missing observations", missing: 2, wantReads: 3},
		{name: "persistent absence", missing: 9, wantReads: 3, wantError: true},
		{name: "permanent denial", code: "AccessDeniedException", wantReads: 1, wantError: true},
		{name: "unknown refusal", code: "UnexpectedProviderCode", wantReads: 1, wantError: true},
		{name: "foreign ARN", wrongARN: true, wantReads: 1, wantError: true},
		{name: "foreign name", wrongName: true, wantReads: 1, wantError: true},
		{name: "cancelled before request", cancelBefore: true, wantReads: 0, wantError: true},
		{name: "cancelled after missing request", missing: 9, cancelAfterRead: true, wantReads: 1, wantError: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if scenario.cancelBefore {
				cancel()
			}
			reads := 0
			api := realAWSMetadataProbe{describe: func(_ context.Context, id string) (awssm.SecretMetadata, error) {
				reads++
				if id != arn {
					t.Error("metadata observation followed an unrecorded identity")
				}
				if scenario.cancelAfterRead {
					cancel()
				}
				if scenario.code != "" {
					return awssm.SecretMetadata{}, &awssm.ResponseError{Status: 400, Code: scenario.code}
				}
				if reads <= scenario.missing {
					return awssm.SecretMetadata{}, &awssm.ResponseError{Status: 400, Code: "ResourceNotFoundException"}
				}
				meta := awssm.SecretMetadata{ARN: arn, Name: name}
				if scenario.wrongARN {
					meta.ARN += "foreign"
				}
				if scenario.wrongName {
					meta.Name += "foreign"
				}
				return meta, nil
			}}
			_, err := readRealAWSMetadata(ctx, api, name, arn)
			if reads != scenario.wantReads || (err != nil) != scenario.wantError {
				t.Fatalf("reads=%d error=%v; want reads=%d error=%v", reads, err != nil, scenario.wantReads, scenario.wantError)
			}
			if (scenario.cancelBefore || scenario.cancelAfterRead) && !errors.Is(err, context.Canceled) {
				t.Fatal("metadata cancellation was not preserved")
			}
			if scenario.code != "" {
				var response *awssm.ResponseError
				if !errors.As(err, &response) || response.Code != scenario.code {
					t.Fatal("metadata refusal code was not preserved")
				}
			}
		})
	}
}

func TestAWSRealMetadataVisibility(t *testing.T) {
	manifest := realAWSManifestFixture(time.Now())
	remote := awssmtest.New(manifest.Account, manifest.Region)
	t.Cleanup(remote.Close)
	roots := x509.NewCertPool()
	roots.AddCert(remote.Certificate())
	raw, err := json.Marshal(awssm.Descriptor{Mode: awssm.AuthStatic, Region: manifest.Region, AccessKeyID: "AKIAHIKYOTEST0000001", SecretKey: "synthetic-metadata-probe"})
	if err != nil {
		t.Fatal(err)
	}
	sandbox := &realAWSSandbox{manifest: manifest, owned: make(map[string]string)}
	h := &realAWSDurable{t: t, s: sandbox, descriptor: string(raw), origin: remote.URL}
	h.build = func(config adapter.Config, credential string) (*adapter.ModuleLease, error) {
		client, err := awssm.NewClient(awssm.ClientConfig{Origin: config.Origin, Credential: credential, Deadline: 5 * time.Second, RootCAs: roots, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}, STSAllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}})
		if err != nil {
			return nil, err
		}
		return adapter.NewModuleLease(&awssm.Module{API: client}, client.Forget)
	}
	name := manifest.Prefix + "metadata-replacement-probe"
	if err := sandbox.reserveCreate(name); err != nil {
		t.Fatal(err)
	}
	remote.Seed(name, "synthetic replacement", nil)
	lease, err := h.build(adapter.Config{Origin: h.origin}, h.descriptor)
	if err != nil {
		t.Fatal(err)
	}
	module := lease.Module.(*awssm.Module)
	expected, err := module.API.DescribeSecret(t.Context(), name)
	lease.Release()
	if err != nil {
		t.Fatal(err)
	}
	if err := sandbox.remember(name, expected.ARN); err != nil {
		t.Fatal(err)
	}
	remote.FailNext("DescribeSecret", 400, "ResourceNotFoundException", "")
	actual := h.metadata(name)
	if actual.ARN != expected.ARN || actual.Name != name || len(sandbox.creations) != 1 {
		t.Fatal("metadata observation changed acknowledged identity or creation accounting")
	}
	for _, operation := range remote.Operations() {
		if operation != "DescribeSecret" {
			t.Fatalf("metadata observation performed %s", operation)
		}
	}
}
