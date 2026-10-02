package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

func (r adapterQueries) Get(ctx context.Context, p authz.Proof, adapterID string) (AdapterRecord, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersGet, r.tok)
	if err != nil {
		return AdapterRecord{}, err
	}
	return r.db.adapterStoreQueries().adapterGet(ctx, chain, adapterID)
}

func (r adapterQueries) Configuration(ctx context.Context, p authz.Proof, adapterID string) (AdapterRecord, []byte, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersConfiguration, r.tok)
	if err != nil {
		return AdapterRecord{}, nil, err
	}
	return r.db.adapterStoreQueries().adapterConfiguration(ctx, chain, adapterID)
}

// ConfigurationForUpdate binds provider verification to the configuration
// that will receive the mutation. The parent lock is held until commit, so
// credential replacement and origin moves cannot race the following write.
// Read-only callers retain Configuration without acquiring a writer lock.
func (r adapterQueries) ConfigurationForUpdate(ctx context.Context, p authz.Proof, adapterID string) (AdapterRecord, []byte, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersConfigurationForUpdate, r.tok)
	if err != nil {
		return AdapterRecord{}, nil, err
	}
	queries := r.db.adapterStoreQueries()
	if _, err := queries.adapterActiveForUpdate(ctx, chain, adapterID); err != nil {
		return AdapterRecord{}, nil, err
	}
	return queries.adapterConfiguration(ctx, chain, adapterID)
}

func (r adapterQueries) List(ctx context.Context, p authz.Proof) ([]AdapterRecord, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersList, r.tok)
	if err != nil {
		return nil, err
	}
	return r.db.adapterStoreQueries().adapterList(ctx, chain)
}

func (r adapterQueries) ListTargets(ctx context.Context, p authz.Proof, adapterID string) ([]AdapterTarget, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersListTargets, r.tok)
	if err != nil {
		return nil, err
	}
	targets, err := r.db.adapterStoreQueries().adapterListTargets(ctx, chain, adapterID)
	if err != nil {
		return nil, err
	}
	for i := range targets {
		targets[i].Findings, err = r.targetFindings(ctx, chain, targets[i])
		if err != nil {
			return nil, err
		}
	}
	return targets, nil
}

func (r adapterQueries) TargetKeyIDs(ctx context.Context, p authz.Proof, targetID string) ([]string, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersTargetKeyIDs, r.tok)
	if err != nil {
		return nil, err
	}
	return r.db.adapterStoreQueries().adapterTargetKeyIDs(ctx, chain, targetID)
}

// TargetKeys is the target's explicit subset by name and classification. It
// reads the catalogue, not the published snapshot, so a member key that has
// no published value yet is still echoed as a member.
func (r adapterQueries) TargetKeys(ctx context.Context, p authz.Proof, targetID string) ([]AdapterTargetKey, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersTargetKeys, r.tok)
	if err != nil {
		return nil, err
	}
	return r.db.adapterStoreQueries().adapterTargetKeys(ctx, chain, targetID)
}

