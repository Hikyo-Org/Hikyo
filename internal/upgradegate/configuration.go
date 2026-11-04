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
	return checkConfigurationProjection(ctx, keys, root, check, false)
}

func checkUpgradeableConfiguration(ctx context.Context, keys configurationReader, root []byte, check ConfigurationCheck) error {
	return checkConfigurationProjection(ctx, keys, root, check, true)
}

func checkConfigurationProjection(ctx context.Context, keys configurationReader, root []byte, check ConfigurationCheck, preview bool) error {
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
	if preview && projection != nil {
		projection, values, err = upgrade.PreviewConfigurationMigrations(projection, values)
		if err != nil {
			return err
		}
		defer clear(values)
	}
	if err := check(ctx, projection, values); err != nil {
		return err
	}
	return ctx.Err()
}

// CheckSourceConfiguration runs check over the stored managed configuration of
// a source that is healthy or frozen for backup. It is read-only: it takes the
// migration lock, grants no admission, fences nothing, and uses the same
// decryption and migration preview as the gate. It reports false, leaving the
// caller on its environment seed, when the datastore has no upgrade ledger yet
// (a legacy source predates managed configuration).
func CheckSourceConfiguration(ctx context.Context, store upgrade.Config, root []byte, check ConfigurationCheck) (bool, error) {
	if _, err := upgrade.InspectControl(ctx, store); errors.Is(err, upgrade.ErrAbsent) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	checked := false
	err := upgrade.WithLock(ctx, store, func(session *upgrade.Session) error {
		state, err := session.Read(ctx)
		if err != nil {
			return err
		}
		if state.Pending == nil {
			return errors.New("source configuration is unavailable: no upgrade operation state")
		}
		switch state.Pending.Phase {
		case upgrade.Healthy:
			keys, err := session.HealthyKeys(ctx, state)
			if err != nil {
				return err
			}
			checked = true
			return checkUpgradeableConfiguration(ctx, keys, root, check)
		case upgrade.BackupPreparing:
			keys, err := session.FrozenKeys(ctx, state)
			if err != nil {
				return err
			}
			checked = true
			return checkUpgradeableConfiguration(ctx, keys, root, check)
		}
		return errors.New("source configuration is unavailable in the current upgrade phase")
	})
	return checked, err
}
