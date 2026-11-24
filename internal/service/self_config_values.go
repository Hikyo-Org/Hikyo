package service

import (
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
)

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
	// Every Prepare refusal names the key it is about and never a secret value
	// (config.managedOwnerRefusal, mail.invalid), so it travels as the
	// caller-safe detail: a self-config publish is post-authorization and the
	// values are the caller's own drafts, and a bare 400 leaves nothing to fix.
	if _, err := runtimeconfig.Prepare(values); err != nil {
		return invalidDetail("%s", err)
	}
	return nil
}
