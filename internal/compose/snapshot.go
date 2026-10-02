package compose

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/gofrs/flock"
)

// Offline snapshot store (compose-integration ADR § "Offline behaviour",
// ops-spec § 6). Every successful delivering fetch writes a ciphertext snapshot
// to persistent disk; serving from it is opt-in per stack, timestamped, and
// refused past a hard maximum age. The snapshot is a values cache at rest — the
// state directory is what protects it.
//
// The snapshot is SELF-DESCRIBING: after a reboot — the offline-boot case this
// exists for — the server is unreachable, so the box cannot reconstruct the
// full AAD tuple (revision, projection, issuance, expiry). The container
// therefore stores that tuple as a cleartext, AEAD-authenticated header and the
// caller supplies one validated scope-only SnapshotBinding containing what it
// knows offline (identity, org, project, environment, credential fingerprint of
// the presented token, config-only mode, target set).
//
//	container = "HKS1" ‖ uint32-BE(len(header)) ‖ header ‖ sealed-payload
//
// where `header` is SnapshotBinding.CanonicalAAD() and the sealed payload's AAD
// IS those exact legacy-compatible bytes, so tampering the header fails the open.
//
// Two guards beyond the AEAD:
//   - a high-water mark of (issuance, header-digest): an older issuance is
//     refused, and an equal issuance whose header identity differs is refused
//     too, so a plain-file rollback cannot resurrect a superseded generation
//     even when it reuses the server timestamp (§ Expiry, clocks and rollback);
//   - expiry: min(server-asserted expires_at, issued_at + per-stack max_age),
//     both server-asserted and integrity-protected in the header.

const (
	snapshotFile = "snapshot.bin" // legacy single-slot filename, load-only
	hwmFile      = "snapshot.hwm" // legacy single-slot filename, load-only

	// snapshotMagic versions the container framing independently of the crypto
	// envelope's own format byte.
	snapshotMagic = "HKS1"
)

// ErrSnapshotExpired, ErrSnapshotRollback and ErrSnapshotContext are the policy
// refusals, each distinguishable from an AEAD failure (crypto.ErrDecrypt).
var (
	ErrSnapshotExpired  = errors.New("compose: snapshot expired")
	ErrSnapshotRollback = errors.New("compose: snapshot issuance is not newer than the recorded high-water mark")
	ErrSnapshotContext  = errors.New("compose: snapshot context does not match local context")
	errSnapshotFraming  = errors.New("compose: snapshot container is malformed")
)

// SnapshotRow is one delivered key inside a snapshot. KeyID is the immutable
// server key id: it travels INSIDE the sealed payload so the offline path can
// map row→key_id for its per-key reconciliation records without a cleartext
// sidecar (the self-describing snapshot subsumed the old offline.meta.json).
type SnapshotRow struct {
	Name           string `json:"name"`
	KeyID          string `json:"key_id"`
	Classification string `json:"classification"`
	Value          string `json:"value"`
	Receipt        string `json:"snapshot_receipt"`
}

// SnapshotPayload is the plaintext a snapshot seals: the delivered rows and the
// generation stamps that fetch produced.
type SnapshotPayload struct {
	Rows             []SnapshotRow     `json:"rows"`
	GenerationStamps map[string]string `json:"generation_stamps"`
}

// hwm is the persisted high-water mark: the newest issuance seen and the digest
// of that snapshot's header, so an equal-issuance record with a different
// identity is still refused as a rollback.
type hwm struct {
	IssuedAt string `json:"issued_at"`
	Digest   string `json:"digest"`
}

