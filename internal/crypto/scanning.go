package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
)

// The scanning-fingerprint key (secret-scanning ADR §4, a declared
// encryption-model amendment). Dismissal rows must recognise a re-saved value
// without storing it, and a bare content hash in a stolen database is an
// offline-verifiable oracle for guessable values. So the fingerprint is a
// KEYED digest under a dedicated tier-3 key used for nothing else, computed
// inside this package — scanning code never touches a hash or HMAC primitive
// itself (the import-boundary test confines crypto/hmac and crypto/sha256 to
// this package, and SS4 additionally bans any hash primitive from
// internal/scanning).

// scanningFingerprintLabel domain-separates the fingerprint HMAC from every
// other keyed value the module computes. New labels use the `hikyo/` prefix;
// the legacy `wenv/` names are frozen protocol data and never reused.
const scanningFingerprintLabel = "hikyo/scanning-fingerprint/v1"

// ScanningFingerprint is the keyed value a dismissal row stores in place of the
// offending value: HMAC-SHA256 over the domain-separation label, the scope
// binding (org, project, environment, key identity) and the canonical stored
// value bytes, each length-prefixed with the same injective encoding the AADs
// use.
//
//	fingerprint = HMAC-SHA256(scanningKey,
//	    LP(label) ‖ LP(org) ‖ LP(project) ‖ LP(env) ‖ LP(key) ‖ LP(value))
//
// The length-prefixing is load-bearing exactly as it is for the change token:
// raw concatenation would let ("a","bc") and ("ab","c") — or a value whose
// leading bytes shift a scope boundary — collide, resurrecting a cross-scope
// equality oracle. Deterministic for identical inputs (a re-saved value
// re-fingerprints to the same bytes, so a sticky dismissal recognises it) and
// distinct across any scope change, including a different key identity for the
// same value. `value` is the canonical stored bytes (post-trim — exactly what
// the value write persists), passed in by the caller; this package neither
// trims nor interprets it.
func (k *Keyring) ScanningFingerprint(orgID, projectID, envID, keyID string, value []byte) []byte {
	msg := appendLP(nil, []byte(scanningFingerprintLabel))
	msg = appendLP(msg, []byte(orgID))
	msg = appendLP(msg, []byte(projectID))
	msg = appendLP(msg, []byte(envID))
	msg = appendLP(msg, []byte(keyID))
	msg = appendLP(msg, value)
	mac := hmac.New(sha256.New, k.scanningKey())
	mac.Write(msg)
	return mac.Sum(nil)
}

// scanningKey atomically snapshots the live scanning-fingerprint key, so a
// fingerprint racing `rotate-scanning-key` sees exactly one immutable handle.
// Callers fingerprint per use and never retain the key.
func (k *Keyring) scanningKey() []byte {
	return k.scanning.get().key
}

// PrepareScanningKeyRotation mints the next scanning-fingerprint key and
// returns its wrapped row plus mutually exclusive adopt and abort functions,
// mirroring PrepareTokenKeyRotation: the material never leaves this package,
// the caller defers abort, persists the row inside its own authorized
// transaction, and adopt runs only AFTER that transaction commits.
//
// `rotate-scanning-key` is OUTRIGHT REPLACEMENT (secret-scanning ADR §4): there
// is no version keyring and no reencrypt walk, because a fingerprint is a keyed
// digest, not a ciphertext to preserve — old fingerprints MUST die, that is the
// operation's purpose, and every dismissal row is dropped in the same
// transaction (re-fire, the safe direction). The row still carries a
// monotonically increasing version for the same reason the token key does: the
// version is bound into the wrapping AAD and the store retires the predecessor
// by a compare-and-swap on it, so two concurrent rotations resolve to one
// winner rather than a torn handle.
//
// The superseded key is NOT zeroed: a concurrent fingerprint may still be
// holding the buffer, and computing under a zeroed key would be a silent
// break rather than a loud failure — the same reason the token key and the DEK
// cache do not zero superseded material.
func (k *Keyring) PrepareScanningKeyRotation() (WrappedKey, func(), func(), error) {
	return k.prepareDerivationKeyRotation(PurposeScanning, &k.scanning)
}
