package isolation

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm/awssmtest"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/aws/aws-sdk-go-v2/aws"
)

type realAWSConsentVisibility struct {
	mu                        sync.Mutex
	arn, approved, prior      string
	lag, remaining            int
	staleReads, consentWrites int
}

type realAWSDelayedConsent struct {
	awssm.API
	visibility *realAWSConsentVisibility
}

func (a realAWSDelayedConsent) Region() string {
	return a.API.(interface{ Region() string }).Region()
}

func (a realAWSDelayedConsent) TagSecret(ctx context.Context, arn string, tags map[string]string) error {
	err := a.API.TagSecret(ctx, arn, tags)
	if err == nil && arn == a.visibility.arn && tags[awssm.VersionTag] == a.visibility.approved {
		a.visibility.mu.Lock()
		a.visibility.remaining = a.visibility.lag
		a.visibility.consentWrites++
		a.visibility.mu.Unlock()
	}
	return err
}

func (a realAWSDelayedConsent) DescribeSecret(ctx context.Context, arn string) (awssm.SecretMetadata, error) {
	meta, err := a.API.DescribeSecret(ctx, arn)
	a.visibility.mu.Lock()
	defer a.visibility.mu.Unlock()
	if err == nil && meta.ARN == a.visibility.arn && a.visibility.remaining > 0 {
		meta.Tags = maps.Clone(meta.Tags)
		meta.Tags[awssm.VersionTag] = a.visibility.prior
		a.visibility.remaining--
		a.visibility.staleReads++
	}
	return meta, err
}

// Exercise the real fixture's partial-conflict path with independent loopback
// writer, cleanup and value-oracle transports. This never loads AWS inputs.
func newRealAWSPartialConflictFixture(t *testing.T, db *store.DB) (*realAWSDurable, *awssmtest.Server) {
	t.Helper()
	h, remote := newRealAWSConvergenceFixture(t, db)
	roots := x509.NewCertPool()
	roots.AddCert(remote.Certificate())
	h.s.cleanup = externalAWS{
		origin: remote.URL, region: h.s.manifest.Region, account: h.s.manifest.Account, roots: roots,
		creds: aws.Credentials{AccessKeyID: "AKIAHIKYOCLEANUP00001", SecretAccessKey: "synthetic-local-cleanup"},
	}
	oracle := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		if r.Header.Get("X-Amz-Target") != "secretsmanager.GetSecretValue" || !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var request map[string]string
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&request) != nil || len(request) != 2 || request["VersionStage"] != "AWSCURRENT" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		h.s.mu.Lock()
		name, owned := h.s.owned[request["SecretId"]]
		h.s.mu.Unlock()
		value, visible := remote.Value(name)
		if !owned || !visible {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"__type": "ResourceNotFoundException"})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"SecretString": value})
	}))
	t.Cleanup(oracle.Close)
	h.s.oracle = externalAWS{
		origin: oracle.URL, region: h.s.manifest.Region, account: h.s.manifest.Account,
		creds: aws.Credentials{AccessKeyID: "AKIAHIKYOORACLE000001", SecretAccessKey: "synthetic-local-oracle"},
	}
	return h, remote
}

func TestAWSRealPartialConflictFlow(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		h, remote := newRealAWSPartialConflictFixture(t, db)
		prefix := h.prefix + "keys/"
		target := h.create(prefix, adapter.PerKey)
		h.publish("original shared", "original alpha")
		h.drain()
		h.converged(target.ID)
		h.partialConflict(target.ID, prefix)
		if len(h.s.creations) != 2 {
			t.Fatal("partial conflict changed creation accounting")
		}
		for _, operation := range remote.Operations() {
			if operation == "GetSecretValue" {
				t.Fatal("writer borrowed the value oracle")
			}
		}
	})
}

func TestAWSRealPartialConflictConsentVisibility(t *testing.T) {
	for _, lag := range []int{1, 2} {
		t.Run(string(rune('0'+lag))+" stale consent reads", func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				h, remote := newRealAWSPartialConflictFixture(t, db)
				prefix := h.prefix + "keys/"
				target := h.create(prefix, adapter.PerKey)
				h.publish("original shared", "original alpha")
				h.drain()
				h.converged(target.ID)
				meta := h.metadata(prefix + "ALPHA")
				visibility := &realAWSConsentVisibility{arn: meta.ARN, approved: strings.Repeat("b", 64), prior: meta.Tags[awssm.VersionTag], lag: lag}
				build := h.build
				h.build = func(config adapter.Config, credential string) (*adapter.ModuleLease, error) {
					lease, err := build(config, credential)
					if err != nil {
						return nil, err
					}
					module, ok := lease.Module.(*awssm.Module)
					if !ok {
						lease.Release()
						t.Fatal("unexpected local AWS module")
					}
					module.API = realAWSDelayedConsent{API: module.API, visibility: visibility}
					return lease, nil
				}
				h.restartWorker()
				h.partialConflict(target.ID, prefix)
				if visibility.consentWrites != 1 || visibility.staleReads != lag || len(h.s.creations) != 2 || remote.Versions(prefix+"ALPHA") != 3 {
					t.Fatal("consent visibility changed mutation, read or version accounting")
				}
			})
		})
	}
}