// SaveSnapshot seals payload under keys with the binding's canonical AAD, frames
// the self-describing container, and writes snapshot.bin atomically (0600). It
// advances snapshot.hwm to (issued_at, header-digest) and REFUSES to save an
// issuance older than the current high-water mark (a rollback attempt).
func SaveSnapshot(keys *crypto.LocalKeys, binding crypto.SnapshotBinding, payload SnapshotPayload) error {
	if err := payload.validateReceipts(); err != nil {
		return err
	}
	header, err := binding.CanonicalAAD()
	if err != nil {
		return err
	}
	aad, err := binding.AAD()
	if err != nil {
		return err
	}
	issued, err := time.Parse(time.RFC3339, aad.IssuedAt)
	if err != nil {
		return fmt.Errorf("compose: snapshot issued_at %q is not RFC3339: %w", aad.IssuedAt, err)
	}
	// The container writes the header length as a 4-byte prefix, so a header
	// beyond uint32 range cannot be framed. It never happens (the AAD is a
	// fixed handful of short fields), but the bound is asserted rather than
	// silently truncated.
	if len(header) > math.MaxUint32 {
		return fmt.Errorf("compose: snapshot header is %d bytes, exceeds the framing limit", len(header))
	}
	digest := headerDigest(header)
	snapshotPath, hwmPath, err := snapshotPaths(binding)
	if err != nil {
		return err
	}
	lock, err := lockSnapshots(binding)
	if err != nil {
		return err
	}
	defer lock.Close()

	mark, err := snapshotHWM(keys, binding)
	if err != nil {
		return err
	}
	if mark != nil {
		markTime, err := time.Parse(time.RFC3339, mark.IssuedAt)
		if err != nil {
			return fmt.Errorf("compose: high-water mark %q is not RFC3339: %w", mark.IssuedAt, err)
		}
		if issued.Before(markTime) {
			return fmt.Errorf("%w (issued %s < hwm %s)", ErrSnapshotRollback, issued, markTime)
		}
		if issued.Equal(markTime) && digest != mark.Digest {
			return fmt.Errorf("%w (issued %s equals hwm but header identity differs)", ErrSnapshotRollback, issued)
		}
	}

	plaintext, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("compose: marshal snapshot payload: %w", err)
	}
	sealed, err := keys.SealSnapshot(header, plaintext)
	if err != nil {
		return fmt.Errorf("compose: seal snapshot: %w", err)
	}
	container := frameSnapshot(header, sealed)
	if err := atomicWrite(snapshotPath, container, 0o600); err != nil {
		return fmt.Errorf("compose: write snapshot: %w", err)
	}
	// Advance the HWM only after the snapshot is durable.
	nextHWM, err := json.Marshal(hwm{IssuedAt: aad.IssuedAt, Digest: digest})
	if err != nil {
		return fmt.Errorf("compose: marshal high-water mark: %w", err)
	}
	if err := atomicWrite(hwmPath, nextHWM, 0o600); err != nil {
		return fmt.Errorf("compose: write high-water mark: %w", err)
	}
	return nil
}

// LoadSnapshot validates the expected binding before reading snapshot.bin,
// parses its self-describing header into a delivery-complete binding, checks the
// offline-known scope, enforces rollback and expiry, then decrypts. maxAge is the
// per-stack downward override; the effective expiry is the earlier bound.
func LoadSnapshot(keys *crypto.LocalKeys, expect crypto.SnapshotBinding, now time.Time, maxAge time.Duration) (SnapshotPayload, crypto.SnapshotBinding, error) {
	var zeroP SnapshotPayload
	var zeroB crypto.SnapshotBinding
	if err := expect.ValidateScope(); err != nil {
		return zeroP, zeroB, err
	}
	stateDir, err := expect.StorageDir()
	if err != nil {
		return zeroP, zeroB, err
	}
	lock, err := lockSnapshots(expect)
	if err != nil {
		return zeroP, zeroB, err
	}
	defer lock.Close()

	snapshotPath, _, err := snapshotPaths(expect)
	if err != nil {
		return zeroP, zeroB, err
	}
	record, err := os.ReadFile(snapshotPath)
	if errors.Is(err, os.ErrNotExist) {
		// Older hashed slots may include the former state-directory path. Find
		// them by their authenticated scope, never by their cleartext header alone.
		compatible, lookupErr := compatibleSnapshots(keys, expect)
		if lookupErr != nil {
			return zeroP, zeroB, lookupErr
		}
		var newest time.Time
		for _, candidate := range compatible {
			if candidate.issued.After(newest) {
				snapshotPath = candidate.path
				record = candidate.record
				err = nil
				newest = candidate.issued
			}
		}
	}
	if errors.Is(err, os.ErrNotExist) {
		// One-release compatibility for the former shared slot. ContextMatches
		// below still authenticates and refuses a snapshot from another scope.
		snapshotPath = filepath.Join(stateDir, snapshotFile)
		record, err = os.ReadFile(snapshotPath)
	}
	if err != nil {
		return zeroP, zeroB, fmt.Errorf("compose: read snapshot: %w", err)
	}
	header, sealed, err := unframeSnapshot(record)
	if err != nil {
		return zeroP, zeroB, err
	}
	binding, err := crypto.ParseSnapshotBinding(stateDir, header)
	if err != nil {
		return zeroP, zeroB, err
	}
	// Refuse a transplanted snapshot by name before any crypto work.
	if err := binding.ContextMatches(expect); err != nil {
		return zeroP, zeroB, fmt.Errorf("%w: %v", ErrSnapshotContext, err)
	}
	aad, err := binding.AAD()
	if err != nil {
		return zeroP, zeroB, err
	}

	issued, err := time.Parse(time.RFC3339, aad.IssuedAt)
	if err != nil {
		return zeroP, binding, fmt.Errorf("compose: snapshot issued_at %q is not RFC3339: %w", aad.IssuedAt, err)
	}
	expires, err := time.Parse(time.RFC3339, aad.ExpiresAt)
	if err != nil {
		return zeroP, binding, fmt.Errorf("compose: snapshot expires_at %q is not RFC3339: %w", aad.ExpiresAt, err)
	}

	mark, err := snapshotHWM(keys, expect)
	if err != nil {
		return zeroP, binding, err
	}
	if mark != nil {
		markTime, err := time.Parse(time.RFC3339, mark.IssuedAt)
		if err != nil {
			return zeroP, binding, fmt.Errorf("compose: high-water mark %q is not RFC3339: %w", mark.IssuedAt, err)
		}
		if issued.Before(markTime) {
			return zeroP, binding, fmt.Errorf("%w (issued %s < hwm %s)", ErrSnapshotRollback, issued, markTime)
		}
		if issued.Equal(markTime) && headerDigest(header) != mark.Digest {
			return zeroP, binding, fmt.Errorf("%w (issued %s equals hwm but header identity differs)", ErrSnapshotRollback, issued)
		}
	}

	// Effective expiry is the earlier of the server bound and the per-stack cap.
	effective := expires
	if capped := issued.Add(maxAge); capped.Before(effective) {
		effective = capped
	}
	if now.After(effective) {
		return zeroP, binding, fmt.Errorf("%w (expired at %s, now %s)", ErrSnapshotExpired, effective, now)
	}

	plaintext, err := keys.OpenSnapshot(header, sealed)
	if err != nil {
		return zeroP, binding, err // crypto.ErrDecrypt on any tamper/AEAD failure
	}
	var payload SnapshotPayload
	dec := json.NewDecoder(bytes.NewReader(plaintext))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&payload); err != nil {
		return zeroP, binding, fmt.Errorf("compose: parse snapshot payload: %w", err)
	}
	if err := payload.validateReceipts(); err != nil {
		return zeroP, binding, err
	}
	return payload, binding, nil
}

