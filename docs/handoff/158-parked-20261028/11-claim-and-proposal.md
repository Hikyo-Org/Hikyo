# C12 claim, proposal and source pins

Historical data, currently PARKED. A null window and unused slots are intentional. Original raw and canonical digests have different meanings; no displayed data is a fresh execution approval.

## ACTIVE-CLAIM.json

Original raw SHA256 `fea113e1367f578b3a0626068532d242826c597edf6f995647f3bcf9e9725e20`.

```json
{
  "claimedAt": "2026-10-28T00:59:07.176595+00:00",
  "kind": "EXPLICIT_C12_BOUNDED_COMPLETION_SUCCESSOR",
  "proposalCanonicalSHA256": "77f086009324260f41d6ca10e2b7a63cbc328fe4935f0865d80931665006b2d9",
  "ownerApprovalSHA256": "845b46c2c166cf66ea9d5bdd61cfdc8278af26c2656c54f5054a28f740e02e6b",
  "ownerApprovalPosition": 3828,
  "ownerApprovalAt": "2026-10-28T00:56:34.014Z",
  "adoptedSourceSHA256": "cc3c4402ce67a0bb7123a4e8492107ee50eb8caf336286eca05744f73475e146",
  "account": "099256073061",
  "region": "eu-central-1",
  "namespace": "hikyo-e2e-20261028c12/",
  "alias": "alias/hikyo-158-20261028c12",
  "candidate": "c253ff9b1ccec7408f13366f8c5640c8c7cf3d0a",
  "trustedMainObserved": "24e198b32a3ffd8d4013207b2f974d76ec49be6f",
  "newReservationsCharged": 32,
  "previousReservationsRetained": 304,
  "lifetimeReservations": 336,
  "maximumCreateSecretCalls": 32,
  "newAdditionalHostedAttemptAllocation": 1,
  "maximumAdditionalHostedDispatches": 1,
  "maximumHostedAttemptNumber": 1,
  "thisGenerationAdditionalKeyLimit": 1,
  "alreadyAttemptedCreationSlots": 0,
  "alreadyAttemptedHostedDispatches": 0,
  "alreadyCreatedNewKeys": 0,
  "keyCancellationCallsAllowed": 0,
  "refund": 0,
  "automaticRetry": false,
  "window": null,
  "windowRequiresLaterAuthenticatedReady": true,
  "bothPrivateConsoleSignInsBeforeReady": true,
  "officialCLIAuthorizationAfterApprovedIAMBootstrap": true,
  "sourcePublicationRequiredBeforeAWSMutation": true,
  "exactFinalPoliciesApprovalStillRequired": true,
  "pendingDeploymentApprovalStillRequired": true,
  "humanApplicationMergeApprovalRequired": true,
  "reviewRoundsConsumed": 5,
  "maximumReviews": 5,
  "independentReview": "OWNER_ADOPTED_FROZEN_C12_SOURCE_NO_NEW_CLEAN",
  "allPreviousReservesRetainedUSD": "6.12",
  "additionalReserveUSD": "0.83",
  "usageCeilingUSD": "7.15",
  "overallTargetStrictlyBelowUSD": "9.00",
  "freshBillingAndPricingStillRequired": true,
  "previousImmutableClaimAndClosurePins": {
    "owner-assisted-20261022c05.once.json": "f178b7c27759ad46b2c08b7e2d2daa147af23678affcc96110e4348ee00bbaa1",
    "owner-assisted-20261026c07.continuation.once.json": "5e9b86693e4c06b961487a1eef36a87bee667c54e0330ad4ce3ef0ecb8cedb57",
    "owner-assisted-20261026c07.renewal.once.json": "113f00ce116c9b8e238b9b40cb0f64d45a9fe84157e7f553bdcb5fa10e149ca2",
    "owner-assisted-20261026c07.continuation.closed.json": "807186c0860645ef0a7025a4287b402a137c95fb06839c4bbcc549b734f8296d",
    "owner-assisted-20261026c07.renewal.closed.json": "807186c0860645ef0a7025a4287b402a137c95fb06839c4bbcc549b734f8296d",
    "owner-assisted-20261027c08.completion.once.json": "59c49708965145bc265daf2faaaea348d0d94bf1e6f9f7b280a094a3b324235d",
    "owner-assisted-20261027c08.completion.closed.json": "7f7547e16590ee27840b728593d2b87a5272f2fdd3cd4c30f7ff4721ec8cdf65",
    "owner-assisted-20261027c08.window.once.json": "fed04f360980d49d2e3e0e1fe0700e905799c87287bd92050a5d29d0ef3e69c0",
    "owner-assisted-20261027c09.completion.once.json": "06c932a4044c2c590d81a19d01fbc03e96393f56e9d37785078331dbc3987113",
    "owner-assisted-20261027c09.completion.closed.json": "a9b6d1b77ba1a2d4426cc82ce636e3ea2970c2148705ae0be1f9e2e6c03e1467",
    "owner-assisted-20261027c09.window.once.json": "e4a6c77c81397fa70cfbd3b3d42c3bd1d21286cc0141a53f56ed773511bf1471",
    "owner-assisted-20261027c10.completion.once.json": "40b21960c0109440d08cb3e5aaad243b8efd434728a329523c3b2159ac3a15d2",
    "owner-assisted-20261027c10.completion.closed.json": "260c1f062d4316474fc355c4b4c8148067561da22006487e718092191cabce38",
    "owner-assisted-20261027c10.window.once.json": "cc8647cedbc8892d7cda67e23acee49d72071f49a172470115567d259e2153a9",
    "owner-assisted-20261027c11.reassign-c10.once.json": "08a101286f0c30f97f0c97a6e8cc9671252eb39ee77149ab7ff323ecf0d6eec9",
    "owner-assisted-20261027c11.reassign-c10.closed.json": "72867f7734ad0c74551ec75b75f705c9ba90570b6361b331c17a4775c468d041",
    "owner-assisted-20261027c11.window.once.json": "a3c893fc5b0de472879eac527493ce0b8001cda6db08f86f0af9e5f0e9c74b71"
  },
  "parentClosedC11ClaimSHA256": "08a101286f0c30f97f0c97a6e8cc9671252eb39ee77149ab7ff323ecf0d6eec9",
  "parentClosedC11ReceiptSHA256": "72867f7734ad0c74551ec75b75f705c9ba90570b6361b331c17a4775c468d041",
  "maximumNewKeysAcrossC05ThroughC12": 8,
  "poolReassignment": false,
  "redispatch": false
}
```

