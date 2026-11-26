# #1039 slice 5: release and restore reconciliation

Owner: [#1039](https://github.com/Hikyo-Org/Hikyo/issues/1039). Stacked on slice 4
(`feat/1039-s4-operator-surface`, PR #1549), which is stacked on slice 3 (PR #1538).
Slice 4 (#1549) is a draft and has not merged, so this PR stays a draft until it does.

## Result

- `Incidents.ReleasePrincipal` (ADR O4 A): audited `incident.principal_released`, needs a
  credential establishment during the containment episode for a password account and every linked-identity
  item attested, refuses an account with neither, revives and regrants nothing.
- Release surface (owner decision A): `POST /api/v1/incidents/{incident}/release` (min revision 22), `hikyo incident release <incident> <principal> [--yes]`, and the web release action with the resume step-up dialog. Same proof rules as resume; uniform 404; tests `TestIncidentReleaseOverHTTPWithAValidProof`, `TestIncidentReleaseRefusesAResponderWithNoProofCredentialOverHTTP`, the CLI exercise and `Incidents.release.test.tsx`.
- Close hands a held principal to another open incident that also recorded it (lowest id), never lifting containment (`incident_handover_test.go`).
- Store evidence: `CredentialEstablishedIn`, `PrincipalHasPassword`,
  `PrincipalContainedIncident`, `OtherOpenIncidentHolding`, `CountPrincipalsContainedBy` (five authn-resolution queries, both
  engines), and `ContainedAccountID` for the linked-identity read. Migration 00100 stamps
  `credential_authorities.established_in_incident` with no backfill: release fails closed for a password account contained
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

- Review-fix round, on #1549 `5bde7954a`: code revision `7956574e7` plus the test-only follow-up `dc7f22e7f`. One `agent-heavy` job ran, with `-p 2 -parallel 2 -timeout 90m`, every part finished, SQLite and PostgreSQL 18: `./internal/isolation` in nine `-run` parts, `./internal/app`, `./internal/service`, `./internal/store/...`, `TestVetDriverRepo` and `golangci-lint-new.sh origin/feat/1039-s4-operator-surface`. All passed except part I (9 failures in three tests that closed an incident while it held a contained principal, now refused by design); those tests release first (`dc7f22e7f`), and part I plus golangci were rerun on `dc7f22e7f` (0 failures, 0 issues). Parts S and T-Z may have compiled with the test edits of `dc7f22e7f` already in the tree; the others ran on `7956574e7` exactly. Mutants killed with `go test -overlay` (both engines): Close check removed, closed-incident release check removed, proof never consumed (spent-TOTP-step assertion), attest of the cut listing nothing, every enumeration cut attestable, `cutPending` branch removed.
- Earlier round: code revision `898222daa` (rebased onto #1549 `adf0eb850`). The branch was later restacked onto #1549 `5bde7954a`, which only adds #1538 test files; the release tests and the new boundary tests were rerun on the restacked head, the full parts were not.
  All runs went through `agent-heavy` with `-p 2 -parallel 2 -timeout 90m`, every part finished, on SQLite and
  PostgreSQL 18 (`forEngines`): `./internal/isolation` in nine `-run` parts (A-B, C-D, E-H, I, L-O, P, R, S, T-Z) with 0 failures
  (`TestInvariant01ClassificationTotality` passes; the skips are env-gated emulator and smoke tests); `./internal/app` whole
  (drill tests included); `./internal/store/...`; `./internal/lint -run TestVetDriverRepo`; and
  `scripts/ci/golangci-lint-new.sh origin/feat/1039-s4-operator-surface` (0 issues, both passes).
- Mutants killed with `go test -overlay`: release without the stamp (real-reset release and the
  count test fail), "any consumed authority counts" (the clock-ahead and count tests fail), the
  service containing-incident check removed (the non-containing-incident test fails, the store
  bind alone does not save it), the store bind removed (the store-level test fails; the service
  test alone survives it), and the registry without `incident.principal_released`
  (`TestInvariantAuditRegistryClosure` fails).
- Mutants of the later security round killed with `go test -overlay` (assertion failures, both
  engines): no listing on attest and a "listed only" rule, auto-attesting the listed identities, a created_at
  rule (slow-clock repro), no relisting by a refused release, the listing's audit event dropped or given the wrong actor (truncation test), the proof step dropped (proof test), the stamp
  clear dropped (re-containment test), the stamp replaced by a clock comparison (the real clock-ahead establishment test), the open-item check dropped (open-item test).
- Review-fix round (Close lockout, LOW findings): Close refuses with a conflict while the incident holds a contained principal (`TestIncidentCloseRefusesWhileItHoldsAContainedPrincipal`), release on a closed incident has its own conflict, migrations 00100 and 00101 are folded into one 00100 (neither was ever on dev or in a release). Correction: the earlier claim that the attest-cut-lists-nothing mutant was killed was wrong; the truncation test now links `xid_cutlist` after the last refused release and asserts the cut's attestation lists it with the attester as audit actor. New tests also cover the spent TOTP step on a second proof-consuming call, a rotation-halt enumeration row refusing attestation, and an unattested cut holding release when every identity is attested.
- `./internal/service` was run whole on the earlier head `2f73e5a8c` (passes); it is not part of the run above.

## Not done

- The recovery rehearsal on an isolated instance is not run.
- The establishment signal is `credential_authorities.established_in_incident`, stamped with the
  containing incident only when a password establishment consumes an authority (never by a
  sweep or a federated claim). The real ways back are the operator reset (refused for linked
  accounts) and the break-glass reset; recovery-code consumption and a federated claim refuse a
  contained account (negative tests). Release applies containment's org classification and reads
  the target's own identities, which an org incident now lists per member.
- Maintainer security review and the human merge gate remain required. Cross-provider review
  did not run and is not CLEAN.
