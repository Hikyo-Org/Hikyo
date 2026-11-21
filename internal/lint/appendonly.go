package lint

import (
	"fmt"
	"go/ast"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Analyzer 6 (audit-model ADR CI invariants 3 and 7): append-only audit
// tables, and no session-level durability downgrade.
//
// The application layer holds INSERT and SELECT only on both audit tables.
// The ADR licenses policy retention and org-deletion cleanup. The retention
// job now carries engine-specific content pins; arbitrary deletion remains
// forbidden. Each trail has its own atomic retention-unit query.

var auditTables = []string{"audit_tenant_events", "audit_instance_events"}

// auditDeletionAllowlist is the content-pinned licensed-deleter set:
// engine/query name to SHA256 of normalized SQL. Only policy-driven audit
// retention deletion is licensed here. Content changes require renewed review.
var auditDeletionAllowlist = map[string]string{
	"sqlite/PruneTenantAuditRetention":     "83cbaf696b02d8d6290e5a7707449c45c1c6c4bdbb95cc75b77339806fe5489e",
	"sqlite/PruneInstanceAuditRetention":   "494e205b046dbe850ea921ca5094b9ca273ba6d8b569c657ae4d2d97cc40629b",
	"postgres/PruneTenantAuditRetention":   "375a9eeddf60d58e04f87a7075cd3d7841497104287d5fd9189886192e692e1a",
	"postgres/PruneInstanceAuditRetention": "fda06448ca43aad9884524b2a9d30dea73ffebc474cdffac275639707c3b431d",
}

func appendOnlySpecs(string) ([]InputSpec, error) {
	specs, _ := queryInputs("")
	return append(specs, InputSpec{Dir: "internal/store", Exts: []string{".sql"}, Recursive: true}), nil
}

// AuditAppendOnly runs once on internal/store: the audit-table statements of
// both engines' query files and the `.sql` files under internal/store come from
// the declared inputs. The Go files of internal/store are not declared (vet
// keys on them already): each package of the subtree scans its own files.
var AuditAppendOnly = newAuditAppendOnly()

func newAuditAppendOnly() *analysis.Analyzer {
	return fileCheck{
		name: "appendonly", doc: "audit tables are append-only at the application layer and no code downgrades commit durability",
		anchor: Module + "/internal/store",
		run: func(src source, root string) ([]string, error) {
			findings := checkAuditQueries(src, root)
			return append(findings, checkNoSyncCommitDowngrade(src, root, []string{".sql"})...), nil
		},
		pkg: func(pass *analysis.Pass, root string) {
			for _, file := range pass.Files {
				name := fileName(pass, file.Package)
				body, err := os.ReadFile(name)
				if err != nil {
					pass.Report(analysis.Diagnostic{Pos: file.Package, Message: "appendonly: " + err.Error()})
					continue
				}
				if finding, ok := syncCommitFinding(root, name, body); ok {
					pass.Report(analysis.Diagnostic{Pos: file.Package, Message: finding})
				}
			}
		},
	}.analyzer()
}

func checkAuditQueries(src source, repoRoot string) []string {
	var findings []string
	for _, engine := range []string{"sqlite", "postgres"} {
		queryDir := filepath.Join(repoRoot, "internal", "store", "queries", engine)
		queries, err := parseQueries(src, queryDir)
		if err != nil {
			return append(findings, "appendonly: "+err.Error())
		}
		for _, q := range queries {
			upper := strings.ToUpper(normalizeSpace(q.SQL))
			touchesAudit := false
			for _, tbl := range auditTables {
				if strings.Contains(strings.ToLower(q.SQL), tbl) {
					touchesAudit = true
				}
			}
			if !touchesAudit {
				continue
			}
			if strings.HasPrefix(upper, "INSERT ") || strings.HasPrefix(upper, "SELECT ") {
				continue
			}
			if want, ok := auditDeletionAllowlist[engine+"/"+q.Name]; ok && q.Hash() == want {
				continue
			}
			findings = append(findings, fmt.Sprintf(
				"appendonly(%s): %s: statement on an audit table is neither INSERT nor SELECT nor a content-pinned licensed deleter — the trails are append-only at the application layer (audit-model ADR CI invariant 3)",
				engine, q.Name))
		}
	}
	return findings
}

// syncCommitRe catches any attempt to downgrade commit durability at
// session or transaction level — the audit-model ADR's boot check verifies
// the server setting, and this ban keeps the store from quietly overriding
// it (CI invariant 7).
// The statement form requires the assignment (`= off` / `TO off`), so prose
// merely naming the banned statement — like this comment — does not trip it.
var syncCommitRe = regexp.MustCompile(`(?i)SET\s+(LOCAL\s+)?synchronous_commit\s*(=|TO\s)`)

// syncCommitFinding is the finding for a file that issues the banned statement.
func syncCommitFinding(root, path string, body []byte) (string, bool) {
	if !syncCommitRe.Match(body) {
		return "", false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	return fmt.Sprintf(
		"appendonly: %s issues SET synchronous_commit — durability is a boot-verified server setting, never a session downgrade (audit-model ADR CI invariant 7)",
		filepath.ToSlash(rel)), true
}

// checkNoSyncCommitDowngrade scans the files with the given extensions under
// internal/store. Walk and read errors surface as findings: an analyzer that
// silently skips what it cannot read is fail-open.
func checkNoSyncCommitDowngrade(src source, repoRoot string, exts []string) []string {
	root := filepath.Join(repoRoot, "internal", "store")
	paths, err := src.WalkFiles(root, exts)
	if err != nil {
		return []string{fmt.Sprintf("appendonly: walk %s: %v", root, err)}
	}
	var findings []string
	for _, path := range paths {
		b, err := src.ReadFile(path)
		if err != nil {
			findings = append(findings, fmt.Sprintf("appendonly: %v", err))
			continue
		}
		if finding, ok := syncCommitFinding(repoRoot, path, b); ok {
			findings = append(findings, finding)
		}
	}
	return findings
}

// ResolutionSurfaceWriters is the PINNED enumerated write list of the
// authorization resolution surface. It is the review artifact: adding a name
// here is how a new proof-free write path gets noticed, and everything not
// named still fails the build.
//
// The audit-model ADR's amendment part 4 pinned exactly one entry,
// WriteDenial, because a failed authorize() mints no proof and its denial
// event therefore cannot travel the proof-carrying store surface. Human
// authentication (#47) is the same circularity seen from the other side:
// resolving, minting and revoking the artifact that decides WHO a caller is
// cannot run under a proof, because the proof is what that answer produces.
//
// Stated as a deviation rather than smuggled in: the ADR says "exactly one",
// and this is more than one. What is preserved is the property the "exactly
// one" was protecting — every proof-free write is named, in one place, with a
// build failure behind it. See docs/handoff/47-first-slice.md, which routes
// the wording to human disposition.
var ResolutionSurfaceWriters = map[string]bool{
	// #1034 approved bounded singleton accounting, with atomic append-only seals.
	"UpdateRequestRefusals":        true,
	"ClaimRemotePairingAttempt":    true,
	"CreateRemotePairingInvite":    true,
	"RevokeRemotePairingInvite":    true,
	"ConsumeRemotePairingInvite":   true,
	"CreateRemotePairingExchange":  true,
	"ConsumeRemotePairingExchange": true,
	"BindRemotePairedConnection":   true,
	"ClaimRemotePairingReplay":     true,
	"PruneRemotePairingState":      true,

	"CreateCLILogin": true, "ApproveCLILogin": true, "PollCLILogin": true, "ConsumeCLILogin": true, "PruneCLILogins": true,

	// Audit (#45, audit-model ADR amendment part 4). writeProofFreeEvent is
	// the shared body WriteDenial and WriteAuthEvent both delegate to, so the
	// two cannot drift; it is the actual call site the analyzer sees.
	"WriteDenial":         true,
	"WriteAuthEvent":      true,
	"writeProofFreeEvent": true,
	// Bootstrap under local host authority (#47) — the closed local-authority
	// exception set's boot/bootstrap member, never reachable over the network.
	"CreatePrincipal":          true,
	"CorrectPrivacyAccount":    true,
	"UpdateAccountProfile":     true,
	"RestrictPrivacyPrincipal": true,
	"ErasePrivacyAccount":      true,
	"CreateAccount":            true,
	"CreateGrant":              true,

	// Reversible incident containment (incident-containment ADR, O1): read by
	// authorize() like privacy_state, so written on the resolution surface under
	// the principal-row lock.
	"ContainPrincipal":            true,
	"ReleasePrincipalContainment": true,

	// Incident containment ledger (incident-containment ADR, O5 option B):
	// mutable ledger state, separate from the append-only audit tables, written
	// on the resolution surface after the incident service proved its own
	// chokepoint operation. No row carries a secret or a free-text field.
	"OpenIncident":           true,
	"CloseIncident":          true,
	"AddIncidentItem":        true,
	"TransitionIncidentItem": true,

	// Approval-mediated temporary access (#152): the time-bound grant rows are
	// read by authorize(), so they are written on the resolution surface after
	// the access service proved its own chokepoint operation, under the
	// principal-row lock like every grant writer.
	"CreateAccessGrant":            true,
	"DeleteAccessGrantsForRequest": true,
	// Member access rules (member-access-rules ADR): read by authorize(), so
	// written on the resolution surface after the rule service proved its own
	// chokepoint operation (or a deletion path proved its own), under the
	// principal-row lock like every grant writer.
	"CreateRule":                true,
	"DeleteRule":                true,
	"DeleteRuleItem":            true,
	"CreateCredentialAuthority": true,
	// Credential establishment and the local floor (#47). None of these can
	// hold a proof: the first has no session by design, the rest are the
	// session's own lifecycle.
	"ConsumeCredentialAuthority": true,
	"CreatePasswordCredential":   true,
	"UpdatePasswordCredential":   true,
	"CreateSession":              true,
	"TouchSession":               true,
	"DeleteSession":              true,
	"DeleteSessionsForPrincipal": true,
	"AdvanceGeneration":          true,
	// The shared body of AdvanceGeneration and the containment setters: it
	// retires the pending password-login and workspace-handoff proofs, which
	// mint sessions without consulting the generation.
	"retirePendingProofs": true,
	// Factors (#54): TOTP enrolment/confirmation/removal, recovery-code batch
	// writes, step-up session rotation, and the outstanding-authority sweep.
	// None can hold a proof — they mutate the artifacts that decide how a
	// caller authenticated, which is resolution, not authorization.
	"CreateTOTP":                    true,
	"ConfirmTOTP":                   true,
	"AdvanceTOTPStep":               true,
	"DeleteTOTPForAccount":          true,
	"DeletePendingTOTPForAccount":   true,
	"CreateRecoveryCodes":           true,
	"UpdateRecoveryCodes":           true,
	"RotateSessionFactors":          true,
	"ConsumeOutstandingAuthorities": true,
	// Login challenge (#760): the single-use authority that proves the password
	// step passed before a session mints. Proof-free by design — no session
	// exists yet — and part of how a caller authenticates, which is resolution.
	"CreateLoginChallenge":  true,
	"ConsumeLoginChallenge": true,
	// OIDC (#54): the transaction, external-identity, federated-session-sweep
	// and reauth-window writers. None can hold a proof - they mutate the
	// artifacts that decide who a caller is and how they authenticated, which is
	// resolution, not authorization. Provider administration is proof-bound and
	// lives on the repository surface, not here.
	"CreateOIDCTransaction": true,
	// OAuth2 #609: closed provider administration, callback CAS, session provenance
	// and first-credential evidence; service guards own epoch/purpose/binding.
	"CreateOAuth2Provider":            true,
	"UpdateOAuth2Provider":            true,
	"DeleteOAuth2Provider":            true,
	"GuardOAuth2ProviderForMint":      true,
	"CreateOAuth2Transaction":         true,
	"ConsumeOAuth2Transaction":        true,
	"BindSessionToOAuth2Provider":     true,
	"DeleteSessionsForOAuth2Provider": true,
	"ClaimFederatedAuthority":         true,
	"StampCredentialEstablish":        true,

	"ConsumeOIDCTransaction":              true,
	"CreateExternalIdentity":              true,
	"DeleteExternalIdentity":              true,
	"DeleteSessionsForPrincipalAndIssuer": true,
	"DeleteSessionsForProvider":           true,
	"CreateReauthWindow":                  true,
	// The Phase-C mint guard: a no-op CAS write that locks the pinned provider
	// row so a concurrent reconfigure serializes behind it (A4 TOCTOU). It is a
	// proof-free write on the resolution surface for the same reason the other
	// OIDC writers are.
	"GuardProviderForMint": true,
	// OIDC provider administration writes to a class=authn table, so the write
	// rides the resolution surface even though the mutation is authorized at the
	// chokepoint (OpProviderPut/Delete) before it runs.
	"CreateProvider": true,
	"UpdateProvider": true,
	"DeleteProvider": true,
	// SCIM provisioning (#73). The credential writers are here for the same
	// reason the session writers are: a SCIM wire request presents a credential
	// BEFORE any operation is authorized, so the artifact that decides who the
	// caller is cannot itself be written under a proof. The two principal
	// writers join them because the provisioning connection is created with its
	// binding and retired with it by the ADR's §6 state machine, in the same
	// transaction as the structural grant it holds — `principals` is
	// instance-class and has no chain to bind a proof against.
	// Only the two writes that genuinely cannot hold a proof remain here. A
	// SCIM request presents its credential BEFORE any operation is authorized,
	// so recording its use is part of authentication; and the provisioning
	// principal is created with, and retired with, its binding — `principals`
	// is instance-class and has no chain for a proof to bind against.
	//
	// Credential MINT, LIST, SHOW, REVOKE and DELETE are deliberately NOT here:
	// they all run after `manage-members(org)` is proved and live on the
	// proof-carrying repository, where the org predicate comes from the proof.
	"TouchSCIMCredential":           true,
	"CreateProvisioningPrincipal":   true,
	"RetireSCIMConnectionPrincipal": true,
	// SAML (#72): provider administration, request/replay lifecycle, SP-key
	// rotation and provider-bound session lifecycle. Each writer is explicit so
	// growth of the proof-free SAML surface remains a reviewed change.
	"CreateSAMLProvider":                 true,
	"UpdateSAMLProvider":                 true,
	"DeleteSAMLProvider":                 true,
	"GuardSAMLProviderForMint":           true,
	"CreateSAMLTransaction":              true,
	"ConsumeSAMLTransaction":             true,
	"ClaimSAMLReplay":                    true,
	"DeleteExpiredSAMLReplay":            true,
	"CreateSAMLSPKey":                    true,
	"MarkSAMLSPKeyRetiring":              true,
	"DeleteRetiringSAMLSPKey":            true,
	"BindSessionToSAMLProvider":          true,
	"DeleteSessionsForSAMLProvider":      true,
	"RebindSAMLExternalIdentityProvider": true,
	// WebAuthn (#54): credential, ceremony and user-handle writers, plus the
	// clone session sweep. None can hold a proof — they mutate the artifacts that
	// decide who a caller is and how strongly they authenticated, which is
	// resolution, not authorization.
	"CreateWebAuthnCredential":            true,
	"AdvanceWebAuthnSignCount":            true,
	"DisableWebAuthnCredential":           true,
	"DeleteWebAuthnCredential":            true,
	"DeleteSessionsForWebAuthnCredential": true,
	"CreateWebAuthnCeremony":              true,
	// Machine identities (#61): the service-account and credential lifecycle.
	// None can hold a proof for the same reason the session writers cannot —
	// they mutate the artifacts that decide WHO a machine caller is, and a
	// machine credential resolves at the same chokepoint as authorize().
	// Service-account creation/deprovisioning are aggregate writers.
	// Deprovisioning locks the principal before revoking credentials or
	// releasing grants, then returns the closed blast radius needed for audit.
	"CreateMachinePrincipal":        true,
	"CreateServiceAccountAggregate": true,
	"DeleteServiceAccountAggregate": true,
	"CreateMachineCredential":       true,
	"RevokeMachineCredential":       true,
	"TouchMachineCredential":        true,
	// The instance connection (#71, multi-instance ADR). Same circularity as
	// the machine credentials above and named for the same reason: the
	// directory credential resolves at the chokepoint that authorize() runs in,
	// so the row that decides who a connected instance IS cannot be written
	// under a proof minted from that answer. Mint and revoke are themselves
	// authorized under `instance-config` before they run; Touch is the
	// last-used stamp a successful serve writes.
	"MintInstanceConnection":   true,
	"RevokeInstanceConnection": true,
	"TouchInstanceConnection":  true,
	// The workspace tier (#71). The origin allowlist is consulted at handoff
	// issuance and by CORS, both of which run PRE-authentication; the handoff
	// transaction resolves a caller exactly as a session verifier does. Neither
	// can be gated behind a proof, because the proof is what the answer
	// produces. The allowlist mutations ARE authorized under `instance-config`
	// at the chokepoint before they run.
	//
	// RevokeWorkspaceSessionsForOrigin is the atomic kill switch's second half
	// and runs in the SAME transaction as RemoveWorkspaceOrigin; it is named
	// separately so removing the pairing is a visible change here.
	// RevokeSessionForPrincipal is the self-scoped revoke behind the
	// active-session listing (criterion 5) — its principal conjunct lives in
	// the SQL, so it cannot reach another caller's row.
	"AllowWorkspaceOrigin":             true,
	"RemoveWorkspaceOrigin":            true,
	"RevokeWorkspaceSessionsForOrigin": true,
	"RevokeSessionForPrincipal":        true,
	"CreateWorkspaceHandoff":           true,
	"ApproveWorkspaceHandoff":          true,
	"CreateCLIReauthHandoff":           true,
	"ApproveCLIReauthHandoff":          true,
	"ConsumeCLIReauthHandoff":          true,
	"ConsumeWorkspaceHandoff":          true,
	"SweepExpiredWorkspaceHandoffs":    true,
	// The instance lifetime controls and the clamp they apply. Both are
	// authorized at the chokepoint under `instance-config` before they run;
	// the write rides this surface because credential_policy is class=authn.
	"SetDeveloperCredentialPolicy":   true,
	"InsertDeveloperCredential":      true,
	"RevokeDeveloperCredential":      true,
	"ClampDeveloperCredentialExpiry": true,
	// #1235: losing qualification purges the owner's personal values in the
	// same authentication transaction as the credential change.
	"PurgePersonalValuesForOwner": true,
	"SetCredentialPolicy":         true,
	"ClampCredentialExpiry":       true,
	"ClampIndefiniteCredentials":  true,
	// Registration policy (#606). The policy tables are class=authn: the
	// sign-up legs (#607, #608) resolve them before any principal exists.
	// Administration is authorized at the chokepoint (registration-policy.*)
	// before these writers run. The pending sign-up writers are here because
	// the policy delete clears them; the request writer itself is #608's.
	"CreateRegistrationPolicy":       true,
	"ReplaceRegistrationPolicy":      true,
	"writeRegistrationChildren":      true,
	"DeleteRegistrationPolicy":       true,
	"CreateRegistrationSignup":       true,
	"ReissueRegistrationSignup":      true,
	"PruneExpiredRegistrationSignup": true,
	"ConsumeRegistrationSignup":      true,
	"SetLocalAccountEmail":           true,
	"DeleteRegistrationSignup":       true,
	// Email invitations of existing accounts (#1207). The table is class=authn:
	// acceptance resolves an opaque bearer verifier for a caller who holds no
	// grant in the inviting org. Send, list and revoke authorize
	// manage-members at the org before these run; acceptance runs after the
	// inviter's re-authorization, and erasure with the account it names.
	"UpsertOrgEmailInvitation":        true,
	"DeleteOrgEmailInvitationInOrg":   true,
	"ConsumeOrgEmailInvitation":       true,
	"PruneExpiredOrgEmailInvitations": true,
	"eraseOrgEmailInvitations":        true,
	// OIDC federation (#62). Issuer configuration is authorized at the
	// chokepoint under `instance-config` before it runs; the write rides this
	// surface because federation_issuers is class=authn, exactly as OIDC and
	// SAML provider administration already do.
	//
	// ReactivateBinding is the restore predicate's writer, and SetPinGeneration
	// is the conditional cursor's pin component. Both touch class=authn tables
	// for the same reason the credential writers do: they change what a machine
	// caller may present, which is resolution.
	"CreateFederationIssuer":  true,
	"UpdateFederationIssuer":  true,
	"DeleteFederationIssuer":  true,
	"ReactivateBinding":       true,
	"SetPinGeneration":        true,
	"ConsumeWebAuthnCeremony": true,
	"SetWebAuthnUserHandle":   true,
	// Reauth-window consumption at disclosure and the effective-window transition
	// (#54): slide the sliding clock, claim a single-decision window once, and
	// invalidate every window on an environment when its effective window is
	// lowered. Proof-free like every other window writer — they mutate the
	// artifact that decides whether a disclosure may proceed.
	"SlideReauthWindow":                 true,
	"ConsumeSingleDecisionWindow":       true,
	"DeleteReauthWindowsForEnvironment": true,
	// The grant surface (#55). Grants live on the resolution surface because
	// authorize() reads them to mint a proof, so a grant write cannot be
	// gated behind one without a cycle; the chokepoint operation the service
	// calls first is the authorization gate, these are the writes.
	"AddGrantOrigin":     true,
	"ReleaseGrantOrigin": true,
	"DeleteGrantRow":     true,
	// Restore reconciliation (#76). These are the sharpest case of the same
	// circularity: a restore leaves every session dead and every grant inert,
	// so at the instant they run there is no principal in existence who could
	// authorize them. Both are local-host-authority operations (the
	// tenant-isolation ADR's SiteRecoveryReconcile), reachable from no network
	// route, and the classification-totality invariant keeps that true.
	// Adapter PATs have no Hikyo credential epoch for the remote provider to
	// enforce, so CompleteRestore must erase them in the same local-host act.
	"AdvanceRestoreEpoch": true,
	// Only AdvanceRestoreEpoch calls this private historical-table retirement
	// helper. Pending human proofs lack an epoch and must die with the bump.
	"retireRestoredPendingHumanProofs":     true,
	"InvalidateRestoredAdapterCredentials": true,
	// #147: like adapter PATs, a dynamic-secret provider's admin credential
	// authenticates to an external engine with no Hikyo credential epoch, so the
	// same local-host restore act erases it.
	"InvalidateRestoredDynamicProviderCredentials": true,
	// Human-login provider secrets and remote credentials cross the same
	// restore boundary and are destroyed by the local-host reconciliation act.
	"InvalidateRestoredExternalCredentials": true,
	// #154: a restore can resurrect certificates revoked after the backup,
	// so the same local-host restore act holds every restored CA issuer.
	"HoldRestoredPKIIssuers": true,
	// #148: a restore holds every restored rotation (or, for an archive that has
	// no hold column yet, pauses the active ones) in the same local-host restore act.
	"HoldRestoredRotations":  true,
	"PauseRestoredRotations": true,
	"ReconcilePrincipal":     true,
	// #73 section 9.1: the reconciliation commit drops restored `scim` origins
	// and any grant row they were the last hold on, in the same act.
	"dropRestoredSCIMOrigins": true,
}

// DenialWriter enforces the enumerated-writer rule as a build failure,
// not a comment. The import boundary alone cannot see this —
// internal/store/authn already holds the generated query handles, so a
// mutating call inside it would be a proof-free writer that every other guard
// admits. Every reference to a generated mutating query from that package, its
// test files included, must sit inside a function named in
// ResolutionSurfaceWriters. The mutating set comes from the declared query
// files.
var DenialWriter = newDenialWriter()

const denialSurface = Module + "/internal/store/authn"

func newDenialWriter() *analysis.Analyzer {
	a := &analysis.Analyzer{Name: "denialwriter", Doc: "the resolution surface writes only from its enumerated writers"}
	surface := denialSurface
	writers := ResolutionSurfaceWriters
	a.Flags.StringVar(&surface, "surface", denialSurface, "the resolution-surface package")
	a.Flags.Func("writers", "comma-separated enumerated writers (default: ResolutionSurfaceWriters)", func(v string) error {
		writers = map[string]bool{}
		for _, name := range strings.Split(v, ",") {
			writers[name] = true
		}
		return nil
	})
	in := flagInputs(a)
	a.Run = func(pass *analysis.Pass) (any, error) {
		if pass.Pkg.Path() != surface {
			return nil, nil
		}
		reportTarget(pass)
		src, root, err := in.open()
		if err != nil {
			reportRefusal(pass, err)
			return nil, nil
		}
		mutating, findings, err := mutatingQueries(src, root)
		if err != nil {
			pass.Report(analysis.Diagnostic{Pos: packagePos(pass), Message: err.Error()})
			return nil, nil
		}
		for _, f := range findings {
			pass.Report(analysis.Diagnostic{Pos: packagePos(pass), Message: f})
		}
		if len(mutating) == 0 {
			pass.Report(analysis.Diagnostic{Pos: packagePos(pass), Message: "no mutating queries found — the analyzer would be vacuously green"})
			return nil, nil
		}
		checkDenialWriterPass(pass, writers, mutating)
		return nil, nil
	}
	return a
}

func checkDenialWriterPass(pass *analysis.Pass, writers, mutating map[string]bool) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				return true
			}
			// Inspect every REFERENCE to a mutating generated query, not
			// just calls. `x.InsertFoo(...)` and `f := x.InsertFoo` select
			// the same method; catching only the first let a method value
			// smuggle the write past the guard (round-2 finding). Naming
			// the method at all, in any position, must sit inside an
			// approved writer.
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				obj, ok := pass.TypesInfo.Uses[sel.Sel]
				if !ok {
					return true
				}
				f, ok := obj.(*types.Func)
				if !ok || f.Pkg() == nil || !generatedPackages[f.Pkg().Path()] {
					return true
				}
				if !mutating[f.Name()] || writers[fn.Name.Name] {
					return true
				}
				pass.Report(analysis.Diagnostic{Pos: sel.Pos(), Message: fmt.Sprintf(
					"%s names the mutating query %s, and %s is not in the pinned enumerated write list — every proof-free writer in the resolution surface must be named there (audit-model ADR amendment part 4, extended by #47)",
					fn.Name.Name, f.Name(), fn.Name.Name)})
				return true
			})
			return false
		})
	}
}

