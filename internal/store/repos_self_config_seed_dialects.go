package store

import (
	"context"

	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

func (r sqliteSelfConfigStorage) seedInputs(ctx context.Context) ([]SelfConfigSeedInput, error) {
	rows, err := r.q.ListSelfConfigSeedInputs(ctx)
	if err != nil {
		return nil, err
	}
	var out []SelfConfigSeedInput
	for _, row := range rows {
		at, err := parseTime("self config seed", "heartbeat_at", row.HeartbeatAt)
		if err != nil {
			return nil, err
		}
		out = append(out, SelfConfigSeedInput{NodeID: row.NodeID, OwnerInstanceID: row.OwnerInstanceID, Incarnation: row.Incarnation, Fingerprint: row.Fingerprint, OwnerFingerprint: row.OwnerFingerprint, Ciphertext: row.Ciphertext, DEKVersion: row.DekVersion, RowVersion: row.RowVersion, SchemaVersion: row.SchemaVersion, UpdatedAt: at})
	}
	return out, nil
}

func (r pgSelfConfigStorage) seedInputs(ctx context.Context) ([]SelfConfigSeedInput, error) {
	rows, err := r.q.ListSelfConfigSeedInputs(ctx)
	if err != nil {
		return nil, err
	}
	var out []SelfConfigSeedInput
	for _, row := range rows {
		out = append(out, SelfConfigSeedInput{NodeID: row.NodeID, OwnerInstanceID: row.OwnerInstanceID, Incarnation: row.Incarnation, Fingerprint: row.Fingerprint, OwnerFingerprint: row.OwnerFingerprint, Ciphertext: row.Ciphertext, DEKVersion: int64(row.DekVersion), RowVersion: int64(row.RowVersion), SchemaVersion: row.SchemaVersion, UpdatedAt: row.HeartbeatAt.Time})
	}
	return out, nil
}

func (r sqliteSelfConfigStorage) putSeedInput(ctx context.Context, input SelfConfigSeedInput) error {
	if err := r.q.PutSelfConfigSeedAttestation(ctx, sqlitegen.PutSelfConfigSeedAttestationParams{NodeID: input.NodeID, SchemaVersion: input.SchemaVersion, Fingerprint: input.OwnerFingerprint, HeartbeatAt: fixedStamp(input.UpdatedAt)}); err != nil {
		return err
	}
	return r.q.PutSelfConfigSeedInput(ctx, sqlitegen.PutSelfConfigSeedInputParams{NodeID: input.NodeID, OwnerInstanceID: input.OwnerInstanceID, Incarnation: input.Incarnation, Fingerprint: input.Fingerprint, Ciphertext: input.Ciphertext, DekVersion: input.DEKVersion})
}

func (r pgSelfConfigStorage) putSeedInput(ctx context.Context, input SelfConfigSeedInput) error {
	if err := r.q.PutSelfConfigSeedAttestation(ctx, pggen.PutSelfConfigSeedAttestationParams{NodeID: input.NodeID, SchemaVersion: input.SchemaVersion, Fingerprint: input.OwnerFingerprint, HeartbeatAt: pgTimestamp(input.UpdatedAt)}); err != nil {
		return err
	}
	return r.q.PutSelfConfigSeedInput(ctx, pggen.PutSelfConfigSeedInputParams{NodeID: input.NodeID, OwnerInstanceID: input.OwnerInstanceID, Incarnation: input.Incarnation, Fingerprint: input.Fingerprint, Ciphertext: input.Ciphertext, DekVersion: int32(input.DEKVersion)})
}

func (r sqliteSelfConfigStorage) clearSeedInputs(ctx context.Context) error {
	return r.q.ClearSelfConfigSeedInputs(ctx)
}

func (r pgSelfConfigStorage) clearSeedInputs(ctx context.Context) error {
	return r.q.ClearSelfConfigSeedInputs(ctx)
}
