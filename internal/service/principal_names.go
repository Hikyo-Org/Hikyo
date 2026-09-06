package service

import (
	"context"
	"errors"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// principalNames resolves only subjects already disclosed by an authorized
// membership or audit read. It is transaction-local and never exposes a
// directory or contact details. Missing/deleted subjects retain no label.
type principalNames struct {
	az    *authz.TxAuthorizer
	names map[domain.PrincipalID]string
}

func newPrincipalNames(az *authz.TxAuthorizer) *principalNames {
	return &principalNames{az: az, names: make(map[domain.PrincipalID]string)}
}

func (n *principalNames) get(ctx context.Context, id domain.PrincipalID) (string, error) {
	if name, ok := n.names[id]; ok {
		return name, nil
	}
	if id == "" {
		return "", nil
	}
	account, err := n.az.AccountByPrincipal(ctx, id)
	var name string
	if err == nil {
		name = account.DisplayName
		if name == "" {
			name = account.Username
		}
	} else if !errors.Is(err, domain.ErrNotFound) {
		return "", err
	} else {
		machine, err := n.az.ServiceAccountByPrincipal(ctx, id)
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
	names := newPrincipalNames(az)
	page.ActorNames = make(map[string]string)
	for _, event := range page.Events {
		name, err := names.get(ctx, domain.PrincipalID(event.Actor.ID))
		if err != nil {
			return err
		}
		if name != "" {
			page.ActorNames[event.Actor.ID] = name
		}
	}
	return nil
}
