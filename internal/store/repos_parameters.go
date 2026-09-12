package store

import (
	"context"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

func nonEmptyJSON(raw string) string {
	if raw == "" {
		return "{}"
	}
	return raw
}

func (r sqliteEnvs) Parameters(ctx context.Context, p authz.Proof) (string, error) {
	chain, err := authz.Verify(p, authz.StoreEnvironmentParametersGet, r.tok)
	if err != nil {
		return "", err
	}
	env, err := envOf(chain, authz.StoreEnvironmentParametersGet)
	if err != nil {
		return "", err
	}
	return r.q.GetEnvironmentParameters(ctx, sqlitegen.GetEnvironmentParametersParams{ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env})
}

func (r pgEnvs) Parameters(ctx context.Context, p authz.Proof) (string, error) {
	chain, err := authz.Verify(p, authz.StoreEnvironmentParametersGet, r.tok)
	if err != nil {
		return "", err
	}
	env, err := envOf(chain, authz.StoreEnvironmentParametersGet)
	if err != nil {
		return "", err
	}
	return r.q.GetEnvironmentParameters(ctx, pggen.GetEnvironmentParametersParams{ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env})
}

func (r sqliteEnvs) SetParameters(ctx context.Context, p authz.Proof, declarations string) error {
	chain, err := authz.Verify(p, authz.StoreEnvironmentParametersSet, r.tok)
	if err != nil {
		return err
	}
	env, err := envOf(chain, authz.StoreEnvironmentParametersSet)
	if err != nil {
		return err
	}
	n, err := r.q.SetEnvironmentParameters(ctx, sqlitegen.SetEnvironmentParametersParams{ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env, ParametersJson: declarations})
	if err != nil {
		return constraint(err)
	}
	if n != 1 {
		return ErrNotFound
	}
	return nil
}

func (r pgEnvs) SetParameters(ctx context.Context, p authz.Proof, declarations string) error {
	chain, err := authz.Verify(p, authz.StoreEnvironmentParametersSet, r.tok)
	if err != nil {
		return err
	}
	env, err := envOf(chain, authz.StoreEnvironmentParametersSet)
	if err != nil {
		return err
	}
	n, err := r.q.SetEnvironmentParameters(ctx, pggen.SetEnvironmentParametersParams{ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env, ParametersJson: declarations})
	if err != nil {
		return constraint(err)
	}
	if n != 1 {
		return ErrNotFound
	}
	return nil
}

func (r sqliteSnapshots) ParameterContract(ctx context.Context, p authz.Proof, snapshot Snapshot) (string, error) {
	chain, err := authz.Verify(p, authz.StoreSnapshotsParameterContract, r.tok)
	if err != nil {
		return "", err
	}
	env, err := envOf(chain, authz.StoreSnapshotsParameterContract)
	if err != nil {
		return "", err
	}
	if err := authz.VerifySelfConfigSnapshot(p, snapshot.ID); err != nil {
		return "", err
	}
	return r.q.GetSnapshotParameterContract(ctx, sqlitegen.GetSnapshotParameterContractParams{ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env, SnapshotID: snapshot.ID})
}

func (r pgSnapshots) ParameterContract(ctx context.Context, p authz.Proof, snapshot Snapshot) (string, error) {
	chain, err := authz.Verify(p, authz.StoreSnapshotsParameterContract, r.tok)
	if err != nil {
		return "", err
	}
	env, err := envOf(chain, authz.StoreSnapshotsParameterContract)
	if err != nil {
		return "", err
	}
	if err := authz.VerifySelfConfigSnapshot(p, snapshot.ID); err != nil {
		return "", err
	}
	return r.q.GetSnapshotParameterContract(ctx, pggen.GetSnapshotParameterContractParams{ChainOrgID: string(chain.Org), ChainProjectID: string(chain.Project), ChainEnvID: env, SnapshotID: snapshot.ID})
}
