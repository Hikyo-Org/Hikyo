package buildcompat

import (
	"encoding/base64"
	"errors"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
)

// Private release-linker values. Runtime configuration and bundle contents
// cannot replace this build's production trust anchor.
var encodedTrustRoot string
var encodedRecoveryPublicKey string

// ProductionTrust returns defensive decoded copies of the build's pinned root
// and recovery key. Custom builds may explicitly stamp their own trust at
// build time. Unstamped binaries refuse production admission.
func ProductionTrust() (releasetrust.PinnedTrust, error) {
	decode := func(encoded string) ([]byte, error) {
		if len(encoded) == 0 || len(encoded) > base64.StdEncoding.EncodedLen(releasetrust.MaxDocumentBytes) {
			return nil, errors.New("binary has no bounded production trust stamp")
		}
		raw, err := base64.StdEncoding.Strict().DecodeString(encoded)
		if err != nil || len(raw) == 0 || len(raw) > releasetrust.MaxDocumentBytes || base64.StdEncoding.EncodeToString(raw) != encoded {
			return nil, errors.New("invalid production trust stamp encoding")
		}
		return raw, nil
	}
	rootRaw, err := decode(encodedTrustRoot)
	if err != nil {
		return releasetrust.PinnedTrust{}, err
	}
	key, err := decode(encodedRecoveryPublicKey)
	if err != nil {
		return releasetrust.PinnedTrust{}, err
	}
	var root releasetrust.Root
	if err := definitions.DecodeStrict(rootRaw, &root); err != nil {
		return releasetrust.PinnedTrust{}, errors.New("invalid closed production trust root")
	}
	if err := releasetrust.ValidateRoot(root, key); err != nil {
		return releasetrust.PinnedTrust{}, err
	}
	if _, err := releasetrust.OperatorKeyID(key); err != nil {
		return releasetrust.PinnedTrust{}, err
	}
	return releasetrust.PinnedTrust{Root: rootRaw, RecoveryPublicKey: key}, nil
}