func validateTargetMutation(m AdapterTargetMutation) error {
	if m.ID == "" || m.AdapterID == "" || m.EnvironmentID == "" || m.DestinationOwner == "" || m.DestinationID <= 0 || len(m.KeyIDs) == 0 {
		return fmt.Errorf("%w: adapter target requires ids, environment, destination, and an explicit non-empty key subset", domain.ErrInvalid)
	}
	switch m.DestinationKind {
	case string(adapter.Repository):
		if m.DestinationName == "" || m.DestinationEnvironment != "" || m.RepositoryID != 0 || m.Visibility != "" || len(m.SelectedRepositoryIDs) != 0 {
			return fmt.Errorf("%w: repository target requires repository name", domain.ErrInvalid)
		}
	case string(adapter.Organization):
		if m.DestinationName != "" || m.DestinationEnvironment != "" || m.RepositoryID != 0 {
			return fmt.Errorf("%w: organization target does not take repository routing fields", domain.ErrInvalid)
		}
		switch m.Visibility {
		case "", "all", "private":
			if len(m.SelectedRepositoryIDs) != 0 {
				return fmt.Errorf("%w: selected repository ids require selected visibility", domain.ErrInvalid)
			}
		case "selected":
			if len(m.SelectedRepositoryIDs) == 0 {
				return fmt.Errorf("%w: selected visibility requires repository ids", domain.ErrInvalid)
			}
		default:
			return fmt.Errorf("%w: organization visibility must be all, private, or selected", domain.ErrInvalid)
		}
	case string(adapter.Environment):
		if m.DestinationName == "" || m.DestinationEnvironment == "" || m.RepositoryID <= 0 || m.Visibility != "" || len(m.SelectedRepositoryIDs) != 0 {
			return fmt.Errorf("%w: environment target requires repository and environment identities", domain.ErrInvalid)
		}
	case string(adapter.JSONObject), string(adapter.PerKey):
		if err := adapter.ValidateAWSSecretsManagerDestination(targetDestination(m)); err != nil {
			return fmt.Errorf("%w: %v", domain.ErrInvalid, err)
		}
	case string(adapter.WorkersScript), string(adapter.PagesProject):
		if err := validateCloudflareTarget(m); err != nil {
			return err
		}
		if m.RepositoryID != 0 {
			return fmt.Errorf("%w: Cloudflare targets do not take repository routing fields", domain.ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: unsupported adapter destination kind", domain.ErrInvalid)
	}
	seenRepositories := map[int64]bool{}
	for _, id := range m.SelectedRepositoryIDs {
		if id <= 0 || seenRepositories[id] {
			return fmt.Errorf("%w: selected repository ids must be positive and unique", domain.ErrInvalid)
		}
		seenRepositories[id] = true
	}
	seen := map[string]bool{}
	for _, id := range m.KeyIDs {
		if id == "" || seen[id] {
			return fmt.Errorf("%w: adapter target key ids must be non-empty and unique", domain.ErrInvalid)
		}
		seen[id] = true
	}
	return nil
}

// targetDestination is the routing identity a stored target mutation names.
func targetDestination(m AdapterTargetMutation) adapter.Destination {
	return adapter.Destination{
		Kind: adapter.DestinationKind(m.DestinationKind), Owner: m.DestinationOwner, Name: m.DestinationName,
		Environment: m.DestinationEnvironment, NumericID: m.DestinationID, RepositoryID: m.RepositoryID,
		Visibility: m.Visibility, SelectedRepositoryIDs: m.SelectedRepositoryIDs,
	}
}

// isAWSDestinationKind reports the destination kinds whose provider names
// derive from the destination as well as the prefix.
func isAWSDestinationKind(kind string) bool {
	return kind == string(adapter.JSONObject) || kind == string(adapter.PerKey)
}

// validateCloudflareTarget checks the routing fields shared by committed and
// pending Cloudflare targets. Owner is the account id; Pages targets name one
// environment so preview and production are separate destinations.
func validateCloudflareTarget(m AdapterTargetMutation) error {
	if m.RepositoryID != 0 || m.DestinationName == "" || m.Visibility != "" || len(m.SelectedRepositoryIDs) != 0 {
		return fmt.Errorf("%w: Cloudflare target requires account and script or project name only", domain.ErrInvalid)
	}
	switch m.DestinationKind {
	case string(adapter.WorkersScript):
		if m.DestinationEnvironment != "" {
			return fmt.Errorf("%w: Workers script target does not take an environment", domain.ErrInvalid)
		}
	case string(adapter.PagesProject):
		if m.DestinationEnvironment != "preview" && m.DestinationEnvironment != "production" {
			return fmt.Errorf("%w: Pages project target environment must be preview or production", domain.ErrInvalid)
		}
	}
	return nil
}

func targetManifest(ctx context.Context, db adapterDB, chain domain.Scope, m AdapterTargetMutation) ([]adapter.ManifestEntry, error) {
	_, manifest, err := targetProviderManifest(ctx, db, chain, m)
	return manifest, err
}

func adapterProvider(ctx context.Context, db adapterDB, chain domain.Scope, adapterID string) (string, error) {
	return db.adapterStoreQueries().adapterProvider(ctx, chain, adapterID)
}

// targetProviderManifest loads the owning adapter's provider and the target's
// key subset, and refuses a name, destination kind, or provider pairing the
// provider cannot represent.
func targetProviderManifest(ctx context.Context, db adapterDB, chain domain.Scope, m AdapterTargetMutation) (string, []adapter.ManifestEntry, error) {
	provider, err := adapterProvider(ctx, db, chain, m.AdapterID)
	if err != nil {
		return "", nil, err
	}
	cloudflareKind := m.DestinationKind == string(adapter.WorkersScript) || m.DestinationKind == string(adapter.PagesProject)
	if cloudflareKind != (provider == string(adapter.CloudflareProvider)) {
		return "", nil, fmt.Errorf("%w: destination kind %q is not supported by provider %q", domain.ErrInvalid, m.DestinationKind, provider)
	}
	manifest, err := db.adapterStoreQueries().manifestKeys(ctx, chain, m.KeyIDs)
	if err != nil {
		return "", nil, err
	}
	if len(manifest) != len(m.KeyIDs) {
		return "", nil, ErrNotFound
	}
	if err := adapter.ValidateTargetManifest(provider, targetDestination(m), m.NamePrefix, manifest, false); err != nil {
		return "", nil, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	return provider, manifest, nil
}

func refuseDestinationNameCollision(ctx context.Context, db adapterDB, chain domain.Scope, m AdapterTargetMutation, manifest []adapter.ManifestEntry, excludeTargetID string) error {
	if isAWSDestinationKind(m.DestinationKind) {
		return refuseAWSNameCollision(ctx, db, chain, m, manifest, excludeTargetID)
	}
	desired := map[string]struct{}{m.NamePrefix + adapter.SentinelName: {}}
	for _, entry := range manifest {
		desired[m.NamePrefix+entry.CanonicalName] = struct{}{}
	}

	rows, err := db.adapterConfigQueries().configuredNames(ctx, chain, m, excludeTargetID)
	if err != nil {
		return err
	}
	for _, configuredNamesRow := range rows {
		if _, found := desired[configuredNamesRow.Prefix+adapter.SentinelName]; found {
			return fmt.Errorf("%w: effective name %q is already configured by target %q on this destination", domain.ErrConflict, configuredNamesRow.Prefix+adapter.SentinelName, configuredNamesRow.TargetID)
		}
		if configuredNamesRow.CanonicalName != "" {
			if _, found := desired[configuredNamesRow.Prefix+configuredNamesRow.CanonicalName]; found {
				return fmt.Errorf("%w: effective name %q is already configured by target %q on this destination", domain.ErrConflict, configuredNamesRow.Prefix+configuredNamesRow.CanonicalName, configuredNamesRow.TargetID)
			}
		}
	}

	pendingRows, err := db.adapterConfigQueries().pendingNames(ctx, chain, m, excludeTargetID)
	if err != nil {
		return err
	}
	for _, pendingNamesRow := range pendingRows {
		if _, found := desired[pendingNamesRow.EffectiveName]; found {
			return fmt.Errorf("%w: effective name %q is reserved by pending target %q on this destination", domain.ErrConflict, pendingNamesRow.EffectiveName, pendingNamesRow.TargetID)
		}
	}
	return nil
}

// refuseAWSNameCollision is the AWS form of the configured-name check. One AWS
// account and region is a single secret namespace shared by both destination
// kinds, and a json-object target owns its secret name rather than prefixed
// key names, so claims are computed per target and compared across kinds.
func refuseAWSNameCollision(ctx context.Context, db adapterDB, chain domain.Scope, m AdapterTargetMutation, manifest []adapter.ManifestEntry, excludeTargetID string) error {
	desired := map[string]bool{}
	for _, claim := range adapter.ClaimedNames(string(adapter.AWSSecretsManagerProvider), targetDestination(m), m.NamePrefix, manifest) {
		desired[strings.ToUpper(claim.EffectiveName)] = true
	}

	rows, err := db.adapterConfigQueries().awsConfiguredNames(ctx, chain, m, excludeTargetID)
	if err != nil {
		return err
	}
	for _, awsConfiguredNamesRow := range rows {
		claimed := awsConfiguredNamesRow.Name
		if awsConfiguredNamesRow.Kind == string(adapter.PerKey) {
			if awsConfiguredNamesRow.KeyName == "" {
				continue
			}
			claimed = awsConfiguredNamesRow.Name + awsConfiguredNamesRow.Prefix + awsConfiguredNamesRow.KeyName
		}
		if desired[strings.ToUpper(claimed)] {
			return fmt.Errorf("%w: effective name %q is already configured by target %q on this destination", domain.ErrConflict, claimed, awsConfiguredNamesRow.TargetID)
		}
	}

	pendingRows, err := db.adapterConfigQueries().awsPendingNames(ctx, chain, m, excludeTargetID)
	if err != nil {
		return err
	}
	for _, awsPendingNamesRow := range pendingRows {
		if desired[strings.ToUpper(awsPendingNamesRow.EffectiveName)] {
			return fmt.Errorf("%w: effective name %q is reserved by pending target %q on this destination", domain.ErrConflict, awsPendingNamesRow.EffectiveName, awsPendingNamesRow.TargetID)
		}
	}
	return nil
}

func insertTargetConfig(ctx context.Context, db adapterDB, chain domain.Scope, m AdapterTargetMutation, at time.Time) error {
	manifest, err := targetManifest(ctx, db, chain, m)
	if err != nil {
		return err
	}
	if err := refuseDestinationNameCollision(ctx, db, chain, m, manifest, ""); err != nil {
		return err
	}
	selected, err := json.Marshal(m.SelectedRepositoryIDs)
	if err != nil {
		return err
	}
	if err := db.adapterStoreQueries().insertTarget(ctx, chain, m, at, selected); err != nil {
		return constraint(err)
	}
	for _, keyID := range m.KeyIDs {
		if err := db.adapterStoreQueries().insertTargetKey(ctx, chain, m, keyID); err != nil {
			return constraint(err)
		}
	}
	return nil
}

func (r adapterQueries) Create(ctx context.Context, p authz.Proof, m AdapterCreate) (AdapterRecord, AdapterTarget, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersCreate, r.tok)
	if err != nil {
		return AdapterRecord{}, AdapterTarget{}, err
	}
	if _, err := adapter.ParseProvider(m.Provider); err != nil {
		return AdapterRecord{}, AdapterTarget{}, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	if err := requireCanonicalAdapterOrigin(m.Provider, m.Origin); err != nil {
		return AdapterRecord{}, AdapterTarget{}, err
	}
	if m.ID == "" || m.Origin == "" || len(m.CredentialCiphertext) == 0 || m.AuthorityPrincipalID == "" || m.Target.AdapterID != m.ID {
		return AdapterRecord{}, AdapterTarget{}, fmt.Errorf("%w: incomplete atomic adapter bootstrap", domain.ErrInvalid)
	}
	if err := validateTargetMutation(m.Target); err != nil {
		return AdapterRecord{}, AdapterTarget{}, err
	}
	at := CanonTime(m.At)
	if err := r.db.adapterStoreQueries().createAdapter(ctx, chain, m, at); err != nil {
		return AdapterRecord{}, AdapterTarget{}, constraint(err)
	}
	if err := insertTargetConfig(ctx, r.db, chain, m.Target, at); err != nil {
		return AdapterRecord{}, AdapterTarget{}, err
	}
	atString := at.Format(timeFormat)
	record := AdapterRecord{ID: m.ID, Provider: m.Provider, Origin: m.Origin, CredentialPresent: true, CredentialSetAt: atString, AuthorityPrincipalID: m.AuthorityPrincipalID, State: "active", CreatedAt: atString, SPKIPin: m.SPKIPin, CABundlePEM: m.CABundlePEM, AllowPersonalToken: m.AllowPersonalToken}
	if !m.CredentialExpiresAt.IsZero() {
		record.CredentialExpiresAt = CanonTime(m.CredentialExpiresAt).Format(timeFormat)
	}
	return record, mutationTarget(record, m.Target, 1), nil
}

func mutationTarget(record AdapterRecord, m AdapterTargetMutation, generation int64) AdapterTarget {
	return AdapterTarget{ID: m.ID, AdapterID: m.AdapterID, Provider: record.Provider, EnvironmentID: m.EnvironmentID, Origin: record.Origin, DestinationKind: m.DestinationKind, DestinationOwner: m.DestinationOwner, DestinationName: m.DestinationName, DestinationEnvironment: m.DestinationEnvironment, DestinationID: m.DestinationID, RepositoryID: m.RepositoryID, Visibility: m.Visibility, SelectedRepositoryIDs: append([]int64(nil), m.SelectedRepositoryIDs...), NamePrefix: m.NamePrefix, Generation: generation, State: "active", SyncStatus: "never", AuthorityPrincipalID: record.AuthorityPrincipalID, DestinationScope: m.DestinationScope, VariableProtected: m.VariableProtected, VariableHidden: m.VariableHidden, VariableExpand: m.VariableExpand}
}

func (r adapterQueries) AddTarget(ctx context.Context, p authz.Proof, m AdapterTargetUpdate) (AdapterTargetAddResult, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersAddTarget, r.tok)
	if err != nil {
		return AdapterTargetAddResult{}, err
	}
	if err := validateTargetMutation(m.Target); err != nil {
		return AdapterTargetAddResult{}, err
	}
	record, err := r.db.adapterStoreQueries().adapterActiveForUpdate(ctx, chain, m.Target.AdapterID)
	if err != nil {
		return AdapterTargetAddResult{}, err
	}
	if !record.CredentialPresent {
		return AdapterTargetAddResult{}, adapter.ErrProviderAuth
	}
	previousAuthority := record.AuthorityPrincipalID
	at := CanonTime(m.At)
	if err := insertTargetConfig(ctx, r.db, chain, m.Target, at); err != nil {
		return AdapterTargetAddResult{}, err
	}
	if rows, err := r.db.adapterStoreQueries().updateAuthorityExpiry(ctx, chain, m.Target.AdapterID, m.AuthorityPrincipalID, m.CredentialExpiresAt); err != nil || rows != 1 {
		return AdapterTargetAddResult{}, errors.Join(err, ErrNotFound)
	}
	record.AuthorityPrincipalID = m.AuthorityPrincipalID
	if !m.CredentialExpiresAt.IsZero() {
		record.CredentialExpiresAt = CanonTime(m.CredentialExpiresAt).Format(timeFormat)
	}
	return AdapterTargetAddResult{Target: mutationTarget(record, m.Target, 1), PreviousAuthorityPrincipalID: previousAuthority, AuthorityPrincipalID: m.AuthorityPrincipalID}, nil
}

func (r adapterQueries) RecordCredentialExpiry(ctx context.Context, p authz.Proof, adapterID string, expiresAt time.Time) error {
	chain, err := authz.Verify(p, authz.StoreAdaptersRecordCredentialExpiry, r.tok)
	if err != nil {
		return err
	}
	if adapterID == "" || expiresAt.IsZero() {
		return fmt.Errorf("%w: credential expiry requires adapter and timestamp", domain.ErrInvalid)
	}
	rows, err := r.db.adapterStoreQueries().recordCredentialExpiry(ctx, chain, adapterID, expiresAt)
	if err != nil || rows != 1 {
		return errors.Join(err, ErrNotFound)
	}
	return nil
}

func validateAdapterConfigureFence(fence AdapterConfigureFence) error {
	if fence.TargetID == "" || fence.EnvironmentID == "" || fence.DestinationKind != string(adapter.Environment) || fence.DestinationOwner == "" || fence.DestinationName == "" || fence.DestinationEnvironment == "" || fence.Generation != 1 || fence.EffectID == "" || fence.LeaseExpiresAt.IsZero() || fence.At.IsZero() || !fence.LeaseExpiresAt.After(fence.At) {
		return fmt.Errorf("%w: incomplete adapter configure fence", ErrConflict)
	}
	return nil
}

func (r adapterQueries) BeginConfigureEffect(ctx context.Context, p authz.Proof, fence AdapterConfigureFence) error {
	chain, err := authz.Verify(p, authz.StoreAdaptersBeginConfigureEffect, r.tok)
	if err != nil {
		return err
	}
	if err := validateAdapterConfigureFence(fence); err != nil {
		return err
	}
	err = r.db.adapterStoreQueries().beginConfigureEffect(ctx, chain, fence)
	return constraint(err)
}

func validateAdapterConfigureOutcome(targetID, effectID, outcome string, at time.Time) error {
	if targetID == "" || effectID == "" || (outcome != "succeeded" && outcome != "failed") || at.IsZero() {
		return fmt.Errorf("%w: incomplete adapter configure outcome", ErrConflict)
	}
	return nil
}

func (r adapterQueries) FinishConfigureEffect(ctx context.Context, p authz.Proof, targetID, effectID, outcome string, at time.Time) error {
	chain, err := authz.Verify(p, authz.StoreAdaptersFinishConfigureEffect, r.tok)
	if err != nil {
		return err
	}
	if err := validateAdapterConfigureOutcome(targetID, effectID, outcome, at); err != nil {
		return err
	}
	rows, err := r.db.adapterStoreQueries().finishConfigureEffect(ctx, chain, targetID, effectID, outcome, at)
	if err != nil {
		return constraint(err)
	}
	if rows != 1 {
		return ErrConflict
	}
	return nil
}

func (r adapterQueries) UpdateTarget(ctx context.Context, p authz.Proof, m AdapterTargetUpdate) (AdapterTargetUpdateResult, error) {
	chain, err := authz.Verify(p, authz.StoreAdaptersUpdateTarget, r.tok)
	if err != nil {
		return AdapterTargetUpdateResult{}, err
	}
	return updateTargetConfig(ctx, r.db, chain, m)
}

func updateTargetConfig(ctx context.Context, db adapterDB, chain domain.Scope, m AdapterTargetUpdate) (AdapterTargetUpdateResult, error) {
	if err := validateTargetMutation(m.Target); err != nil {
		return AdapterTargetUpdateResult{}, err
	}
	if m.ExpectedGeneration <= 0 || m.AuthorityPrincipalID == "" {
		return AdapterTargetUpdateResult{}, fmt.Errorf("%w: target update requires generation and authority", domain.ErrInvalid)
	}
	manifest, err := targetManifest(ctx, db, chain, m.Target)
	if err != nil {
		return AdapterTargetUpdateResult{}, err
	}
	if err := refuseDestinationNameCollision(ctx, db, chain, m.Target, manifest, m.Target.ID); err != nil {
		return AdapterTargetUpdateResult{}, err
	}
	current, err := db.adapterStoreQueries().adapterActiveTargetForUpdate(ctx, chain, m.Target.ID)
	if err != nil {
		return AdapterTargetUpdateResult{}, err
	}
	if current.Generation != m.ExpectedGeneration {
		return AdapterTargetUpdateResult{}, adapter.ErrSuperseded
	}
	previousAuthority := current.AuthorityPrincipalID
	if current.AdapterID != m.Target.AdapterID || current.EnvironmentID != m.Target.EnvironmentID || current.DestinationKind != m.Target.DestinationKind || current.DestinationOwner != m.Target.DestinationOwner || current.DestinationName != m.Target.DestinationName || current.DestinationEnvironment != m.Target.DestinationEnvironment || current.DestinationID != m.Target.DestinationID || current.RepositoryID != m.Target.RepositoryID {
		return AdapterTargetUpdateResult{}, fmt.Errorf("%w: moving an adapter target requires the scrub transition", domain.ErrConflict)
	}
	if current.DestinationScope != m.Target.DestinationScope {
		// Owned variables are keyed by scope; changing it in place would
		// orphan them. Remove the target (retain or prune) and add a new one.
		return AdapterTargetUpdateResult{}, fmt.Errorf("%w: a GitLab environment scope is immutable; remove the target and add a new one", domain.ErrConflict)
	}
	activeJob, err := db.adapterStoreQueries().targetActiveJob(ctx, chain, m.Target.ID, m.Target.EnvironmentID)
	if err != nil {
		return AdapterTargetUpdateResult{}, err
	}
	if err := db.adapterStoreQueries().deleteTargetKeys(ctx, chain, m.Target.ID, m.Target.EnvironmentID); err != nil {
		return AdapterTargetUpdateResult{}, err
	}
	for _, keyID := range m.Target.KeyIDs {
		if err := db.adapterStoreQueries().insertTargetKey(ctx, chain, m.Target, keyID); err != nil {
			return AdapterTargetUpdateResult{}, constraint(err)
		}
	}
	selectedJSON, err := json.Marshal(m.Target.SelectedRepositoryIDs)
	if err != nil {
		return AdapterTargetUpdateResult{}, err
	}
	n, err := db.adapterStoreQueries().updateTargetConfig(ctx, chain, m, selectedJSON)
	if err != nil {
		return AdapterTargetUpdateResult{}, err
	}
	if n != 1 {
		return AdapterTargetUpdateResult{}, adapter.ErrProviderBusy
	}
	enqueued, err := enqueuePublishedTargets(ctx, db, chain, []publishedAdapterTarget{{
		id: m.Target.ID, environmentID: m.Target.EnvironmentID, generation: current.Generation,
		activeJob: activeJob, authority: m.AuthorityPrincipalID,
	}}, CanonTime(m.At))
	if err != nil {
		return AdapterTargetUpdateResult{}, err
	}
	if n, err = db.adapterStoreQueries().updateActiveAuthority(ctx, chain, m.Target.AdapterID, m.AuthorityPrincipalID); err != nil || n != 1 {
		return AdapterTargetUpdateResult{}, errors.Join(err, ErrNotFound)
	}
	current.NamePrefix = m.Target.NamePrefix
	current.VariableProtected, current.VariableHidden, current.VariableExpand = m.Target.VariableProtected, m.Target.VariableHidden, m.Target.VariableExpand
	current.Visibility = m.Target.Visibility
	current.SelectedRepositoryIDs = append([]int64(nil), m.Target.SelectedRepositoryIDs...)
	current.Generation = enqueued[0].Generation
	current.SyncStatus = "converging"
	current.FailureNames = nil
	current.AuthorityPrincipalID = m.AuthorityPrincipalID
	slices.Sort(m.Target.KeyIDs)
	return AdapterTargetUpdateResult{Target: current, Enqueue: enqueued[0], PreviousAuthorityPrincipalID: previousAuthority, AuthorityPrincipalID: m.AuthorityPrincipalID}, nil
}

// targetFindings projects the latest completed effect per name in the current
// generation. A later successful effect clears its finding; older generations
// cannot describe a reconfigured destination. Values and audit payloads are never read.
func (r adapterQueries) targetFindings(ctx context.Context, chain domain.Scope, target AdapterTarget) ([]AdapterFinding, error) {
	return r.db.adapterConfigQueries().findings(ctx, chain, target)
}
