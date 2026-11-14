package authz

import (
	"context"
	"errors"
	"fmt"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
)

// Member access rules (member-access-rules ADR) at the chokepoint.
//
// Rules are read ONLY inside authorize(), only after the legacy grants alone
// failed the formula, and only for callers that can be human. A rule that is
// narrowed by keys satisfies an atom only when the operation names a key via
// AuthorizeKey and that key, resolved from the database in this transaction,
// matches. Every other operation is out of a key-narrowed rule's reach.

type keyTargetKind int

const (
	keyByID keyTargetKind = iota + 1
	keyByName
	keyToCreate
	keyMovedTo
	keyPendingVersion
	keyApprovalRequest
)

// KeyTarget names the key a key-aware authorization addresses. The folder a
// rule matches on is always read from the database, except for a key being
// created (it has no row yet: the folder IS what is being authorized) and
// the destination of a move (the id is resolved, the new folder is what is
// being authorized).
type KeyTarget struct {
	kind   keyTargetKind
	id     string
	name   string
	folder string
}

// KeyByID addresses an existing key by its stable id.
func KeyByID(id string) KeyTarget { return KeyTarget{kind: keyByID, id: id} }

// KeyPendingVersion addresses an owned live draft by key metadata only.
func KeyPendingVersion(id string) KeyTarget { return KeyTarget{kind: keyPendingVersion, id: id} }

// KeyApprovalRequest addresses the first pinned key of an approval request.
// Eligible approvers may act on another person's request; ownership is checked
// separately by the approval policy, never inferred from this metadata lookup.
func KeyApprovalRequest(id string) KeyTarget { return KeyTarget{kind: keyApprovalRequest, id: id} }

// KeyByName addresses an existing key by its name within the project.
func KeyByName(name string) KeyTarget { return KeyTarget{kind: keyByName, name: name} }

// KeyToCreate addresses a key that does not exist yet, by destination folder.
func KeyToCreate(folder string) KeyTarget { return KeyTarget{kind: keyToCreate, folder: folder} }

// KeyMovedTo addresses an existing key as it would be after moving to folder.
func KeyMovedTo(id, folder string) KeyTarget {
	return KeyTarget{kind: keyMovedTo, id: id, folder: folder}
}

// AuthorizeKey is Authorize for an operation acting on exactly one key. It
// evaluates the same formula; the key only lets a key-narrowed rule take part.
// A key that does not exist is the uniform nonexistent outcome.
func (a *TxAuthorizer) AuthorizeKey(ctx context.Context, caller Identity, op Operation, scope domain.Scope, key KeyTarget) (Proof, error) {
	if key.kind == 0 {
		return nil, errors.New("authz: zero key target")
	}
	spec, ok := registry.authorizationSpec(op)
	if !ok {
		return nil, fmt.Errorf("authz: operation %q is not in the operation registry", op)
	}
	if spec.class != ClassTenant {
		return nil, fmt.Errorf("authz: key-aware operation %q must be tenant-scoped", op)
	}
	if caller.Principal == "" {
		return nil, errors.New("authz: empty principal")
	}
	if caller.Class == domain.ClassInstanceConn {
		// The instance-connection confinement lives in Authorize; never widen
		// it through the key-aware path.
		return a.Authorize(ctx, caller, op, scope)
	}
	return a.authorizeTenant(ctx, caller, op, spec, scope, &key, nil)
}

// rulesApply reports whether a caller's rules may be consulted. The rule
// lookup itself admits only human principals; this spares machine callers
// the read and keeps their query count what it was.
func rulesApply(caller Identity) bool {
	return caller.Class == "" || caller.Class == domain.ClassHuman
}

// ruleSatisfiable identifies ordinary project/environment formulas that may
// consult rules. Management uses AuthorizeRule's complete selector instead;
// explicit metadata navigation operations use a filtered rule projection.
func ruleSatisfiable(f Formula) bool {
	for _, atom := range f {
		if _, ok := domain.RuleShapeOf(atom.Cap); !ok || atom.Cap == domain.CapManageMembers {
			continue
		}
		if atom.At == domain.LevelProject || atom.At == domain.LevelEnv {
			return true
		}
	}
	return false
}

func (a *TxAuthorizer) resolveKeyTarget(ctx context.Context, chain domain.Scope, k KeyTarget, principal domain.PrincipalID) (domain.RuleKey, error) {
	switch k.kind {
	case keyApprovalRequest:
		return a.r.ResolveRuleApprovalKey(ctx, chain, k.id)
	case keyPendingVersion:
		return a.r.ResolveRulePendingKey(ctx, chain, principal, k.id)
	case keyByID:
		return a.r.ResolveRuleKey(ctx, string(chain.Org), string(chain.Project), k.id, "")
	case keyByName:
		return a.r.ResolveRuleKey(ctx, string(chain.Org), string(chain.Project), "", k.name)
	case keyToCreate:
		return domain.RuleKey{Folder: k.folder}, nil
	case keyMovedTo:
		got, err := a.r.ResolveRuleKey(ctx, string(chain.Org), string(chain.Project), k.id, "")
		if err != nil {
			return domain.RuleKey{}, err
		}
		got.Folder = k.folder
		return got, nil
	default:
		return domain.RuleKey{}, errors.New("authz: unknown key target")
	}
}

