package store

import (
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

func sqliteTransitKey(c sqlitegen.TransitKeyByIDRow) (TransitKeyRecord, error) {
	item := TransitKeyRecord{ID: c.ID, EnvironmentID: c.EnvironmentID, Name: c.Name, Algorithm: c.Algorithm, Custody: c.Custody, AllowedOperations: decodeTransitOps(c.AllowedOperations), State: c.State, LatestVersion: uint32(c.LatestVersion), MinEncryptVersion: uint32(c.MinEncryptVersion), MinDecryptVersion: uint32(c.MinDecryptVersion), MinAvailableVersion: uint32(c.MinAvailableVersion), CompromisedThroughVersion: uint32(c.CompromisedThroughVersion), RotationPeriodSeconds: c.RotationPeriodSeconds, DeletionAfter: c.DeletionAfter.String, CreatedBy: c.CreatedBy, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
	if err := normalizeStoredTimes(&item.DeletionAfter, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return TransitKeyRecord{}, err
	}
	return item, nil
}
func sqliteTransitVersion(c sqlitegen.TransitListVersionsRow) (TransitVersionRecord, error) {
	item := TransitVersionRecord{ID: c.ID, Version: uint32(c.Version), PublicKey: c.PublicKey, HasMaterial: c.HasMaterial == 1, ExternalHeld: c.ExternalHeld == 1, CreatedAt: c.CreatedAt}
	if err := normalizeStoredTimes(&item.CreatedAt); err != nil {
		return TransitVersionRecord{}, err
	}
	return item, nil
}
func sqliteTransitCaller(c sqlitegen.TransitListCallersRow) (TransitCaller, error) {
	item := TransitCaller{PrincipalID: c.PrincipalID, Operations: decodeTransitOps(c.Operations)}
	return item, nil
}
func sqliteTransitMaterial(c sqlitegen.TransitVersionMaterialRow) (TransitVersionMaterial, error) {
	item := TransitVersionMaterial{ID: c.ID, Version: uint32(c.Version), Sealed: c.MaterialCiphertext, ExternalRef: c.ExternalRef.String, PublicKey: c.PublicKey}
	return item, nil
}
func sqliteTransitReencrypt(c sqlitegen.TransitListReencryptRow) (ReencryptFieldRow, error) {
	item := ReencryptFieldRow{ID: c.ID, EnvironmentID: c.EnvironmentID, KeyID: c.KeyID, Ciphertext: c.MaterialCiphertext}
	return item, nil
}
func sqliteTransitDeletion(c sqlitegen.TransitSelectDeletionDueRow) (TransitDueKey, error) {
	item := TransitKeyRecord{ID: c.ID, EnvironmentID: c.EnvironmentID, Name: c.Name, Algorithm: c.Algorithm, Custody: c.Custody, AllowedOperations: decodeTransitOps(c.AllowedOperations), State: c.State, LatestVersion: uint32(c.LatestVersion), MinEncryptVersion: uint32(c.MinEncryptVersion), MinDecryptVersion: uint32(c.MinDecryptVersion), MinAvailableVersion: uint32(c.MinAvailableVersion), CompromisedThroughVersion: uint32(c.CompromisedThroughVersion), RotationPeriodSeconds: c.RotationPeriodSeconds, DeletionAfter: c.DeletionAfter.String, CreatedBy: c.CreatedBy, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
	if err := normalizeStoredTimes(&item.DeletionAfter, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return TransitDueKey{}, err
	}
	out := TransitDueKey{OrgID: c.OrgID, ProjectID: c.ProjectID, EnvironmentID: c.EnvironmentID, TransitKeyRecord: item}
	return out, nil
}
func sqliteTransitRotation(c sqlitegen.TransitSelectRotationDueRow) (TransitDueKey, error) {
	item := TransitKeyRecord{ID: c.ID, EnvironmentID: c.EnvironmentID, Name: c.Name, Algorithm: c.Algorithm, Custody: c.Custody, AllowedOperations: decodeTransitOps(c.AllowedOperations), State: c.State, LatestVersion: uint32(c.LatestVersion), MinEncryptVersion: uint32(c.MinEncryptVersion), MinDecryptVersion: uint32(c.MinDecryptVersion), MinAvailableVersion: uint32(c.MinAvailableVersion), CompromisedThroughVersion: uint32(c.CompromisedThroughVersion), RotationPeriodSeconds: c.RotationPeriodSeconds, DeletionAfter: c.DeletionAfter.String, CreatedBy: c.CreatedBy, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
	if err := normalizeStoredTimes(&item.DeletionAfter, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return TransitDueKey{}, err
	}
	out := TransitDueKey{OrgID: c.OrgID, ProjectID: c.ProjectID, EnvironmentID: c.EnvironmentID, TransitKeyRecord: item}
	latest, err := readStoredTime(c.LatestCreatedAt)
	if err != nil {
		return TransitDueKey{}, err
	}
	if latest != nil {
		out.LatestCreatedAt = *latest
	}
	return out, nil
}
func sqliteTransitExternal(c sqlitegen.TransitExternalVersionsRow) (TransitVersionMaterial, error) {
	item := TransitVersionMaterial{ID: c.ID, Version: uint32(c.Version), ExternalRef: c.ExternalRef.String}
	return item, nil
}
func sqliteTransitGauge(c sqlitegen.TransitGaugeRotationCandidatesRow) (transitRotationCandidate, error) {
	created, err := readStoredTime(c.CreatedAt)
	return transitRotationCandidate{period: c.RotationPeriodSeconds, createdAt: created}, err
}
func pgTransitKey(c pggen.TransitKeyByIDRow) (TransitKeyRecord, error) {
	item := TransitKeyRecord{ID: c.ID, EnvironmentID: c.EnvironmentID, Name: c.Name, Algorithm: c.Algorithm, Custody: c.Custody, AllowedOperations: decodeTransitOps(c.AllowedOperations), State: c.State, LatestVersion: uint32(c.LatestVersion), MinEncryptVersion: uint32(c.MinEncryptVersion), MinDecryptVersion: uint32(c.MinDecryptVersion), MinAvailableVersion: uint32(c.MinAvailableVersion), CompromisedThroughVersion: uint32(c.CompromisedThroughVersion), RotationPeriodSeconds: c.RotationPeriodSeconds, DeletionAfter: pgStoredStamp(c.DeletionAfter), CreatedBy: c.CreatedBy, CreatedAt: pgStoredStamp(c.CreatedAt), UpdatedAt: pgStoredStamp(c.UpdatedAt)}
	if err := normalizeStoredTimes(&item.DeletionAfter, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return TransitKeyRecord{}, err
	}
	return item, nil
}
func pgTransitVersion(c pggen.TransitListVersionsRow) (TransitVersionRecord, error) {
	item := TransitVersionRecord{ID: c.ID, Version: uint32(c.Version), PublicKey: c.PublicKey, HasMaterial: c.HasMaterial == 1, ExternalHeld: c.ExternalHeld == 1, CreatedAt: pgStoredStamp(c.CreatedAt)}
	if err := normalizeStoredTimes(&item.CreatedAt); err != nil {
		return TransitVersionRecord{}, err
	}
	return item, nil
}
func pgTransitCaller(c pggen.TransitListCallersRow) (TransitCaller, error) {
	item := TransitCaller{PrincipalID: c.PrincipalID, Operations: decodeTransitOps(c.Operations)}
	return item, nil
}
func pgTransitMaterial(c pggen.TransitVersionMaterialRow) (TransitVersionMaterial, error) {
	item := TransitVersionMaterial{ID: c.ID, Version: uint32(c.Version), Sealed: c.MaterialCiphertext, ExternalRef: c.ExternalRef.String, PublicKey: c.PublicKey}
	return item, nil
}
func pgTransitReencrypt(c pggen.TransitListReencryptRow) (ReencryptFieldRow, error) {
	item := ReencryptFieldRow{ID: c.ID, EnvironmentID: c.EnvironmentID, KeyID: c.KeyID, Ciphertext: c.MaterialCiphertext}
	return item, nil
}
func pgTransitDeletion(c pggen.TransitSelectDeletionDueRow) (TransitDueKey, error) {
	item := TransitKeyRecord{ID: c.ID, EnvironmentID: c.EnvironmentID, Name: c.Name, Algorithm: c.Algorithm, Custody: c.Custody, AllowedOperations: decodeTransitOps(c.AllowedOperations), State: c.State, LatestVersion: uint32(c.LatestVersion), MinEncryptVersion: uint32(c.MinEncryptVersion), MinDecryptVersion: uint32(c.MinDecryptVersion), MinAvailableVersion: uint32(c.MinAvailableVersion), CompromisedThroughVersion: uint32(c.CompromisedThroughVersion), RotationPeriodSeconds: c.RotationPeriodSeconds, DeletionAfter: pgStoredStamp(c.DeletionAfter), CreatedBy: c.CreatedBy, CreatedAt: pgStoredStamp(c.CreatedAt), UpdatedAt: pgStoredStamp(c.UpdatedAt)}
	if err := normalizeStoredTimes(&item.DeletionAfter, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return TransitDueKey{}, err
	}
	out := TransitDueKey{OrgID: c.OrgID, ProjectID: c.ProjectID, EnvironmentID: c.EnvironmentID, TransitKeyRecord: item}
	return out, nil
}
func pgTransitRotation(c pggen.TransitSelectRotationDueRow) (TransitDueKey, error) {
	item := TransitKeyRecord{ID: c.ID, EnvironmentID: c.EnvironmentID, Name: c.Name, Algorithm: c.Algorithm, Custody: c.Custody, AllowedOperations: decodeTransitOps(c.AllowedOperations), State: c.State, LatestVersion: uint32(c.LatestVersion), MinEncryptVersion: uint32(c.MinEncryptVersion), MinDecryptVersion: uint32(c.MinDecryptVersion), MinAvailableVersion: uint32(c.MinAvailableVersion), CompromisedThroughVersion: uint32(c.CompromisedThroughVersion), RotationPeriodSeconds: c.RotationPeriodSeconds, DeletionAfter: pgStoredStamp(c.DeletionAfter), CreatedBy: c.CreatedBy, CreatedAt: pgStoredStamp(c.CreatedAt), UpdatedAt: pgStoredStamp(c.UpdatedAt)}
	if err := normalizeStoredTimes(&item.DeletionAfter, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return TransitDueKey{}, err
	}
	out := TransitDueKey{OrgID: c.OrgID, ProjectID: c.ProjectID, EnvironmentID: c.EnvironmentID, TransitKeyRecord: item}
	latest, err := readStoredTime(pgStoredStamp(c.LatestCreatedAt))
	if err != nil {
		return TransitDueKey{}, err
	}
	if latest != nil {
		out.LatestCreatedAt = *latest
	}
	return out, nil
}
func pgTransitExternal(c pggen.TransitExternalVersionsRow) (TransitVersionMaterial, error) {
	item := TransitVersionMaterial{ID: c.ID, Version: uint32(c.Version), ExternalRef: c.ExternalRef.String}
	return item, nil
}
func pgTransitGauge(c pggen.TransitGaugeRotationCandidatesRow) (transitRotationCandidate, error) {
	created, err := readStoredTime(pgStoredStamp(c.CreatedAt))
	return transitRotationCandidate{period: c.RotationPeriodSeconds, createdAt: created}, err
}
