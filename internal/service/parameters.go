package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/delivery"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/parameters"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func (s *Environments) Parameters(ctx context.Context, actor Actor, scope domain.Scope) (map[string]string, error) {
	var out map[string]string
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		_, p, err := authorize(ctx, az, actor, authz.OpEnvRead, scope, time.Now().UTC())
		if err != nil {
			return err
		}
		out, err = environmentParameters(ctx, r.Environments(), p)
		return err
	})
	return out, err
}

// SetParameter changes declarations for the next publication only. Existing
// delivery remains bound to the immutable contract captured by its snapshot.
func (s *Environments) SetParameter(ctx context.Context, actor Actor, scope domain.Scope, name, pattern string, remove bool) error {
	if err := parameters.CheckName(name); err != nil {
		return invalidDetail("%s", err)
	}
	if remove {
		if pattern != "" {
			return invalidDetail("pattern must be omitted when deleting a parameter")
		}
	} else if err := parameters.CheckDeclaration(name, pattern); err != nil {
		return invalidDetail("%s", err)
	}

	var charged bool
	return tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpEnvParameterSet, scope, time.Now().UTC())
		if err != nil {
			return err
		}
		if err := r.Projects().Lock(ctx, p); err != nil {
			return err
		}
		if err := requireDBManagedDefinitions(ctx, r, p); err != nil {
			return err
		}
		declarations, err := environmentParameters(ctx, r.Environments(), p)
		if err != nil {
			return err
		}
		action := "add"
		if remove {
			if _, ok := declarations[name]; !ok {
				return invalidDetail("parameter %s is not declared", name)
			}
			delete(declarations, name)
			action = "delete"
		} else {
			if _, ok := declarations[name]; ok {
				return invalidDetail("parameter %s is already declared; delete it before changing its pattern", name)
			}
			if len(declarations) >= parameters.MaxCount {
				return invalidDetail("at most %d parameters are allowed", parameters.MaxCount)
			}
			declarations[name] = pattern
		}
		if err := r.Environments().SetParameters(ctx, p, parameters.Encode(declarations)); err != nil {
			return err
		}
		if err := bumpSchemaRevision(ctx, r, p, s.Budget, &charged, scope.Project); err != nil {
			return err
		}
		ev, err := domainEvent(ctx, audit.EventEnvParameterChanged, caller.Principal, audit.Object{Type: "environment", ID: string(scope.Env)}, audit.Payload{"name": audit.SanitizeFreeText(name), "pattern": audit.SanitizeFreeText(pattern), "action": action})
		if err != nil {
			return err
		}
		return r.Audit().InsertTenant(ctx, p, ev)
	})
}

func environmentParameters(ctx context.Context, r store.EnvironmentReader, p authz.Proof) (map[string]string, error) {
	raw, err := r.Parameters(ctx, p)
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, fmt.Errorf("service: invalid stored environment parameters: %w", err)
	}
	if values == nil {
		return nil, fmt.Errorf("service: stored environment parameters must be an object")
	}
	return values, nil
}

func snapshotParameters(ctx context.Context, r store.SnapshotReader, p authz.Proof, snapshot store.Snapshot, supplied map[string]string) (parameters.Contract, error) {
	contract, err := readSnapshotParameterContract(ctx, r, p, snapshot)
	if err != nil {
		return contract, err
	}

	if err := parameters.Validate(contract.Declarations, supplied); err != nil {
		return contract, invalidDetail("%s", err)
	}
	return contract, nil
}

// Same-version additive metadata is tolerated; unknown semantic versions fail
// closed. Pre-feature empty contracts preserve literal snapshot values.
func readSnapshotParameterContract(ctx context.Context, r store.SnapshotReader, p authz.Proof, snapshot store.Snapshot) (parameters.Contract, error) {
	raw, err := r.ParameterContract(ctx, p, snapshot)
	if err != nil {
		return parameters.Contract{}, err
	}
	return decodeParameterContract(raw)
}

func decodeParameterContract(raw string) (parameters.Contract, error) {
	var contract *parameters.Contract
	if err := json.Unmarshal([]byte(raw), &contract); err != nil {
		return parameters.Contract{}, fmt.Errorf("service: invalid stored parameter contract: %w", err)
	}
	if contract == nil {
		return parameters.Contract{}, fmt.Errorf("service: stored parameter contract must be an object")
	}
	if contract.Version != 0 && contract.Version != 1 {
		return parameters.Contract{}, fmt.Errorf("service: unsupported parameter contract version %d", contract.Version)
	}
	if contract.Version != 1 && (len(contract.Declarations) != 0 || len(contract.Schemas) != 0) {
		return parameters.Contract{}, fmt.Errorf("service: nonempty parameter contract requires version 1")
	}
	return *contract, nil
}

func resolveConfig(contract parameters.Contract, supplied map[string]string, name, classification, value string) (string, error) {
	if classification != string(schema.Config) {
		return value, nil
	}
	declaration, templated := contract.Schemas[name]
	if !templated {
		return value, nil
	}
	resolved, err := parameters.Resolve(value, supplied, schema.MaxValueBytes)
	if err != nil {
		return "", invalidDetail("key %q: %s", name, err)
	}
	if err := validateLiteralValue(store.CatalogueKey{Name: name, Classification: classification, Declaration: declaration}, resolved); err != nil {
		return "", err
	}
	return resolved, nil
}

func auditedParameters(supplied map[string]string) map[string]string {
	out := make(map[string]string, len(supplied))
	for name, value := range supplied {
		out[name] = audit.SanitizeFreeText(value)
	}
	return out
}

// No-param manifests retain their exact legacy encoding. Parameterized fetches
// bind even unused declarations, so switching caller inputs always moves the
// change token and every downstream keyed delivery stamp.
func parameterizedManifest(rows []delivery.Row, supplied map[string]string) []byte {
	manifest := delivery.Manifest(rows)
	if len(supplied) == 0 {
		return manifest
	}
	return append(append([]byte("hikyo/parameterized-delivery/v1\x00"+parameters.Encode(supplied)+"\x00"), manifest...), '\x00')
}
