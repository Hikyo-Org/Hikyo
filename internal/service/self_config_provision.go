package service

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

type SelfConfigAdoptionPreview struct {
	OwnerInstanceID          string
	SchemaVersion            int
	ConfiguredKeys, Warnings []string
	PreviewToken             string
}

type SelfConfigAdoptRequest struct{ PreviewToken, IdempotencyKey string }

type selfConfigSeed struct {
	values                    map[string]string
	owner, incarnation, token string
}

func (s *SelfConfig) prepareSeed() (selfConfigSeed, error) {
	s.seedMu.Lock()
	defer s.seedMu.Unlock()
	if s.seed != nil {
		return *s.seed, nil
	}
	owner, incarnation, err := s.DB.RecoveryIdentity()
	if err != nil {
		return selfConfigSeed{}, err
	}
	values := map[string]string{"HIKYO_UPDATE_CHANNEL": "stable"}
	if s.Seed != nil {
		values, err = s.Seed()
		if err != nil {
			return selfConfigSeed{}, err
		}
	}
	values = maps.Clone(values)
	if _, err := runtimeconfig.Prepare(values); err != nil {
		return selfConfigSeed{}, fmt.Errorf("%w: %s", domain.ErrInvalid, err)
	}
	encoded, err := json.Marshal(struct {
		Schema int
		Values map[string]string
	}{runtimeconfig.SchemaVersion, values})
	if err != nil {
		return selfConfigSeed{}, err
	}
	defer crypto.Zero(encoded)
	token, err := s.Keyring.SelfConfigAdoptionToken(owner, encoded)
	if err != nil {
		return selfConfigSeed{}, err
	}
	s.seed = &selfConfigSeed{values: values, owner: owner, incarnation: incarnation, token: token}
	return *s.seed, nil
}

// PreviewAdoption returns names only. Neither secret values nor unkeyed
// fingerprints reach the browser, and an existing binding forbids re-import.
func (s *SelfConfig) PreviewAdoption(ctx context.Context, actor Actor) (SelfConfigAdoptionPreview, error) {
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpSelfConfigPreview, domain.Scope{}, s.now())
		if err != nil {
			return err
		}
		_, err = r.SelfConfig().Binding(ctx, p)
		if err == nil {
			return fmt.Errorf("%w: instance configuration is already managed", domain.ErrConflict)
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		owner, err := az.InstanceIdentity(ctx)
		if err != nil {
			return err
		}
		return selfConfigSeedEvent(ctx, r, p, caller.Principal, audit.EventSelfConfigStatusRead, "instance", owner, audit.Payload{"owner_instance_id": owner}, true)
	})
	if err != nil {
		return SelfConfigAdoptionPreview{}, err
	}
	seed, err := s.prepareSeed()
	if err != nil {
		return SelfConfigAdoptionPreview{}, err
	}
	return SelfConfigAdoptionPreview{OwnerInstanceID: seed.owner, SchemaVersion: runtimeconfig.SchemaVersion, ConfiguredKeys: slices.Sorted(maps.Keys(seed.values)), Warnings: []string{}, PreviewToken: seed.token}, nil
}

