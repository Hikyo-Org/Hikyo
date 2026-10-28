# Full bounded failed evidence: completion-c11

The original validated nonsecret artifact is reproduced in full. It proves this failed historical result and contains only synthetic namespace identities and closed diagnostics. Independent final cleanup does not change acceptance to pass. Raw logs and private provider/session material were not fetched.

## evidence.json

Original raw SHA256 `fc7845e26914f43a0fea943fc8375f9e9d5152c58895e2c42c3c6f689f38e398`.

```json
{"schema":"hikyo.aws-acceptance/v2","candidate_sha":"361e02e3915eef840429d88589c39689f4a58dfb","workflow_sha":"24e198b32a3ffd8d4013207b2f974d76ec49be6f","run_id":"40600194878","status":"failed-reconcile-before-rerun","stage":"real-suite","account":"099256073061","region":"eu-central-1","prefix":"hikyo-e2e-20261027c11/","recorded_arns":["arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/sqlite/document-BwSvlz","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/sqlite/keys/ALPHA-mzrmlF","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/sqlite/keys/SHARED_KEY-csLTdI","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/sqlite/adopted-waylXz","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/sqlite/moved-document-buJL14","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/postgres/document-2zTnrI","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/postgres/keys/ALPHA-MSSNNq","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/postgres/keys/SHARED_KEY-voEo2r","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/ambient/sqlite/role-document-q3sZGc","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/ambient/postgres/role-document-tQN9ZQ","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/assume-role/sqlite/role-document-qWWNCt","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/assume-role/postgres/role-document-4W0M69","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/web-identity/sqlite/role-document-nQlvsz","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/web-identity/postgres/role-document-SjfQ04","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/ambient/iam-canary-Cttbu2","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/sqlite/version-before-adopt-nVyWAx","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/postgres/version-before-adopt-EOeuWN","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/sqlite/version-after-adopt-Xvegrb","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/postgres/version-after-adopt-0BPbzm","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/sqlite/replacement-after-adopt-X8OH8j","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/sqlite/replacement-after-adopt-mXz3E5","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/postgres/replacement-after-adopt-H6G55H","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/postgres/replacement-after-adopt-Gupyy6","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/sqlite/promoted-finish-loss-eZ2gup","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/postgres/promoted-finish-loss-DvAZzz","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/sqlite/promoted-marker-refusal-Hs4i1Y","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/postgres/promoted-marker-refusal-RuAgp3"],"verified_absent_arns":["arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/assume-role/postgres/role-document-4W0M69","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/web-identity/sqlite/role-document-nQlvsz","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/sqlite/version-after-adopt-Xvegrb","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/postgres/document-2zTnrI","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/ambient/iam-canary-Cttbu2","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/postgres/replacement-after-adopt-H6G55H","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/postgres/version-before-adopt-EOeuWN","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/sqlite/promoted-marker-refusal-Hs4i1Y","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/postgres/keys/SHARED_KEY-voEo2r","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/sqlite/version-before-adopt-nVyWAx","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/postgres/version-after-adopt-0BPbzm","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/sqlite/promoted-finish-loss-eZ2gup","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/sqlite/replacement-after-adopt-X8OH8j","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/postgres/replacement-after-adopt-Gupyy6","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/sqlite/keys/SHARED_KEY-csLTdI","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/sqlite/adopted-waylXz","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/sqlite/replacement-after-adopt-mXz3E5","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/sqlite/document-BwSvlz","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/sqlite/moved-document-buJL14","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/sqlite/keys/ALPHA-mzrmlF","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/web-identity/postgres/role-document-SjfQ04","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/postgres/promoted-marker-refusal-RuAgp3","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/ambient/sqlite/role-document-q3sZGc","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/assume-role/sqlite/role-document-qWWNCt","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/postgres/keys/ALPHA-MSSNNq","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/ambient/postgres/role-document-tQN9ZQ","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261027c11/static/postgres/promoted-finish-loss-DvAZzz"],"unresolved_names":[],"historical_cleanup":{"arn":null,"status":"not-requested","delete_attempts":0,"delete_response_exact_arn":false,"metadata_verified":false,"absence_checks":0,"verified_absent":false},"tests":{"TestAWSSecretsManagerRealSmoke/preflight-static":"pass","TestAWSSecretsManagerRealSmoke/preflight-ambient":"pass","TestAWSSecretsManagerRealSmoke/preflight-assume-role":"pass","TestAWSSecretsManagerRealSmoke/preflight-web-identity":"pass","TestAWSSecretsManagerRealSmoke/preflight-kms-denied":"pass","TestAWSSecretsManagerRealSmoke/auth-boundaries/expired-static":"pass","TestAWSSecretsManagerRealSmoke/auth-boundaries/expired-assume-role":"pass","TestAWSSecretsManagerRealSmoke/auth-boundaries/expired-ambient":"pass","TestAWSSecretsManagerRealSmoke/auth-boundaries/expired-web-identity":"pass","TestAWSSecretsManagerRealSmoke/auth-boundaries/configured-identity-refusal":"pass","TestAWSSecretsManagerRealSmoke/auth-boundaries":"pass","TestAWSSecretsManagerRealSmoke/lifecycle/sqlite":"pass","TestAWSSecretsManagerRealSmoke/lifecycle/postgres":"fail","TestAWSSecretsManagerRealSmoke/lifecycle":"fail","TestAWSSecretsManagerRealSmoke/role-auth/ambient/sqlite":"pass","TestAWSSecretsManagerRealSmoke/role-auth/ambient/postgres":"pass","TestAWSSecretsManagerRealSmoke/role-auth/ambient":"pass","TestAWSSecretsManagerRealSmoke/role-auth/assume-role/sqlite":"pass","TestAWSSecretsManagerRealSmoke/role-auth/assume-role/postgres":"pass","TestAWSSecretsManagerRealSmoke/role-auth/assume-role":"pass","TestAWSSecretsManagerRealSmoke/role-auth/web-identity/sqlite":"pass","TestAWSSecretsManagerRealSmoke/role-auth/web-identity/postgres":"pass","TestAWSSecretsManagerRealSmoke/role-auth/web-identity":"pass","TestAWSSecretsManagerRealSmoke/role-auth":"pass","TestAWSSecretsManagerRealSmoke/kms-denial/sqlite":"pass","TestAWSSecretsManagerRealSmoke/kms-denial/postgres":"pass","TestAWSSecretsManagerRealSmoke/kms-denial":"pass","TestAWSSecretsManagerRealSmoke/iam-denial":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness/version-before-adopt/sqlite":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness/version-before-adopt/postgres":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness/version-before-adopt":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness/version-after-adopt/sqlite":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness/version-after-adopt/postgres":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness/version-after-adopt":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness/replacement-after-adopt/sqlite":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness/replacement-after-adopt/postgres":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness/replacement-after-adopt":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness":"pass","TestAWSSecretsManagerRealSmoke/promoted-replay/finish-loss/sqlite":"pass","TestAWSSecretsManagerRealSmoke/promoted-replay/finish-loss/postgres":"pass","TestAWSSecretsManagerRealSmoke/promoted-replay/finish-loss":"pass","TestAWSSecretsManagerRealSmoke/promoted-replay/marker-refusal/sqlite":"pass","TestAWSSecretsManagerRealSmoke/promoted-replay/marker-refusal/postgres":"pass","TestAWSSecretsManagerRealSmoke/promoted-replay/marker-refusal":"pass","TestAWSSecretsManagerRealSmoke/promoted-replay":"pass","TestAWSSecretsManagerRealSmoke":"fail"},"maximum_create_reservations":32,"creation_reservations":["hikyo-e2e-20261027c11/static/sqlite/document","hikyo-e2e-20261027c11/static/sqlite/keys/ALPHA","hikyo-e2e-20261027c11/static/sqlite/keys/SHARED_KEY","hikyo-e2e-20261027c11/static/sqlite/adopted","hikyo-e2e-20261027c11/static/sqlite/moved-document","hikyo-e2e-20261027c11/static/postgres/document","hikyo-e2e-20261027c11/static/postgres/keys/ALPHA","hikyo-e2e-20261027c11/static/postgres/keys/SHARED_KEY","hikyo-e2e-20261027c11/ambient/sqlite/role-document","hikyo-e2e-20261027c11/ambient/postgres/role-document","hikyo-e2e-20261027c11/assume-role/sqlite/role-document","hikyo-e2e-20261027c11/assume-role/postgres/role-document","hikyo-e2e-20261027c11/web-identity/sqlite/role-document","hikyo-e2e-20261027c11/web-identity/postgres/role-document","hikyo-e2e-20261027c11/kms-denied/sqlite/denied-document","hikyo-e2e-20261027c11/kms-denied/postgres/denied-document","hikyo-e2e-20261027c11/ambient/iam-canary","hikyo-e2e-20261027c11/static/sqlite/version-before-adopt","hikyo-e2e-20261027c11/static/postgres/version-before-adopt","hikyo-e2e-20261027c11/static/sqlite/version-after-adopt","hikyo-e2e-20261027c11/static/postgres/version-after-adopt","hikyo-e2e-20261027c11/static/sqlite/replacement-after-adopt","hikyo-e2e-20261027c11/static/sqlite/replacement-after-adopt","hikyo-e2e-20261027c11/static/postgres/replacement-after-adopt","hikyo-e2e-20261027c11/static/postgres/replacement-after-adopt","hikyo-e2e-20261027c11/static/sqlite/promoted-finish-loss","hikyo-e2e-20261027c11/static/postgres/promoted-finish-loss","hikyo-e2e-20261027c11/static/sqlite/promoted-marker-refusal","hikyo-e2e-20261027c11/static/postgres/promoted-marker-refusal"],"complete_creation_ledger":false,"diagnostics":[{"test":"TestAWSSecretsManagerRealSmoke/lifecycle/sqlite","operation":"UpdateSecretVersionStage","code":"InvalidParameterException"},{"test":"TestAWSSecretsManagerRealSmoke/kms-denial/sqlite","operation":"CreateSecret","code":"AccessDeniedException"},{"test":"TestAWSSecretsManagerRealSmoke/kms-denial/postgres","operation":"CreateSecret","code":"AccessDeniedException"},{"test":"TestAWSSecretsManagerRealSmoke","operation":"GetSecretValue","code":"ResourceNotFoundException"}],"diagnostics_truncated":false,"failure_assertions":[{"test":"TestAWSSecretsManagerRealSmoke/lifecycle/postgres","assertion":"assertion-227"}],"failure_assertions_truncated":false}
```

