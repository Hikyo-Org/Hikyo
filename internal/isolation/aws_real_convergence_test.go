package isolation

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"net/netip"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm/awssmtest"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

type realAWSConvergenceFailure struct {
	awssm.API
	reads *int
}

func (a realAWSConvergenceFailure) DescribeSecret(context.Context, string) (awssm.SecretMetadata, error) {
	*a.reads = *a.reads + 1
	return awssm.SecretMetadata{}, &awssm.ResponseError{Status: 500, Code: "InternalServiceError"}
}

func TestAWSRealConvergenceAttemptBudget(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		h, remote := newRealAWSConvergenceFixture(t, db)
		name := h.prefix + "persistent-retry"
		target := h.create(name, adapter.JSONObject)
		h.publish("original shared", "original alpha")
		h.drain()
		h.converged(target.ID)
		h.publish("new shared", "new alpha")
		build := h.build
		reads := 0
		h.build = func(config adapter.Config, credential string) (*adapter.ModuleLease, error) {
			lease, err := build(config, credential)
			if err != nil {
				return nil, err
			}
			module := lease.Module.(*awssm.Module)
			module.API = realAWSConvergenceFailure{API: module.API, reads: &reads}
			return lease, nil
		}
		h.restartWorker()
		h.drain()
		before := reads
		// A smaller remaining slice proves positive convergence does not reset
		// the original drain budget or turn a persistent refusal into success.
		h.drainRemaining = 2
		state := h.retryConvergence(target.ID)
		if reads != before+2 || h.drainRemaining != 0 || state.Health() == adapter.HealthConverged || state.LastErrorClass != adapter.ErrorClassNetwork {
			t.Fatalf("persistent retry reads=%d remaining=%d health=%s class=%s", reads-before, h.drainRemaining, state.Health(), state.LastErrorClass)
		}
		if remote.Versions(name) != 1 || len(h.s.creations) != 1 {
			t.Fatal("persistent retry changed value versions or creation accounting")
		}
	})
}

func newRealAWSConvergenceFixture(t *testing.T, db *store.DB) (*realAWSDurable, *awssmtest.Server) {
	t.Helper()
	manifest := realAWSManifestFixture(time.Now())
	remote := awssmtest.New(manifest.Account, manifest.Region)
	t.Cleanup(remote.Close)
	roots := x509.NewCertPool()
	roots.AddCert(remote.Certificate())
	raw, err := json.Marshal(awssm.Descriptor{Mode: awssm.AuthStatic, Region: manifest.Region, AccessKeyID: "AKIAHIKYOTEST0000001", SecretKey: "synthetic-retry-probe"})
	if err != nil {
		t.Fatal(err)
	}
	sandbox := &realAWSSandbox{manifest: manifest, owned: make(map[string]string)}
	h := newRealAWSDurableFixture(t, db, sandbox, "static", string(raw))
	h.origin = remote.URL
	h.build = func(config adapter.Config, credential string) (*adapter.ModuleLease, error) {
		client, err := awssm.NewClient(awssm.ClientConfig{Origin: config.Origin, Credential: credential, Deadline: 5 * time.Second, RootCAs: roots, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}, STSAllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}})
		if err != nil {
			return nil, err
		}
		return adapter.NewModuleLease(&awssm.Module{API: &realAWSWriter{API: client, s: sandbox, t: t}}, client.Forget)
	}
	h.restartWorker()
	return h, remote
}

func TestAWSRealConvergenceRetry(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		h, remote := newRealAWSConvergenceFixture(t, db)
		name := h.prefix + "retry-probe"
		target := h.create(name, adapter.JSONObject)
		h.publish("retry shared", "retry alpha")
		h.drain()
		h.converged(target.ID)
		h.publish("retry updated shared", "retry updated alpha")
		remote.FailNext("DescribeSecret", 500, "InternalServiceError", "")
		h.drain()
		h.converged(target.ID)
		if got, ok := remote.Value(name); !ok || got != `{"ALPHA":"retry updated alpha","SHARED_KEY":"retry updated shared"}` {
			t.Fatal("retry did not deliver the synthetic value")
		}
		creates := 0
		for _, operation := range remote.Operations() {
			if operation == "CreateSecret" {
				creates++
			}
			if operation == "GetSecretValue" {
				t.Fatal("writer read secret plaintext")
			}
		}
		if creates != 1 || len(h.s.creations) != 1 {
			t.Fatalf("retry changed creation accounting: wire=%d reservations=%d", creates, len(h.s.creations))
		}
		if remote.Versions(name) != 2 {
			t.Fatal("retry duplicated a delivered version")
		}
	})
}

