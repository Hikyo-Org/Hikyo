package domain

// Incident ledger vocabulary (incident-containment ADR, O5 option B). Every
// value here is a closed set. The ledger stores no free text and no string an
// attacker can choose, so a field that is not one of these constants or a
// Hikyo-generated identifier cannot reach it.

// IncidentScopeKind is what an incident is opened on.
type IncidentScopeKind string

const (
	IncidentScopeAccount IncidentScopeKind = "account"
	IncidentScopeOrg     IncidentScopeKind = "org"
	IncidentScopeProject IncidentScopeKind = "project"
	IncidentScopeAdapter IncidentScopeKind = "adapter"
)

// Valid reports whether k is a known scope kind.
func (k IncidentScopeKind) Valid() bool {
	switch k {
	case IncidentScopeAccount, IncidentScopeOrg, IncidentScopeProject, IncidentScopeAdapter:
		return true
	}
	return false
}

// IncidentStatus is where an incident stands. Closing with open items needs the
// explicit residual status, which lists them (ADR - Failure, interruption).
type IncidentStatus string

const (
	IncidentOpen                IncidentStatus = "open"
	IncidentClosed              IncidentStatus = "closed"
	IncidentClosedWithResiduals IncidentStatus = "closed_with_residuals"
)

// ItemOutcome is the closed outcome set of one ledger item. Pending, unknown
// and blocked are open: resume re-reads them and re-runs only those.
type ItemOutcome string

const (
	ItemPending    ItemOutcome = "pending"
	ItemDone       ItemOutcome = "done"
	ItemUnknown    ItemOutcome = "unknown"
	ItemAttested   ItemOutcome = "attested"
	ItemBlocked    ItemOutcome = "blocked"
	ItemOutOfScope ItemOutcome = "out_of_scope"
)

// Valid reports whether o is a known outcome.
func (o ItemOutcome) Valid() bool {
	switch o {
	case ItemPending, ItemDone, ItemUnknown, ItemAttested, ItemBlocked, ItemOutOfScope:
		return true
	}
	return false
}

// Open reports whether the item still needs work. A lost response, a restart or
// an unreachable provider leaves an item open; nothing infers done.
func (o ItemOutcome) Open() bool {
	return o == ItemPending || o == ItemUnknown || o == ItemBlocked
}

// ItemReason is the closed reason vocabulary. Empty means the outcome needs no
// reason. The out-of-scope reasons name what the workflow does not cover.
type ItemReason string

const (
	ReasonNone                  ItemReason = ""
	ReasonNeedsAnotherOperator  ItemReason = "needs_another_operator"
	ReasonOwnerUnconfirmed      ItemReason = "owner_unconfirmed"
	ReasonOwnerFailed           ItemReason = "owner_failed"
	ReasonEnumerationTruncated  ItemReason = "enumeration_truncated"
	ReasonNoRevokeOwner         ItemReason = "no_revoke_owner"
	ReasonDeliveredPlaintext    ItemReason = "delivered_plaintext"
	ReasonLocalCopy             ItemReason = "cache"
	ReasonRunningProcess        ItemReason = "running_process"
	ReasonExternalSession       ItemReason = "external_session"
	ReasonIssuedCertificateUsed ItemReason = "issued_certificate_in_use"
	ReasonUpstreamNotObservable ItemReason = "upstream_not_observable"
)

// OutOfScopeReason reports whether r may accompany an out-of-scope item.
func (r ItemReason) OutOfScopeReason() bool {
	switch r {
	case ReasonNoRevokeOwner, ReasonDeliveredPlaintext, ReasonLocalCopy, ReasonRunningProcess, ReasonExternalSession, ReasonIssuedCertificateUsed:
		return true
	}
	return false
}

// Valid reports whether r is a known reason.
func (r ItemReason) Valid() bool {
	return r == ReasonNone || r.OutOfScopeReason() || r == ReasonNeedsAnotherOperator || r == ReasonOwnerUnconfirmed ||
		r == ReasonOwnerFailed || r == ReasonEnumerationTruncated || r == ReasonUpstreamNotObservable
}
