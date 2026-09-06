package upgradegate

import (
	"bytes"
	"context"
	"errors"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
)

// ConfigurationCheck receives a fixed owner projection and its opened values,
// never datastore authority. Nil projection means bootstrap configuration.
// It must start no listeners, workers, or provider requests.
type ConfigurationCheck func(context.Context, *upgrade.CandidateConfiguration, map[string]string) error

type configurationReader interface {
	crypto.ExistingHierarchyStore
	Configuration(context.Context) (*upgrade.CandidateConfiguration, error)
}

func checkExistingConfiguration(ctx context.Context, keys configurationReader, root []byte, check ConfigurationCheck) error {
	if check == nil {
		return nil
	}
	projection, err := keys.Configuration(ctx)
	if err != nil {
		return errors.New("candidate configuration inventory unavailable")
	}
	var values map[string]string
	if projection != nil {
		values, err = crypto.OpenExistingProjectFields(ctx, keys, bytes.Clone(root), projection.OrgID, projection.ProjectID, projection.Fields)
		if err != nil {
			return errors.New("candidate configuration cannot authenticate its saved values")
		}
		defer clear(values)
	}
	if err := check(ctx, projection, values); err != nil {
		return err
	}
	return ctx.Err()
}
