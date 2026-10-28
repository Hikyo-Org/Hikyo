# Full bounded failed evidence: completion-c08

The original validated nonsecret artifact is reproduced in full. It proves this failed historical result and contains only synthetic namespace identities and closed diagnostics. Independent final cleanup does not change acceptance to pass. Raw logs and private provider/session material were not fetched.

## evidence.json

Original raw SHA256 `59d3c54de2686fb60066bdf3717095bb8fd982b9e21fb57c37e491ab146bb7df`.

```json
{"schema":"hikyo.aws-acceptance/v2","candidate_sha":"327dec159d680fab7b33a251ab46a2d1c16e9fa1","workflow_sha":"9d620dd451a56408ccab8360283dfeef0320eac7","run_id":"40485386416","status":"failed-reconcile-before-rerun","stage":"real-suite","account":"099256073061","region":"eu-central-1","prefix":"hikyo-e2e-20261027c08/","recorded_arns":["arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/sqlite/document-H0aiSh","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/sqlite/keys/ALPHA-Xteig8","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/sqlite/keys/SHARED_KEY-o2EP6d","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/postgres/document-zIeZNs","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/postgres/keys/ALPHA-iKLxh4","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/postgres/keys/SHARED_KEY-ZqGd6l","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/postgres/adopted-aP2Oje","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/postgres/moved-document-g1zUXQ","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/ambient/sqlite/role-document-DhyQx8","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/ambient/postgres/role-document-o79nM7","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/assume-role/sqlite/role-document-jyMcUT","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/assume-role/postgres/role-document-sLQnH1","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/web-identity/sqlite/role-document-Rm9y1V","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/web-identity/postgres/role-document-6HDjD4","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/ambient/iam-canary-UumhjJ","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/sqlite/version-before-adopt-xytksQ","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/postgres/version-before-adopt-S77fWS","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/sqlite/version-after-adopt-yiYMRy","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/postgres/version-after-adopt-jLZbGl","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/sqlite/replacement-after-adopt-kPyf3U","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/sqlite/replacement-after-adopt-cubkWD","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/postgres/replacement-after-adopt-h921hc","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/postgres/replacement-after-adopt-8XMWHQ","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/sqlite/promoted-finish-loss-CDNr9j","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/postgres/promoted-finish-loss-n0t9xV","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/sqlite/promoted-marker-refusal-2rUn5L","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/postgres/promoted-marker-refusal-VNwFZq"],"verified_absent_arns":["arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/assume-role/postgres/role-document-sLQnH1","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/web-identity/sqlite/role-document-Rm9y1V","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/postgres/adopted-aP2Oje","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/web-identity/postgres/role-document-6HDjD4","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/postgres/version-after-adopt-jLZbGl","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/postgres/promoted-marker-refusal-VNwFZq","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/sqlite/document-H0aiSh","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/sqlite/keys/SHARED_KEY-o2EP6d","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/sqlite/keys/ALPHA-Xteig8","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/postgres/version-before-adopt-S77fWS","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/sqlite/replacement-after-adopt-cubkWD","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/ambient/sqlite/role-document-DhyQx8","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/sqlite/version-after-adopt-yiYMRy","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/postgres/promoted-finish-loss-n0t9xV","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/postgres/moved-document-g1zUXQ","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/ambient/postgres/role-document-o79nM7","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/sqlite/replacement-after-adopt-kPyf3U","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/postgres/replacement-after-adopt-h921hc","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/sqlite/version-before-adopt-xytksQ","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/ambient/iam-canary-UumhjJ","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/sqlite/promoted-marker-refusal-2rUn5L","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/postgres/document-zIeZNs","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/postgres/replacement-after-adopt-8XMWHQ","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/postgres/keys/SHARED_KEY-ZqGd6l","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/postgres/keys/ALPHA-iKLxh4","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/static/sqlite/promoted-finish-loss-CDNr9j","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c08/assume-role/sqlite/role-document-jyMcUT"],"unresolved_names":[],"historical_cleanup":{"arn":null,"status":"not-requested","delete_attempts":0,"delete_response_exact_arn":false,"metadata_verified":false,"absence_checks":0,"verified_absent":false},"tests":{"TestAWSSecretsManagerRealSmoke/preflight-static":"pass","TestAWSSecretsManagerRealSmoke/preflight-ambient":"pass","TestAWSSecretsManagerRealSmoke/preflight-assume-role":"pass","TestAWSSecretsManagerRealSmoke/preflight-web-identity":"pass","TestAWSSecretsManagerRealSmoke/preflight-kms-denied":"pass","TestAWSSecretsManagerRealSmoke/auth-boundaries/expired-static":"pass","TestAWSSecretsManagerRealSmoke/auth-boundaries/expired-assume-role":"pass","TestAWSSecretsManagerRealSmoke/auth-boundaries/expired-ambient":"pass","TestAWSSecretsManagerRealSmoke/auth-boundaries/expired-web-identity":"pass","TestAWSSecretsManagerRealSmoke/auth-boundaries/configured-identity-refusal":"pass","TestAWSSecretsManagerRealSmoke/auth-boundaries":"pass","TestAWSSecretsManagerRealSmoke/lifecycle/sqlite":"fail","TestAWSSecretsManagerRealSmoke/lifecycle/postgres":"pass","TestAWSSecretsManagerRealSmoke/lifecycle":"fail","TestAWSSecretsManagerRealSmoke/role-auth/ambient/sqlite":"pass","TestAWSSecretsManagerRealSmoke/role-auth/ambient/postgres":"pass","TestAWSSecretsManagerRealSmoke/role-auth/ambient":"pass","TestAWSSecretsManagerRealSmoke/role-auth/assume-role/sqlite":"pass","TestAWSSecretsManagerRealSmoke/role-auth/assume-role/postgres":"pass","TestAWSSecretsManagerRealSmoke/role-auth/assume-role":"pass","TestAWSSecretsManagerRealSmoke/role-auth/web-identity/sqlite":"pass","TestAWSSecretsManagerRealSmoke/role-auth/web-identity/postgres":"pass","TestAWSSecretsManagerRealSmoke/role-auth/web-identity":"pass","TestAWSSecretsManagerRealSmoke/role-auth":"pass","TestAWSSecretsManagerRealSmoke/kms-denial/sqlite":"pass","TestAWSSecretsManagerRealSmoke/kms-denial/postgres":"pass","TestAWSSecretsManagerRealSmoke/kms-denial":"pass","TestAWSSecretsManagerRealSmoke/iam-denial":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness/version-before-adopt/sqlite":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness/version-before-adopt/postgres":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness/version-before-adopt":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness/version-after-adopt/sqlite":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness/version-after-adopt/postgres":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness/version-after-adopt":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness/replacement-after-adopt/sqlite":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness/replacement-after-adopt/postgres":"fail","TestAWSSecretsManagerRealSmoke/adoption-witness/replacement-after-adopt":"fail","TestAWSSecretsManagerRealSmoke/adoption-witness":"fail","TestAWSSecretsManagerRealSmoke/promoted-replay/finish-loss/sqlite":"pass","TestAWSSecretsManagerRealSmoke/promoted-replay/finish-loss/postgres":"pass","TestAWSSecretsManagerRealSmoke/promoted-replay/finish-loss":"pass","TestAWSSecretsManagerRealSmoke/promoted-replay/marker-refusal/sqlite":"pass","TestAWSSecretsManagerRealSmoke/promoted-replay/marker-refusal/postgres":"pass","TestAWSSecretsManagerRealSmoke/promoted-replay/marker-refusal":"pass","TestAWSSecretsManagerRealSmoke/promoted-replay":"pass","TestAWSSecretsManagerRealSmoke":"fail"},"maximum_create_reservations":32,"creation_reservations":["hikyo-e2e-20261027c08/static/sqlite/document","hikyo-e2e-20261027c08/static/sqlite/keys/ALPHA","hikyo-e2e-20261027c08/static/sqlite/keys/SHARED_KEY","hikyo-e2e-20261027c08/static/postgres/document","hikyo-e2e-20261027c08/static/postgres/keys/ALPHA","hikyo-e2e-20261027c08/static/postgres/keys/SHARED_KEY","hikyo-e2e-20261027c08/static/postgres/adopted","hikyo-e2e-20261027c08/static/postgres/moved-document","hikyo-e2e-20261027c08/ambient/sqlite/role-document","hikyo-e2e-20261027c08/ambient/postgres/role-document","hikyo-e2e-20261027c08/assume-role/sqlite/role-document","hikyo-e2e-20261027c08/assume-role/postgres/role-document","hikyo-e2e-20261027c08/web-identity/sqlite/role-document","hikyo-e2e-20261027c08/web-identity/postgres/role-document","hikyo-e2e-20261027c08/kms-denied/sqlite/denied-document","hikyo-e2e-20261027c08/kms-denied/postgres/denied-document","hikyo-e2e-20261027c08/ambient/iam-canary","hikyo-e2e-20261027c08/static/sqlite/version-before-adopt","hikyo-e2e-20261027c08/static/postgres/version-before-adopt","hikyo-e2e-20261027c08/static/sqlite/version-after-adopt","hikyo-e2e-20261027c08/static/postgres/version-after-adopt","hikyo-e2e-20261027c08/static/sqlite/replacement-after-adopt","hikyo-e2e-20261027c08/static/sqlite/replacement-after-adopt","hikyo-e2e-20261027c08/static/postgres/replacement-after-adopt","hikyo-e2e-20261027c08/static/postgres/replacement-after-adopt","hikyo-e2e-20261027c08/static/sqlite/promoted-finish-loss","hikyo-e2e-20261027c08/static/postgres/promoted-finish-loss","hikyo-e2e-20261027c08/static/sqlite/promoted-marker-refusal","hikyo-e2e-20261027c08/static/postgres/promoted-marker-refusal"],"complete_creation_ledger":false,"diagnostics":[{"test":"TestAWSSecretsManagerRealSmoke/lifecycle/postgres","operation":"TagResource","code":"ThrottlingException"},{"test":"TestAWSSecretsManagerRealSmoke/lifecycle/postgres","operation":"UpdateSecretVersionStage","code":"InvalidParameterException"},{"test":"TestAWSSecretsManagerRealSmoke/kms-denial/sqlite","operation":"CreateSecret","code":"AccessDeniedException"},{"test":"TestAWSSecretsManagerRealSmoke/kms-denial/postgres","operation":"CreateSecret","code":"AccessDeniedException"}],"diagnostics_truncated":false,"failure_assertions":[{"test":"TestAWSSecretsManagerRealSmoke/lifecycle/sqlite","assertion":"assertion-066"},{"test":"TestAWSSecretsManagerRealSmoke/adoption-witness/replacement-after-adopt/postgres","assertion":"assertion-013"}],"failure_assertions_truncated":false}
```

