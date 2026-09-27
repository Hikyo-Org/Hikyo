package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

const gitLabPin = "47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU="

func TestGitLabAdapterPersistsTransportPolicyScopeAndFlags(t *testing.T) {
	db := adapterServiceDB(t)
	seedKey(t, db, "key_gitlab", "API_TOKEN")
	kr := adapterKeyring(t, db)
	var configs []adapter.Config
	var destinations []adapter.Destination
	svc := &Adapters{DB: db, Keyring: kr, ModuleFactory: func(provider adapter.Provider, config adapter.Config, _ string) (*adapter.ModuleLease, error) {
		if provider != adapter.GitLabProvider {
			t.Fatalf("provider = %q", provider)
		}
		configs = append(configs, config)
		return adapter.NewModuleLease(recordingConfigureModule{destinations: &destinations}, nil)
	}}
	view, err := svc.Create(t.Context(), LocalPrincipal("usr_adapter"), adapterScope, CreateAdapterRequest{
		Provider: "gitlab", Origin: "https://gitlab.example", Credential: []byte("glpat-token"),
		SPKIPin: gitLabPin, AllowPersonalToken: true,
		Target: AdapterTargetInput{EnvironmentID: "env_one", DestinationKind: "repository", DestinationOwner: "platform", DestinationName: "api", KeyIDs: []string{"key_gitlab"}, VariableProtected: true, VariableHidden: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) != 1 || configs[0].SPKIPin != gitLabPin || !configs[0].AllowPersonalToken {
		t.Fatalf("module config = %+v", configs)
	}
	if destinations[0].Scope != "*" {
		t.Fatalf("connection destination scope = %q, want default *", destinations[0].Scope)
	}
	target := view.Targets[0]
	if target.DestinationScope != "*" || !target.VariableProtected || !target.VariableHidden || target.VariableExpand {
		t.Fatalf("target = %+v", target)
	}
	shown, err := svc.Get(t.Context(), LocalPrincipal("usr_adapter"), adapterScope, view.Adapter.ID)
	if err != nil {
		t.Fatal(err)
	}
	if shown.Adapter.SPKIPin != gitLabPin || !shown.Adapter.AllowPersonalToken || shown.Targets[0].DestinationScope != "*" || !shown.Targets[0].VariableProtected {
		t.Fatalf("Get() = %+v", shown)
	}

	// Older clients omit the optional flags. Preserve all persisted options.
	result, err := svc.ApplyTargetMutation(t.Context(), LocalPrincipal("usr_adapter"), adapterScope, UpdateAdapterTargetRequest{
		TargetID: target.ID, ExpectedGeneration: target.Generation,
		Target: AdapterTargetInput{EnvironmentID: "env_one", DestinationKind: "repository", DestinationOwner: "platform", DestinationName: "api", KeyIDs: []string{"key_gitlab"}},
		Flags:  &AdapterTargetFlagPatch{},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	updated, ok := result.(TargetMutationUpdated)
	if !ok || !updated.Target.VariableProtected || !updated.Target.VariableHidden || updated.Target.VariableExpand || updated.Target.DestinationScope != "*" {
		t.Fatalf("omitted flags changed target: %+v", result)
	}
	// Explicit false must still be applied, rather than mistaken for omission.
	disabled := false
	result, err = svc.ApplyTargetMutation(t.Context(), LocalPrincipal("usr_adapter"), adapterScope, UpdateAdapterTargetRequest{
		TargetID: target.ID, ExpectedGeneration: updated.Target.Generation,
		Target: AdapterTargetInput{EnvironmentID: "env_one", DestinationKind: "repository", DestinationOwner: "platform", DestinationName: "api", KeyIDs: []string{"key_gitlab"}},
		Flags:  &AdapterTargetFlagPatch{VariableHidden: &disabled},
	}, false)
	if err != nil {
		t.Fatal(err)
	}

	updated, ok = result.(TargetMutationUpdated)
	if !ok || updated.Target.VariableHidden || !updated.Target.VariableProtected {
		t.Fatalf("explicit false was not applied: %+v", result)
	}

	if _, err := db.SQLiteWrite().ExecContext(t.Context(), `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('gr_gitlab_two','usr_adapter','reveal','org_adapter','prj_adapter','env_two','2026-08-17T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	// Same project, same key, same scope: the second target would fight the
	// first over one GitLab variable.
	_, err = svc.AddTarget(t.Context(), LocalPrincipal("usr_adapter"), adapterScope, view.Adapter.ID, AdapterTargetInput{
		EnvironmentID: "env_two", DestinationKind: "repository", DestinationOwner: "platform", DestinationName: "api", DestinationScope: "*", KeyIDs: []string{"key_gitlab"},
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("same-scope AddTarget() = %v, want conflict", err)
	}
	// A different environment scope is a different GitLab variable.
	added, err := svc.AddTarget(t.Context(), LocalPrincipal("usr_adapter"), adapterScope, view.Adapter.ID, AdapterTargetInput{
		EnvironmentID: "env_two", DestinationKind: "repository", DestinationOwner: "platform", DestinationName: "api", DestinationScope: "staging", KeyIDs: []string{"key_gitlab"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if added.DestinationScope != "staging" || configs[len(configs)-1].SPKIPin != gitLabPin {
		t.Fatalf("added = %+v configs = %+v", added, configs)
	}

	// The scope keys owned variables, so it cannot change in place.
	_, err = svc.updateTarget(t.Context(), LocalPrincipal("usr_adapter"), adapterScope, UpdateAdapterTargetRequest{
		TargetID: added.ID, ExpectedGeneration: added.Generation,
		Target: AdapterTargetInput{EnvironmentID: "env_two", DestinationKind: "repository", DestinationOwner: "platform", DestinationName: "api", DestinationScope: "review/*", KeyIDs: []string{"key_gitlab"}},
	})
	if !errors.Is(err, domain.ErrConflict) || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("scope change = %v, want immutable refusal", err)
	}
}

func TestGitLabOnlyOptionsAreRefusedForOtherProviders(t *testing.T) {
	db := adapterServiceDB(t)
	seedKey(t, db, "key_other", "API_TOKEN")
	kr := adapterKeyring(t, db)
	calls := 0
	svc := &Adapters{DB: db, Keyring: kr, ModuleFactory: func(adapter.Provider, adapter.Config, string) (*adapter.ModuleLease, error) {
		calls++
		return adapter.NewModuleLease(recordingConfigureModule{destinations: new([]adapter.Destination)}, nil)
	}}
	base := AdapterTargetInput{EnvironmentID: "env_one", DestinationKind: "repository", DestinationOwner: "acme", DestinationName: "app", KeyIDs: []string{"key_other"}}
	withScope := base
	withScope.DestinationScope = "production"
	withFlag := base
	withFlag.VariableProtected = true
	for name, request := range map[string]CreateAdapterRequest{
		"pin":           {Provider: "github-actions", Origin: "https://api.github.com", Credential: []byte("github_pat_x"), SPKIPin: gitLabPin, Target: base},
		"personal":      {Provider: "forgejo", Origin: "https://git.example", Credential: []byte("token"), AllowPersonalToken: true, Target: base},
		"scope":         {Provider: "forgejo", Origin: "https://git.example", Credential: []byte("token"), Target: withScope},
		"variable flag": {Provider: "github-actions", Origin: "https://api.github.com", Credential: []byte("github_pat_x"), Target: withFlag},
	} {
		if _, err := svc.Create(t.Context(), LocalPrincipal("usr_adapter"), adapterScope, request); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%s: Create() = %v, want invalid", name, err)
		}
	}
	gitlabEnvironment := base
	gitlabEnvironment.DestinationKind = "environment"
	gitlabEnvironment.DestinationEnvironment = "production"
	if _, err := svc.Create(t.Context(), LocalPrincipal("usr_adapter"), adapterScope, CreateAdapterRequest{Provider: "gitlab", Origin: "https://gitlab.example", Credential: []byte("glpat"), Target: gitlabEnvironment}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("GitLab environment kind: %v, want invalid", err)
	}
	if calls != 0 {
		t.Fatalf("refused requests reached the module factory %d times", calls)
	}
}

type recordingConfigureModule struct {
	fakeAdapterPlanModule
	destinations *[]adapter.Destination
}

func (recordingConfigureModule) ValidateConfig(adapter.Config) error { return nil }

func (m recordingConfigureModule) TestConnection(ctx context.Context, request adapter.ConnectionRequest) (adapter.Connection, error) {
	if err := request.Gate(ctx); err != nil {
		return adapter.Connection{}, err
	}
	*m.destinations = append(*m.destinations, request.Destination)
	return adapter.Connection{Version: "17.5.0", DestinationID: 77}, nil
}

func TestGitLabDisablingHiddenRequiresCeremony(t *testing.T) {
	db := adapterServiceDB(t)
	seedAdapterUpdateKeys(t, db, false)
	for _, query := range []string{
		`UPDATE adapters SET provider='gitlab' WHERE id='adp_1'`,
		`UPDATE adapter_targets SET destination_scope='*', variable_hidden=1 WHERE id='tgt_one'`,
	} {
		if _, err := db.SQLiteWrite().ExecContext(t.Context(), query); err != nil {
			t.Fatal(err)
		}
	}
	bearer := adapterCLISession(t, db)
	_, err := (&Adapters{DB: db, Auth: &Auth{DB: db}}).updateTarget(t.Context(), Bearer(bearer), adapterScope, UpdateAdapterTargetRequest{
		TargetID: "tgt_one", ExpectedGeneration: 1,
		Target: AdapterTargetInput{EnvironmentID: "env_one", DestinationKind: "repository", DestinationOwner: "acme", DestinationName: "app", DestinationScope: "*", NamePrefix: "ONE_", KeyIDs: []string{"key_update_a"}},
	})
	if !errors.Is(err, ErrReauthRequired) {
		t.Fatalf("unhide error = %v, want reauthentication", err)
	}
	var hidden, generation int
	if err := db.SQLiteRead().QueryRowContext(t.Context(), `SELECT variable_hidden,generation FROM adapter_targets WHERE id='tgt_one'`).Scan(&hidden, &generation); err != nil {
		t.Fatal(err)
	}
	if hidden != 1 || generation != 1 {
		t.Fatalf("refused unhide mutated target: hidden=%d generation=%d", hidden, generation)
	}
}
