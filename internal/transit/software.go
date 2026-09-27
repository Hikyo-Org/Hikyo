package transit

import (
	"context"
	"errors"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
)

// MaterialOwnerTable and MaterialFieldTag anchor a software-custody version's
// sealed material: a project_field envelope bound to the version row, the
// environment and the transit key. The reencrypt walk re-seals exactly this
// AAD, so the two must never drift.
const (
	MaterialOwnerTable = "transit_key_versions"
	MaterialFieldTag   = "material"
)

// MaterialAAD is the project_field AAD of one version's sealed material.
func MaterialAAD(t Target) crypto.ProjectFieldAAD {
	b := t.Binding
	return MaterialAADFor(b.OrgID, b.ProjectID, b.EnvID, b.KeyID, t.VersionRowID)
}

// MaterialAADFor is MaterialAAD from the row coordinates alone, for the
// reencrypt walk, which re-seals rows without knowing their algorithm.
func MaterialAADFor(orgID, projectID, envID, keyID, versionRowID string) crypto.ProjectFieldAAD {
	return crypto.ProjectFieldAAD{
		OrgID: orgID, ProjectID: projectID,
		OwnerTable: MaterialOwnerTable, OwnerRowID: versionRowID, FieldTag: MaterialFieldTag,
		EnvironmentID: envID, KeyID: keyID,
	}
}

// ProjectSealers is the slice of the keyring software custody needs.
// *crypto.Keyring satisfies it.
type ProjectSealers interface {
	ForProject(ctx context.Context, orgID, projectID string) (*crypto.ProjectSealer, error)
}

// Software keeps material sealed under the owning project DEK (transit ADR D3).
// It is always available while the keyring is.
type Software struct {
	Keyring ProjectSealers
}

var _ Custody = (*Software)(nil)

func (s *Software) Kind() CustodyKind { return CustodySoftware }

func (s *Software) Available(context.Context) error {
	if s == nil || s.Keyring == nil {
		return ErrUnavailable
	}
	return nil
}

func (s *Software) sealer(ctx context.Context, t Target) (*crypto.ProjectSealer, error) {
	if err := s.Available(ctx); err != nil {
		return nil, err
	}
	return s.Keyring.ForProject(ctx, t.Binding.OrgID, t.Binding.ProjectID)
}

// Create seals fresh material under the project DEK.
//
// fence:delegated. It returns the DEK version it sealed under
// (Version.SealedDEKVersion); the service fences on exactly that version in the
// transaction that writes the row (service.Transit.fenceSealed), so material
// sealed under a DEK version a concurrent rotate-dek is retiring is refused
// rather than stranded.
func (s *Software) Create(ctx context.Context, t Target) (Version, error) {
	if t.VersionRowID == "" {
		return Version{}, errors.New("transit: software custody requires the version row id")
	}
	sealer, err := s.sealer(ctx, t)
	if err != nil {
		return Version{}, err
	}
	material, err := crypto.NewTransitMaterial()
	if err != nil {
		return Version{}, err
	}
	defer crypto.Zero(material)
	public, err := PublicKeyFor(material, t)
	if err != nil {
		return Version{}, err
	}
	sealed, err := sealer.SealField(MaterialAAD(t), material)
	if err != nil {
		return Version{}, err
	}
	return Version{Sealed: sealed, SealedDEKVersion: sealer.ActiveVersion(), PublicKey: public}, nil
}

// with opens the sealed material for exactly one operation and zeroes it after.
func (s *Software) with(ctx context.Context, t Target, v Version, fn func(*crypto.TransitKey) ([]byte, error)) ([]byte, error) {
	if len(v.Sealed) == 0 {
		return nil, ErrMaterialMissing
	}
	sealer, err := s.sealer(ctx, t)
	if err != nil {
		return nil, err
	}
	material, err := sealer.OpenField(MaterialAAD(t), v.Sealed)
	if err != nil {
		return nil, err
	}
	defer crypto.Zero(material)
	return Operate(material, t, fn)
}

func (s *Software) Encrypt(ctx context.Context, t Target, v Version, plaintext, aad []byte) ([]byte, error) {
	return s.with(ctx, t, v, func(k *crypto.TransitKey) ([]byte, error) { return k.Encrypt(plaintext, aad) })
}

func (s *Software) Decrypt(ctx context.Context, t Target, v Version, record, aad []byte) ([]byte, error) {
	return s.with(ctx, t, v, func(k *crypto.TransitKey) ([]byte, error) { return k.Decrypt(record, aad) })
}

func (s *Software) Sign(ctx context.Context, t Target, v Version, message []byte) ([]byte, error) {
	return s.with(ctx, t, v, func(k *crypto.TransitKey) ([]byte, error) { return k.Sign(message) })
}

func (s *Software) MAC(ctx context.Context, t Target, v Version, message []byte) ([]byte, error) {
	return s.with(ctx, t, v, func(k *crypto.TransitKey) ([]byte, error) { return k.MAC(message) })
}

// Destroy has nothing to erase at a provider: the material exists only in the
// version row, which the caller nulls or deletes in its own transaction.
func (s *Software) Destroy(context.Context, Target, Version) error { return nil }
