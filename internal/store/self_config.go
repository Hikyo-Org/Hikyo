package store

import (
	"context"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
)

// SelfConfigPreparationTTL bounds preparation and the administrator review window.
const SelfConfigPreparationTTL = 5 * time.Minute

// SelfConfigBinding owns one logical instance's runtime configuration. Values
// live only in ordinary project-encrypted snapshot entries.
type SelfConfigBinding struct {
	AdoptionKey, AdoptedBy string
	// SeedFingerprint is a keyed, transient adoption attestation; not a second config store.
	SeedFingerprint string
	SeedNodes       []SelfConfigSeedReference
	// HostSeedDiscovery preserves closed-host discovery semantics until the final membership-locked check; never persisted.
	HostSeedDiscovery                                  bool
	OwnerInstanceID, OrgID, ProjectID, EnvironmentID   string
	SchemaVersion, Generation, DesiredRevision         int64
	DesiredSnapshotID, PreviousSnapshotID, Incarnation string
	Suspended                                          bool
	CreatedAt, UpdatedAt                               time.Time
}

// SelfConfigSeedReference binds the exact node inputs reviewed for adoption.
// References are transient and are checked under the membership lock.
type SelfConfigSeedReference struct {
	NodeID, Fingerprint string
}

// SelfConfigSeedInput exists only before adoption. Ciphertext is instance-DEK
// sealed with the node ID as AAD; plaintext is never stored on this surface.
type SelfConfigSeedInput struct {
	NodeID, OwnerInstanceID, Incarnation, Fingerprint, OwnerFingerprint string
	Ciphertext                                                          []byte
	DEKVersion, RowVersion, SchemaVersion                               int64
	UpdatedAt                                                           time.Time
}

type SelfConfigJob struct {
	ConfirmRestoredCredentials bool
	// LocalNodeID is used only when no HA registry exists; it is not persisted as request identity.
	LocalNodeID                                             string
	ID, IdempotencyKey, PrincipalID, SnapshotID             string
	Revision, SchemaVersion, ExpectedGeneration, Generation int64
	Status, ErrorCode                                       string
	CreatedAt, UpdatedAt                                    time.Time
}

type SelfConfigNode struct {
	NodeID, JobID                    string
	SchemaVersion                    int64
	Prepared                         bool
	ActiveGeneration, ActiveRevision int64
	Incarnation, ErrorCode           string
	UpdatedAt                        time.Time
}

// SelfConfigRollout holds only source-alias commands and secret-free receipts.
// Project values, database locators and key bytes are never valid contents.
type SelfConfigRollout struct {
	JobID, EnrollmentID, Incarnation, PlanDigest string
	CommandJSON, ResponseJSON, ExternalPhase     string
	Sequence, RowVersion                         int64
}

type SelfConfigReader interface {
	PreviousRevision(context.Context, authz.Proof) (int64, error)
	Rollout(context.Context, authz.Proof, string) (SelfConfigRollout, error)
	SeedInputs(context.Context, authz.Proof, string, time.Time) ([]SelfConfigSeedInput, error)
	HostSeedInputs(context.Context, authz.Proof, time.Time) ([]SelfConfigSeedInput, error)
	Binding(context.Context, authz.Proof) (SelfConfigBinding, error)
	Jobs(context.Context, authz.Proof) ([]SelfConfigJob, error)
	Job(context.Context, authz.Proof, string) (SelfConfigJob, error)
	JobByIdempotencyKey(context.Context, authz.Proof, string) (SelfConfigJob, error)
	Nodes(context.Context, authz.Proof) ([]SelfConfigNode, error)
	Retained(context.Context, authz.Proof) ([]string, error)
}

type SelfConfigRepo interface {
	NextRolloutSequence(context.Context, authz.Proof, string) (int64, error)
	PutRollout(context.Context, authz.Proof, SelfConfigRollout) error
	PutSeedInput(context.Context, authz.Proof, SelfConfigSeedInput) error
	RecoverTarget(context.Context, authz.Proof, int64, int64, string, time.Time) (SelfConfigBinding, error)
	SelfConfigReader
	CreateBinding(context.Context, authz.Proof, SelfConfigBinding) error
	BeginJob(context.Context, authz.Proof, SelfConfigJob) (SelfConfigJob, error)
	CommitJob(context.Context, authz.Proof, string, time.Time) (SelfConfigBinding, error)
	FinishJob(context.Context, authz.Proof, string, string, string, time.Time) error
	PutNode(context.Context, authz.Proof, SelfConfigNode) error
	FenceRestored(context.Context, authz.Proof, string, time.Time) error
}
