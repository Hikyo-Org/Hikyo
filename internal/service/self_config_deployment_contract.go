package service

import (
	"context"
	"errors"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/configrollout"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
)

// ErrDeploymentPreparationExpired requires another read-only preparation and a
// fresh exact-MFA decision. Uncommitted private source proofs are intentionally
// unavailable after restart; already committed signed commands remain replayable.
var ErrDeploymentPreparationExpired = errors.New("deployment preparation expired; prepare again")

// ErrDeploymentSourcesPending means the current process has not proved the
// requested installed selectors and must not acknowledge their generation.
var ErrDeploymentSourcesPending = errors.New("deployment sources are not yet installed")

// DeploymentIdentity is metadata from operator-installed custody and the Pod's
// downward selection projection. TemplateStamp must match the durable prepared
// response before the coordinator acknowledges a deployed application generation.
type DeploymentIdentity struct {
	EnrollmentID    string
	OwnerInstanceID string
	Incarnation     string
	DeploymentUID   string
	TemplateStamp   string
}

// BootstrapDeployment owns installed source proof and signed command transport.
// Service owns all durable jobs, sequence allocation, authorization and target
// changes. None of these methods writes Hikyo service/store state. In particular,
// DecisionCommand reproves/signs into private memory outside the final write
// transaction, avoiding another source-pool acquisition while it holds the only
// connection. The service then rechecks the bound decision, consumes exact MFA
// and atomically commits target plus signed command. It neither sends nor exposes
// the signature before commit; a worker sends only committed bytes.
type BootstrapDeployment interface {
	Identity() DeploymentIdentity
	SeedSources(context.Context) (config.ManagedBootstrapSources, error)
	PrepareCommand(context.Context, configrollout.Intent, *runtimeconfig.Bundle, uint64) (configrollout.SignedCommand, error)
	// RootPreparation returns detached encrypted material only. The final exact-
	// MFA transaction must separately authorize and persist the root wrapper.
	RootPreparation(context.Context, configrollout.SignedCommand) (*crypto.WrappedKey, error)
	DecisionCommand(context.Context, configrollout.SignedCommand, configrollout.Action, uint64, string, *configrollout.ApplicationAcknowledgement) (configrollout.SignedCommand, error)
	// RenewCommand changes transport sequence/timestamps only. Its input must
	// come from a committed rollout row; preparation authority cannot be renewed.
	RenewCommand(context.Context, configrollout.SignedCommand, uint64) (configrollout.SignedCommand, error)
	Send(context.Context, configrollout.SignedCommand) error
	Response(context.Context, configrollout.SignedCommand) (configrollout.Response, error)
	VerifyInstalled(context.Context, *runtimeconfig.Bundle) error
}
