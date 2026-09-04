// Package operation carries the transport-independent authorization contract
// attached by every network adapter before it invokes a service operation.
package operation

import (
	"context"
	"errors"
	"slices"
)

const (
	ArtifactNone               = "none"
	ArtifactHumanSession       = "human-session"
	ArtifactMachineCredential  = "machine-credential"
	ArtifactSCIMCredential     = "scim-credential"
	ArtifactInstanceCredential = "instance-credential"
	ArtifactLocal              = "local"
)

// Contract is one immutable network-operation admission row. It is built from
// trusted compiled registry data, never request input.
type Contract struct {
	ID                     string
	AuthorizationOperation string
	formula                []string
	artifacts              []string
}

// NewContract constructs a complete immutable operation contract.
func NewContract(id, authorizationOperation string, formula, artifacts []string) (Contract, error) {
	switch {
	case id == "":
		return Contract{}, errors.New("operation: contract id is required")
	case authorizationOperation == "":
		return Contract{}, errors.New("operation: authorization operation is required")
	case len(formula) == 0:
		return Contract{}, errors.New("operation: authorization formula is required")
	case len(artifacts) == 0:
		return Contract{}, errors.New("operation: artifact allowlist is required")
	}
	return Contract{
		ID:                     id,
		AuthorizationOperation: authorizationOperation,
		formula:                slices.Clone(formula),
		artifacts:              slices.Clone(artifacts),
	}, nil
}

// NewArtifactContract constructs an immutable admission row for a network
// operation that has an artifact policy but no domain authorization formula.
func NewArtifactContract(id string, artifacts []string) (Contract, error) {
	if id == "" {
		return Contract{}, errors.New("operation: contract id is required")
	}
	if len(artifacts) == 0 {
		return Contract{}, errors.New("operation: artifact allowlist is required")
	}
	return Contract{ID: id, artifacts: slices.Clone(artifacts)}, nil
}

// Formula returns a copy of the authorization formula.
func (c Contract) Formula() []string { return slices.Clone(c.formula) }

// Artifacts returns a copy of the artifact allowlist.
func (c Contract) Artifacts() []string { return slices.Clone(c.artifacts) }

// AdmitsArtifact reports whether class is explicitly admitted.
func (c Contract) AdmitsArtifact(class string) bool {
	return slices.Contains(c.artifacts, class)
}

type contextKey struct{}
type networkContextKey struct{}

// WithNetwork marks work entered through a network adapter, including public
// metadata operations that have no authorization contract.
func WithNetwork(ctx context.Context) context.Context {
	return context.WithValue(ctx, networkContextKey{}, true)
}

// IsNetwork reports whether a trusted adapter attached network provenance.
func IsNetwork(ctx context.Context) bool {
	network, _ := ctx.Value(networkContextKey{}).(bool)
	return network
}

// WithContract attaches a trusted operation contract to a network request.
func WithContract(ctx context.Context, contract Contract) context.Context {
	if contract.ID == "" {
		panic("operation: cannot attach an empty contract")
	}
	return context.WithValue(WithNetwork(ctx), contextKey{}, contract)
}

// FromContext returns the network operation contract, when one was attached.
func FromContext(ctx context.Context) (Contract, bool) {
	contract, ok := ctx.Value(contextKey{}).(Contract)
	return contract, ok && contract.ID != ""
}
