package authz

import (
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
)

// The grant surface's in-transaction face (#55). Service code never sees the
// resolver; it reaches the grant table through these, inside the same
// transaction its chokepoint proof was minted in.

// GrantRow, Origin and GrantLine are re-exported so the service layer never
// names the resolution-surface package.
type (
	GrantRow   = authn.GrantRow
	Origin     = authn.Origin
	GrantLine  = authn.GrantLine
	EnvSetting = authn.EnvironmentSettings
	// GrantOriginRow and LockoutRetention arrive with the SCIM release
	// algorithm (#73 §2.4): the first is one (grant row, origin) pair, the
	// second is a row a retention origin is holding alive.
	GrantOriginRow   = authn.GrantOriginRow
	LockoutRetention = authn.LockoutRetention
	// The SCIM provisioning credential as AUTHENTICATION sees it (#73 §7).
	// Its administration lives on the proof-carrying repository instead.
	SCIMCredential = authn.SCIMCredential
)

// ---------------------------------------------------------------------------
// SCIM provisioning credentials (#73 §7)
//
// They sit on the resolution surface for the same reason sessions do: a SCIM
// wire request presents one BEFORE any operation is authorized.
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Approval-mediated temporary access (#152)
//
// The time-bound grant rows ride the resolution surface because authorize()
// reads them. The access service reaches them only through these, after its
// own chokepoint operation has been proved in the same transaction.
// ---------------------------------------------------------------------------

// AccessGrant is re-exported so the service layer never names authn.
type AccessGrant = authn.AccessGrant