func (p SnapshotPayload) validateReceipts() error {
	for _, row := range p.Rows {
		if strings.TrimSpace(row.Receipt) == "" {
			return errors.New("compose: snapshot lacks server-authenticated delivery receipts; refresh online to regenerate the snapshot before offline use")
		}
	}
	return nil
}

// Serialize the complete snapshot/watermark transaction across processes.
// The directory-wide lock also covers compatibility slots after relocation.
func lockSnapshots(binding crypto.SnapshotBinding) (*flock.Flock, error) {
	dir, err := binding.StorageDir()
	if err != nil {
		return nil, err
	}
	lock := flock.New(filepath.Join(dir, "snapshots.lock"), flock.SetPermissions(0o600))
	if err := lock.Lock(); err != nil {
		return nil, fmt.Errorf("compose: lock snapshot state: %w", err)
	}
	return lock, nil
}

func headerDigest(header []byte) string {
	sum := sha256.Sum256(header)
	return hex.EncodeToString(sum[:])
}

// frameSnapshot builds the container: magic ‖ uint32-BE(len(header)) ‖ header ‖ sealed.
// The caller (SaveSnapshot) guarantees len(header) fits uint32.
func frameSnapshot(header, sealed []byte) []byte {
	lenPrefix := binary.BigEndian.AppendUint32(nil, uint32(len(header)))
	return slices.Concat([]byte(snapshotMagic), lenPrefix, header, sealed)
}

// unframeSnapshot validates the magic and header length prefix and splits the
// record into (header, sealed).
func unframeSnapshot(record []byte) (header, sealed []byte, err error) {
	if len(record) < len(snapshotMagic)+4 {
		return nil, nil, errSnapshotFraming
	}
	if string(record[:len(snapshotMagic)]) != snapshotMagic {
		return nil, nil, fmt.Errorf("%w: bad magic", errSnapshotFraming)
	}
	pos := len(snapshotMagic)
	n := int(binary.BigEndian.Uint32(record[pos:]))
	pos += 4
	if n < 0 || pos+n > len(record) {
		return nil, nil, fmt.Errorf("%w: header length out of range", errSnapshotFraming)
	}
	return record[pos : pos+n], record[pos+n:], nil
}

