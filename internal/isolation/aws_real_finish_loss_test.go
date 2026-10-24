package isolation

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm/awssmtest"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Observe the real SQL settlement result without synthesizing its error.
type awsFinishLossStore struct {
	adapter.JobStore
	superseded *bool
	t          *testing.T
}

func (s awsFinishLossStore) Retry(ctx context.Context, job adapter.Job, due time.Time, revision int64, failed []adapter.Change, warnings []string, cause error) error {
	err := s.JobStore.Retry(ctx, job, due, revision, failed, warnings, cause)
	if errors.Is(err, adapter.ErrSuperseded) {
		*s.superseded = true
		s.t.Log("real SQL settlement refused the interrupted fence as ErrSuperseded")
	}
	return err
}

func TestAWSRealFinishLossDrain(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		m := realAWSManifestFixture(time.Now())
		remote := awssmtest.New(m.Account, m.Region)
		defer remote.Close()
		roots := x509.NewCertPool()
		roots.AddCert(remote.Certificate())
		raw, _ := json.Marshal(awssm.Descriptor{Mode: awssm.AuthStatic, Region: m.Region, AccessKeyID: "AKIAHIKYOTEST0000001", SecretKey: "synthetic-finish-loss"})
		s := &realAWSSandbox{manifest: m, owned: map[string]string{}}
		h := newRealAWSDurableFixture(t, db, s, "static", string(raw))
		h.origin = remote.URL
		h.build = func(config adapter.Config, credential string) (*adapter.ModuleLease, error) {
			client, err := awssm.NewClient(awssm.ClientConfig{Origin: config.Origin, Credential: credential, Deadline: time.Second, RootCAs: roots, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}, STSAllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}})
			if err != nil {
				return nil, err
			}
			var module adapter.Module = &awssm.Module{API: &realAWSWriter{API: client, s: s, t: t}}
			if h.finishAdoption != nil {
				module = &awsAdoptionFinishModule{Module: module, finish: h.finishAdoption}
			}
			return adapter.NewModuleLease(module, client.Forget)
		}
		h.restartWorker()
		h.publish("promoted source", "promoted alpha")
		name := h.prefix + "promoted-finish-loss"
		remote.Seed(name, "reviewed predecessor synthetic", nil)
		if err := s.reserveCreate(name); err != nil {
			t.Fatal(err)
		}
		lease, err := h.build(adapter.Config{Origin: h.origin}, h.descriptor)
		if err != nil {
			t.Fatal(err)
		}
		meta, err := lease.Module.(*awssm.Module).API.DescribeSecret(t.Context(), name)
		lease.Release()
		if err != nil {
			t.Fatal(err)
		}
		if err := s.remember(name, meta.ARN); err != nil {
			t.Fatal(err)
		}
		target := h.create(name, adapter.JSONObject)
		plan := h.planAdoption(target)
		h.adoptPlan(target, plan.ArtifactID, name)
		before := h.metadata(name)
		interrupted, superseded := false, false
		h.finishAdoption = func(id string) error {
			if id == target.ID && !interrupted {
				interrupted = true
				return errors.New("injected journal transaction loss after real promotion")
			}
			return nil
		}
		observer := &awsAdoptionReplayLoader{Loader: h.worker.Loader, t: t, targetID: target.ID}
		h.worker.Loader = observer
		h.worker.Store = awsFinishLossStore{JobStore: h.worker.Store, superseded: &superseded, t: t}
		h.drainWithFinishLoss(&interrupted)
		h.finishAdoption = nil
		if !interrupted || !superseded || !realAWSAddedVersion(before, h.metadata(name), target.ID, s.recordedPutTokens(before.ARN)) {
			t.Fatal("Finish fault did not retain exact promoted version and unresolved fence")
		}
		if queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_ledger WHERE target_id='%s' AND state='owned'`, target.ID)) != 1 {
			t.Fatal("Finish fault lost Owned custody")
		}
		if value, ok := remote.Value(name); !ok || value != `{"ALPHA":"promoted alpha","SHARED_KEY":"promoted source"}` {
			t.Fatal("Finish fault preceded expected wire delivery")
		}
		h.restartWorker()
		observer.Loader = h.worker.Loader
		h.worker.Loader = observer
		h.clock = h.clock.Add(adapter.LeaseTime + adapter.RetryCap + time.Second)
		h.drain()
		h.converged(target.ID)
		if observer.loads != 2 || !realAWSAddedVersion(before, h.metadata(name), target.ID, s.recordedPutTokens(before.ARN)) || remote.Versions(name) != 2 {
			t.Fatal("Finish recovery changed job, witness or exact single delivered version")
		}
		if queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_effects WHERE target_id='%s' AND outcome='unknown'`, target.ID)) != 1 {
			t.Fatal("Finish recovery lost unfinished-intent reconciliation")
		}
	})
}

func TestAWSRealFinishLossRefusalGuards(t *testing.T) {
	for _, scenario := range []struct {
		name              string
		err               error
		interrupted, want bool
	}{
		{"confirmed fence", adapter.ErrSuperseded, true, true},
		{"wrapped confirmed fence", fmt.Errorf("settlement: %w", adapter.ErrSuperseded), true, true},
		{"uninjected fence", adapter.ErrSuperseded, false, false},
		{"ordinary worker failure", errors.New("synthetic worker failure"), true, false},
		{"provider auth refusal", adapter.ErrProviderAuth, true, false},
		{"conflict refusal", adapter.ErrConflict, true, false},
		{"no error", nil, true, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			if realAWSFinishLossRefusal(scenario.err, scenario.interrupted) != scenario.want {
				t.Fatal("Finish-loss admission accepted the wrong interruption or error")
			}
		})
	}
}
