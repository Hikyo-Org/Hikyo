package store

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

func adapterVersionWitness(value int64, valid bool) *int64 {
	if !valid || value <= 0 {
		return nil
	}
	return &value
}

func adapterOptionalVersion(value *int64) sql.NullInt64 {
	if value == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *value, Valid: true}
}

func adapterRequireVersion(provider string) int64 {
	if provider == "vault-kv" {
		return 1
	}
	return 0
}

func adapterCounter(value int64, valid bool) *int64 {
	if !valid {
		return nil
	}
	return &value
}

func decodeAdapterTarget(target AdapterTarget, paused, lastAttempt, nextAttempt string, attempts int64, failureRaw, warningRaw, selectedRaw []byte) (AdapterTarget, error) {
	var err error
	target.PausedAt, err = readStoredTime(paused)
	if err != nil {
		return AdapterTarget{}, err
	}
	target.LastAttemptedAt, err = readStoredTime(lastAttempt)
	if err != nil {
		return AdapterTarget{}, err
	}
	// A fresh queued job is enqueued, while an attempted queued job is retrying.
	if target.ActiveJobState == "queued" && attempts > 0 {
		retryAt, err := readStoredTime(nextAttempt)
		if err != nil {
			return AdapterTarget{}, err
		}
		target.RetryAt = retryAt
	}
	if len(failureRaw) != 0 {
		if err := json.Unmarshal(failureRaw, &target.FailureNames); err != nil {
			return AdapterTarget{}, fmt.Errorf("store: adapter target failure names: %w", err)
		}
	}
	if len(warningRaw) != 0 {
		if err := json.Unmarshal(warningRaw, &target.Warnings); err != nil {
			return AdapterTarget{}, fmt.Errorf("store: adapter target warning names: %w", err)
		}
	}
	if len(selectedRaw) != 0 {
		if err := json.Unmarshal(selectedRaw, &target.SelectedRepositoryIDs); err != nil {
			return AdapterTarget{}, fmt.Errorf("store: adapter target selected repository ids: %w", err)
		}
	}
	return target, nil
}

func adapterRecordSQLite(c sqlitegen.AdapterGetRow) (AdapterRecord, error) {
	out := AdapterRecord{ID: c.ID, Provider: c.Provider, Origin: c.Origin, CredentialPresent: c.CredentialPresent == 1, CredentialSetAt: c.CredentialSetAt.String, CredentialExpiresAt: c.CredentialExpiresAt.String, AuthorityPrincipalID: c.AuthorityPrincipalID, State: c.State, CreatedAt: c.CreatedAt, SPKIPin: c.SpkiPin, CABundlePEM: c.CaBundlePem, AllowPersonalToken: c.AllowPersonalToken == 1}
	if err := normalizeStoredTimes(&out.CredentialSetAt, &out.CredentialExpiresAt, &out.CreatedAt); err != nil {
		return AdapterRecord{}, err
	}
	return out, nil
}

func adapterTargetSQLite(c sqlitegen.AdapterGetTargetRow) (AdapterTarget, error) {
	out := AdapterTarget{ID: c.ID, AdapterID: c.AdapterID, Provider: c.Provider, EnvironmentID: c.EnvironmentID, Origin: c.Origin, DestinationKind: c.DestinationKind, DestinationOwner: c.DestinationOwner, DestinationName: c.DestinationName, DestinationEnvironment: c.DestinationEnvironment, DestinationID: c.DestinationID, RepositoryID: c.RepositoryID, Visibility: c.Visibility, NamePrefix: c.NamePrefix, DestinationScope: c.DestinationScope, VariableProtected: c.VariableProtected == 1, VariableHidden: c.VariableHidden == 1, VariableExpand: c.VariableExpand == 1, Generation: c.Generation, State: c.State, SyncStatus: c.SyncStatus, ConvergedRevision: adapterCounter(c.ConvergedRevision.Int64, c.ConvergedRevision.Valid), AuthorityPrincipalID: c.AuthorityPrincipalID, LastAttemptedRevision: adapterCounter(c.LastAttemptedRevision.Int64, c.LastAttemptedRevision.Valid), LastErrorClass: adapter.ErrorClass(c.LastErrorClass.String), DriftAttention: c.DriftAttention == 1, ActiveJobState: c.ActiveJobState}
	return decodeAdapterTarget(out, c.PausedAt.String, c.LastAttemptedAt.String, c.NextAttemptAt.String, c.AttemptCount, []byte(c.FailureNames), []byte(c.Warnings), []byte(c.SelectedRepositoryIds))
}

func adapterRecordPG(c pggen.AdapterGetRow) (AdapterRecord, error) {
	out := AdapterRecord{ID: c.ID, Provider: c.Provider, Origin: c.Origin, CredentialPresent: c.CredentialPresent == 1, CredentialSetAt: pgStoredStamp(c.CredentialSetAt), CredentialExpiresAt: pgStoredStamp(c.CredentialExpiresAt), AuthorityPrincipalID: c.AuthorityPrincipalID, State: c.State, CreatedAt: pgStoredStamp(c.CreatedAt), SPKIPin: c.SpkiPin, CABundlePEM: c.CaBundlePem, AllowPersonalToken: c.AllowPersonalToken == 1}
	if err := normalizeStoredTimes(&out.CredentialSetAt, &out.CredentialExpiresAt, &out.CreatedAt); err != nil {
		return AdapterRecord{}, err
	}
	return out, nil
}

func adapterTargetPG(c pggen.AdapterGetTargetRow) (AdapterTarget, error) {
	out := AdapterTarget{ID: c.ID, AdapterID: c.AdapterID, Provider: c.Provider, EnvironmentID: c.EnvironmentID, Origin: c.Origin, DestinationKind: c.DestinationKind, DestinationOwner: c.DestinationOwner, DestinationName: c.DestinationName, DestinationEnvironment: c.DestinationEnvironment, DestinationID: c.DestinationID, RepositoryID: c.RepositoryID, Visibility: c.Visibility, NamePrefix: c.NamePrefix, DestinationScope: c.DestinationScope, VariableProtected: c.VariableProtected == 1, VariableHidden: c.VariableHidden == 1, VariableExpand: c.VariableExpand == 1, Generation: c.Generation, State: c.State, SyncStatus: c.SyncStatus, ConvergedRevision: adapterCounter(c.ConvergedRevision.Int64, c.ConvergedRevision.Valid), AuthorityPrincipalID: c.AuthorityPrincipalID, LastAttemptedRevision: adapterCounter(c.LastAttemptedRevision.Int64, c.LastAttemptedRevision.Valid), LastErrorClass: adapter.ErrorClass(c.LastErrorClass.String), DriftAttention: c.DriftAttention == 1, ActiveJobState: c.ActiveJobState}
	return decodeAdapterTarget(out, pgStoredStamp(c.PausedAt), pgStoredStamp(c.LastAttemptedAt), pgStoredStamp(c.NextAttemptAt), c.AttemptCount, c.FailureNames, c.Warnings, c.SelectedRepositoryIds)
}
