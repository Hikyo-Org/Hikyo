# Full bounded failed evidence: completion-c09

The original validated nonsecret artifact is reproduced in full. It proves this failed historical result and contains only synthetic namespace identities and closed diagnostics. Independent final cleanup does not change acceptance to pass. Raw logs and private provider/session material were not fetched.

## evidence.json

Original raw SHA256 `122be28ffe19fd13600e3b5e9da9a7f64ceff8a2381ef5ae1ae584d6ca70f85f`.

```json
{"schema":"hikyo.aws-acceptance/v2","candidate_sha":"f44f9fd717c1980fddc11ac6690a8a6e6e311104","workflow_sha":"9d620dd451a56408ccab8360283dfeef0320eac7","run_id":"40510334025","status":"failed-reconcile-before-rerun","stage":"real-suite","account":"099256073061","region":"eu-central-1","prefix":"hikyo-e2e-20261027c09/","recorded_arns":["arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/sqlite/document-fDqSti","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/sqlite/keys/ALPHA-baytTP","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/sqlite/keys/SHARED_KEY-zqU3S2","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/sqlite/adopted-tFGGvC","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/sqlite/moved-document-JRjYcX","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/postgres/document-erjMQX","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/postgres/keys/ALPHA-oLqc0c","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/postgres/keys/SHARED_KEY-tPXMmS","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/ambient/sqlite/role-document-GLniML","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/ambient/postgres/role-document-qWPD57","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/assume-role/sqlite/role-document-ujsrhi","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/assume-role/postgres/role-document-V6z3Gg","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/web-identity/sqlite/role-document-3Gkwec","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/web-identity/postgres/role-document-8oUZ14","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/ambient/iam-canary-VNbNLf","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/sqlite/version-before-adopt-PJxboU","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/postgres/version-before-adopt-lnYNba","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/sqlite/version-after-adopt-W51lCO","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/postgres/version-after-adopt-Q9uLCK","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/sqlite/replacement-after-adopt-wdJ9ct","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/sqlite/replacement-after-adopt-hIsDTz","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/postgres/replacement-after-adopt-QGp00L","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/postgres/replacement-after-adopt-EbZEy2","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/sqlite/promoted-finish-loss-M42v6Z","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/postgres/promoted-finish-loss-sPr1T3","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/sqlite/promoted-marker-refusal-kiB5iw","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/postgres/promoted-marker-refusal-Lt1D29"],"verified_absent_arns":["arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/sqlite/moved-document-JRjYcX","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/postgres/version-after-adopt-Q9uLCK","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/sqlite/keys/SHARED_KEY-zqU3S2","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/postgres/promoted-finish-loss-sPr1T3","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/sqlite/version-after-adopt-W51lCO","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/sqlite/replacement-after-adopt-wdJ9ct","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/ambient/iam-canary-VNbNLf","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/postgres/promoted-marker-refusal-Lt1D29","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/web-identity/sqlite/role-document-3Gkwec","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/postgres/version-before-adopt-lnYNba","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/web-identity/postgres/role-document-8oUZ14","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/assume-role/sqlite/role-document-ujsrhi","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/assume-role/postgres/role-document-V6z3Gg","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/sqlite/promoted-finish-loss-M42v6Z","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/postgres/keys/ALPHA-oLqc0c","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/postgres/keys/SHARED_KEY-tPXMmS","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/postgres/replacement-after-adopt-EbZEy2","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/sqlite/document-fDqSti","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/postgres/replacement-after-adopt-QGp00L","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/sqlite/keys/ALPHA-baytTP","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/sqlite/replacement-after-adopt-hIsDTz","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/postgres/document-erjMQX","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/sqlite/promoted-marker-refusal-kiB5iw","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/sqlite/adopted-tFGGvC","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/static/sqlite/version-before-adopt-PJxboU","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/ambient/sqlite/role-document-GLniML","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c09/ambient/postgres/role-document-qWPD57"],"unresolved_names":[],"historical_cleanup":{"arn":null,"status":"not-requested","delete_attempts":0,"delete_response_exact_arn":false,"metadata_verified":false,"absence_checks":0,"verified_absent":false},"tests":{"TestAWSSecretsManagerRealSmoke/preflight-static":"pass","TestAWSSecretsManagerRealSmoke/preflight-ambient":"pass","TestAWSSecretsManagerRealSmoke/preflight-assume-role":"pass","TestAWSSecretsManagerRealSmoke/preflight-web-identity":"pass","TestAWSSecretsManagerRealSmoke/preflight-kms-denied":"pass","TestAWSSecretsManagerRealSmoke/auth-boundaries/expired-static":"pass","TestAWSSecretsManagerRealSmoke/auth-boundaries/expired-assume-role":"pass","TestAWSSecretsManagerRealSmoke/auth-boundaries/expired-ambient":"pass","TestAWSSecretsManagerRealSmoke/auth-boundaries/expired-web-identity":"pass","TestAWSSecretsManagerRealSmoke/auth-boundaries/configured-identity-refusal":"pass","TestAWSSecretsManagerRealSmoke/auth-boundaries":"pass","TestAWSSecretsManagerRealSmoke/lifecycle/sqlite":"pass","TestAWSSecretsManagerRealSmoke/lifecycle/postgres":"fail","TestAWSSecretsManagerRealSmoke/lifecycle":"fail","TestAWSSecretsManagerRealSmoke/role-auth/ambient/sqlite":"pass","TestAWSSecretsManagerRealSmoke/role-auth/ambient/postgres":"pass","TestAWSSecretsManagerRealSmoke/role-auth/ambient":"pass","TestAWSSecretsManagerRealSmoke/role-auth/assume-role/sqlite":"pass","TestAWSSecretsManagerRealSmoke/role-auth/assume-role/postgres":"pass","TestAWSSecretsManagerRealSmoke/role-auth/assume-role":"pass","TestAWSSecretsManagerRealSmoke/role-auth/web-identity/sqlite":"pass","TestAWSSecretsManagerRealSmoke/role-auth/web-identity/postgres":"fail","TestAWSSecretsManagerRealSmoke/role-auth/web-identity":"fail","TestAWSSecretsManagerRealSmoke/role-auth":"fail","TestAWSSecretsManagerRealSmoke/kms-denial/sqlite":"pass","TestAWSSecretsManagerRealSmoke/kms-denial/postgres":"pass","TestAWSSecretsManagerRealSmoke/kms-denial":"pass","TestAWSSecretsManagerRealSmoke/iam-denial":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness/version-before-adopt/sqlite":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness/version-before-adopt/postgres":"fail","TestAWSSecretsManagerRealSmoke/adoption-witness/version-before-adopt":"fail","TestAWSSecretsManagerRealSmoke/adoption-witness/version-after-adopt/sqlite":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness/version-after-adopt/postgres":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness/version-after-adopt":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness/replacement-after-adopt/sqlite":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness/replacement-after-adopt/postgres":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness/replacement-after-adopt":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness":"fail","TestAWSSecretsManagerRealSmoke/promoted-replay/finish-loss/sqlite":"pass","TestAWSSecretsManagerRealSmoke/promoted-replay/finish-loss/postgres":"pass","TestAWSSecretsManagerRealSmoke/promoted-replay/finish-loss":"pass","TestAWSSecretsManagerRealSmoke/promoted-replay/marker-refusal/sqlite":"fail","TestAWSSecretsManagerRealSmoke/promoted-replay/marker-refusal/postgres":"pass","TestAWSSecretsManagerRealSmoke/promoted-replay/marker-refusal":"fail","TestAWSSecretsManagerRealSmoke/promoted-replay":"fail","TestAWSSecretsManagerRealSmoke":"fail"},"maximum_create_reservations":32,"creation_reservations":["hikyo-e2e-20261027c09/static/sqlite/document","hikyo-e2e-20261027c09/static/sqlite/keys/ALPHA","hikyo-e2e-20261027c09/static/sqlite/keys/SHARED_KEY","hikyo-e2e-20261027c09/static/sqlite/adopted","hikyo-e2e-20261027c09/static/sqlite/moved-document","hikyo-e2e-20261027c09/static/postgres/document","hikyo-e2e-20261027c09/static/postgres/keys/ALPHA","hikyo-e2e-20261027c09/static/postgres/keys/SHARED_KEY","hikyo-e2e-20261027c09/ambient/sqlite/role-document","hikyo-e2e-20261027c09/ambient/postgres/role-document","hikyo-e2e-20261027c09/assume-role/sqlite/role-document","hikyo-e2e-20261027c09/assume-role/postgres/role-document","hikyo-e2e-20261027c09/web-identity/sqlite/role-document","hikyo-e2e-20261027c09/web-identity/postgres/role-document","hikyo-e2e-20261027c09/kms-denied/sqlite/denied-document","hikyo-e2e-20261027c09/kms-denied/postgres/denied-document","hikyo-e2e-20261027c09/ambient/iam-canary","hikyo-e2e-20261027c09/static/sqlite/version-before-adopt","hikyo-e2e-20261027c09/static/postgres/version-before-adopt","hikyo-e2e-20261027c09/static/sqlite/version-after-adopt","hikyo-e2e-20261027c09/static/postgres/version-after-adopt","hikyo-e2e-20261027c09/static/sqlite/replacement-after-adopt","hikyo-e2e-20261027c09/static/sqlite/replacement-after-adopt","hikyo-e2e-20261027c09/static/postgres/replacement-after-adopt","hikyo-e2e-20261027c09/static/postgres/replacement-after-adopt","hikyo-e2e-20261027c09/static/sqlite/promoted-finish-loss","hikyo-e2e-20261027c09/static/postgres/promoted-finish-loss","hikyo-e2e-20261027c09/static/sqlite/promoted-marker-refusal","hikyo-e2e-20261027c09/static/postgres/promoted-marker-refusal"],"complete_creation_ledger":false,"diagnostics":[{"test":"TestAWSSecretsManagerRealSmoke/lifecycle/sqlite","operation":"UpdateSecretVersionStage","code":"InvalidParameterException"},{"test":"TestAWSSecretsManagerRealSmoke/kms-denial/sqlite","operation":"CreateSecret","code":"AccessDeniedException"},{"test":"TestAWSSecretsManagerRealSmoke/kms-denial/postgres","operation":"CreateSecret","code":"AccessDeniedException"}],"diagnostics_truncated":false,"failure_assertions":[{"test":"TestAWSSecretsManagerRealSmoke/lifecycle/postgres","assertion":"assertion-066"},{"test":"TestAWSSecretsManagerRealSmoke/role-auth/web-identity/postgres","assertion":"assertion-066"},{"test":"TestAWSSecretsManagerRealSmoke/adoption-witness/version-before-adopt/postgres","assertion":"assertion-017"},{"test":"TestAWSSecretsManagerRealSmoke/promoted-replay/marker-refusal/sqlite","assertion":"assertion-160"}],"failure_assertions_truncated":false}
```

