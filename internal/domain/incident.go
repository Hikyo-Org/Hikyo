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

// MaxIncidentPage is the one page size every incident read takes: the ledger,
// the enumeration sources, the lease listing and the delegation listing. It is
// re-exported through authz so the service and the store share one bound.
const MaxIncidentPage = 500

// Owner functions: the closed vocabulary of who revokes or resolves an item. An
// item whose owner function is not listed here never reaches the ledger or the
// audit trail.
const (
	OwnerSessionRevoke        = "session.revoke"
	OwnerDeveloperRevoke      = "developer_credential.revoke"
	OwnerExternalIdentity     = "external_identity.remove"
	OwnerFactorRemoval        = "factor.remove"
	OwnerServiceAccountReview = "service_account.review"
	OwnerMachineCredential    = "machine_credential.revoke"
	OwnerAccountContain       = "account.contain"
	OwnerEmailInvitation      = "email_invitation.revoke"
	OwnerSCIMCredential       = "scim_credential.revoke"
	OwnerLeaseRevoke          = "lease.revoke"
	OwnerAdapterTargetPause   = "adapter_target.pause"
	OwnerAdapterMove          = "adapter_move.resume"
	OwnerAdapterScrub         = "adapter_scrub.complete"
	OwnerAdapterCredential    = "adapter_credential.clear"
	OwnerAdapterUpstream      = "adapter_credential.upstream_revoke"
	OwnerDynamicCredential    = "dynamic_provider.credential_clear"
	OwnerDynamicUpstream      = "dynamic_provider.upstream_revoke"
	// OwnerConsumerAdoption is a consumer adopting a replacement credential: the
	// ADR records it as attested, since Hikyo cannot observe the consumer.
	OwnerConsumerAdoption = "consumer.adopt"
	// OwnerNone marks an object no workflow revokes (out of scope).
	OwnerNone = "none"
)

// IncidentOwnerFunctions lists every owner function.
var IncidentOwnerFunctions = []string{
	OwnerSessionRevoke, OwnerDeveloperRevoke, OwnerExternalIdentity, OwnerFactorRemoval, OwnerServiceAccountReview,
	OwnerMachineCredential, OwnerAccountContain, OwnerEmailInvitation, OwnerSCIMCredential, OwnerLeaseRevoke,
	OwnerAdapterTargetPause, OwnerAdapterMove, OwnerAdapterScrub, OwnerAdapterCredential, OwnerAdapterUpstream,
	OwnerDynamicCredential, OwnerDynamicUpstream, OwnerConsumerAdoption, OwnerNone,
}

// Object kinds: the closed vocabulary of what an item names.
const (
	ObjectKindSession            = "session"
	ObjectKindDeveloperCred      = "developer_credential"
	ObjectKindExternalIdentity   = "external_identity"
	ObjectKindTOTPFactor         = "totp_factor"
	ObjectKindPasskey            = "passkey"
	ObjectKindServiceAccount     = "service_account"
	ObjectKindMachineCredential  = "machine_credential"
	ObjectKindAccount            = "account"
	ObjectKindEmailInvitation    = "email_invitation"
	ObjectKindSCIMCredential     = "scim_credential"
	ObjectKindSCIMBinding        = "scim_binding"
	ObjectKindLease              = "lease"
	ObjectKindAdapter            = "adapter"
	ObjectKindAdapterTarget      = "adapter_target"
	ObjectKindDynamicProvider    = "dynamic_provider"
	ObjectKindEnumeration        = "enumeration"
	ObjectKindOrg                = "org"
	ObjectKindProject            = "project"
	ObjectKindDeliveredPlaintext = "delivered_plaintext"
	ObjectKindLocalCopy          = "cache"
	ObjectKindRunningProcess     = "running_process"
	ObjectKindExternalSession    = "external_session"
	ObjectKindIssuedCertificate  = "issued_certificate"
)

// IncidentObjectKinds lists every object kind.
var IncidentObjectKinds = []string{
	ObjectKindSession, ObjectKindDeveloperCred, ObjectKindExternalIdentity, ObjectKindTOTPFactor, ObjectKindPasskey, ObjectKindServiceAccount,
	ObjectKindMachineCredential, ObjectKindAccount, ObjectKindEmailInvitation, ObjectKindSCIMCredential, ObjectKindSCIMBinding, ObjectKindLease,
	ObjectKindAdapter, ObjectKindAdapterTarget, ObjectKindDynamicProvider, ObjectKindEnumeration, ObjectKindOrg, ObjectKindProject,
	ObjectKindDeliveredPlaintext, ObjectKindLocalCopy, ObjectKindRunningProcess, ObjectKindExternalSession, ObjectKindIssuedCertificate,
}

// ValidOwnerFunction and ValidObjectKind report membership in the closed sets.
func ValidOwnerFunction(f string) bool { return inList(IncidentOwnerFunctions, f) }
func ValidObjectKind(k string) bool    { return inList(IncidentObjectKinds, k) }

func inList(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Valid reports whether r is a known reason.
func (r ItemReason) Valid() bool {
	return r == ReasonNone || r.OutOfScopeReason() || r == ReasonNeedsAnotherOperator || r == ReasonOwnerUnconfirmed ||
		r == ReasonOwnerFailed || r == ReasonEnumerationTruncated || r == ReasonUpstreamNotObservable
}
