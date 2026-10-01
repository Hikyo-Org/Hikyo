package service

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// Receipts attest delivery, not the client's later assertion that it served the
// value offline. They contain no plaintext or publicly invertible value hash.
// Revision and the keyed projection token identify the server-selected values.
type offlineReceiptClaims struct {
	Principal      string    `json:"principal"`
	Credential     string    `json:"credential"`
	KeyID          string    `json:"key_id"`
	Name           string    `json:"name"`
	Classification string    `json:"classification"`
	Revision       int64     `json:"revision"`
	ChangeToken    string    `json:"change_token"`
	IssuedAt       time.Time `json:"issued_at"`
	ExpiresAt      time.Time `json:"expires_at"`
}

func (s *Delivery) issueOfflineReceipt(signer *crypto.DeliveryTokenSnapshot, principal domain.PrincipalID, out FetchResult, key DeliveredKey) (string, error) {
	claims, err := json.Marshal(offlineReceiptClaims{
		Principal: string(principal), Credential: out.CredentialID,
		KeyID: key.KeyID, Name: key.Name, Classification: key.Classification,
		Revision: out.Revision, ChangeToken: out.ChangeToken,
		IssuedAt:  out.IssuedAt.UTC(),
		ExpiresAt: out.SnapshotExpiresAt.UTC(),
	})
	if err != nil {
		return "", err
	}
	signature := signer.Sign(claims)
	return "sr1:" + base64.RawURLEncoding.EncodeToString(claims) + "." + signature, nil
}

func (s *Delivery) verifyOfflineReceipt(signer *crypto.DeliveryTokenSnapshot, principal domain.PrincipalID, record OfflineRecord, now time.Time) (offlineReceiptClaims, error) {
	refuse := func() (offlineReceiptClaims, error) {
		return offlineReceiptClaims{}, invalidDetail("offline snapshot receipt is missing, invalid, or does not authorize this record; fetch online to refresh the snapshot")
	}
	if s.Keyring == nil || len(record.SnapshotReceipt) > 4096 || !strings.HasPrefix(record.SnapshotReceipt, "sr1:") {
		return refuse()
	}
	payload, signature, ok := strings.Cut(strings.TrimPrefix(record.SnapshotReceipt, "sr1:"), ".")
	if !ok {
		return refuse()
	}
	encoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return refuse()
	}
	if !signer.Verify(encoded, signature) {
		return refuse()
	}
	var claims offlineReceiptClaims
	if err := json.Unmarshal(encoded, &claims); err != nil {
		return refuse()
	}
	if claims.Principal != string(principal) || claims.Credential != record.CredentialID ||
		claims.KeyID != record.KeyID || claims.Name != record.KeyName || claims.Classification != record.Classification ||
		!claims.IssuedAt.Equal(record.ServedFrom) || claims.IssuedAt.IsZero() || !claims.ExpiresAt.After(claims.IssuedAt) ||
		record.OccurredAt.Before(claims.IssuedAt) || record.OccurredAt.After(claims.ExpiresAt) || record.OccurredAt.After(now) {
		return refuse()
	}
	return claims, nil
}
