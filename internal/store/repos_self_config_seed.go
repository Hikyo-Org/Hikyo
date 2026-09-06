package store

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

func (r selfConfigRepo) requireUnmanaged(ctx context.Context) error {
	_, err := r.q.binding(ctx, false)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return domain.ErrConflict
}

func (r selfConfigRepo) SeedInputs(ctx context.Context, p authz.Proof, localNodeID string, at time.Time) ([]SelfConfigSeedInput, error) {
	if localNodeID == "" {
		return nil, domain.ErrInvalid
	}
	if _, err := authz.Verify(p, authz.StoreSelfConfigSeedInputs, r.tok); err != nil {
		return nil, err
	}
	if err := r.requireUnmanaged(ctx); err != nil {
		return nil, err
	}
	return r.currentSeedInputs(ctx, localNodeID, at)
}

// HostSeedInputs discovers the actual server identity under closed host
// authority. Human preview reads retain their explicit node selection.
func (r selfConfigRepo) HostSeedInputs(ctx context.Context, p authz.Proof, at time.Time) ([]SelfConfigSeedInput, error) {
	if _, err := authz.Verify(p, authz.StoreSelfConfigHostSeedInputs, r.tok); err != nil {
		return nil, err
	}
	if err := r.requireUnmanaged(ctx); err != nil {
		return nil, err
	}
	return r.currentSeedInputs(ctx, "", at)
}

// MaxSelfConfigSeedInputBytes includes all bounded owner and node values,
// worst-case JSON escaping, metadata and AEAD framing. Final project values
// still obey their individual 64 KiB limits; this is encrypted transport only.
const MaxSelfConfigSeedInputBytes = 16 << 20

func (r selfConfigRepo) currentSeedInputs(ctx context.Context, localNodeID string, at time.Time) ([]SelfConfigSeedInput, error) {
	if at.IsZero() {
		return nil, domain.ErrInvalid
	}
	ids, err := r.q.participants(ctx, at.Add(-30*time.Second))
	if err != nil {
		return nil, err
	}
	inputs, err := r.q.seedInputs(ctx)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		if localNodeID != "" {
			ids = []string{localNodeID}
		} else {
			// No HA registry means standalone discovery, not permission to merge
			// unrelated or ambiguous fresh node attestations.
			for _, input := range inputs {
				if !input.UpdatedAt.Before(at.Add(-30*time.Second)) && !input.UpdatedAt.After(at) {
					ids = append(ids, input.NodeID)
				}
			}
			if len(ids) != 1 {
				return nil, ErrSelfConfigSeedDisagreement
			}
		}
	}
	var out []SelfConfigSeedInput
	for _, input := range inputs {
		if !slices.Contains(ids, input.NodeID) {
			continue
		}
		if input.UpdatedAt.Before(at.Add(-30*time.Second)) || input.UpdatedAt.After(at) {
			return nil, ErrSelfConfigSeedDisagreement
		}
		out = append(out, input)
	}
	if len(out) != len(ids) {
		return nil, ErrSelfConfigSeedDisagreement
	}
	return out, nil
}

func (r selfConfigRepo) PutSeedInput(ctx context.Context, p authz.Proof, input SelfConfigSeedInput) error {
	if _, err := authz.Verify(p, authz.StoreSelfConfigPutSeedInput, r.tok); err != nil {
		return err
	}
	if input.NodeID == "" || input.OwnerInstanceID == "" || input.Incarnation == "" || input.Fingerprint == "" || input.OwnerFingerprint == "" || input.SchemaVersion < 1 || input.DEKVersion < 1 || len(input.Ciphertext) == 0 || len(input.Ciphertext) > MaxSelfConfigSeedInputBytes || input.UpdatedAt.IsZero() {
		return domain.ErrInvalid
	}
	// The same lock as adoption prevents a writer from recreating temporary
	// input payloads after the atomic import cleared them.
	if err := r.q.lockMembership(ctx); err != nil {
		return err
	}
	if err := r.requireUnmanaged(ctx); err != nil {
		return err
	}
	return r.q.putSeedInput(ctx, input)
}

func (r selfConfigRepo) verifySeedReferences(ctx context.Context, binding SelfConfigBinding) error {
	// CreateBinding holds the membership lock. Re-read this transaction's
	// clock after acquiring it so queued imports cannot revive expired seeds.
	at, err := r.q.currentTime(ctx)
	if err != nil {
		return err
	}
	localNodeID := binding.SeedNodes[0].NodeID
	if binding.HostSeedDiscovery {
		localNodeID = ""
	}
	inputs, err := r.currentSeedInputs(ctx, localNodeID, at)
	if err != nil {
		return err
	}
	if len(inputs) != len(binding.SeedNodes) {
		return ErrSelfConfigSeedDisagreement
	}
	want := make(map[string]string, len(binding.SeedNodes))
	for _, node := range binding.SeedNodes {
		if node.NodeID == "" || node.Fingerprint == "" || want[node.NodeID] != "" {
			return domain.ErrInvalid
		}
		want[node.NodeID] = node.Fingerprint
	}
	for _, input := range inputs {
		if input.OwnerInstanceID != binding.OwnerInstanceID || input.Incarnation != binding.Incarnation || input.OwnerFingerprint != binding.SeedFingerprint || input.SchemaVersion != binding.SchemaVersion || want[input.NodeID] != input.Fingerprint {
			return ErrSelfConfigSeedDisagreement
		}
	}
	return nil
}
