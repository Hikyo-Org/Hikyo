// Package upgradecustody owns encrypted, operator-only installation custody.
// Host custody remains root-only. An explicitly enrolled unattended container
// may use the local-owner API, wrapping custody with its existing root key.
// Private material must never enter child argv/env or public configuration.
package upgradecustody

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"sync"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	hikyocrypto "github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/sigstore/sigstore/pkg/signature"
)

const (
	fileName      = "operator.age"
	vaultFormat   = "hikyo.operator-custody/v1"
	maxCiphertext = 16 << 10
	maxPlaintext  = 8 << 10
)

// Vault holds unlocked material in memory until Close. Callers own and must
// clear the copies returned by RootKey. Go and age may retain temporary secret
// strings until garbage collection; Close is best effort, not locked memory.
type Vault struct {
	mu        sync.Mutex
	key       *ecdsa.PrivateKey
	identity  []byte
	root      []byte
	public    []byte
	recipient string
	pin       backupreceipt.PinnedOperator
}

type record struct {
	Format     string `json:"format"`
	Instance   string `json:"instance"`
	Identity   []byte `json:"backup_identity"`
	PrivateKey []byte `json:"attestation_private_key"`
	RootKey    []byte `json:"root_escrow"`
}

func (r *record) clear() {
	clear(r.Identity)
	clear(r.PrivateKey)
	clear(r.RootKey)
}

// Create initializes operator.age without replacing existing custody. The
// parent directory must already exist; directory is created with mode 0700.
// passphrase is borrowed and never persisted. The caller must clear it.
func Create(directory string, passphrase, rootKey []byte, instance string) (*Vault, error) {
	return create(directory, passphrase, rootKey, instance, 0)
}

func create(directory string, passphrase, rootKey []byte, instance string, owner int) (*Vault, error) {
	if len(rootKey) != 32 || len(passphrase) == 0 || len(passphrase) > 1024 {
		return nil, errors.New("operator custody requires a 32-byte root escrow and a nonempty bounded passphrase")
	}
	dir, err := custodyDirectory(directory, true, owner)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	identity, _, err := backup.GenerateIdentity()
	if err != nil {
		return nil, errors.New("generate operator backup identity")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, errors.New("generate operator signing key")
	}
	defer func() { clear(key.D.Bits()); key.D.SetInt64(0) }()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, errors.New("encode operator signing key")
	}
	r := record{Format: vaultFormat, Instance: instance, Identity: []byte(identity), PrivateKey: der, RootKey: bytes.Clone(rootKey)}
	defer r.clear()
	vault, err := decodeRecord(r, instance)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			vault.Close()
		}
	}()
	ciphertext, err := seal(r, passphrase)
	if err != nil {
		return nil, err
	}
	if err := publish(dir, ciphertext, false); err != nil {
		return nil, err
	}
	ok = true
	return vault, nil
}

// seal encrypts one custody record. The container is the same age scrypt
// profile the backup package uses for exports; age's default work factor
// applies.
func seal(r record, passphrase []byte) ([]byte, error) {
	plain, err := json.Marshal(r)
	if err != nil {
		return nil, errors.New("encode operator custody")
	}
	defer clear(plain)
	var ciphertext bytes.Buffer
	w, err := backup.Encrypt(&ciphertext, backup.Options{Passphrase: string(passphrase)})
	if err != nil {
		return nil, errors.New("encrypt operator custody")
	}
	if _, err = w.Write(plain); err != nil {
		return nil, errors.New("encrypt operator custody")
	}
	if err = w.Close(); err != nil {
		return nil, errors.New("finish encrypted operator custody")
	}
	return ciphertext.Bytes(), nil
}

// RootKeySecret derives the custody wrapping secret from the installation's
// root key. Everything the vault protects is already reachable by whoever can
// read that key, so wrapping with it keeps the file encrypted at rest without
// asking a human for a second secret, which lets upgrades run unattended.
func RootKeySecret(rootKey []byte) ([]byte, error) {
	secret, err := hikyocrypto.CustodyWrapKey(rootKey)
	if err != nil {
		return nil, err
	}
	out := make([]byte, hex.EncodedLen(len(secret)))
	hex.Encode(out, secret)
	clear(secret)
	return out, nil
}

// Rewrap re-encrypts existing custody under a new secret, replacing the file
// atomically. The record itself, including the backup identity that decrypts
// earlier upgrade backups, is unchanged. Used once to move a passphrase vault
// to root-key wrapping.
func Rewrap(directory string, current, next []byte, instance string) error {
	return rewrap(directory, current, next, instance, 0)
}

func rewrap(directory string, current, next []byte, instance string, owner int) error {
	if len(next) == 0 || len(next) > 1024 {
		return errors.New("invalid replacement custody secret")
	}
	r, err := unseal(directory, current, instance, owner)
	if err != nil {
		return err
	}
	defer r.clear()
	ciphertext, err := seal(r, next)
	if err != nil {
		return err
	}
	dir, err := custodyDirectory(directory, false, owner)
	if err != nil {
		return err
	}
	defer dir.Close()
	return publish(dir, ciphertext, true)
}

// Open decrypts custody only after checking directory/file ownership and modes.
// instance must come from inspection of the installation, never from an archive.
func Open(directory string, passphrase []byte, instance string) (*Vault, error) {
	return open(directory, passphrase, instance, 0)
}