func (s *SelfConfig) Adopt(ctx context.Context, actor Actor, req SelfConfigAdoptRequest) (SelfConfigStatus, error) {
	if req.IdempotencyKey == "" || len(req.IdempotencyKey) > 128 || req.PreviewToken == "" {
		return SelfConfigStatus{}, domain.ErrInvalid
	}
	var existing *store.SelfConfigBinding
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpSelfConfigPreview, domain.Scope{}, s.now())
		if err != nil {
			return err
		}
		b, err := r.SelfConfig().Binding(ctx, p)
		if err == nil {
			if b.AdoptionKey != req.IdempotencyKey || b.AdoptedBy != string(caller.Principal) {
				return fmt.Errorf("%w: instance configuration is already managed", domain.ErrConflict)
			}
			existing = &b
		} else if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		owner, err := az.InstanceIdentity(ctx)
		if err != nil {
			return err
		}
		return selfConfigSeedEvent(ctx, r, p, caller.Principal, audit.EventSelfConfigStatusRead, "instance", owner, audit.Payload{"owner_instance_id": owner}, true)
	})
	if err != nil {
		return SelfConfigStatus{}, err
	}
	if existing != nil {
		return s.Status(ctx, actor)
	}
	// Authorization precedes reading local seed files, including on retries.
	preview, err := s.PreviewAdoption(ctx, actor)
	if err != nil {
		return SelfConfigStatus{}, err
	}
	if subtle.ConstantTimeCompare([]byte(preview.PreviewToken), []byte(req.PreviewToken)) != 1 {
		return SelfConfigStatus{}, fmt.Errorf("%w: adoption preview changed", domain.ErrConflict)
	}
	seed, err := s.prepareSeed()
	if err != nil {
		return SelfConfigStatus{}, err
	}
	if subtle.ConstantTimeCompare([]byte(seed.token), []byte(req.PreviewToken)) != 1 {
		return SelfConfigStatus{}, fmt.Errorf("%w: adoption preview changed", domain.ErrConflict)
	}
	at, err := s.runtimeTimestamp(ctx)
	if err != nil {
		return SelfConfigStatus{}, err
	}
	var binding store.SelfConfigBinding
	err = tx.WriteSerialized(ctx, s.DB, "hikyo:self-config-provision", func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, _, err := authorize(ctx, az, actor, authz.OpSelfConfigAdopt, domain.Scope{}, s.now())
		if err != nil {
			return err
		}
		intent, err := NewSelfConfigReauthIntent(SelfConfigReauthTarget{Action: "adopt", OwnerInstanceID: seed.owner, SchemaVersion: runtimeconfig.SchemaVersion, PreviewToken: req.PreviewToken})
		if err != nil {
			return err
		}
		if s.Auth == nil {
			return domain.ErrUnauthorized
		}
		if err := s.Auth.ConsumeSelfConfigReauth(ctx, az, caller, intent, s.now()); err != nil {
			return err
		}
		binding, err = s.provision(ctx, r, az, caller, seed, req.IdempotencyKey, at)
		return err
	})
	if err != nil {
		return SelfConfigStatus{}, err
	}
	// Grant creation invalidates the caller's sessions under the normal grant
	// contract. Return the committed write result without caching its authority.
	revision := binding.DesiredRevision
	return SelfConfigStatus{OwnerInstanceID: binding.OwnerInstanceID, Managed: true, Binding: &SelfConfigBindingView{OrgID: binding.OrgID, ProjectID: binding.ProjectID, EnvironmentID: binding.EnvironmentID, SchemaVersion: int(binding.SchemaVersion)}, Generation: binding.Generation, DesiredRevision: &revision, LatestRevision: &revision, State: "pending", Nodes: []SelfConfigNodeView{}}, nil
}

