package upgrade

import "github.com/Hikyo-Org/hikyo/internal/crypto"

// CandidateConfiguration contains only the owner-bound desired configuration.
// It exposes neither an arbitrary query nor a caller-selected project/snapshot.
type CandidateConfiguration struct {
	OrgID, ProjectID string
	SchemaVersion    int
	Catalogue        []CandidateConfigurationKey
	Fields           []crypto.ExistingProjectField
}

type CandidateConfigurationKey struct {
	Name, Classification, Declaration, RequiredMode, ForbiddenMode, GroupID, FolderPath string
}
