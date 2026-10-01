package authz

import (
	"context"
	"errors"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
)

// Machine identities at the chokepoint (#61, machine-identities ADR).
//
// The ADR's propagation to the architecture ticket is literal: "MUST resolve
// machine credentials at the same chokepoint as authorize(), in-transaction
// and uncached". So this file adds a resolution leg to the SAME Authenticate
// the human paths already call — not a parallel middleware, not a cached
// principal. Every operation that authorizes a session therefore authorizes a
// machine credential by the same code, and revocation bites at the next
// request because the predicate is read in that request's own transaction.
//
// What it deliberately does NOT share is storage: a machine credential is its
// own artifact type with its own table, lifetime and revocation surface
// (#16's propagation). A machine principal has no session row, no cookie and
// no assurance record, and Identity.SessionID stays empty for one — which is
// also what exempts it from the MFA-mandatory check it could never satisfy.

// Re-exported so the service layer never names the resolution-surface
// package.
type (
	ServiceAccount                     = authn.ServiceAccount
	MachineCredential                  = authn.MachineCredential
	CredentialPolicy                   = authn.CredentialPolicy
	AffectedCredential                 = authn.AffectedCredential
	NewServiceAccount                  = authn.NewServiceAccount
	ServiceAccountCreation             = authn.ServiceAccountCreation
	DeleteServiceAccountAggregateInput = authn.DeleteServiceAccountAggregateInput
	ServiceAccountDeletion             = authn.ServiceAccountDeletion
	NewCredential                      = authn.NewMachineCredential
)

// authenticateMachine resolves a presented machine bearer value.
//
// It performs the SAME THREE READS in the same order whatever the value turns
// out to be, and evaluates every predicate after all three. Returning as soon
// as one failed would make an unknown credential cost one query and a revoked
// one three — a query-count oracle for which credentials exist.
//
// Equal query counts are necessary and not sufficient, so three things carry
// the property together, and it is worth naming them because two of them are
// easy to remove by accident:
//
//  1. the fixed three reads below, in a fixed order;
//  2. the same per-query ROW WORK on every outcome — the resolver's miss paths
//     decode a decoy row rather than returning early, so an unknown credential
//     costs the same timestamp parses as a revoked one (see machine.go's decoy
//     block in internal/store/authn);
//  3. the constant-time verifier comparison on the resolved row.
//
// What remains is the storage engine's own B-tree probe, which differs between
// a hit and a miss and which no application-level code can equalise. The
// tenant-isolation ADR already records engine-internal microtiming as the
// accepted residual; everything above it is equal.
func (a *TxAuthorizer) authenticateMachine(ctx context.Context, presented string, now time.Time) (Identity, error) {
	cred, credErr := a.r.MachineCredentialByVerifier(ctx, crypto.ArtifactVerifier(presented))
	if credErr != nil && !errors.Is(credErr, domain.ErrNotFound) {
		return Identity{}, credErr
	}

	// A missing credential still resolves a service account, for the empty id
	// — which resolves to nothing, at the same cost.
	sa, saErr := a.r.ServiceAccountByID(ctx, cred.ServiceAccountID)
	if saErr != nil && !errors.Is(saErr, domain.ErrNotFound) {
		return Identity{}, saErr
	}

	epoch, err := a.r.CredentialEpoch(ctx)
	if err != nil {
		return Identity{}, err
	}

	// The constant-time comparison the ADR requires on the resolved row runs
	// inside MachineCredentialByVerifier, like every other bearer artifact in
	// this codebase, and so does the decoy work that keeps a miss costing what
	// a hit costs. A mismatch answers domain.ErrNotFound — what a miss already
	// answered — so the three-read discipline above is untouched.
	//
	// Live() carries the whole predicate — revoked, epoch-superseded, and
	// expired-unless-indefinite — so the listing surface and this one cannot
	// answer differently about the same row.
	if credErr != nil || saErr != nil || !cred.Live(now, epoch) {
		return Identity{}, domain.ErrUnauthenticated
	}
	// A machine principal whose class is not one of the closed service-account
	// kinds fails closed. The row cannot normally hold anything else — the
	// CHECK constraint is the floor — but the class is what the normative
	// allowlists key on, so an unrecognised one authenticates nothing.
	if !domain.IsServiceAccountKind(sa.Kind) {
		return Identity{}, domain.ErrUnauthenticated
	}

	return Identity{
		Principal:           sa.PrincipalID,
		Artifact:            string(cred.Kind),
		Class:               sa.Kind,
		CredentialID:        cred.ID,
		CredentialExpiresAt: cred.ExpiresAt,
		CreatedAt:           cred.CreatedAt,
		LastSeenAt:          cred.LastUsedAt,
	}, nil
}

// CreateMachinePrincipal, CreateServiceAccount and the rest are the service
// layer's in-transaction face onto the machine tables. Authorization for them
// happens at the chokepoint first — every caller mints a proof through
// Authorize before reaching here — but the rows are `class=authn`, so the
// reads and writes ride the resolution surface, exactly as grant
// administration does.

// Reachable is the ADR's reachability computation, and the comment is the
// point of the function: the two authority classes are computed
// INDEPENDENTLY and never collapsed into one "can reach plaintext" boolean.
//
// Collapsing them is a named bypass in the ADR, and a subtle one: a service
// account already holding read(E) ∧ reveal(E) shows an EMPTY delta when
// granted reveal-history(E), so an actor with no historical access at all
// could hand a machine principal the power to read superseded secrets. The
// permission-model ADR fixed the rule this violates — "reveal-history implies
// nothing about reveal, and vice versa".
type Reachable struct {
	// Current is where read(E) ∧ reveal(E) holds — current plaintext.
	Current map[domain.EnvID]bool
	// Historical is where read(E) ∧ reveal-history(E) holds — superseded
	// plaintext, which may still be live in an external service.
	Historical map[domain.EnvID]bool
}