// evaluateWithRules answers the formula atom by atom: an atom is held when a
// grant covers it (exactly as evaluate) or a rule reaches it. Rules are
// additive; an except inside one rule never blocks another rule or a grant.
func evaluateWithRules(f Formula, chain domain.Scope, grants []domain.Grant, rules []domain.Rule, key *domain.RuleKey) bool {
	for _, atom := range f {
		if evaluate(Formula{atom}, chain, grants) {
			continue
		}
		held := false
		for _, r := range rules {
			if r.Validate() == nil && r.Reaches(atom.Cap, atom.At, chain, key) {
				held = true
				break
			}
		}
		if !held {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// The rule surface's in-transaction face for the service layer. Service code
// reaches these only after its own chokepoint operation has been proved in
// the same transaction.
// ---------------------------------------------------------------------------

// StoredRule and StoredRuleItem are re-exported so the service layer never
// names the resolution-surface package.
type (
	StoredRule     = authn.StoredRule
	StoredRuleItem = authn.StoredRuleItem
)

// RulesForProject is the folder-move widening census: every rule of a human
// principal naming this project, a superset of what the chokepoint honours
// (a currently restricted principal is still named).
func (a *TxAuthorizer) RulesForProject(ctx context.Context, org domain.OrgID, project domain.ProjectID) ([]domain.Rule, error) {
	return a.r.RulesForProject(ctx, string(org), string(project))
}

func (a *TxAuthorizer) RuleIDsForEnvironment(ctx context.Context, s domain.Scope) ([]string, error) {
	return a.r.RuleIDsForEnvironment(ctx, string(s.Org), string(s.Project), string(s.Env))
}

func (a *TxAuthorizer) RuleIDsForKey(ctx context.Context, s domain.Scope, keyID string) ([]string, error) {
	return a.r.RuleIDsForKey(ctx, string(s.Org), string(s.Project), keyID)
}

func (a *TxAuthorizer) RuleIDsForProject(ctx context.Context, s domain.Scope) ([]string, error) {
	return a.r.RuleIDsForProject(ctx, string(s.Org), string(s.Project))
}

func (a *TxAuthorizer) RuleIDsForOrg(ctx context.Context, org domain.OrgID) ([]string, error) {
	return a.r.RuleIDsForOrg(ctx, string(org))
}

// RuleKeyExists reports ErrNotFound unless the key id exists in the project
// scope names; the rule writer uses it to refuse a dangling key item clearly.
func (a *TxAuthorizer) RuleKeyExists(ctx context.Context, s domain.Scope, keyID string) error {
	_, err := a.r.ResolveRuleKey(ctx, string(s.Org), string(s.Project), keyID, "")
	return err
}

// RuleLine is re-exported for the service layer's rule listing.
type RuleLine = authn.RuleLine

// RuleLinesInOrg lists the rule surface for one org.
func (a *TxAuthorizer) RuleLinesInOrg(ctx context.Context, org domain.OrgID) ([]RuleLine, error) {
	return a.r.RuleLinesInOrg(ctx, string(org))
}

// RuleLinesInProject lists the rule surface for one project, reading only
// that project's items.
func (a *TxAuthorizer) RuleLinesInProject(ctx context.Context, s domain.Scope) ([]RuleLine, error) {
	return a.r.RuleLinesInProject(ctx, string(s.Org), string(s.Project))
}

// AuthorizePublishKeys proves the complete changed-key set of one environment.
// Snapshot materialization may carry unchanged cells forward, but may mutate
// only these keys. Empty selections never mint a collection proof.
func (a *TxAuthorizer) AuthorizePublishKeys(ctx context.Context, caller Identity, scope domain.Scope, ids []string) (Proof, error) {
	if len(ids) == 0 {
		return nil, errors.New("authz: empty publish key selection")
	}
	var first Proof
	keys := make(map[string]bool, len(ids))
	for _, id := range ids {
		p, err := a.AuthorizeKey(ctx, caller, OpValuePublish, scope, KeyByID(id))
		if err != nil {
			return nil, err
		}
		if first == nil {
			first = p
		}
		keys[id] = true
	}
	// Only the chokepoint can construct this proof. Preserve all assurance,
	// transaction, operation and tenant bindings from the checked proof.
	result := *first.proof()
	result.key = nil
	result.publishKeys = keys
	return &result, nil
}