func TestAWSRealConvergenceRetryRefusals(t *testing.T) {
	now := time.Now().UTC()
	due := now.Add(30 * time.Second)
	for _, scenario := range []struct {
		name     string
		state    store.AdapterTarget
		expires  time.Time
		wantNext bool
	}{
		{"network", store.AdapterTarget{ActiveJobState: "queued", RetryAt: &due, LastErrorClass: adapter.ErrorClassNetwork}, now.Add(time.Hour), true},
		{"provider limit", store.AdapterTarget{ActiveJobState: "queued", RetryAt: &due, LastErrorClass: adapter.ErrorClassProviderLimit}, now.Add(time.Hour), true},
		{"auth", store.AdapterTarget{ActiveJobState: "queued", RetryAt: &due, LastErrorClass: adapter.ErrorClassAuth}, now.Add(time.Hour), false},
		{"conflict", store.AdapterTarget{ActiveJobState: "queued", RetryAt: &due, LastErrorClass: adapter.ErrorClassConflict}, now.Add(time.Hour), false},
		{"ambiguity", store.AdapterTarget{ActiveJobState: "queued", RetryAt: &due, LastErrorClass: adapter.ErrorClassProviderAmbiguous}, now.Add(time.Hour), false},
		{"refused", store.AdapterTarget{ActiveJobState: "queued", RetryAt: &due, LastErrorClass: adapter.ErrorClassRefused}, now.Add(time.Hour), false},
		{"running", store.AdapterTarget{ActiveJobState: "running", RetryAt: &due, LastErrorClass: adapter.ErrorClassNetwork}, now.Add(time.Hour), false},
		{"no schedule", store.AdapterTarget{ActiveJobState: "queued", LastErrorClass: adapter.ErrorClassNetwork}, now.Add(time.Hour), false},
		{"paused", store.AdapterTarget{ActiveJobState: "queued", RetryAt: &due, PausedAt: &now, LastErrorClass: adapter.ErrorClassNetwork}, now.Add(time.Hour), false},
		{"operator attention", store.AdapterTarget{ActiveJobState: "queued", RetryAt: &due, DriftAttention: true, LastErrorClass: adapter.ErrorClassNetwork}, now.Add(time.Hour), false},
		{"expired", store.AdapterTarget{ActiveJobState: "queued", RetryAt: &due, LastErrorClass: adapter.ErrorClassNetwork}, now, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			next, retry := realAWSConvergenceRetry(scenario.state, now, now, scenario.expires)
			if retry != scenario.wantNext {
				t.Fatalf("retry=%v; want %v", retry, scenario.wantNext)
			}
			if retry && !next.After(due) {
				t.Fatal("retry preceded the recorded queue schedule")
			}
		})
	}
}

func TestAWSRealConvergenceRetrySeparatesClocks(t *testing.T) {
	wallNow := time.Now().UTC()
	queueClock := wallNow.Add(2 * time.Hour)
	due := queueClock.Add(time.Minute)
	state := store.AdapterTarget{ActiveJobState: "queued", RetryAt: &due, LastErrorClass: adapter.ErrorClassNetwork}
	if next, ok := realAWSConvergenceRetry(state, queueClock, wallNow, wallNow.Add(time.Minute)); !ok || !next.After(due) {
		t.Fatal("logical replay advance refused a live authorized retry")
	}
	for _, deadline := range []time.Time{wallNow, wallNow.Add(-time.Second)} {
		if _, ok := realAWSConvergenceRetry(state, queueClock, wallNow, deadline); ok {
			t.Fatal("queue replay bypassed an expired real authorization")
		}
	}
}

func TestAWSRealConvergenceAssertionVocabulary(t *testing.T) {
	for _, scenario := range []struct {
		class adapter.ErrorClass
		want  string
	}{
		{adapter.ErrorClassAuth, "durable AWS target did not converge: auth"},
		{adapter.ErrorClassNetwork, "durable AWS target did not converge: network"},
		{adapter.ErrorClassConflict, "durable AWS target did not converge: conflict"},
		{adapter.ErrorClassProviderLimit, "durable AWS target did not converge: provider_limit"},
		{adapter.ErrorClassProviderAmbiguous, "durable AWS target did not converge: provider_ambiguous"},
		{adapter.ErrorClassRefused, "durable AWS target did not converge: refused"},
		{"", "durable AWS target did not converge"},
		{"synthetic-private-credential\nnetwork", "durable AWS target did not converge"},
	} {
		if got := realAWSConvergenceAssertion(scenario.class); got != scenario.want {
			t.Fatal("convergence assertion escaped its fixed vocabulary")
		}
	}
}
