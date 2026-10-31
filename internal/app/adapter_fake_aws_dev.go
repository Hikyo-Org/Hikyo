package app

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm"
)

// AWS browser fixtures use the production module with a metadata-aware peer.
// Only authentication and the remote API are simulated. No SDK client, default
// credential chain, STS call or network transport is constructed here. The
// existing DevAdapterFakeProvider boot and generation-switch gates own access.
type devAWSModule struct {
	provider    *devFakeProvider
	origin      string
	region      string
	seedUnowned bool
}

func (p *devFakeProvider) awsModule(config adapter.Config, credential string) (*adapter.ModuleLease, error) {
	if credential == "revoked" {
		return nil, adapter.ErrProviderAuth
	}
	descriptor, err := awssm.ParseDescriptor(credential)
	if err != nil {
		return nil, err
	}
	origin, err := adapter.CanonicalOrigin(adapter.AWSSecretsManagerProvider, config.Origin)
	if err != nil {
		return nil, err
	}
	// Explicit fixture identity prevents accidental reliance on workstation AWS
	// settings. This dev seam does not resolve or authenticate a real region.
	if descriptor.Region == "" {
		return nil, errors.New("dev AWS provider requires an explicit descriptor region")
	}
	return adapter.NewModuleLease(&devAWSModule{provider: p, origin: origin, region: descriptor.Region, seedUnowned: descriptor.SecretKey == "e2e-aws-unowned"}, func() {})
}

func (m *devAWSModule) ValidateConfig(config adapter.Config) error {
	return (&awssm.Module{}).ValidateConfig(config)
}

func (m *devAWSModule) module(destination adapter.Destination) (*awssm.Module, error) {
	if err := adapter.ValidateAWSSecretsManagerDestination(destination); err != nil {
		return nil, err
	}
	account, err := strconv.ParseInt(destination.Owner, 10, 64)
	if err != nil || account <= 0 {
		return nil, adapter.ErrDestinationID
	}
	// The configured account is a synthetic identity, not proof of authority.
	key := m.origin + "\x00" + m.region + "\x00" + destination.Owner
	m.provider.mu.Lock()
	store := m.provider.awsStores[key]
	if store == nil {
		store = &devAWSStore{secrets: make(map[string]*devAWSSecret)}
		m.provider.awsStores[key] = store
	}
	m.provider.mu.Unlock()
	return &awssm.Module{API: &devAWSAPI{provider: m.provider, store: store, account: destination.Owner, region: m.region}}, nil
}

func (m *devAWSModule) TestConnection(ctx context.Context, request adapter.ConnectionRequest) (adapter.Connection, error) {
	module, err := m.module(request.Destination)
	if err != nil {
		return adapter.Connection{}, err
	}
	return module.TestConnection(ctx, request)
}

func (m *devAWSModule) Plan(ctx context.Context, request adapter.PlanRequest) (adapter.Plan, error) {
	module, err := m.module(request.Target.Destination)
	if err != nil {
		return adapter.Plan{}, err
	}
	if err := m.seedFixture(ctx, module, request.Target, request.Manifest, request.Gate, false); err != nil {
		return adapter.Plan{}, err
	}
	return module.Plan(ctx, request)
}

func (m *devAWSModule) Sync(ctx context.Context, request adapter.SyncRequest, journal adapter.Journal) (adapter.SyncResult, error) {
	module, err := m.module(request.Target.Destination)
	if err != nil {
		return adapter.SyncResult{}, err
	}
	if !request.Teardown && journal != nil {
		gate := func(ctx context.Context) error {
			return journal.Gate(ctx, adapter.Effect{Surface: adapter.Secret, EffectiveName: "*", Disposition: adapter.Update})
		}
		if err := m.seedFixture(ctx, module, request.Target, request.Manifest, gate, true); err != nil {
			return adapter.SyncResult{}, err
		}
	}
	return module.Sync(ctx, request, journal)
}

// The marker represents a foreign resource that predates this adapter. Seed it
// before either the worker or an operator reaches the real module, so scheduling
// cannot turn the adoption fixture into an ordinary create. Authorization and
// manifest validation precede this dev-only metadata change.
func (m *devAWSModule) seedFixture(ctx context.Context, module *awssm.Module, target adapter.Target, manifest []adapter.ManifestEntry, gate func(context.Context) error, values bool) error {
	if !m.seedUnowned {
		return nil
	}
	if gate == nil {
		return adapter.ErrUnauthorized
	}
	if err := gate(ctx); err != nil {
		return err
	}
	if err := adapter.ValidateAWSSecretsManagerManifest(target.Destination, target.NamePrefix, manifest, values); err != nil {
		return err
	}
	rows, err := adapter.AWSDesiredRows(target.Destination, target.NamePrefix, manifest)
	if err != nil {
		return err
	}
	api, ok := module.API.(*devAWSAPI)
	if !ok {
		return errors.New("dev AWS provider peer is not configured")
	}
	return api.seedUnowned(ctx, rows)
}

