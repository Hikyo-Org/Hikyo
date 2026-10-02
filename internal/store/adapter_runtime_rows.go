package store

import (
	"encoding/json"
	"fmt"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

func workerExecutionMetadata(c adapterWorkerLoadExecutionQueryRow, targetID, environmentID string) (AdapterExecution, error) {
	if len(c.CredentialCiphertext) == 0 {
		return AdapterExecution{}, fmt.Errorf("%w: adapter credential is absent", adapter.ErrProviderAuth)
	}
	out := AdapterExecution{Provider: c.Provider, Origin: c.Origin, CredentialOwnerID: c.ID, CredentialCiphertext: append([]byte(nil), c.CredentialCiphertext...), Transport: AdapterTransport{SPKIPin: c.SpkiPin, CABundlePEM: c.CaBundlePem, AllowPersonalToken: c.AllowPersonalToken == 1}}
	out.Target.ID = targetID
	out.Target.Environment = environmentID
	out.Target.Destination.Kind = adapter.DestinationKind(c.DestinationKind)
	out.Target.Destination.Owner = c.DestinationOwner
	out.Target.Destination.Name = c.DestinationName
	out.Target.Destination.Environment = c.DestinationEnvironment
	out.Target.Destination.NumericID = c.DestinationID
	out.Target.Destination.RepositoryID = c.RepositoryID
	out.Target.Destination.Visibility = c.Visibility
	out.Target.Destination.Scope = c.DestinationScope
	out.Target.NamePrefix = c.NamePrefix
	out.Target.Generation = c.Generation
	out.Revision = c.Revision
	out.Target.Options = adapter.VariableOptions{Protected: c.VariableProtected == 1, Hidden: c.VariableHidden == 1, Expand: c.VariableExpand == 1}
	if err := json.Unmarshal(c.SelectedRepositoryIds, &out.Target.Destination.SelectedRepositoryIDs); err != nil {
		return AdapterExecution{}, fmt.Errorf("store: adapter selected repository ids: %w", err)
	}
	return out, nil
}

func workerActivationMetadata(c adapterWorkerLoadActivationQueryRow, targetID string) (AdapterActivation, error) {
	execution, err := workerExecutionMetadata(adapterWorkerLoadExecutionQueryRow{Provider: c.Provider, Origin: c.PendingOrigin, ID: c.ID, CredentialCiphertext: c.PendingCredentialCiphertext, DestinationKind: c.DestinationKind, DestinationOwner: c.DestinationOwner, DestinationName: c.DestinationName, DestinationEnvironment: c.DestinationEnvironment, DestinationID: c.DestinationID, RepositoryID: c.RepositoryID, Visibility: c.Visibility, SelectedRepositoryIds: c.SelectedRepositoryIds, NamePrefix: c.NamePrefix, Generation: c.Generation, SpkiPin: c.SpkiPin, CaBundlePem: c.CaBundlePem, AllowPersonalToken: c.AllowPersonalToken, DestinationScope: c.DestinationScope, VariableProtected: c.VariableProtected, VariableHidden: c.VariableHidden, VariableExpand: c.VariableExpand}, targetID, c.EnvironmentID)
	if err != nil {
		return AdapterActivation{}, err
	}
	return AdapterActivation{Provider: execution.Provider, Origin: execution.Origin, CredentialOwnerID: execution.CredentialOwnerID, CredentialCiphertext: execution.CredentialCiphertext, Transport: execution.Transport, Target: execution.Target}, nil
}
