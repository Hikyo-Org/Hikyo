# #1039 slice 5: release and restore reconciliation

Owner: [#1039](https://github.com/Hikyo-Org/Hikyo/issues/1039). Stacked on slice 4
(`feat/1039-s4-operator-surface`, PR #1549), which is stacked on slice 3 (PR #1538).
Local only until slice 4 merges.

## Result

- `Incidents.ReleasePrincipal` (ADR O4 A): audited `incident.principal_released`, needs a
  credential establishment since containment for a password account and every linked-identity
  item attested, refuses an account with neither, revives and regrants nothing.
- Store evidence: `CredentialEstablishedSince`, `PrincipalHasPassword`,
  `PrincipalContainedIncident` (three authn-resolution queries, both engines).
- `Restore.Status` lists the contained pending principals and `hikyo restore status` marks
  them; reconcile does not lift containment.
- ADR section "Slice 5: release and restore (O4 A)".

## Evidence

- `TestIncidentReleaseRequiresEstablishmentOrAttestation`,
  `TestIncidentReleaseNeedsEstablishmentForAPasswordAccount`,
  `TestIncidentReleaseRefusesAnAccountWithNothingToReplace`,
  `TestIncidentReleaseAcceptsARealResetEstablishment` (real reset, establish, release),
  `TestRestoreReconcileDoesNotLiftContainment` (real archive restore and reopen).

## Not done

- No HTTP route, CLI verb or web control for release: the operator surface is a later slice.
- The recovery rehearsal on an isolated instance is not run.
- The establishment signal is read from the authority rows. Traced: every re-establishment
  path of a contained account (operator reset, break-glass reset, recovery code, federated
  claim) mints one after containment, and no path writes a credential without one (ADR slice 5).
- Maintainer security review and the human merge gate remain required. Cross-provider review
  did not run and is not CLEAN.