// ReachableFrom evaluates the two classes over a project's environments for
// one grant set. `grants` is the WHOLE set a principal would hold in the
// state being tested, so the caller computes a pre-state and a post-state and
// diffs them per class.
func ReachableFrom(scope domain.Scope, envs []domain.EnvID, grants []domain.Grant) Reachable {
	out := Reachable{Current: map[domain.EnvID]bool{}, Historical: map[domain.EnvID]bool{}}
	for _, env := range envs {
		at := domain.Scope{Org: scope.Org, Project: scope.Project, Env: env}
		if !coveredBy(grants, domain.CapRead, at) {
			// No read means no delivery at all, so neither disclosure
			// capability reaches plaintext however it is granted.
			continue
		}
		if coveredBy(grants, domain.CapReveal, at) {
			out.Current[env] = true
		}
		if coveredBy(grants, domain.CapRevealHistory, at) {
			out.Historical[env] = true
		}
	}
	return out
}

// coveredBy reuses the chokepoint's own coverage rule, so reachability and
// authorization cannot disagree about what a grant reaches.
func coveredBy(grants []domain.Grant, cap domain.Capability, at domain.Scope) bool {
	for _, g := range grants {
		if g.Capability == cap && covers(g.Scope, at) {
			return true
		}
	}
	return false
}

// authenticateInstanceConnection resolves a presented directory credential
// (#71). It is the machine leg's sibling and keeps the same discipline, with
// one fewer read: the connection row holds the principal AND the credential,
// because the ADR mints them as one unit, so there is no second lookup to
// make. What matters for the oracle is that EVERY presentation of this
// artifact does the same two reads, the same decode and the same compare —
// uniformity is within the leg, not across legs, and a caller cannot choose
// which leg runs without already knowing the artifact type they typed.
func (a *TxAuthorizer) authenticateInstanceConnection(ctx context.Context, presented string, now time.Time) (Identity, error) {
	conn, connErr := a.r.InstanceConnectionByVerifier(ctx, crypto.ArtifactVerifier(presented))
	if connErr != nil && !errors.Is(connErr, domain.ErrNotFound) {
		return Identity{}, connErr
	}

	epoch, err := a.r.CredentialEpoch(ctx)
	if err != nil {
		return Identity{}, err
	}

	if connErr != nil || !conn.Live(now, epoch) {
		return Identity{}, domain.ErrUnauthenticated
	}

	return Identity{
		Principal: conn.PrincipalID,
		// NOT string(conn.Kind). The credential kind is "hikyo-token" and says
		// nothing about the artifact presented. ClassInstanceConn drives the
		// distinct OpenAPI `instance-credential` admission class; Artifact keeps
		// the exact `ic` type for identity and forensic records.
		Artifact:     string(crypto.ArtifactInstanceConn),
		Class:        domain.ClassInstanceConn,
		CredentialID: conn.ID,
		CreatedAt:    conn.CreatedAt,
		LastSeenAt:   conn.LastUsedAt,
	}, nil
}

// The instance-connection tables' in-transaction face (#71). Same shape as the
// machine surface above: authorization happens at the chokepoint first — every
// caller mints a proof through Authorize before reaching here — but the rows
// are `class=authn`, so the reads and writes ride the resolution surface.

// The workspace tier's carrier types, re-exported so the service layer never
// names internal/store/authn — the import-boundary test enforces that the
// resolution surface is reachable only through this package.
type (
	WorkspaceOrigin       = authn.WorkspaceOrigin
	WorkspaceHandoff      = authn.WorkspaceHandoff
	NewWorkspaceHandoff   = authn.NewWorkspaceHandoff
	SessionSummary        = authn.SessionSummary
	HandoffPurpose        = authn.HandoffPurpose
	NewInstanceConnection = authn.NewInstanceConnection
	InstanceConnection    = authn.InstanceConnection
)

const (
	HandoffEstablishment = authn.HandoffEstablishment
	HandoffStepUp        = authn.HandoffStepUp
)

// The workspace tier's in-transaction face (#71). Every one of these rides the
// resolution surface for the reason stated above the connection block: the
// origin allowlist is consulted PRE-authentication (at handoff issuance and by
// CORS), the handoff transaction resolves a caller exactly as a session
// verifier does, and the session statements are session statements. The
// mutating ones are still authorized at the chokepoint first, except the two
// that cannot be — StartHandoff and RedeemHandoff, where no principal exists
// yet, which is what a handoff transaction is for.

// InstanceConnectionByPrincipal answers "which connection is this caller",
// which is what the directory serve needs to stamp last-used and to name the
// actor in its audit event.
func (a *TxAuthorizer) InstanceConnectionByPrincipal(ctx context.Context, p domain.PrincipalID) (authn.InstanceConnection, error) {
	conns, err := a.r.InstanceConnections(ctx)
	if err != nil {
		return authn.InstanceConnection{}, err
	}
	for _, c := range conns {
		if c.PrincipalID == p {
			return c, nil
		}
	}
	return authn.InstanceConnection{}, domain.ErrNotFound
}
