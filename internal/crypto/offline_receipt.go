package crypto

import "crypto/subtle"

// OfflineSnapshotReceipt authenticates server-selected disclosure metadata.
// The dedicated, scope-derived key prevents change tokens, cursors, and receipts
// from being substituted for one another. Token-key rotation invalidates receipts.
func (k *Keyring) OfflineSnapshotReceipt(orgID, projectID, envID string, claims []byte) (string, error) {
	key, err := k.deriveScopedTokenKey("hikyo/offline-snapshot-receipt/v1", orgID, projectID, envID)
	if err != nil {
		return "", err
	}
	defer Zero(key)
	return tag(key, claims), nil
}

func (k *Keyring) VerifyOfflineSnapshotReceipt(orgID, projectID, envID string, claims []byte, signature string) (bool, error) {
	want, err := k.OfflineSnapshotReceipt(orgID, projectID, envID, claims)
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare([]byte(want), []byte(signature)) == 1, nil
}
