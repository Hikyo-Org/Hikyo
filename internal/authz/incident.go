package authz

import (
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
)

// MaxIncidentPage is the one page size every incident read takes, re-exported
// so the service layer names the same bound the store enforces.
const MaxIncidentPage = domain.MaxIncidentPage

// Incident ledger carriers (incident-containment ADR, O5). The ledger rides the
// resolution surface: its operations authorize credential-reset at the
// incident's scope first and then write through these forwarders.
type (
	Incident        = authn.Incident
	NewIncident     = authn.NewIncident
	IncidentItem    = authn.IncidentItem
	NewIncidentItem = authn.NewIncidentItem
	ItemTransition  = authn.ItemTransition
)