// snapshotPaths locates the directory-independent scope's cache and watermark.
func snapshotPaths(binding crypto.SnapshotBinding) (string, string, error) {
	stateDir, err := binding.StorageDir()
	if err != nil {
		return "", "", err
	}
	key, err := binding.StorageKey()
	if err != nil {
		return "", "", err
	}
	return filepath.Join(stateDir, "snapshot-"+key+".bin"), filepath.Join(stateDir, "snapshot-"+key+".hwm"), nil
}

type compatibleSnapshot struct {
	path   string
	record []byte
	issued time.Time
}

// compatibleSnapshots admits former cache slots only after their complete
// scope matches and AEAD authenticates that header. The old pathname is not
// available after relocation, so enumerating local slots is intentional.
func compatibleSnapshots(keys *crypto.LocalKeys, expect crypto.SnapshotBinding) ([]compatibleSnapshot, error) {
	stateDir, err := expect.StorageDir()
	if err != nil {
		return nil, err
	}
	current, _, err := snapshotPaths(expect)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(stateDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("compose: discover prior snapshot slots: %w", err)
	}
	var out []compatibleSnapshot
	for _, entry := range entries {
		name := entry.Name()
		if name != snapshotFile {
			if !strings.HasPrefix(name, "snapshot-") || !strings.HasSuffix(name, ".bin") {
				continue
			}
			key := strings.TrimSuffix(strings.TrimPrefix(name, "snapshot-"), ".bin")
			if decoded, err := hex.DecodeString(key); err != nil || len(decoded) != sha256.Size {
				continue
			}
		}
		path := filepath.Join(stateDir, name)
		if path == current || !entry.Type().IsRegular() {
			continue
		}
		record, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("compose: read prior snapshot slot: %w", err)
		}
		header, sealed, err := unframeSnapshot(record)
		if err != nil {
			continue
		}
		binding, err := crypto.ParseSnapshotBinding(stateDir, header)
		if err != nil || binding.ContextMatches(expect) != nil {
			continue
		}
		plaintext, err := keys.OpenSnapshot(header, sealed)
		if err != nil {
			continue
		}
		clear(plaintext) // discovery authenticates metadata, never retains values
		aad, err := binding.AAD()
		if err != nil {
			return nil, err
		}
		issued, err := time.Parse(time.RFC3339, aad.IssuedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, compatibleSnapshot{path: path, record: record, issued: issued})
	}
	return out, nil
}

// snapshotHWM keeps the newest mark across the stable slot and all authenticated
// compatibility slots. Keeping old files is safe: neither loading nor saving a
// new-format slot can ignore the paired watermark of a moved old-format slot.
func snapshotHWM(keys *crypto.LocalKeys, binding crypto.SnapshotBinding) (*hwm, error) {
	_, currentMark, err := snapshotPaths(binding)
	if err != nil {
		return nil, err
	}
	stateDir, err := binding.StorageDir()
	if err != nil {
		return nil, err
	}
	legacyKey, err := binding.LegacyStorageKey()
	if err != nil {
		return nil, err
	}
	paths := []string{currentMark, filepath.Join(stateDir, "snapshot-"+legacyKey+".hwm")}
	compatible, err := compatibleSnapshots(keys, binding)
	if err != nil {
		return nil, err
	}
	for _, candidate := range compatible {
		if filepath.Base(candidate.path) == snapshotFile {
			paths = append(paths, filepath.Join(stateDir, hwmFile))
		} else {
			paths = append(paths, strings.TrimSuffix(candidate.path, ".bin")+".hwm")
		}
	}
	var newest *hwm
	var newestTime time.Time
	var marks []*hwm
	for _, path := range paths {
		mark, err := readHWM(path)
		if err != nil {
			return nil, err
		}
		if mark == nil {
			continue
		}
		issued, err := time.Parse(time.RFC3339, mark.IssuedAt)
		if err != nil {
			return nil, fmt.Errorf("compose: high-water mark %q is not RFC3339: %w", mark.IssuedAt, err)
		}
		marks = append(marks, mark)
		if newest == nil || issued.After(newestTime) {
			newest, newestTime = mark, issued
		}
	}
	for _, mark := range marks {
		issued, _ := time.Parse(time.RFC3339, mark.IssuedAt) // validated above
		if issued.Equal(newestTime) && mark.Digest != newest.Digest {
			return nil, fmt.Errorf("%w (equal high-water issuances have different header identities)", ErrSnapshotRollback)
		}
	}
	return newest, nil
}

// readHWM returns the recorded high-water mark, or nil if none.
func readHWM(name string) (*hwm, error) {
	b, err := os.ReadFile(name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("compose: read high-water mark: %w", err)
	}
	var m hwm
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("compose: parse high-water mark: %w", err)
	}
	return &m, nil
}
