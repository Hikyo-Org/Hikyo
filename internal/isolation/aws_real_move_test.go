package isolation

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// The real fixture adds the activation half of the existing SQL loader seam.
// Admission, authority, pending destination and sealed credential remain owned
// by the real runtime; no test constructs a successful activation result.
type awsRealLoader struct{ awsTestLoader }

func (l awsRealLoader) LoadActivation(ctx context.Context, job adapter.Job, journal adapter.Journal) (adapter.LoadedActivation, error) {
	gate := func(ctx context.Context) error {
		return journal.Gate(ctx, adapter.Effect{Surface: adapter.Secret, EffectiveName: "route", Disposition: adapter.Update})
	}
	if err := gate(ctx); err != nil {
		return adapter.LoadedActivation{}, err
	}
	material, err := l.runtime.LoadActivation(ctx, job)
	if err != nil {
		return adapter.LoadedActivation{}, err
	}
	if material.Provider != string(adapter.AWSSecretsManagerProvider) {
		return adapter.LoadedActivation{}, errors.New("unexpected activation provider")
	}
	sealer, err := l.keyring.ForProject(ctx, job.OrgID, job.ProjectID)
	if err != nil {
		return adapter.LoadedActivation{}, err
	}
	if err := gate(ctx); err != nil {
		return adapter.LoadedActivation{}, err
	}
	credential, err := sealer.OpenField(adapter.CredentialAAD(job.OrgID, job.ProjectID, material.CredentialOwnerID), material.CredentialCiphertext)
	if err != nil {
		return adapter.LoadedActivation{}, err
	}
	defer crypto.Zero(credential)
	lease, err := l.build(adapter.Config{Origin: material.Origin}, string(credential))
	if err != nil {
		return adapter.LoadedActivation{}, err
	}
	return adapter.LoadedActivation{
		Module:  lease.Module,
		Request: adapter.ConnectionRequest{Config: adapter.Config{Origin: material.Origin}, Destination: material.Target.Destination, Gate: gate},
		Release: lease.Release,
	}, nil
}

func (h *realAWSDurable) moveTarget(targetID, oldPrefix, newName string) {
	t := h.t
	current := h.inspect(targetID)
	result, err := h.svc.ApplyTargetMutation(t.Context(), service.LocalPrincipal(alice), domain.Scope{Org: orgA, Project: prjA1}, service.UpdateAdapterTargetRequest{
		TargetID: targetID, ExpectedGeneration: current.Generation,
		Target: service.AdapterTargetInput{
			EnvironmentID: string(envA1), DestinationKind: string(adapter.JSONObject),
			DestinationOwner: h.s.manifest.Account, DestinationName: newName,
			DestinationEnvironment: h.s.manifest.KMSKeyARN,
			KeySelection:           &service.AdapterKeySelection{Names: []string{"SHARED_KEY", "ALPHA"}},
		},
	}, false)
	if err != nil {
		t.Fatal("AWS destination move was refused")
	}
	if _, ok := result.(service.TargetMutationMoveStarted); !ok {
		t.Fatal("AWS destination change bypassed the durable move workflow")
	}
	// Before activation, the route must still identify the old destination.
	if got := h.inspect(targetID); got.DestinationName != oldPrefix {
		t.Fatal("AWS destination changed before its old names were scrubbed")
	}
	h.drain()
	h.converged(targetID)
	if got := h.inspect(targetID); got.DestinationName != newName || got.DestinationKind != string(adapter.JSONObject) || got.Generation <= current.Generation {
		t.Fatal("AWS move did not activate the new generation and destination")
	}
	h.deleted(oldPrefix+"SHARED_KEY", true)
	h.deleted(oldPrefix+"ALPHA", true)
	if h.s.value(t, newName) != `{"ALPHA":"sibling alpha","SHARED_KEY":"sibling synthetic"}` {
		t.Fatal("moved AWS destination did not receive the current selected source")
	}
}

func TestAWSRealReplayCannotCreate(t *testing.T) {
	s := &realAWSSandbox{replayOnly: true}
	if err := s.reserveCreate("ignored"); err == nil || len(s.creations) != 0 {
		t.Fatal("replay child gained a fresh creation budget")
	}
}

func (h *realAWSDurable) retainAdapter(firstID, secondID string) {
	t := h.t
	h.s.mu.Lock()
	before := len(h.s.ops)
	h.s.mu.Unlock()
	result, err := h.svc.Delete(t.Context(), service.LocalPrincipal(alice), domain.Scope{Org: orgA, Project: prjA1}, h.adapterID, true)
	if err != nil || len(result.Targets) != 2 {
		t.Fatal("retain did not remove both active targets together")
	}
	h.clock = h.clock.Add(2 * adapter.LeaseTime)
	h.drain()
	for _, id := range []string{firstID, secondID} {
		if queryInt(t, h.db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_ledger WHERE target_id='%s' AND state<>'released'`, id)) != 0 {
			t.Fatal("multi-target retain kept active custody")
		}
	}
	h.s.mu.Lock()
	defer h.s.mu.Unlock()
	for _, operation := range h.s.ops[before:] {
		if operation == "DeleteSecret" {
			t.Fatal("multi-target retain deleted a remote secret")
		}
	}
}
