package operator

import (
	"fmt"
	"regexp"
	"slices"

	corev1 "k8s.io/api/core/v1"

	"github.com/Hikyo-Org/hikyo/internal/delivery"
	hikyov1 "github.com/Hikyo-Org/hikyo/internal/operator/api/v1alpha1"
	opclient "github.com/Hikyo-Org/hikyo/internal/operator/client"
)

var secretDataKeySyntax = regexp.MustCompile(`^[-._a-zA-Z0-9]+$`)

// validateSelection also protects clusters that have not installed the current
// admission schema. Invalid selection must never acquire a credential or fetch.
func validateSelection(cr *hikyov1.HikyoSecret) error {
	if cr.Spec.AllKeys == (len(cr.Spec.Mapping) > 0) {
		return fmt.Errorf("set exactly one of allKeys: true or a nonempty mapping")
	}
	if cr.Spec.AllKeys && effectiveSecretType(cr.Spec.Target.Type) != corev1.SecretTypeOpaque {
		return fmt.Errorf("allKeys requires an Opaque target; native Secret types require explicit mapping")
	}
	if len(cr.Spec.Mapping) > 256 {
		return fmt.Errorf("mapping must contain at most 256 entries")
	}
	seen := make(map[string]bool, len(cr.Spec.Mapping))
	for i, m := range cr.Spec.Mapping {
		if !m.Key.Valid() {
			return fmt.Errorf("mapping entry %d has an invalid source key", i)
		}
		dest := m.EffectiveSecretKey()
		if len(dest) > 253 || !secretDataKeySyntax.MatchString(dest) {
			return fmt.Errorf("mapping entry %d has an invalid destination key", i)
		}
		if seen[dest] {
			return fmt.Errorf("mapping destination keys must be unique")
		}
		seen[dest] = true
	}
	return nil
}

// validateAllKeysAcknowledgements allows prospective grants only for exact
// baseline names. In particular, LD_* is not an acknowledgement of LD_PRELOAD.
func validateAllKeysAcknowledgements(cr *hikyov1.HikyoSecret) error {
	if len(cr.Spec.AcknowledgedLoaderKeys) > 64 {
		return fmt.Errorf("acknowledgedLoaderKeys must contain at most 64 names")
	}
	seen := make(map[hikyov1.KeyName]bool, len(cr.Spec.AcknowledgedLoaderKeys))
	for i, name := range cr.Spec.AcknowledgedLoaderKeys {
		if !name.Valid() || !delivery.IsLoaderControlKey(string(name)) || seen[name] {
			return fmt.Errorf("acknowledgedLoaderKeys entry %d must be a unique, exact loader-control baseline name", i)
		}
		seen[name] = true
	}
	return nil
}

// selectedMapping treats the server's authorized manifest as the complete set
// in allKeys mode. It does not enumerate environments or broaden server grants.
func selectedMapping(cr *hikyov1.HikyoSecret, keys []opclient.DeliveredKey) []hikyov1.Mapping {
	if !cr.Spec.AllKeys {
		return cr.Spec.Mapping
	}
	mapping := make([]hikyov1.Mapping, 0, len(keys))
	for _, key := range keys {
		mapping = append(mapping, hikyov1.Mapping{Key: hikyov1.KeyName(key.Name)})
	}
	return mapping
}

func unacknowledgedManifestKeys(cr *hikyov1.HikyoSecret, keys []opclient.DeliveredKey) []string {
	var refused []string
	for _, key := range keys {
		if delivery.IsLoaderControlKey(key.Name) && !slices.Contains(cr.Spec.AcknowledgedLoaderKeys, hikyov1.KeyName(key.Name)) {
			refused = append(refused, key.Name)
		}
	}
	slices.Sort(refused)
	return refused
}