## FINISH-158-PROPOSAL.json

Original raw SHA256 `958428f506c4acc8e8be1e2867788363a51c1a8670e7191095b582125e05caa9`.

```json
{
  "schema": "hikyo-158/c12-bounded-successor-proposal/v1",
  "preparedAt": "2026-10-28T00:39:34.678010+00:00",
  "status": "PROPOSAL_ONLY_NO_EXECUTION_AUTHORITY",
  "goal": "Finish issue158 through genuine AWS acceptance, independently verified cleanup, final current-base CI, human application merge and actual issue closure. Controller qualification remains issue1057.",
  "account": "099256073061",
  "region": "eu-central-1",
  "candidate": "c253ff9b1ccec7408f13366f8c5640c8c7cf3d0a",
  "trustedMain": "24e198b32a3ffd8d4013207b2f974d76ec49be6f",
  "sourceDisposition": {
    "proposedOwnerAdoptionOnly": true,
    "manifestCanonicalSHA256": "cc3c4402ce67a0bb7123a4e8492107ee50eb8caf336286eca05744f73475e146",
    "applicationRepairManifestCanonicalSHA256": "45d8bd283104aae7a6bd73e74ee156560221dd071eda1d3ab99941419fc7282a",
    "changes": [
      "After one explicit consent-tag mutation, observe the exact ARN at most three times in the same 15-second tag/observation deadline before the queued retry; preserve identity/owner/current-version/marker/key/deletion/refusal/cancellation and mutation/version/creation counts.",
      "Local emulator models AWSPREVIOUS movement when PutSecretValue moves AWSCURRENT, retaining idempotent and custom-stage behavior.",
      "C12 data worksheet uses one fresh charged pool and attempt, not C11 closed slots; add exact C11 schedule and C12 absence selector to read-only reconciliation; preserve all prior schedules.",
      "Native IAM limiter changes only owned setup deletion literal v10 to v11; current-head GitHub correspondence uses the new application SHA and explicitly permits only terminal historical C11 run40600194878 in its existing run inventory."
    ],
    "localFixtureTerminalRecordsPassed": 208,
    "localAdapterConsumerTerminalRecordsPassed": 218,
    "offlineProposalPythonTestsPassed": 16,
    "nativeGuardPositiveScenariosPassed": 4,
    "nativeGuardNegativeScenariosPassed": 13,
    "c11ActualCauseEstablished": false,
    "independentCLEAN": false,
    "reviewRoundsConsumed": 5,
    "reviewRoundsMaximum": 5,
    "sixthReviewGranted": false
  },
  "closedParent": {
    "claimSHA256": "08a101286f0c30f97f0c97a6e8cc9671252eb39ee77149ab7ff323ecf0d6eec9",
    "closureCanonicalSHA256": "72867f7734ad0c74551ec75b75f705c9ba90570b6361b331c17a4775c468d041",
    "hostedAttemptConsumed": true,
    "unusedSlotsClosedChargedWithoutRefund": 3,
    "reuseOrReassignment": false
  },
  "newPool": {
    "requiresExplicitOwnerException": true,
    "additionalHostedAttemptAllocation": 1,
    "maximumActualDispatches": 1,
    "maximumWorkflowAttempt": 1,
    "newReservationsCharged": 32,
    "allLifetimeReservationsRemainCharged": 336,
    "maximumCreateSecretCalls": 32,
    "allPriorReservationsRemainCharged": true,
    "automaticRetryOrRedispatch": false,
    "refund": false,
    "cancellation": false
  },
  "resourceException": {
    "newKeyAllowance": 1,
    "newKeyMaximumAcrossC05ThroughC12": 8,
    "alias": "alias/hikyo-158-20261028c12",
    "namespace": "hikyo-e2e-20261028c12/",
    "externalId": "hikyo-158-20261028c12",
    "maximumRoles": 8,
    "maximumBoundaries": 1,
    "maximumIssuers": 1,
    "reusePendingDeletionKey": false,
    "cancelAnyDeletionSchedule": false,
    "allPriorSchedulesAndReservesRetained": true
  },
  "IAMHistory": {
    "initialDefaultMustBeExactV5": true,
    "retainVersions": [
      "v2",
      "v3",
      "v4",
      "v5"
    ],
    "archiveDeleteOnlyOwnedNondefault": "v11",
    "ownedV11DocumentCanonicalSHA256": "1ca190778d617cd90ec21e757b2b286d8f6c3f9110d7a6f44637b29cdd86f2f3",
    "ownedV11CreateDate": "2026-10-27T23:03:49Z",
    "maximumNewSetupVersions": 1,
    "newSetupVersionUsesActualAcknowledgedID": true,
    "maximumNewBoundaries": 1,
    "restoreExactV5DuringCleanup": true,
    "retainNewSetupVersionAndAllV2V5Dates": true,
    "newPermissionsBeyondThe18Templates": false
  },
  "budgetException": {
    "allOldReservesRetainedUSD": "6.12",
    "newReserveUSD": "0.83",
    "reserveComponentsUSD": {
      "key": "0.24",
      "storage": "0.43",
      "API": "0.10",
      "rounding": "0.06"
    },
    "oldUsageCeilingUSD": "6.30",
    "proposedUsageCeilingUSD": "7.15",
    "oldOverallTargetStrictlyBelowUSD": "8.00",
    "proposedOverallTargetStrictlyBelowUSD": "9.00",
    "modelCeilingUSD": "10.00",
    "hardGrossAndBillableEachStrictlyBelowUSD": "15.00",
    "taxFeeFloorUSD": "1.00",
    "historicalNativeSnapshot": {
      "at": "2026-10-27T23:52:07.698Z",
      "grossUSD": "0.07",
      "billableUSD": "0.00",
      "taxUSD": "0.00",
      "freshBeforeNextKey": false
    },
    "historicalSnapshotProjectionUSD": {
      "usage": "7.02",
      "gross": "8.02",
      "billable": "7.95"
    },
    "freshCompleteBillingRegionalPricesAndInventoryBeforeAnyNewKey": true,
    "unknownOrOverLimitHoldsSpendingCleanupRemainsAuthorized": true,
    "providerHardCapClaimed": false
  },
  "window": {
    "startsWithThisGrant": false,
    "calendarWindowApproved": false,
    "fullActualClaimQualificationAndNativePreflightBeforeReady": true,
    "bothPrivateConsoleSignInsBeforeReady": true,
    "laterSeparateAuthenticatedReadyRequired": true,
    "t0MinutesAfterReady": 10,
    "roundUpToWholeUTCSecond": true,
    "maximumProvisioningMinutesAfterT0": 55,
    "maximumOrdinaryMutationHoursAfterT0": 2,
    "maximumSetupCleanupHoursAfterT0": 4,
    "ownerAvailableFirstMinutesAfterT0": 40,
    "automaticShift": false,
    "officialCLIInvocationsMaximum": 1,
    "officialCLILoginAfterApprovedIAMBootstrap": true,
    "onFailedAdmissionOrMissedCutoff": "Hold new spending/dispatch and retire exactly owned resources under cleanup authority. No automatic renewal."
  },
  "acceptanceAndApprovalGates": {
    "all46RealTerminalRecordsMustPass": true,
    "bothDatastoreEngines": true,
    "genuineAuthExpiryKMSIAM": true,
    "independentSyntheticValueOracle": true,
    "explicitWriterValueReadDenial": true,
    "literalSetupMFAListRolesAndUnusedSentinelAfterBootstrapBeforeFixture": true,
    "durableSameJobProcessReplayNoDuplicateVersion": true,
    "sourceDependencyWorkflowCorrespondence": true,
    "changedTrustedLogicRequiresNewConcreteOwnerDisposition": true,
    "exactFinal18PolicyHashApprovalSeparate": true,
    "exactPendingDeploymentApprovalSeparate": true,
    "fullCurrentBaseApplicationCIRequired": true,
    "finalHumanApplicationMergeApprovalSeparate": true,
    "independentCleanupAndRealIssueClosureRequired": true,
    "currentHeadApplicableChecksRequiredBeforeAnyNewKey": true
  },
  "noAuthorityFromRemindersOrElapsedTime": true,
  "remainingPreconditionsUnrun": [
    "Current-head applicable hosted checks completed and concrete authenticated c12 grant with immutable fresh charged claim",
    "Actual-claim-bound operators and full qualification, fresh owner/IAM/billing/prices/inventory, both separate private console sign-ins before Ready",
    "Later separate Ready binds T0 once, key readback and separate exact final 18-policy hash approval",
    "Approved IAM bootstrap, one official private setup CLI authorization, literal effective-MFA and unused-sentinel probes",
    "Exact fixture, one hosted dispatch, nine bindings and separate exact pending deployment approval",
    "All46 genuine acceptance records, independent cleanup, mandatory full current-base CI, final human application merge and actual issue closure"
  ]
}
```