// mutatingQueries derives the set of generated queries that write, from the
// sqlc command annotation on each query rather than from its name.
//
// The previous version guessed from a prefix list, and that was FAIL-OPEN in
// the worst way: `ConsumeCredentialAuthority`, `TouchSession` and
// `AdvancePrincipalGeneration` all mutate and none of them starts with a
// listed prefix, so three real writers slipped past the enforcement that
// exists to catch exactly them. A classifier that has to be remembered is a
// classifier that will be forgotten.
//
// `:exec`, `:execrows`, `:execresult` and `:execlastid` are sqlc's write
// commands; `:one` and `:many` read. An unrecognised command is treated as
// MUTATING — the fail-closed direction — and reported, so a sqlc release that
// adds a command cannot silently widen the hole.
func mutatingQueries(src source, repoRoot string) (map[string]bool, []string, error) {
	out := map[string]bool{}
	var findings []string
	for _, engine := range []string{"sqlite", "postgres"} {
		queries, err := parseQueries(src, filepath.Join(repoRoot, "internal", "store", "queries", engine))
		if err != nil {
			return nil, nil, err
		}
		for _, q := range queries {
			switch q.Cmd {
			case "one", "many", "batchmany", "batchone":
				continue
			case "exec", "execrows", "execresult", "execlastid", "copyfrom", "batchexec":
				out[q.Name] = true
			default:
				out[q.Name] = true
				findings = append(findings, fmt.Sprintf(
					"query %s has unrecognised sqlc command %q — treated as mutating (fail-closed); classify it deliberately",
					q.Name, q.Cmd))
			}
		}
	}
	return out, findings, nil
}
