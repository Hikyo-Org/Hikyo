package service

import (
	"fmt"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Managed SMTP credentials preserve their effective bytes through the normal
// draft and publication pipeline. The database-resolved profile, not a key's
// user-controlled name alone, selects this exception to ordinary normalization.
func normalizeStoredValue(p authz.Proof, key store.CatalogueKey, value string) string {
	if authz.IsSelfConfig(p) && key.Name == "HIKYO_MAIL_PASSWORD" {
		return value
	}
	return schema.Normalize(value)
}

func validateSelfConfigCells(p authz.Proof, cells []resolvedCell) error {
	if !authz.IsSelfConfig(p) {
		return nil
	}
	values := make(map[string]string, len(cells))
	for _, cell := range cells {
		if cell.set {
			values[cell.key.Name] = cell.value
		}
	}
	if _, err := runtimeconfig.Prepare(values); err != nil {
		return fmt.Errorf("%w: %s", domain.ErrInvalid, err)
	}
	return nil
}