## HOSTED-EVIDENCE-RECEIPT.json

Original raw SHA256 `2fb7178a020b7addd2b3ffc11a70f7379052e6d79ddb4795c35bcc96295ecfe2`.

```json
{
  "at": "2026-10-27T23:40:53.885732+00:00",
  "runId": "40600194878",
  "attempt": 1,
  "runConclusion": "failure",
  "artifact": {
    "id": 12780025133,
    "node_id": "MDg6QXJ0aWZhY3QxMjc4MDAyNTEzMw==",
    "name": "aws-158-40600194878-361e02e3915eef840429d88589c39689f4a58dfb",
    "size_in_bytes": 1664,
    "url": "https://api.github.com/repos/Hikyo-Org/Hikyo/actions/artifacts/12780025133",
    "archive_download_url": "https://api.github.com/repos/Hikyo-Org/Hikyo/actions/artifacts/12780025133/zip",
    "expired": false,
    "digest": "sha256:c840ced5f53b12f85980e227b3c64543227ef996e62956ba334d5e1fe138cbfc",
    "created_at": "2026-10-27T23:36:04Z",
    "updated_at": "2026-10-27T23:36:04Z",
    "expires_at": "2026-11-10T23:36:04Z",
    "workflow_run": {
      "id": 40600194878,
      "repository_id": 1316165429,
      "head_repository_id": 1316165429,
      "head_branch": "main",
      "head_sha": "24e198b32a3ffd8d4013207b2f974d76ec49be6f"
    }
  },
  "artifactZipSHA256": "c840ced5f53b12f85980e227b3c64543227ef996e62956ba334d5e1fe138cbfc",
  "evidenceSHA256": "fc7845e26914f43a0fea943fc8375f9e9d5152c58895e2c42c3c6f689f38e398",
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
    "TestAWSSecretsManagerRealSmoke/adoption-witness/replacement-after-adopt/postgres": "pass",
    "TestAWSSecretsManagerRealSmoke/adoption-witness/replacement-after-adopt": "pass",
    "TestAWSSecretsManagerRealSmoke/adoption-witness": "pass",
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
      "test": "TestAWSSecretsManagerRealSmoke/lifecycle/postgres",
      "assertion": "assertion-227"
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

## FINAL-CLEANUP.json

Original raw SHA256 `6c82131e5d37cac25d45584125c63c0da233dfbef366fd7ec449311ee103d419`.

```json
{
  "at": "2026-10-27T23:53:43.584594+00:00",
  "status": "C11_FINAL_CLEANUP_INDEPENDENTLY_VERIFIED_ACCEPTANCE_FAILED",
  "runId": "40600194878",
  "attempt": 1,
  "candidate": "361e02e3915eef840429d88589c39689f4a58dfb",
  "workflowSHA": "24e198b32a3ffd8d4013207b2f974d76ec49be6f",
  "providerAcceptance": "FAILED_ONE_LEAF_POSTGRES_LIFECYCLE_CONFLICT",
  "terminalTestRecords": 46,
  "passedTestRecords": 43,
  "failedTestRecordsIncludingParents": 3,
  "independentStrictNotFoundTargets": 29,
  "recordedARNs": 27,
  "reservedUnacknowledgedNames": 2,
  "completeRegionalSecretsIncludingPlannedDeletion": 0,
  "terminalQuiescenceSeconds": 335.922,
  "rolesAbsent": 8,
  "issuerAbsent": true,
  "boundaryAbsent": true,
  "aliasAbsent": true,
  "protectedVariablesAbsent": 9,
  "setupDefault": "v5",
  "retainedSetupVersions": [
    "v11",
    "v2",
    "v3",
    "v4",
    "v5"
  ],
  "setupVersionDates": {
    "v11": "2026-10-27T23:03:49Z",
    "v5": "2026-10-20T21:45:02Z",
    "v4": "2026-10-20T20:51:04Z",
    "v3": "2026-10-19T23:33:57Z",
    "v2": "2026-10-19T14:13:52Z"
  },
  "setupIdentityMFAAndSoleAttachmentPreserved": true,
  "newKeyId": "81294276-467e-45b2-8279-1abb6fea8e34",
  "newKeyState": "PendingDeletion",
  "newKeyDeletionDateUTC": "2026-11-03T23:50:52.008000+00:00",
  "scheduledDeletionNotPhysicalDeletion": true,
  "priorAndNewKeyReconciliation": {
    "pendingIds": [
      "03dcf56b-9687-4173-97d5-6f4925d08797",
      "4005b859-369d-4fa5-a96c-7b8e865ee712",
      "64224f0e-697e-4090-9978-ea153625f6d9",
      "81294276-467e-45b2-8279-1abb6fea8e34",
      "88e19f70-fee7-4f9b-9b35-847bba607a55",
      "b71f3c57-626a-4a89-9fcf-f7901a421e49",
      "b8806a19-57c8-4d77-9d1d-39c611a933a1",
      "bfa44dc1-ee8a-43b8-aec7-d8a7fbb38db1"
    ],
    "naturallyDeletedIds": [
      "990c479f-d673-4bc9-8bca-7b9b86f4b205"
    ],
    "strictAbsentSelectors": [
      "10908b8d-fe54-40e3-9210-a3eefea709b6",
      "alias/hikyo-158-20261022c05",
      "alias/hikyo-158-20261026c06",
      "alias/hikyo-158-20261026c07",
      "alias/hikyo-158-20261027c08",
      "alias/hikyo-158-20261027c09",
      "alias/hikyo-158-20261027c10",
      "alias/hikyo-158-20261027c11",
      "fa374267-a456-4e79-b5b1-6cdf356b7d0e"
    ],
    "readStartedAt": "2026-10-27T23:51:19.037000+00:00",
    "readFinishedAt": "2026-10-27T23:51:34.482000+00:00",
    "mutations": 0
  },
  "budget": {
    "displayedGrossUSD": "0.07",
    "displayedBillableUSD": "0.00",
    "displayedTaxUSD": "0.00",
    "allOldReservesRetainedUSD": "5.29",
    "newReserveUSD": "0.83",
    "usageForecastUSD": "6.19",
    "grossAllReservesUSD": "7.19",
    "billableAllReservesUSD": "7.12",
    "usageCeilingUSD": "6.30"
  },
  "allPriorReservesAndSchedulesRetained": true,
  "snapshotPins": {
    "c11-final-kms-reconciliation.json": "d8be5693a4e7c91698cf5b7262def47a3f2122cdb0e7aac482f88593c7e69687",
    "c11-final-kms-complete.json": "47d024c2a31fdf0c6273ca8208f3920fd1dc3f90525c75f315d50e5fc2ee1e7a",
    "c11-final-iam-closure.json": "3d341b82c1747d02feaedfce55c9162f501e35a1c51540f3f7047650a1d4abda",
    "c11-final-secret-closure.json": "5a774c725ca82178808fab71f7276b71dafd08fc682abf5b0af308325f2dac23",
    "c11-billing-admission.json": "d8363057ab0f840bb78950eccf0f6590b19b620b1c13086e0d6633892ed1b338",
    "KEY-DELETION-ACKNOWLEDGEMENT.json": "4ea84e90e7a5cb9fc1ee87087a876abce31e0753ef97aa899dff4ea0cc706262",
    "ALIAS-ABSENCE-READBACK.json": "50b0810bfdcebf9749aba71098b7f87081dc66e3cfc4ee004bfe44fe5b4540e0",
    "FINAL-V5-RESTORATION-READBACK.json": "90d4b430016c36d8ab30e0d9e2172d8247bbb42e8258b90098dfde236abb76fc",
    "IAM-RETIREMENT-RESULT.json": "8d75b6f4a669ea7d32a2142b44a7bf4f77b169bbf39ec00cf0e96fd07dc13365",
    "VARIABLE-RETIREMENT-RESULT.json": "949fa570fb96f24de11ba940015877840db2975e9ae6d1e1f78a47a87a3dcd7f",
    "FINAL-GITHUB-CLEANUP-READBACK.json": "b948fda36a4f5dd1f794188dd50570ab1858af339716a59f3e6e8e202d4bd23c",
    "NAMESPACE-SECRET-CLEANUP-READBACK.json": "1d32636da7629177aaf6687a899dc3a2c24df8f6d466397ee236d4fdbd23385e",
    "NAMESPACE-QUIESCENCE-FIRST-NATIVE.json": "c355ea1b0eff5b40add0131d81595d8610a2fac502a5bec2409aadb7b5ff7c67",
    "INDEPENDENT-SECRET-ABSENCE.json": "1418bc75d3e9765af78d0f8a0a448ed6f0ccf8bdafb75cb049291e0b5a7fc934",
    "BILLING-NORMALIZED.json": "08bda53ac9324c45e934de60a7ea4636dad63af09d962126bd73cc9d6a9de0ea",
    "HOSTED-EVIDENCE-RECEIPT.json": "2fb7178a020b7addd2b3ffc11a70f7379052e6d79ddb4795c35bcc96295ecfe2",
    "HOSTED-RUN-TERMINAL.json": "094217c55c32863cc92573e50ab8d368fec9e51005ccbbc2ea5f9306060c4c5e"
  },
  "rawPrivateProjectionOrLogsDownloaded": false,
  "newReservationsCharged": 0,
  "lifetimeReservationsCharged": 304,
  "originalReassignedSlotsRemainCharged": 32,
  "creationAttempts": 29,
  "unusedChargedSlotsClosedWithoutRefund": 3,
  "hostedAttemptsConsumed": 1,
  "maximumReviews": "5/5 consumed, no independent CLEAN, no sixth review",
  "newAWSExecutionAuthority": false,
  "retainedRuntimeResources": "Separate private owner/setup consoles retained at the owner request; no owned local test worker remains; owned v11 retained nondefault by explicit grant"
}
```
