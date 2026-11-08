package authz

import "github.com/Hikyo-Org/hikyo/internal/store/authn"

// Email invitation seam (#1207). The table is class=authn, so its reads and
// writes ride the proof-free resolution surface after the owning operation
// authorized, or for the session's own account binding at acceptance.

// EmailInvitation is one pending row of an organisation and address.
type EmailInvitation = authn.EmailInvitation

// EmailInvitationListing is the administrator's view of a pending row.
type EmailInvitationListing = authn.EmailInvitationListing
