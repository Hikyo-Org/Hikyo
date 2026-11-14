package authz

import "github.com/Hikyo-Org/hikyo/internal/store/authn"

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
