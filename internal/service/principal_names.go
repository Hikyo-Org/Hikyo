package service

import (
	"context"
	"errors"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// principalNames resolves only subjects already disclosed by an authorized
// membership or audit read. It never exposes a directory or contact details:
// it is fed ids that already appear on rows the caller is authorized to read,
// and answers only id -> name for those. Missing/deleted subjects retain no
// label. The id->name cache is stable, so it is reused across the several
// read transactions of an export (each passes its own authorizer to get).
type principalNames struct {
	names   map[domain.PrincipalID]string
	visible map[visibility]bool
}

type visibility struct {
	id      domain.PrincipalID
	org     domain.OrgID
	project domain.ProjectID
}

func newPrincipalNames() *principalNames {
	return &principalNames{names: make(map[domain.PrincipalID]string), visible: make(map[visibility]bool)}
}

// cached returns an already-resolved name without a lookup, for callers that
// warmed the cache inside a read transaction and must decide outside it (the
// export loop). An unresolved id returns "".
func (n *principalNames) cached(id domain.PrincipalID) string {
	return n.names[id]
}

func (n *principalNames) get(ctx context.Context, az *authz.TxAuthorizer, id domain.PrincipalID) (string, error) {
	if name, ok := n.names[id]; ok {
		return name, nil
	}
	if id == "" {
		return "", nil
	}
	account, err := az.AccountByPrincipal(ctx, id)
	var name string
	if err == nil {
		name = account.DisplayName
		if name == "" {
			name = account.Username
		}
	} else if !errors.Is(err, domain.ErrNotFound) {
		return "", err
	} else {
		machine, err := az.ServiceAccountByPrincipal(ctx, id)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return "", err
		}
		if err == nil {
			name = machine.Name
		}
	}
	n.names[id] = name
	return name, nil
}

// label is get for a row anyone who may read the row can see, including workload
// tokens. SCIM stores userName, commonly an email address, as the display name,
// and an address is contact detail rather than a name: that row keeps the ID.
func (n *principalNames) label(ctx context.Context, az *authz.TxAuthorizer, id domain.PrincipalID) (string, error) {
	name, err := n.get(ctx, az, id)
	if strings.Contains(name, "@") {
		return "", err
	}
	return name, err
}

// visibleInProject requires an independently stored grant in the scope's own
// tenant before a caller-authored roster entry can trigger a global name lookup.
// An instance-level grant does not count: it says nothing about this tenant, so
// a roster editor who knows such a principal's ID must not learn its name.
func (n *principalNames) visibleInProject(ctx context.Context, az *authz.TxAuthorizer, id domain.PrincipalID, scope domain.Scope) (bool, error) {
	key := visibility{id: id, org: scope.Org, project: scope.Project}
	if visible, ok := n.visible[key]; ok {
		return visible, nil
	}
	grants, err := az.GrantsOf(ctx, id)
	if err != nil {
		return false, err
	}
	visible := false
	for _, grant := range grants {
		if grant.Scope.Org == scope.Org && (grant.Scope.Project == "" || grant.Scope.Project == scope.Project) {
			visible = true
			break
		}
	}
	n.visible[key] = visible
	return visible, nil
}

func nameAuditActors(ctx context.Context, az *authz.TxAuthorizer, page *AuditPage) error {
	names := newPrincipalNames()
	page.ActorNames = make(map[string]string)
	for _, event := range page.Events {
		name, err := names.get(ctx, az, domain.PrincipalID(event.Actor.ID))
		if err != nil {
			return err
		}
		if name != "" {
			page.ActorNames[event.Actor.ID] = name
		}
	}
	return nil
}