// provision uses ordinary hierarchy, grant, schema and publish repositories.
// The immutable binding is committed last, in the same transaction as every
// seeded resource, so rollback cannot leave a half-created management project.
func (s *SelfConfig) provision(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, caller authz.Identity, seed selfConfigSeed, idempotencyKey string, now time.Time) (store.SelfConfigBinding, error) {
	adminProof, err := az.Authorize(ctx, caller, authz.OpSelfConfigAdopt, domain.Scope{})
	if err != nil {
		return store.SelfConfigBinding{}, err
	}
	if b, err := r.SelfConfig().Binding(ctx, adminProof); err == nil {
		if b.AdoptionKey != idempotencyKey || b.AdoptedBy != string(caller.Principal) {
			return store.SelfConfigBinding{}, domain.ErrConflict
		}
		return b, nil
	} else if !errors.Is(err, domain.ErrNotFound) {
		return store.SelfConfigBinding{}, err
	}
	orgID, err := newID("org")
	if err != nil {
		return store.SelfConfigBinding{}, err
	}
	projectID, err := newID("prj")
	if err != nil {
		return store.SelfConfigBinding{}, err
	}
	envID, err := newID("env")
	if err != nil {
		return store.SelfConfigBinding{}, err
	}
	orgProof, err := az.Authorize(ctx, caller, authz.OpOrgCreate, domain.Scope{})
	if err != nil {
		return store.SelfConfigBinding{}, err
	}
	name := "Hikyo"
	listProof, err := az.Authorize(ctx, caller, authz.OpOrgList, domain.Scope{})
	if err != nil {
		return store.SelfConfigBinding{}, err
	}
	orgs, err := r.Orgs().List(ctx, listProof)
	if err != nil {
		return store.SelfConfigBinding{}, err
	}
	for _, org := range orgs {
		if org.Name == name {
			name = "Hikyo " + orgID
			break
		}
	}
	if err := r.Orgs().Create(ctx, orgProof, store.Org{ID: orgID, Name: name, Active: true, Metadata: json.RawMessage(`{}`), CreatedAt: store.CanonTime(now)}); err != nil {
		return store.SelfConfigBinding{}, err
	}
	if err := selfConfigSeedEvent(ctx, r, orgProof, caller.Principal, audit.EventOrgCreated, "org", orgID, audit.Payload{"org_id": orgID, "org_name": name}, true); err != nil {
		return store.SelfConfigBinding{}, err
	}
	orgScope := domain.Scope{Org: domain.OrgID(orgID)}
	grantProof, err := az.Authorize(ctx, caller, authz.OpTemplateApplyOrg, orgScope)
	if err != nil {
		return store.SelfConfigBinding{}, err
	}
	grants := &Grants{DB: s.DB, Now: func() time.Time { return now }}
	if _, err := grants.applyTemplate(ctx, r, az, grantProof, caller, domain.TemplateAdmin, caller.Principal, orgScope, domain.LevelOrg); err != nil {
		return store.SelfConfigBinding{}, err
	}
	projectProof, err := az.Authorize(ctx, caller, authz.OpProjectCreate, orgScope)
	if err != nil {
		return store.SelfConfigBinding{}, err
	}
	projectName := "Hikyo " + seed.owner
	if err := r.Projects().Create(ctx, projectProof, store.NewProject{ID: projectID, Name: projectName, CreatedAt: store.CanonTime(now)}); err != nil {
		return store.SelfConfigBinding{}, err
	}
	if err := selfConfigSeedEvent(ctx, r, projectProof, caller.Principal, audit.EventProjectCreated, "project", projectID, audit.Payload{"name": projectName}, false); err != nil {
		return store.SelfConfigBinding{}, err
	}
	scope := domain.Scope{Org: domain.OrgID(orgID), Project: domain.ProjectID(projectID)}
	keyProof, err := az.Authorize(ctx, caller, authz.OpKeyCreate, scope)
	if err != nil {
		return store.SelfConfigBinding{}, err
	}
	dekProof, err := az.Authorize(ctx, caller, authz.OpSelfConfigProvisionProject, scope)
	if err != nil {
		return store.SelfConfigBinding{}, err
	}
	wrapped, sealer, err := s.Keyring.PrepareNewProject(orgID, projectID)
	if err != nil {
		return store.SelfConfigBinding{}, err
	}
	if err := r.Keys().InsertInitialProjectDEK(ctx, dekProof, wrapped); err != nil {
		return store.SelfConfigBinding{}, err
	}
	if err := selfConfigSeedEvent(ctx, r, dekProof, caller.Principal, audit.EventSelfConfigProjectPrepared, "project", projectID, audit.Payload{"owner_instance_id": seed.owner}, false); err != nil {
		return store.SelfConfigBinding{}, err
	}
	for _, key := range runtimeconfig.Catalogue() {
		id, err := newID("key")
		if err != nil {
			return store.SelfConfigBinding{}, err
		}
		compiled, err := schema.CompileClassified(key.Classification, key.Declaration)
		if err != nil {
			return store.SelfConfigBinding{}, err
		}
		decl, err := compiled.Canonical()
		if err != nil {
			return store.SelfConfigBinding{}, err
		}
		if err := r.Catalogue().Create(ctx, keyProof, store.NewCatalogueKey{ID: id, Name: key.Name, Classification: string(key.Classification), Description: key.Description, Declaration: string(decl), RequiredMode: string(schema.PresenceNone), ForbiddenMode: string(schema.PresenceNone), CreatedAt: store.CanonTime(now)}); err != nil {
			return store.SelfConfigBinding{}, err
		}
		if err := selfConfigSeedEvent(ctx, r, keyProof, caller.Principal, audit.EventKeyCreated, "key", id, audit.Payload{"name": key.Name, "classification": string(key.Classification), "namespace": ""}, false); err != nil {
			return store.SelfConfigBinding{}, err
		}
	}
	schemaCharged := false
	if err := bumpSchemaRevision(ctx, r, keyProof, s.Budget, &schemaCharged, scope.Project); err != nil {
		return store.SelfConfigBinding{}, err
	}
	envProof, err := az.Authorize(ctx, caller, authz.OpEnvCreate, scope)
	if err != nil {
		return store.SelfConfigBinding{}, err
	}
	if err := r.Environments().Create(ctx, envProof, store.NewEnvironment{ID: envID, Name: "Production", CreatedAt: store.CanonTime(now)}); err != nil {
		return store.SelfConfigBinding{}, err
	}
	if err := selfConfigSeedEvent(ctx, r, envProof, caller.Principal, audit.EventEnvCreated, "environment", envID, audit.Payload{"name": "Production"}, false); err != nil {
		return store.SelfConfigBinding{}, err
	}
	scope.Env = domain.EnvID(envID)
	publishProof, err := az.Authorize(ctx, caller, authz.OpValuePublish, scope)
	if err != nil {
		return store.SelfConfigBinding{}, err
	}
	if err := fenceProject(ctx, r, publishProof, sealer, scope); err != nil {
		return store.SelfConfigBinding{}, err
	}
	index, err := loadGroupIndex(ctx, r.Catalogue(), publishProof)
	if err != nil {
		return store.SelfConfigBinding{}, err
	}
	for _, key := range index.keys {
		if value, ok := seed.values[key.Name]; ok {
			// Seed writes preserve the effective input bytes. Snapshot creation
			// then uses the same encrypted cells and lineage as normal publish.
			id, err := newID("val")
			if err != nil {
				return store.SelfConfigBinding{}, err
			}
			entry := store.ValueEntry{ID: id, OrgID: orgID, ProjectID: projectID, EnvironmentID: envID, KeyID: key.ID}
			sealed, err := sealer.SealValue(valueAAD(entry), []byte(value))
			if err != nil {
				return store.SelfConfigBinding{}, err
			}
			if err := r.Values().Put(ctx, publishProof, store.NewValueEntry{ID: id, KeyID: key.ID, Ciphertext: sealed, UpdatedAt: store.CanonTime(now), UpdatedBy: string(caller.Principal)}); err != nil {
				return store.SelfConfigBinding{}, err
			}
			if err := selfConfigSeedEvent(ctx, r, publishProof, caller.Principal, audit.EventValueSet, "key", key.ID, audit.Payload{"key_id": key.ID, "name": key.Name, "classification": key.Classification}, false); err != nil {
				return store.SelfConfigBinding{}, err
			}
		}
	}
	pub, err := materialize(ctx, r, publishProof, sealer, s.Keyring, scope, caller.Principal, now, nil, MaxProjectStorageBytes, index, nil)
	if err != nil {
		return store.SelfConfigBinding{}, err
	}
	if err := recordPublish(ctx, r, publishProof, caller.Principal, envID, pub, 0, "environment-create", now); err != nil {
		return store.SelfConfigBinding{}, err
	}
	snapshot, err := r.Snapshots().Latest(ctx, publishProof)
	if err != nil {
		return store.SelfConfigBinding{}, err
	}
	binding := store.SelfConfigBinding{OwnerInstanceID: seed.owner, OrgID: orgID, ProjectID: projectID, EnvironmentID: envID, SchemaVersion: runtimeconfig.SchemaVersion, Generation: 1, DesiredRevision: pub.Revision, DesiredSnapshotID: snapshot.ID, Incarnation: seed.incarnation, CreatedAt: store.CanonTime(now), UpdatedAt: store.CanonTime(now), SeedFingerprint: seed.token, AdoptionKey: idempotencyKey, AdoptedBy: string(caller.Principal)}
	if err := r.SelfConfig().CreateBinding(ctx, adminProof, binding); err != nil {
		return store.SelfConfigBinding{}, err
	}
	if err := selfConfigSeedEvent(ctx, r, adminProof, caller.Principal, audit.EventSelfConfigAdopted, "instance", seed.owner, audit.Payload{"owner_instance_id": seed.owner, "revision": pub.Revision, "generation": int64(1)}, true); err != nil {
		return store.SelfConfigBinding{}, err
	}
	return binding, nil
}

func selfConfigSeedEvent(ctx context.Context, r store.Repos, p authz.Proof, principal domain.PrincipalID, typ audit.EventType, kind, id string, payload audit.Payload, instance bool) error {
	event, err := domainEvent(ctx, typ, principal, audit.Object{Type: kind, ID: id}, payload)
	if err != nil {
		return err
	}
	if instance {
		return r.Audit().InsertInstance(ctx, p, event)
	}
	return r.Audit().InsertTenant(ctx, p, event)
}