func TestAWSRealConsentObservationGuards(t *testing.T) {
	name := "hikyo-offline/consent"
	arn := "arn:aws:secretsmanager:eu-central-1:123456789012:secret:" + name + "-AbCd12"
	prior, approved, foreign := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	for _, scenario := range []struct {
		name      string
		stale     int
		wantReads int
		wantError bool
	}{
		{"visible", 0, 1, false},
		{"one stale tag", 1, 2, false},
		{"two stale tags", 2, 3, false},
		{"persistent stale tag", 9, 3, true},
		{"foreign ARN", 0, 1, true},
		{"foreign name", 0, 1, true},
		{"foreign owner", 0, 1, true},
		{"foreign current", 0, 1, true},
		{"foreign marker", 0, 1, true},
		{"changed key", 0, 1, true},
		{"deleted", 0, 1, true},
		{"access denied", 0, 1, true},
		{"cancelled before", 0, 0, true},
		{"cancelled during read", 0, 1, true},
		{"unapproved version", 0, 0, true},
		{"missing ARN", 0, 0, true},
		{"missing owner", 0, 0, true},
		{"missing marker", 0, 0, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			before := awssm.SecretMetadata{
				ARN: arn, Name: name, KMSKeyID: "synthetic-customer-key",
				Tags:   map[string]string{adapter.SentinelName: "owned-target", awssm.VersionTag: prior},
				Stages: map[string][]string{approved: {"AWSCURRENT"}, prior: {awssm.CurrentStage, "AWSPREVIOUS"}},
			}
			version := approved
			switch scenario.name {
			case "unapproved version":
				version = foreign
			case "missing ARN":
				before.ARN = ""
			case "missing owner":
				delete(before.Tags, adapter.SentinelName)
			case "missing marker":
				before.Stages[prior] = []string{"AWSPREVIOUS"}
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if scenario.name == "cancelled before" {
				cancel()
			}
			reads := 0
			api := realAWSMetadataProbe{describe: func(_ context.Context, id string) (awssm.SecretMetadata, error) {
				reads++
				if id != arn {
					t.Fatal("consent observation followed a name or foreign ARN")
				}
				meta := awssm.SecretMetadata{
					ARN: arn, Name: name, KMSKeyID: before.KMSKeyID,
					Tags:   map[string]string{adapter.SentinelName: "owned-target", awssm.VersionTag: approved},
					Stages: map[string][]string{approved: {"AWSCURRENT"}, prior: {awssm.CurrentStage, "AWSPREVIOUS"}},
				}
				if reads <= scenario.stale {
					meta.Tags[awssm.VersionTag] = prior
				}
				switch scenario.name {
				case "foreign ARN":
					meta.ARN += "foreign"
				case "foreign name":
					meta.Name += "foreign"
				case "foreign owner":
					meta.Tags[adapter.SentinelName] = "foreign"
				case "foreign current":
					delete(meta.Stages, approved)
					meta.Stages[foreign] = []string{"AWSCURRENT"}
				case "foreign marker":
					meta.Stages[prior] = []string{"AWSPREVIOUS"}
					meta.Stages[foreign] = []string{awssm.CurrentStage}
				case "changed key":
					meta.KMSKeyID = "different-customer-key"
				case "deleted":
					meta.Deleted = true
				case "access denied":
					return awssm.SecretMetadata{}, &awssm.ResponseError{Status: http.StatusForbidden, Code: "AccessDeniedException"}
				case "cancelled during read":
					cancel()
				}
				return meta, nil
			}}
			meta, err := readRealAWSConsentMetadata(ctx, api, before, version)
			if reads != scenario.wantReads || (err != nil) != scenario.wantError {
				t.Fatalf("consent observation reads=%d error=%t; want reads=%d error=%t", reads, err != nil, scenario.wantReads, scenario.wantError)
			}
			if strings.HasPrefix(scenario.name, "cancelled") && !errors.Is(err, context.Canceled) {
				t.Fatal("consent observation masked cancellation")
			}
			if scenario.name == "access denied" {
				var response *awssm.ResponseError
				if !errors.As(err, &response) || response.Code != "AccessDeniedException" {
					t.Fatal("consent observation masked provider refusal")
				}
			}
			if err == nil && (meta.ARN != arn || meta.Tags[awssm.VersionTag] != approved || realAWSCurrentVersion(meta) != approved) {
				t.Fatal("consent observation accepted a changed witness")
			}
		})
	}
}