type devAWSStore struct {
	secrets       map[string]*devAWSSecret
	sequence      uint64
	fixtureSeeded bool
}

type devAWSSecret struct {
	arn     string
	kms     string
	tags    map[string]string
	stages  map[string][]string
	values  map[string]string
	deleted bool
}

type devAWSAPI struct {
	provider *devFakeProvider
	store    *devAWSStore
	account  string
	region   string
}

var _ awssm.API = (*devAWSAPI)(nil)
var _ adapter.Module = (*devAWSModule)(nil)

func (a *devAWSAPI) Region() string { return a.region }

func (a *devAWSAPI) ResolveIdentity(ctx context.Context) (awssm.Identity, error) {
	if err := ctx.Err(); err != nil {
		return awssm.Identity{}, err
	}
	return awssm.Identity{Account: a.account, ARN: "arn:aws:iam::" + a.account + ":role/dev-fixture"}, nil
}

// lookup runs under the provider lock. A full ARN must match exactly; an ARN
// for a deleted/recreated resource never falls back to its former name.
func (a *devAWSAPI) lookup(id string) (*devAWSSecret, string, error) {
	if strings.HasPrefix(id, "arn:") {
		for name, secret := range a.store.secrets {
			if secret.arn == id {
				return secret, name, nil
			}
		}
	} else if secret := a.store.secrets[id]; secret != nil {
		return secret, id, nil
	}
	return nil, "", &awssm.ResponseError{Status: 400, Code: "ResourceNotFoundException"}
}

func (a *devAWSAPI) DescribeSecret(ctx context.Context, id string) (awssm.SecretMetadata, error) {
	if err := ctx.Err(); err != nil {
		return awssm.SecretMetadata{}, err
	}
	a.provider.mu.Lock()
	defer a.provider.mu.Unlock()
	secret, name, err := a.lookup(id)
	if err != nil {
		return awssm.SecretMetadata{}, err
	}
	stages := make(map[string][]string, len(secret.stages))
	for version, labels := range secret.stages {
		stages[version] = slices.Clone(labels)
	}
	return awssm.SecretMetadata{ARN: secret.arn, Name: name, KMSKeyID: secret.kms, Deleted: secret.deleted, Tags: maps.Clone(secret.tags), Stages: stages}, nil
}

func (a *devAWSAPI) ListSecretNames(ctx context.Context, prefix string, limit int) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a.provider.mu.Lock()
	defer a.provider.mu.Unlock()
	var names []string
	// The real closed client requests IncludePlannedDeletion=true. Pending
	// deletion retains the name and must remain visible to Plan/adoption.
	for name := range a.store.secrets {
		if strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	if limit > 0 && len(names) > limit {
		names = names[:limit]
	}
	return names, nil
}

func (a *devAWSAPI) CreateSecret(ctx context.Context, input awssm.CreateSecretInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.provider.mu.Lock()
	defer a.provider.mu.Unlock()
	return a.createSecret(input)
}

// createSecret runs under the provider lock.
func (a *devAWSAPI) createSecret(input awssm.CreateSecretInput) error {
	if a.store.secrets[input.Name] != nil {
		return &awssm.ResponseError{Status: 400, Code: "ResourceExistsException"}
	}
	// Six hex digits give each replacement a distinct complete resource ARN.
	// Refuse exhaustion instead of recycling an identity within this process.
	if a.store.sequence >= 0xffffff {
		return errors.New("dev AWS provider resource identity space exhausted")
	}
	a.store.sequence++
	partition := "aws"
	if strings.HasPrefix(a.region, "cn-") {
		partition = "aws-cn"
	} else if strings.HasPrefix(a.region, "us-gov-") {
		partition = "aws-us-gov"
	}
	a.store.secrets[input.Name] = &devAWSSecret{
		arn: fmt.Sprintf("arn:%s:secretsmanager:%s:%s:secret:%s-%06x", partition, a.region, a.account, input.Name, a.store.sequence),
		kms: input.KMSKeyID, tags: maps.Clone(input.Tags), stages: make(map[string][]string), values: make(map[string]string),
	}
	if a.store.secrets[input.Name].tags == nil {
		a.store.secrets[input.Name].tags = make(map[string]string)
	}
	return nil
}

