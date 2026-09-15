package service

import (
	"context"
	"errors"

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
	names map[domain.PrincipalID]string
}

func newPrincipalNames() *principalNames {
	return &principalNames{names: make(map[domain.PrincipalID]string)}
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