## HOSTED-EVIDENCE-RECEIPT.json

Original raw SHA256 `08cf90a029db2181d9d7eea5e63028a2fd7efe4e20e34cd645c7b6e16bd13e94`.

```json
{
  "at": "2026-10-27T14:10:14.229689+00:00",
  "runId": "40510334025",
  "attempt": 1,
  "runConclusion": "failure",
  "artifact": {
    "id": 12738459949,
    "node_id": "MDg6QXJ0aWZhY3QxMjczODQ1OTk0OQ==",
    "name": "aws-158-40510334025-f44f9fd717c1980fddc11ac6690a8a6e6e311104",
    "size_in_bytes": 1661,
    "url": "https://api.github.com/repos/Hikyo-Org/Hikyo/actions/artifacts/12738459949",
    "archive_download_url": "https://api.github.com/repos/Hikyo-Org/Hikyo/actions/artifacts/12738459949/zip",
    "expired": false,
    "digest": "sha256:871c0828cc36ec1b0ad12c3b0d29d4dc145e42dffab7b32dbfe3590b2a30c3d4",
    "created_at": "2026-10-27T14:05:38Z",
    "updated_at": "2026-10-27T14:05:38Z",
    "expires_at": "2026-11-10T14:05:37Z",
    "workflow_run": {
      "id": 40510334025,
      "repository_id": 1316165429,
      "head_repository_id": 1316165429,
      "head_branch": "main",
      "head_sha": "9d620dd451a56408ccab8360283dfeef0320eac7"
    }
  },
  "artifactZipSHA256": "871c0828cc36ec1b0ad12c3b0d29d4dc145e42dffab7b32dbfe3590b2a30c3d4",
  "evidenceSHA256": "122be28ffe19fd13600e3b5e9da9a7f64ceff8a2381ef5ae1ae584d6ca70f85f",
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
    "TestAWSSecretsManagerRealSmoke/lifecycle/sqlite": "pass",
    "TestAWSSecretsManagerRealSmoke/lifecycle/postgres": "fail",
    "TestAWSSecretsManagerRealSmoke/lifecycle": "fail",
    "TestAWSSecretsManagerRealSmoke/role-auth/ambient/sqlite": "pass",
    "TestAWSSecretsManagerRealSmoke/role-auth/ambient/postgres": "pass",
    "TestAWSSecretsManagerRealSmoke/role-auth/ambient": "pass",
    "TestAWSSecretsManagerRealSmoke/role-auth/assume-role/sqlite": "pass",
    "TestAWSSecretsManagerRealSmoke/role-auth/assume-role/postgres": "pass",
    "TestAWSSecretsManagerRealSmoke/role-auth/assume-role": "pass",
    "TestAWSSecretsManagerRealSmoke/role-auth/web-identity/sqlite": "pass",
    "TestAWSSecretsManagerRealSmoke/role-auth/web-identity/postgres": "fail",
    "TestAWSSecretsManagerRealSmoke/role-auth/web-identity": "fail",
    "TestAWSSecretsManagerRealSmoke/role-auth": "fail",
    "TestAWSSecretsManagerRealSmoke/kms-denial/sqlite": "pass",
    "TestAWSSecretsManagerRealSmoke/kms-denial/postgres": "pass",
    "TestAWSSecretsManagerRealSmoke/kms-denial": "pass",
    "TestAWSSecretsManagerRealSmoke/iam-denial": "pass",
    "TestAWSSecretsManagerRealSmoke/adoption-witness/version-before-adopt/sqlite": "pass",
    "TestAWSSecretsManagerRealSmoke/adoption-witness/version-before-adopt/postgres": "fail",
    "TestAWSSecretsManagerRealSmoke/adoption-witness/version-before-adopt": "fail",
    "TestAWSSecretsManagerRealSmoke/adoption-witness/version-after-adopt/sqlite": "pass",
    "TestAWSSecretsManagerRealSmoke/adoption-witness/version-after-adopt/postgres": "pass",
    "TestAWSSecretsManagerRealSmoke/adoption-witness/version-after-adopt": "pass",
    "TestAWSSecretsManagerRealSmoke/adoption-witness/replacement-after-adopt/sqlite": "pass",
    "TestAWSSecretsManagerRealSmoke/adoption-witness/replacement-after-adopt/postgres": "pass",
    "TestAWSSecretsManagerRealSmoke/adoption-witness/replacement-after-adopt": "pass",
    "TestAWSSecretsManagerRealSmoke/adoption-witness": "fail",
    "TestAWSSecretsManagerRealSmoke/promoted-replay/finish-loss/sqlite": "pass",
    "TestAWSSecretsManagerRealSmoke/promoted-replay/finish-loss/postgres": "pass",
    "TestAWSSecretsManagerRealSmoke/promoted-replay/finish-loss": "pass",
    "TestAWSSecretsManagerRealSmoke/promoted-replay/marker-refusal/sqlite": "fail",
    "TestAWSSecretsManagerRealSmoke/promoted-replay/marker-refusal/postgres": "pass",
    "TestAWSSecretsManagerRealSmoke/promoted-replay/marker-refusal": "fail",
    "TestAWSSecretsManagerRealSmoke/promoted-replay": "fail",
    "TestAWSSecretsManagerRealSmoke": "fail"
  },
  "failureAssertions": [
    {
      "test": "TestAWSSecretsManagerRealSmoke/lifecycle/postgres",
      "assertion": "assertion-066"
    },
    {
      "test": "TestAWSSecretsManagerRealSmoke/role-auth/web-identity/postgres",
      "assertion": "assertion-066"
    },
    {
      "test": "TestAWSSecretsManagerRealSmoke/adoption-witness/version-before-adopt/postgres",
      "assertion": "assertion-017"
    },
    {
      "test": "TestAWSSecretsManagerRealSmoke/promoted-replay/marker-refusal/sqlite",
      "assertion": "assertion-160"
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

Original raw SHA256 `4db0330e33816d30041f9904c2dfad0c70acb0a00040ea22944d5151b0fee434`.

```json
{
  "at": "2026-10-27T14:24:31.421931+00:00",
  "status": "C09_CLEANUP_VERIFIED_ACCEPTANCE_FAILED_NO_FURTHER_EXECUTION_AUTHORITY",
  "runId": "40510334025",
  "attempt": 1,
  "candidate": "f44f9fd717c1980fddc11ac6690a8a6e6e311104",
  "workflow": "9d620dd451a56408ccab8360283dfeef0320eac7",
  "acceptance": "FAILED_4_LEAVES_34_PASS_12_FAIL_RECORDS",
  "failureAssertions": [
    {
      "test": "TestAWSSecretsManagerRealSmoke/lifecycle/postgres",
      "assertion": "assertion-066"
    },
    {
      "test": "TestAWSSecretsManagerRealSmoke/role-auth/web-identity/postgres",
      "assertion": "assertion-066"
    },
    {
      "test": "TestAWSSecretsManagerRealSmoke/adoption-witness/version-before-adopt/postgres",
      "assertion": "assertion-017"
    },
    {
      "test": "TestAWSSecretsManagerRealSmoke/promoted-replay/marker-refusal/sqlite",
      "assertion": "assertion-160"
    }
  ],
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
    "v9"
  ],
  "retainedCreationDatesUnchanged": true,
  "setupUserIdentityMFAAndSoleAttachmentPreserved": true,
  "setupKeysGroupsInlinePolicies": 0,
  "executionVariables": 0,
  "protectedEnvironmentAndOIDCPreserved": true,
  "aliasAbsent": "alias/hikyo-158-20261027c09",
  "keyId": "88e19f70-fee7-4f9b-9b35-847bba607a55",
  "keyState": "PendingDeletion",
  "keyDeletionDateUTC": "2026-11-03T14:19:45.807000+00:00",
  "physicalKeyDeletionClaimed": false,
  "keyCancellationCalls": 0,
  "allPriorKeySchedulesUnchanged": true,
  "completeCustomerKeyInventory": 7,
  "newKeyCreations": 1,
  "newKeysAcrossC05ThroughC09": 5,
  "newReservationsCharged": 32,
  "creationReservationAttempts": 29,
  "unusedSlotsClosedWithoutRefund": 3,
  "lifetimeReservationsCharged": 272,
  "allOldReservesRetained": true,
  "budget": {
    "at": "2026-10-27T14:21:15.123Z",
    "displayedGrossUSD": "0.07",
    "displayedBillableUSD": "0.00",
    "displayedTaxUSD": "0.00",
    "allOldReservesRetainedUSD": "3.63",
    "newReserveUSD": "0.83",
    "usageForecastUSD": "4.53",
    "grossAllReservesUSD": "5.53",
    "billableAllReservesUSD": "5.46",
    "usageCeilingUSD": "4.60",
    "overallTargetBelowUSD": "6.00",
    "taxFeeFloorUSD": "1.00",
    "providerHardCapClaimed": false,
    "nativeReceiptSHA256": "b6a6cb99c0b22414b7a4f7b5bf3bd81cc04c20bf83bf944f63a0ef7e9daf990a"
  },
  "reviews": "5/5 consumed; owner disposition, no independent CLEAN or sixth review",
  "crossProvider": "unavailable",
  "applicationPR969Merged": false,
  "issue158Closed": false,
  "frozenPreparationPinsVerified": 86,
  "nativeReceipts": {
    "c09-final-iam-closure": "e948a250a9b2de947ac03b5fce574d6ca8e342aa9f12425db6b6977d66d59b8b",
    "c09-final-secret-closure": "002adbcd50e5ef051fd493c9869de01032e44a634618aa3e7baeaff2ac64815a",
    "c09-final-kms-reconciliation": "2c1b6631413f6e3e317702f9a01d4a085105b613f8d858033b97b351fdfc21b0",
    "c09-final-kms-complete": "02ef9b3f6b72caedd6f6938f1d238dc674d57cdf280afe360c75e6ca4c9ea9c5",
    "c09-final-billing": "b6a6cb99c0b22414b7a4f7b5bf3bd81cc04c20bf83bf944f63a0ef7e9daf990a"
  },
  "independentAbsenceSHA256": "41621bcf117c4d0f30b7ca06b8ae78f061a392beedf0140fea2d9ce7be72bf25",
  "namespaceQuiescenceSHA256": "5a83a62c3ebf8177fac9fee85b89f763e6ec45579b3ebfed2e9cfa17f721d26b",
  "githubClosureSHA256": "452429c4aabc175ca94c60528416796f069200eec213a6ba0bff76804dc62ef8",
  "artifactZipSHA256": "871c0828cc36ec1b0ad12c3b0d29d4dc145e42dffab7b32dbfe3590b2a30c3d4",
  "evidenceSHA256": "122be28ffe19fd13600e3b5e9da9a7f64ceff8a2381ef5ae1ae584d6ca70f85f"
}
```
