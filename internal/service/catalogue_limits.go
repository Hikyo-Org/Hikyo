package service

import (
	"fmt"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

func checkProjectKeyGrowth(current, next int64, configuredLimit int) error {
	limit := config.EffectiveDefaultKeysPerProject(configuredLimit)
	if limit < 1 || limit > config.MaxKeysPerProjectCeiling {
		return fmt.Errorf("%w: invalid project key limit", domain.ErrInvalid)
	}
	// Reductions and edits remain possible after an operator lowers the cap.
	if next > int64(limit) && next > current {
		return definitionLimit("a project declares at most %d keys (effective project key capacity)", limit)
	}
	return nil
}

// checkResolvedCells uses division so even an unexpected large count cannot
// overflow the multiplication. Call under the same project lock as the write.
func checkResolvedCells(keys, environments int64) error {
	if environments > 0 && keys > MaxResolvedCells/environments {
		return definitionLimit("project environments times declared keys must not exceed %d resolved cells", MaxResolvedCells)
	}
	return nil
}
