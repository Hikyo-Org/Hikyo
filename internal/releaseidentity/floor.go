package releaseidentity

import "errors"

// SnapshotFloor is an untrusted persisted claim, not signature authority.
// The verifier authenticates new values; the ledger atomically preserves them.
type SnapshotFloor struct {
	MetadataSequence       int64  `json:"metadata_sequence"`
	MetadataSHA256         Digest `json:"metadata_sha256"`
	HighestReleaseSequence int64  `json:"highest_release_sequence"`
	CatalogSequence        int64  `json:"catalog_sequence"`
	CatalogSHA256          Digest `json:"catalog_sha256"`
}

func (f SnapshotFloor) Validate() error {
	if f.MetadataSequence < 0 || f.CatalogSequence < 0 || f.HighestReleaseSequence < 0 ||
		(f.MetadataSequence == 0) != (f.MetadataSHA256 == "") ||
		(f.CatalogSequence == 0) != (f.CatalogSHA256 == "") ||
		(f.MetadataSequence == 0) != (f.CatalogSequence == 0) ||
		(f.MetadataSequence == 0 && f.HighestReleaseSequence != 0) {
		return errors.New("invalid persisted trust floor")
	}
	if f.MetadataSequence > 0 && (f.MetadataSHA256.Validate() != nil || f.CatalogSHA256.Validate() != nil) {
		return errors.New("invalid persisted trust digest")
	}
	return nil
}

// Advance refuses counter rollback and equivocation. Root/domain equality is a
// separate mandatory installation binding owned by the calling ledger.
func (f SnapshotFloor) Advance(next SnapshotFloor) error {
	if err := f.Validate(); err != nil {
		return err
	}
	if err := next.Validate(); err != nil {
		return err
	}
	if next.MetadataSequence < f.MetadataSequence || next.CatalogSequence < f.CatalogSequence || next.HighestReleaseSequence < f.HighestReleaseSequence {
		return errors.New("trust floor rollback refused")
	}
	if (next.MetadataSequence == f.MetadataSequence && next.MetadataSHA256 != f.MetadataSHA256) || (next.CatalogSequence == f.CatalogSequence && next.CatalogSHA256 != f.CatalogSHA256) {
		return errors.New("conflicting trust bytes at known sequence")
	}
	return nil
}
