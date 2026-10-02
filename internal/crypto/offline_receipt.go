package crypto

import (
	"crypto/subtle"
	"errors"
)

// DeliveryTokenSnapshot holds one transaction's immutable scoped delivery keys.
// Its caller selects and locks the active wrapped token row inside the SAME
// authorized transaction as issuance/reconciliation, never through a nested
// KeyStore refresh. A process-local token handle is not evidence of freshness.
type DeliveryTokenSnapshot struct{ receipt, change, cursor []byte }

func (k *Keyring) DeliveryTokenSnapshot(row WrappedKey, orgID, projectID, envID string) (*DeliveryTokenSnapshot, error) {
	if k == nil || row.Purpose != PurposeToken || row.OrgID != "" || row.ProjectID != "" || row.Version == 0 {
		return nil, errors.New("crypto: receipt signer requires the active root token key")
	}
	handle, err := k.unwrapTier3(row)
	if err != nil {
		return nil, err
	}
	defer Zero(handle.key)
	snapshot := &DeliveryTokenSnapshot{}
	for _, derivation := range []struct {
		label string
		key   *[]byte
	}{
		{"hikyo/offline-snapshot-receipt/v1", &snapshot.receipt},
		{tokenInfoLabel, &snapshot.change},
		{cursorInfoLabel, &snapshot.cursor},
	} {
		*derivation.key, err = deriveScopedTokenKeyFrom(handle.key, derivation.label, orgID, projectID, envID)
		if err != nil {
			snapshot.Close()
			return nil, err
		}
	}
	return snapshot, nil
}

func (s *DeliveryTokenSnapshot) ChangeToken(manifest []byte) string { return tag(s.change, manifest) }
func (s *DeliveryTokenSnapshot) DeliveryCursor(tuple []byte) string { return tag(s.cursor, tuple) }
func (s *DeliveryTokenSnapshot) Sign(claims []byte) string          { return tag(s.receipt, claims) }
func (s *DeliveryTokenSnapshot) Verify(claims []byte, signature string) bool {
	return subtle.ConstantTimeCompare([]byte(s.Sign(claims)), []byte(signature)) == 1
}
func (s *DeliveryTokenSnapshot) Close() {
	Zero(s.receipt)
	Zero(s.change)
	Zero(s.cursor)
}