// The explicit e2e-aws-unowned descriptor marker models a pre-existing foreign
// resource once per namespace. It never changes an existing name, copies a
// published Hikyo value, or fabricates an adoption witness. Module.Plan derives
// that witness from the resulting metadata through the normal closed API.
func (a *devAWSAPI) seedUnowned(ctx context.Context, rows []adapter.DesiredRow) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.provider.mu.Lock()
	defer a.provider.mu.Unlock()
	if a.store.fixtureSeeded {
		return nil
	}
	for _, row := range rows {
		if a.store.secrets[row.EffectiveName] != nil {
			continue
		}
		if err := a.createSecret(awssm.CreateSecretInput{Name: row.EffectiveName}); err != nil {
			return err
		}
		secret := a.store.secrets[row.EffectiveName]
		secret.values["external-fixture-version"] = "external-fixture-value"
		devAWSMoveStage(secret, "external-fixture-version", "AWSCURRENT")
	}
	a.store.fixtureSeeded = true
	return nil
}

func devAWSMoveStage(secret *devAWSSecret, version, stage string) {
	for id, labels := range secret.stages {
		secret.stages[id] = slices.DeleteFunc(labels, func(label string) bool { return label == stage })
	}
	secret.stages[version] = append(secret.stages[version], stage)
}

func (a *devAWSAPI) PutSecretValue(ctx context.Context, id, token, value string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.provider.mu.Lock()
	defer a.provider.mu.Unlock()
	secret, _, err := a.lookup(id)
	if err != nil {
		return err
	}
	if secret.deleted || token == "" {
		return &awssm.ResponseError{Status: 400, Code: "InvalidRequestException"}
	}
	if previous, exists := secret.values[token]; exists {
		if previous != value {
			return &awssm.ResponseError{Status: 400, Code: "ResourceExistsException"}
		}
		return nil
	}
	first := len(secret.values) == 0
	secret.values[token] = value
	devAWSMoveStage(secret, token, awssm.PendingStage)
	if first {
		devAWSMoveStage(secret, token, "AWSCURRENT")
	}
	return nil
}

func (a *devAWSAPI) UpdateSecretVersionStage(ctx context.Context, id, stage, moveTo, removeFrom string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.provider.mu.Lock()
	defer a.provider.mu.Unlock()
	secret, _, err := a.lookup(id)
	if err != nil {
		return err
	}
	if secret.deleted {
		return &awssm.ResponseError{Status: 400, Code: "InvalidRequestException"}
	}
	if _, exists := secret.values[moveTo]; !exists || moveTo == "" {
		return &awssm.ResponseError{Status: 400, Code: "InvalidParameterException"}
	}
	owner := ""
	for version, labels := range secret.stages {
		if slices.Contains(labels, stage) {
			owner = version
		}
	}
	if (owner != "" && owner != moveTo && removeFrom != owner) || (removeFrom != "" && removeFrom != owner) {
		return &awssm.ResponseError{Status: 400, Code: "InvalidParameterException"}
	}
	devAWSMoveStage(secret, moveTo, stage)
	if stage == "AWSCURRENT" && owner != "" && owner != moveTo {
		devAWSMoveStage(secret, owner, "AWSPREVIOUS")
	}
	return nil
}

func (a *devAWSAPI) TagSecret(ctx context.Context, id string, tags map[string]string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.provider.mu.Lock()
	defer a.provider.mu.Unlock()
	secret, _, err := a.lookup(id)
	if err != nil {
		return err
	}
	if secret.deleted {
		return &awssm.ResponseError{Status: 400, Code: "InvalidRequestException"}
	}
	maps.Copy(secret.tags, tags)
	return nil
}

func (a *devAWSAPI) deletion(ctx context.Context, id string, deleted bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.provider.mu.Lock()
	defer a.provider.mu.Unlock()
	secret, _, err := a.lookup(id)
	if err != nil {
		return err
	}
	secret.deleted = deleted
	return nil
}

func (a *devAWSAPI) RestoreSecret(ctx context.Context, id string) error {
	return a.deletion(ctx, id, false)
}
func (a *devAWSAPI) DeleteSecret(ctx context.Context, id string) error {
	return a.deletion(ctx, id, true)
}
