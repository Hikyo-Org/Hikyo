// Package authz is the authorization chokepoint (permission +
// tenant-isolation ADRs): authorize(principal, capability-formula, scope)
// evaluated in-transaction against current policy with no cache, returning a
// proof-carrying value the store accepts calls only with. This package, the
// resolution surface (internal/store/authn), the transaction package, and
// the store's query registry form the enumerated trusted set — changes here
// are the highest-scrutiny diffs in the repo.
package authz

import (
	"sync/atomic"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// Proof is evidence that authorize() ran, for one operation, inside one
// still-live transaction. It is an interface with an unexported method,
// implemented by exactly one unexported type in this package: outside code
// cannot implement it and cannot construct a non-nil value of it — the only
// forgeable value is nil, and the store boundary rejects that fail-closed
// (tenant-isolation ADR § chokepoint pattern). A proof is not a capability
// token: it dies with its transaction, so it cannot be stored, replayed, or
// reused across retries.
type Proof interface {
	// proof returns the canonical concrete value; unexported so no other
	// package can satisfy this interface, structurally or otherwise.
	proof() *proof
}

type proofKind int

const (
	kindTenant proofKind = iota
	kindInstance
	kindSystem
)

// proof is the single concrete Proof implementation. All three ADR proof
// kinds (tenant, instance, system) are this one type: what varies is the
// binding — tenant proofs carry a resolved chain, instance proofs carry a
// grant-evaluated instance formula and no chain, system proofs carry their
// mint site. Operation and transaction binding are identical across kinds.
type proof struct {
	selfConfig        bool
	selfConfigAdmin   bool
	runtimeSnapshotID string

	kind  proofKind
	op    Operation
	site  SystemSite   // kindSystem only
	chain domain.Scope // tenant proofs and scoped system proofs: resolved chain, never caller input
	tok   *TxToken
	// key is set when a member access rule was consulted for a key-aware
	// authorization: the proof then vouches for that one key (resolved from
	// the database) and nothing wider. Nil on every other proof.
	key *domain.RuleKey
	// publishKeys binds a collection proof to the complete authorized change set.
	navigation  []domain.Rule
	publishKeys map[string]bool
}

// BoundKey reports the key a proof was minted for when member access rules
// decided it. A service holding such a proof must act on that key alone.
func BoundKey(p Proof) (domain.RuleKey, bool) {
	v, ok := p.(*proof)
	if !ok || v == nil || v.key == nil {
		return domain.RuleKey{}, false
	}
	return *v.key, true
}

// deadcode reports this marker as unreachable by design. Its signature closes
// Proof to implementations from this package; removing it breaks that boundary.
func (p *proof) proof() *proof { return p }

// TxToken is the transaction identity a proof is bound to. The transaction
// package mints one per transaction attempt and invalidates it at commit or
// rollback; a proof presented under any other token — a later retry attempt,
// a different transaction, or after its own transaction ended — dies at the
// store boundary. Constructing tokens is not privileged (a token without a
// matching proof authorizes nothing); minting proofs is.
type TxToken struct {
	dead atomic.Bool
}

// NewTxToken mints a live transaction identity.
func NewTxToken() *TxToken { return &TxToken{} }

// Invalidate marks the transaction ended. Idempotent; only ever tightens.
func (t *TxToken) Invalidate() { t.dead.Store(true) }

func (t *TxToken) alive() bool { return !t.dead.Load() }

// PublishKeyAllowed checks the changed-key binding of a collection publish proof.
// Existing whole-environment proofs have no collection binding.
func PublishKeyAllowed(p Proof, id string) bool {
	v, ok := p.(*proof)
	if !ok || v == nil {
		return false
	}
	if v.publishKeys != nil {
		return v.publishKeys[id]
	}
	return v.key == nil || v.key.ID == id
}

// NavigationProjectAllowed narrows a rule-based project directory proof to
// the projects named by the caller's own rules. Ordinary proofs are unchanged.
func NavigationProjectAllowed(p Proof, id domain.ProjectID) bool {
	if p == nil {
		return false
	}
	v := p.proof()
	if v.navigation == nil {
		return true
	}
	for _, rule := range v.navigation {
		for _, project := range rule.Where.Projects {
			if project == id {
				return true
			}
		}
	}
	return false
}

// NavigationEnvironmentAllowed preserves the environment selector on a
// directory read authorized through See, including rule-local exceptions.
func NavigationEnvironmentAllowed(p Proof, id domain.EnvID) bool {
	if p == nil {
		return false
	}
	v := p.proof()
	if v.navigation == nil {
		return true
	}
	scope := v.chain
	scope.Env = id
	for _, rule := range v.navigation {
		if rule.Reaches(domain.CapRead, domain.LevelEnv, scope, nil) {
			return true
		}
	}
	return false
}
