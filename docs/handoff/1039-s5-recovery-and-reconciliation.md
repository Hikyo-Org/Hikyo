# #1039 slice 5: release and restore reconciliation

Owner: [#1039](https://github.com/Hikyo-Org/Hikyo/issues/1039). Stacked on slice 4
(`feat/1039-s4-operator-surface`, PR #1549), which is stacked on slice 3 (PR #1538).
Slice 4 (#1549) is a draft and has not merged, so this PR stays a draft until it does.

## Result

- `Incidents.ReleasePrincipal` (ADR O4 A): audited `incident.principal_released`, needs a
  credential establishment since containment for a password account and every linked-identity
  item attested, refuses an account with neither, revives and regrants nothing.
- Store evidence: `CredentialEstablishedIn`, `PrincipalHasPassword`,
  `PrincipalContainedIncident`, `OtherOpenIncidentHolding` (four authn-resolution queries, both
  engines), and `ContainedAccountID` for the linked-identity read. Migration 00101 stamps
  `credential_authorities.established_in_incident` (it supersedes 00100's clock-based
  `established_at`) with no backfill: release fails closed for a password account contained
  before the upgrade until it establishes a credential again. Release compares incident ids and
  reads no clock.
- Release needs the Resume step-up proof (`incidentProof`, shared with Resume): the incident is
  authorized first, evidence is consumed in the release write transaction, host-local actors
  are exempt. The service is the only entry point; no route or CLI verb carries a proof yet.
- Coverage of linked identities reads no clock: attesting an enumeration cut lists each
  unlisted identity as an open `unknown` item (never attested automatically); an identity
  linked later holds release, and the refused release lists it as an open item to attest. Establishment
  stamps are cleared by `ContainPrincipal` (a new episode needs a new establishment), and release
  waits for the `account.contain` item to be terminal.
- `Restore.Status` lists the contained pending principals and `hikyo restore status` marks
  them; reconcile does not lift containment.
- ADR section "Slice 5: release and restore (O4 A)".

## Evidence

- `TestIncidentReleaseRequiresEstablishmentOrAttestation`,
  `TestIncidentReleaseNeedsEstablishmentForAPasswordAccount`,
  `TestIncidentReleaseRefusesAnAccountWithNothingToReplace`,
  `TestIncidentReleaseAcceptsARealResetEstablishment` (real reset, establish, release),
  `TestIncidentReleaseOrgIncidentWaitsForIdentityAttestation`, `TestIncidentReleaseKeepsContainmentsOrgBound`,
  `TestIncidentReleaseCountsOnlyAnEstablishmentAfterContainment`,
  `TestIncidentReleaseIgnoresAClockAheadEstablishmentBeforeContainment` (a future-dated
  password and authority before containment do not release, nor does another incident's stamp),
  `TestIncidentReleaseRefusesANonContainingIncident` (account incident contains, org incident
  also holds, first closed, release through the second is not found, attested or not),
  `TestReleasePrincipalContainmentBindsTheContainingIncident` (the store bind alone),
  `TestInvariantAuditRegistryClosure` (`incident.principal_released` is in the incident
  operations' event lists),
  `TestIncidentReleaseTruncationIsKeyedPerMember` (including an identity linked on a slow-clock
  node after the cut was attested), `TestIncidentReleaseNeedsAFreshProof`,
  `TestIncidentReleaseEvidenceDoesNotSurviveARecontainment`,
  `TestIncidentReleaseRefusesWhileTheContainmentItemIsOpen`,
  `TestIncidentReleaseWaitsForAnotherOpenIncidentHoldingThePrincipal`,
  `TestIncidentReleaseCutDoesNotStandInForAListedIdentity`, and the negative
  `TestIncidentContainedAccountRefusesRecoveryCodeEstablishment` and `...RefusesFederatedClaim`,
  `TestRestoreReconcileDoesNotLiftContainment` (real archive restore and reopen).

## Tested revision

- Code revision `6d420e5f1`, before a later restack onto #1549 that changed no slice 5 file: the release, audit-registry and
  pin tests (`TestIncidentRelease*`, `TestReleasePrincipalContainmentBindsTheContainingIncident`,
  `TestInvariantAuditRegistryClosure`, `TestInvariant13AllowlistPinning`, `TestAuditCore`) pass
  on SQLite and PostgreSQL 18 (checked before the restack onto #1549; the restack changed no
  slice 5 file).
- Mutants killed with `go test -overlay`: release without the stamp (real-reset release and the
  count test fail), "any consumed authority counts" (the clock-ahead and count tests fail), the
  service containing-incident check removed (the non-containing-incident test fails, the store
  bind alone does not save it), the store bind removed (the store-level test fails; the service
  test alone survives it), and the registry without `incident.principal_released`
  (`TestInvariantAuditRegistryClosure` fails).
- Mutants of the later security round killed with `go test -overlay` (assertion failures, both
  engines): no listing on attest and a "listed only" rule, auto-attesting the listed identities, a created_at
  rule (slow-clock repro), no relisting by a refused release, the listing's audit event dropped or given the wrong actor (truncation test), the proof step dropped (proof test), the stamp
  clear dropped (re-containment test), the open-item check dropped (open-item test).
- Full-package results (`./internal/isolation`, `./internal/store/...`, `./internal/service`, `./internal/app`, `./internal/lint` including `TestVetDriverRepo`, both engines) are not claimed here: the implementation ran only the focused tests above, and the full run is the review's. Lint is hosted CI (the local golangci-lint is built with go1.26 and refuses the module's go1.27.4).

## Not done

- No HTTP route, CLI verb or web control for release: the operator surface is a later slice.
- The recovery rehearsal on an isolated instance is not run.
- The establishment signal is `credential_authorities.established_in_incident`, stamped with the
  containing incident only when a password establishment consumes an authority (never by a
  sweep or a federated claim). The real ways back are the operator reset (refused for linked
  accounts) and the break-glass reset; recovery-code consumption and a federated claim refuse a
  contained account (negative tests). Release applies containment's org classification and reads
  the target's own identities, which an org incident now lists per member.
- Maintainer security review and the human merge gate remain required. Cross-provider review
  did not run and is not CLEAN.