## HOSTED-EVIDENCE-RECEIPT.json

Original raw SHA256 `2997ee5576040f7a217d02af794aac2f4d6c60b408d7fd83f623393fab05c658`.

```json
{
  "at": "2026-10-27T10:38:23.241306+00:00",
  "runId": "40485386416",
  "attempt": 1,
  "runConclusion": "failure",
  "artifact": {
    "id": 12727965264,
    "node_id": "MDg6QXJ0aWZhY3QxMjcyNzk2NTI2NA==",
    "name": "aws-158-40485386416-327dec159d680fab7b33a251ab46a2d1c16e9fa1",
    "size_in_bytes": 1679,
    "url": "https://api.github.com/repos/Hikyo-Org/Hikyo/actions/artifacts/12727965264",
    "archive_download_url": "https://api.github.com/repos/Hikyo-Org/Hikyo/actions/artifacts/12727965264/zip",
    "expired": false,
    "digest": "sha256:e35c8f4b3414d7b9fe5541e2d34834283df61cf3ba284d3de9cce4dda06d1ee8",
    "created_at": "2026-10-27T10:33:49Z",
    "updated_at": "2026-10-27T10:33:49Z",
    "expires_at": "2026-11-10T10:33:49Z",
    "workflow_run": {
      "id": 40485386416,
      "repository_id": 1316165429,
      "head_repository_id": 1316165429,
      "head_branch": "main",
      "head_sha": "9d620dd451a56408ccab8360283dfeef0320eac7"
    }
  },
  "artifactZipSHA256": "e35c8f4b3414d7b9fe5541e2d34834283df61cf3ba284d3de9cce4dda06d1ee8",
  "evidenceSHA256": "59d3c54de2686fb60066bdf3717095bb8fd982b9e21fb57c37e491ab146bb7df",
  "evidenceStatus": "failed-reconcile-before-rerun",
  "stage": "real-suite",
  "tests": {
    "TestAWSSecretsManagerRealSmoke/preflight-static": "pass",
    "TestAWSSecretsManagerRealSmoke/preflight-ambient": "pass",
    "TestAWSSecretsManagerRealSmoke/preflight-assume-role": "pass",
    "TestAWSSecretsManagerRealSmoke/preflight-web-identity": "pass",
    "TestAWSSecretsManagerRealSmoke/preflight-kms-denied": "pass",
    "TestAWSSecretsManagerRealSmoke/auth-boundaries/expired-static": "pass",
    "TestAWSSecretsManagerRealSmoke/auth-boundaries/expired-assume-role": "pass",
    "TestAWSSecretsManagerRealSmoke/auth-boundaries/expired-ambient": "pass",
    "TestAWSSecretsManagerRealSmoke/auth-boundaries/expired-web-identity": "pass",
    "TestAWSSecretsManagerRealSmoke/auth-boundaries/configured-identity-refusal": "pass",
    "TestAWSSecretsManagerRealSmoke/auth-boundaries": "pass",
    "TestAWSSecretsManagerRealSmoke/lifecycle/sqlite": "fail",
    "TestAWSSecretsManagerRealSmoke/lifecycle/postgres": "pass",
    "TestAWSSecretsManagerRealSmoke/lifecycle": "fail",
    "TestAWSSecretsManagerRealSmoke/role-auth/ambient/sqlite": "pass",
    "TestAWSSecretsManagerRealSmoke/role-auth/ambient/postgres": "pass",
    "TestAWSSecretsManagerRealSmoke/role-auth/ambient": "pass",
    "TestAWSSecretsManagerRealSmoke/role-auth/assume-role/sqlite": "pass",
    "TestAWSSecretsManagerRealSmoke/role-auth/assume-role/postgres": "pass",
    "TestAWSSecretsManagerRealSmoke/role-auth/assume-role": "pass",
    "TestAWSSecretsManagerRealSmoke/role-auth/web-identity/sqlite": "pass",
    "TestAWSSecretsManagerRealSmoke/role-auth/web-identity/postgres": "pass",
    "TestAWSSecretsManagerRealSmoke/role-auth/web-identity": "pass",
    "TestAWSSecretsManagerRealSmoke/role-auth": "pass",
    "TestAWSSecretsManagerRealSmoke/kms-denial/sqlite": "pass",
    "TestAWSSecretsManagerRealSmoke/kms-denial/postgres": "pass",
    "TestAWSSecretsManagerRealSmoke/kms-denial": "pass",
    "TestAWSSecretsManagerRealSmoke/iam-denial": "pass",
    "TestAWSSecretsManagerRealSmoke/adoption-witness/version-before-adopt/sqlite": "pass",
    "TestAWSSecretsManagerRealSmoke/adoption-witness/version-before-adopt/postgres": "pass",
    "TestAWSSecretsManagerRealSmoke/adoption-witness/version-before-adopt": "pass",
    "TestAWSSecretsManagerRealSmoke/adoption-witness/version-after-adopt/sqlite": "pass",
    "TestAWSSecretsManagerRealSmoke/adoption-witness/version-after-adopt/postgres": "pass",
    "TestAWSSecretsManagerRealSmoke/adoption-witness/version-after-adopt": "pass",
    "TestAWSSecretsManagerRealSmoke/adoption-witness/replacement-after-adopt/sqlite": "pass",
    "TestAWSSecretsManagerRealSmoke/adoption-witness/replacement-after-adopt/postgres": "fail",
    "TestAWSSecretsManagerRealSmoke/adoption-witness/replacement-after-adopt": "fail",
    "TestAWSSecretsManagerRealSmoke/adoption-witness": "fail",
    "TestAWSSecretsManagerRealSmoke/promoted-replay/finish-loss/sqlite": "pass",
    "TestAWSSecretsManagerRealSmoke/promoted-replay/finish-loss/postgres": "pass",
    "TestAWSSecretsManagerRealSmoke/promoted-replay/finish-loss": "pass",
    "TestAWSSecretsManagerRealSmoke/promoted-replay/marker-refusal/sqlite": "pass",
    "TestAWSSecretsManagerRealSmoke/promoted-replay/marker-refusal/postgres": "pass",
    "TestAWSSecretsManagerRealSmoke/promoted-replay/marker-refusal": "pass",
    "TestAWSSecretsManagerRealSmoke/promoted-replay": "pass",
    "TestAWSSecretsManagerRealSmoke": "fail"
  },
  "failureAssertions": [
    {
      "test": "TestAWSSecretsManagerRealSmoke/lifecycle/sqlite",
      "assertion": "assertion-066"
    },
    {
      "test": "TestAWSSecretsManagerRealSmoke/adoption-witness/replacement-after-adopt/postgres",
      "assertion": "assertion-013"
    }
  ],
  "failureAssertionsTruncated": false,
  "recordedArnCount": 27,
  "verifiedAbsentArnCount": 27,
  "unresolvedNames": [],
  "reservationCount": 29,
  "completeCreationLedger": false,
  "historicalCleanup": {
    "arn": null,
    "status": "not-requested",
    "delete_attempts": 0,
    "delete_response_exact_arn": false,
    "metadata_verified": false,
    "absence_checks": 0,
    "verified_absent": false
  },
  "rawPrivateProjectionDownloaded": false
}
```

