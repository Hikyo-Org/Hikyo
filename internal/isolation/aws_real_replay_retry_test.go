package isolation

import (
	"context"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

type realAWSReplayReadFailure struct {
	awssm.API
	fail *bool
}

func (a realAWSReplayReadFailure) DescribeSecret(ctx context.Context, id string) (awssm.SecretMetadata, error) {
	if *a.fail {
		*a.fail = false
		return awssm.SecretMetadata{}, &awssm.ResponseError{Status: 500, Code: "InternalServiceError"}
	}
	return a.API.DescribeSecret(ctx, id)
}

func TestAWSRealMarkerReplayReadRetry(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		h, remote := newRealAWSConvergenceFixture(t, db)
		build := h.build
		fail := false
		h.build = func(config adapter.Config, credential string) (*adapter.ModuleLease, error) {
			lease, err := build(config, credential)
			if err != nil {
				return nil, err
			}
			module := lease.Module.(*awssm.Module)
			writer := module.API.(*realAWSWriter)
			writer.failCurrentMarker = &h.failCurrentMarker
			module.API = realAWSReplayReadFailure{API: writer, fail: &fail}
			return lease, nil
		}
		h.restartWorker()
		h.publish("promoted source", "promoted alpha")
		name := h.prefix + "promoted-marker-refusal"
		remote.Seed(name, "reviewed predecessor synthetic", nil)
		if err := h.s.reserveCreate(name); err != nil {
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
		if err := h.s.remember(name, meta.ARN); err != nil {
			t.Fatal(err)
		}
		target := h.create(name, adapter.JSONObject)
		plan := h.planAdoption(target)
		h.adoptPlan(target, plan.ArtifactID, name)
		before := h.metadata(name)
		observer := &awsAdoptionReplayLoader{Loader: h.worker.Loader, t: t, targetID: target.ID}
		h.worker.Loader = observer
		h.failCurrentMarker = true
		h.drain()
		if h.failCurrentMarker || !realAWSMarkerRefused(before, h.metadata(name), target.ID, h.s.recordedPutTokens(before.ARN)) {
			t.Fatal("intentional marker refusal was confused with completed delivery")
		}
		h.restartWorker()
		observer.Loader = h.worker.Loader
		h.worker.Loader = observer
		h.clock = h.clock.Add(adapter.LeaseTime + adapter.RetryCap + time.Second)
		fail = true
		h.drain()
		state := h.inspect(target.ID)
		if !h.s.manifest.ExpiresAt.After(time.Now()) || h.s.manifest.ExpiresAt.After(h.clock) || state.LastErrorClass != adapter.ErrorClassNetwork || state.ActiveJobState != "queued" || state.RetryAt == nil {
			t.Fatal("replay control did not separate live authorization from its advanced queue clock")
		}
		h.converged(target.ID)
		if fail || observer.loads != 3 || remote.Versions(name) != 2 {
			t.Fatalf("replay control: failurePending=%v loads=%d versions=%d", fail, observer.loads, remote.Versions(name))
		}
		if !realAWSReplayCompleted(observer.loads, before, h.metadata(name), target.ID, h.s.recordedPutTokens(before.ARN)) {
			t.Fatal("real promoted replay changed job/witness or duplicated a version")
		}
		if value, ok := remote.Value(name); !ok || value != `{"ALPHA":"promoted alpha","SHARED_KEY":"promoted source"}` {
			t.Fatal("exact current version did not contain the expected synthetic value")
		}
		for _, operation := range remote.Operations() {
			if operation == "GetSecretValue" {
				t.Fatal("writer read secret plaintext")
			}
		}
	})
}
