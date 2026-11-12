package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/upgradecustody"
)

// unattendedRootCustody re-seals the runtime-owned vault of an enrolled
// unattended installation across a root rotation. A host vault is root-only and
// unreachable from the server, so other installations get nil.
func unattendedRootCustody(cfg *config.Config) service.RootCustody {
	if !cfg.Upgrade.Unattended {
		return nil
	}
	return unattendedCustody{directory: filepath.Join(cfg.Upgrade.StateDirectory, "unattended")}
}

type unattendedCustody struct{ directory string }

// vault returns the custody directory and bound instance, or "" when no vault
// is enrolled yet.
func (c unattendedCustody) vault() (custody, instance string, err error) {
	custody = filepath.Join(c.directory, "custody")
	if _, err := os.Lstat(filepath.Join(custody, "operator.age")); errors.Is(err, os.ErrNotExist) {
		return "", "", nil
	} else if err != nil {
		return "", "", err
	}
	raw, err := readUnattendedPrivate(filepath.Join(c.directory, "enrollment.json"))
	if err != nil {
		return "", "", err
	}
	var enrollment unattendedEnrollment
	if definitions.DecodeStrict(raw, &enrollment) != nil || enrollment.Format != "hikyo.unattended-enrollment/v1" || enrollment.Instance == "" {
		return "", "", errors.New("unattended custody has no bound installation")
	}
	return custody, enrollment.Instance, nil
}

func (c unattendedCustody) Stage(ctx context.Context, current, next []byte) error {
	custody, instance, err := c.vault()
	if err != nil || custody == "" || ctx.Err() != nil {
		return errors.Join(err, ctx.Err())
	}
	return upgradecustody.StageRootRotation(custody, current, next, instance)
}

func (c unattendedCustody) Commit(ctx context.Context, root []byte) error {
	custody, instance, err := c.vault()
	if err != nil || custody == "" || ctx.Err() != nil {
		return errors.Join(err, ctx.Err())
	}
	return upgradecustody.CommitRootRotation(custody, root, instance)
}

func (c unattendedCustody) Check(ctx context.Context, root []byte) error {
	custody, instance, err := c.vault()
	if err != nil || custody == "" || ctx.Err() != nil {
		return errors.Join(err, ctx.Err())
	}
	return upgradecustody.CheckRootRotation(custody, root, instance)
}