## FINAL-CLEANUP-RECEIPT.json

Original raw SHA256 `794c73583f5923109f57a703bf2c54d669bba9df427c220a1d50467b164f3e45`.

```json
{
  "at": "2026-10-27T10:55:08.499250+00:00",
  "status": "C08_CLEANUP_VERIFIED_ACCEPTANCE_FAILED_NO_FURTHER_EXECUTION_AUTHORITY",
  "runId": "40485386416",
  "attempt": 1,
  "candidate": "327dec159d680fab7b33a251ab46a2d1c16e9fa1",
  "workflow": "9d620dd451a56408ccab8360283dfeef0320eac7",
  "acceptance": "FAILED_2_LEAVES_40_PASS_6_FAIL_RECORDS",
  "hostedCompleteCreationLedgerFlagPreserved": false,
  "knownARNsStrictNotFound": 27,
  "allKnownARNAndReservedNameTargetsStrictNotFound": 29,
  "completeNamespaceIncludingPlannedDeletion": 0,
  "ownedRolesAbsent": 8,
  "ownedOIDCIssuerAbsent": true,
  "ownedBoundaryAbsent": true,
  "setupDefault": "v5",
  "originalV5RestoredExactly": true,
  "retainedVersions": [
    "v2",
    "v3",
    "v4",
    "v5",
    "v8"
  ],
  "retainedCreationDatesUnchanged": true,
  "setupUserIdentityMFAAndSoleAttachmentPreserved": true,
  "setupKeysGroupsInlinePolicies": 0,
  "executionVariables": 0,
  "protectedEnvironmentAndOIDCPreserved": true,
  "aliasAbsent": "alias/hikyo-158-20261027c08",
  "keyId": "b71f3c57-626a-4a89-9fcf-f7901a421e49",
  "keyState": "PendingDeletion",
  "keyDeletionDateUTC": "2026-11-03T10:51:15.444Z",
  "physicalKeyDeletionClaimed": false,
  "keyCancellationCalls": 0,
  "allPriorKeySchedulesUnchanged": true,
  "completeCustomerKeyInventory": 6,
  "newKeyCreations": 1,
  "newKeysAcrossC05ThroughC08": 4,
  "newReservationsCharged": 32,
  "creationReservationAttempts": 29,
  "unusedSlotsClosedWithoutRefund": 3,
  "lifetimeReservationsCharged": 240,
  "allOldReservesRetained": true,
  "budget": {
    "displayedGrossUSD": "0.05",
    "displayedBillableUSD": "0.00",
    "displayedTaxUSD": "0.00",
    "allOldReservesRetainedUSD": "2.80",
    "newReserveUSD": "0.83",
    "usageForecastUSD": "3.68",
    "grossAllReservesUSD": "4.68",
    "billableAllReservesUSD": "4.63",
    "usageCeilingUSD": "3.75"
  },
  "reviews": "5/5 consumed; owner disposition, no independent CLEAN or sixth review",
  "crossProvider": "unavailable",
  "applicationPR969Merged": false,
  "issue158Closed": false,
  "frozenPreparationPinsVerified": 73,
  "nativeReceipts": {
    "c08-final-iam-closure": "2f1d03c082c42e668a86538a735789ada38c758878678f891f8e8d766723987c",
    "c08-final-secret-closure": "4e63db2ff1c3c9cd9880364939b423441848f053da7b62f8589491b6253d0867",
    "c08-final-kms-reconciliation": "131916cbce1493d9dbdb5d785b8adbae4f328c3c0f25e33d397e00e7f94174da",
    "c08-final-kms-complete": "9d4a1a6479ce59effad597427c9f41bcc67151f5e8e3f948761e6d70d1883ed1",
    "c08-final-billing": "2aa8cf123ad0ac82632277206ae87e86d3ba66e98e78abfb54e920fd02b51882"
  },
  "independentAbsenceSHA256": "15f79658fb236829cb3156611df530ea27b55a59591efd5ab90951b3ff93ff20",
  "namespaceQuiescenceSHA256": "93e5f565456b241b7938736798d3110a48897f2adecb4ac151495eb79de3cca0",
  "githubClosureSHA256": "f3953d5cb50c8a5bc1d86f9d3ee355e8c5e09c05b851cda1c15d2ef683f61610",
  "artifactZipSHA256": "e35c8f4b3414d7b9fe5541e2d34834283df61cf3ba284d3de9cce4dda06d1ee8"
}
```