func open(directory string, passphrase []byte, instance string, owner int) (*Vault, error) {
	r, err := unseal(directory, passphrase, instance, owner)
	if err != nil {
		return nil, err
	}
	defer r.clear()
	return decodeRecord(r, instance)
}

// ErrUnlock reports a custody file that did not open under the given secret.
var ErrUnlock = errors.New("operator custody unlock failed")

func unseal(directory string, passphrase []byte, instance string, owner int) (record, error) {
	if len(passphrase) == 0 || len(passphrase) > 1024 {
		return record{}, errors.New("invalid operator passphrase")
	}
	dir, err := custodyDirectory(directory, false, owner)
	if err != nil {
		return record{}, err
	}
	defer dir.Close()
	ciphertext, err := read(dir, owner)
	if err != nil {
		return record{}, err
	}
	var plaintext boundedBuffer
	defer clear(plaintext.buf)
	if err := backup.ExtractTo(&plaintext, bytes.NewReader(ciphertext), backup.Unlock{Passphrase: string(passphrase)}); err != nil {
		return record{}, ErrUnlock
	}
	var r record
	if definitions.DecodeStrict(plaintext.buf, &r) != nil {
		return record{}, errors.New("invalid encrypted operator custody")
	}
	if r.Format != vaultFormat || r.Instance != instance {
		r.clear()
		return record{}, errors.New("operator custody does not match installation")
	}
	return r, nil
}

func decodeRecord(r record, instance string) (*Vault, error) {
	if r.Format != vaultFormat || r.Instance != instance || len(r.RootKey) != 32 || len(r.Identity) > 256 || len(r.PrivateKey) > 1024 {
		return nil, errors.New("operator custody does not match installation")
	}
	recipient, err := backup.RecipientOf(string(r.Identity))
	if err != nil {
		return nil, errors.New("invalid operator backup identity")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(r.PrivateKey)
	if err != nil {
		return nil, errors.New("invalid operator signing key")
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, errors.New("operator signing key must use P-256")
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		key.D.SetInt64(0)
		return nil, errors.New("invalid operator public key")
	}
	public := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})
	pin, err := backupreceipt.PinOperator(instance, public)
	if err != nil {
		key.D.SetInt64(0)
		return nil, errors.New("invalid operator installation pin")
	}
	return &Vault{key: key, identity: bytes.Clone(r.Identity), root: bytes.Clone(r.RootKey), public: public, recipient: recipient, pin: pin}, nil
}

// PublicKey returns a copy of the public attestation key, safe for the runtime.
func (v *Vault) PublicKey() []byte                 { v.mu.Lock(); defer v.mu.Unlock(); return bytes.Clone(v.public) }
func (v *Vault) Recipient() string                 { v.mu.Lock(); defer v.mu.Unlock(); return v.recipient }
func (v *Vault) Pin() backupreceipt.PinnedOperator { v.mu.Lock(); defer v.mu.Unlock(); return v.pin }
func (v *Vault) RootKey() []byte                   { v.mu.Lock(); defer v.mu.Unlock(); return bytes.Clone(v.root) }

// BackupUnlock is operator-only; never hand this to the server or host adapter.
func (v *Vault) BackupUnlock() backup.Unlock {
	v.mu.Lock()
	defer v.mu.Unlock()
	return backup.Unlock{Identity: string(v.identity)}
}

// SignAttestation signs only a validated, currently usable statement bound to
// this vault's instance and operator key. Drill proof remains the caller's job.
func (v *Vault) SignAttestation(raw []byte, now time.Time) ([]byte, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(raw) > backupreceipt.MaxArtifactBytes {
		return nil, errors.New("operator attestation exceeds size bound")
	}
	raw = bytes.Clone(raw)
	a, err := backupreceipt.ParseAttestation(raw)
	if err != nil || v.key == nil || a.InstanceID != v.pin.InstanceID() || a.OperatorKeyID != v.pin.KeyID() || now.Before(a.IssuedAt) || !now.Before(a.ExpiresAt) {
		return nil, errors.New("operator attestation does not match unlocked custody or validity window")
	}
	signer, err := signature.LoadSigner(v.key, crypto.SHA256)
	if err != nil {
		return nil, errors.New("load operator attestation signer")
	}
	sig, err := signer.SignMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, errors.New("sign operator attestation")
	}
	bundle, err := json.Marshal(releasetrust.LegacyBundle{Base64Signature: base64.StdEncoding.EncodeToString(sig)})
	if err != nil || backupreceipt.CheckOperatorSignature(v.pin, bundle, raw) != nil {
		return nil, errors.New("verify generated operator attestation signature")
	}
	return bundle, nil
}

func (v *Vault) String() string   { return "[operator custody]" }
func (v *Vault) GoString() string { return v.String() }

// Close clears owned secret buffers and disables signing. It is idempotent.
func (v *Vault) Close() {
	v.mu.Lock()
	defer v.mu.Unlock()
	clear(v.identity)
	clear(v.root)
	v.identity, v.root = nil, nil
	if v.key != nil {
		clear(v.key.D.Bits())
		v.key.D.SetInt64(0)
		v.key = nil
	}
}

// boundedBuffer refuses plaintext beyond the custody record bound so a
// hostile container cannot make unlock allocate without limit.
type boundedBuffer struct{ buf []byte }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(b.buf)+len(p) > maxPlaintext {
		return 0, errors.New("operator custody exceeds size bound")
	}
	b.buf = append(b.buf, p...)
	return len(p), nil
}
