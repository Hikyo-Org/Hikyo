// Package upgradecustody owns encrypted, operator-only installation custody.
// Only the interactive operator process may open a Vault. Its private material
// must never enter server configuration, deployment adapters, or child argv/env.
package upgradecustody

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"sync"
	"time"

	"filippo.io/age"
	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
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
	workFactor    = 18 // Maintained age default; cap hostile decrypt requests here too.
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
	identity, err := age.GenerateX25519Identity()
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
	r := record{Format: vaultFormat, Instance: instance, Identity: []byte(identity.String()), PrivateKey: der, RootKey: bytes.Clone(rootKey)}
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
	plain, err := json.Marshal(r)
	if err != nil {
		return nil, errors.New("encode operator custody")
	}
	defer clear(plain)
	recipient, err := age.NewScryptRecipient(string(passphrase))
	if err != nil {
		return nil, errors.New("invalid operator passphrase")
	}
	recipient.SetWorkFactor(workFactor)
	var ciphertext bytes.Buffer
	w, err := age.Encrypt(&ciphertext, recipient)
	if err != nil {
		return nil, errors.New("encrypt operator custody")
	}
	if _, err = w.Write(plain); err != nil {
		return nil, errors.New("encrypt operator custody")
	}
	if err = w.Close(); err != nil {
		return nil, errors.New("finish encrypted operator custody")
	}
	if err := publish(dir, ciphertext.Bytes()); err != nil {
		return nil, err
	}
	ok = true
	return vault, nil
}

// Open decrypts custody only after checking directory/file ownership and modes.
// instance must come from inspection of the installation, never from an archive.
func Open(directory string, passphrase []byte, instance string) (*Vault, error) {
	return open(directory, passphrase, instance, 0)
}

func open(directory string, passphrase []byte, instance string, owner int) (*Vault, error) {
	if len(passphrase) == 0 || len(passphrase) > 1024 {
		return nil, errors.New("invalid operator passphrase")
	}
	dir, err := custodyDirectory(directory, false, owner)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	ciphertext, err := read(dir, owner)
	if err != nil {
		return nil, err
	}
	identity, err := age.NewScryptIdentity(string(passphrase))
	if err != nil {
		return nil, errors.New("invalid operator passphrase")
	}
	identity.SetMaxWorkFactor(workFactor)
	reader, err := age.Decrypt(bytes.NewReader(ciphertext), identity)
	if err != nil {
		return nil, errors.New("operator custody unlock failed")
	}
	plain, err := io.ReadAll(io.LimitReader(reader, maxPlaintext+1))
	defer clear(plain)
	if err != nil || len(plain) > maxPlaintext {
		return nil, errors.New("operator custody unlock failed")
	}
	var r record
	defer r.clear()
	if definitions.DecodeStrict(plain, &r) != nil {
		return nil, errors.New("invalid encrypted operator custody")
	}
	return decodeRecord(r, instance)
}

func decodeRecord(r record, instance string) (*Vault, error) {
	if r.Format != vaultFormat || r.Instance != instance || len(r.RootKey) != 32 || len(r.Identity) > 256 || len(r.PrivateKey) > 1024 {
		return nil, errors.New("operator custody does not match installation")
	}
	identity, err := age.ParseX25519Identity(string(r.Identity))
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
	return &Vault{key: key, identity: bytes.Clone(r.Identity), root: bytes.Clone(r.RootKey), public: public, recipient: identity.Recipient().String(), pin: pin}, nil
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
