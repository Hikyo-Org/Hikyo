package crypto

import "context"

// InitializeFreshHierarchy uses the maintained hierarchy generator, then
// discards all plaintext key material before returning. The supplied store must
// enforce empty, exclusively owned first-boot state and atomic persistence.
// No runtime keyring or retained datastore capability escapes this operation.
func InitializeFreshHierarchy(ctx context.Context, store KeyStore, root []byte) error {
	keyring, err := LoadKeyring(ctx, store, root)
	if err != nil {
		return err
	}
	if master := keyring.master.Swap(nil); master != nil {
		for _, key := range master.byVer {
			Zero(key)
		}
	}
	if instance := keyring.instance.Swap(nil); instance != nil {
		for _, handle := range instance.byVer {
			Zero(handle.key)
		}
	}
	if token := keyring.token.current.Swap(nil); token != nil {
		Zero(token.key)
	}
	if scanning := keyring.scanning.current.Swap(nil); scanning != nil {
		Zero(scanning.key)
	}
	return nil
}