## SOURCE-PUBLICATION-CANDIDATE.json

Original raw SHA256 `74537a1c447f9a453d947a56aebd36c2e8ea8d0b4039c741c2cc6b029f6f318a`.

```json
{
  "status": "PUBLISHED_TRUSTED_SOURCE_ONLY_NEW_APPLICATION_DISPOSITION_PROPOSED",
  "currentSourceFiles": {
    ".github/workflows/aws-acceptance.yml": "0bb9e4fe28055d03ba7a055c3e7595aa316b3869d22eddd59bfafe2d54314b6a",
    "scripts/ci/aws-acceptance/README.md": "177b718cd1a35223ee4e3faa0e1c0e0232a6d1acb2d1d4a385deb415890b6bf9",
    "scripts/ci/aws-acceptance/runner.py": "00f8117346bd4d50aa5defe4968333822b388a727966141b6bcd9236f28ff3d0",
    "scripts/ci/aws-acceptance/runtime-test2json.jsonl": "7aea54a7ff2eb049cae759950912577e9a79ac79f8a4d2979e6bb1620f186bba",
    "scripts/ci/aws-acceptance/test_evidence_schema.py": "f96cafc7f3e647adf850d4e5a1eb37d308443441189520c07d0bf364790a2c50",
    "scripts/ci/aws-acceptance/test_failure_assertions.py": "8bdd0bfdc71c085f2b226a99726f137ce0867ef0afb0cc427648e83100d7eed9",
    "scripts/ci/fast-feedback.sh": "cdac5c946d14b26295af2b397f5ce075a27d6757f1b0e48198502abfc1361187",
    "scripts/ci/fast-feedback_test.sh": "cfc0c4c59afcd4cc1880eeb90cf891bda7d9e3d28cc2fd63fa9f489c730f7f6c",
    "scripts/ci/release-admission/EXECUTION-MAP.json": "953782228802e479dbdc39afc25c22a07c386ca15909ec95b2ee250af0686519",
    "scripts/ci/release-admission/IMPLEMENTATION-MANIFEST.json": "ffdf52eaf7cae93205de057ca2fc74118b1aab23ede5e373dc7825cf08be3034",
    "scripts/ci/release-admission/adapters/SOURCE-PINS.json": "1fa92e3449a7c61993d7ddc2a89bfc01f263a90079ff8811e79a3101efd5eda3",
    "scripts/ci/release-admission/core/next_hosted_result.source-only.py": "33bbfaa610fe1bb95ee8b0574b13bb4f4cc361c6c15f041e66a0c074b8c768ba",
    "scripts/ci/release-admission/core/next_operator_orchestration.source-only.py": "1274ce7b4d0e36f071c27ac39e93b374f45949ccde2d8a9a828213ad558a3265",
    "scripts/ci/release-admission/core/next_operator_orchestration_stdout.source-only.py": "8b644a4788fc2027dadb87ccb8dfefd5e7219a23397a1a70362fc74e1a8ee92a",
    "scripts/ci/release-admission/core/next_owner_finalization.source-only.py": "6680b842358be939f0953963c353d2f8392465a59a28db64c8f45dc45dff42ce",
    "scripts/ci/release-admission/core/partial_setup_cleanup_inventory.source-only.py": "2bda713a1d9a216140ad23a7db64a57f234143fcaf14aa51fd61706f7786ee6b",
    "scripts/ci/release-admission/run_checks.py": "525ebfaeeae1684424b55de94e3bb419c845d1fbe7232f63990480e051b88836",
    "scripts/ci/release-admission/test_aws_acceptance_contract.py": "4362cd8666061c37257478db04aaeb2c7baa2c415bc593f1470551296979f02b"
  },
  "trustedMain": "24e198b32a3ffd8d4013207b2f974d76ec49be6f",
  "applicationCandidate": "c253ff9b1ccec7408f13366f8c5640c8c7cf3d0a",
  "independentCLEAN": false,
  "review6": false
}
```
