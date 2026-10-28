# Full bounded failed evidence: continuation-c07/renewal-proposal

The original validated nonsecret artifact is reproduced in full. It proves this failed historical result and contains only synthetic namespace identities and closed diagnostics. Independent final cleanup does not change acceptance to pass. Raw logs and private provider/session material were not fetched.

## evidence.json

Original raw SHA256 `63323f856fd4913874a3bdc26bedb116a2ec4d13ded1ba604851a00f90893bb3`.

```json
{"schema":"hikyo.aws-acceptance/v1","candidate_sha":"327dec159d680fab7b33a251ab46a2d1c16e9fa1","workflow_sha":"a25091501268b83ffff92aa558fce3207c868544","run_id":"40390662284","status":"failed-reconcile-before-rerun","stage":"real-suite","account":"099256073061","region":"eu-central-1","prefix":"hikyo-e2e-20261026c07/","recorded_arns":["arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/sqlite/document-0cMXky","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/sqlite/keys/ALPHA-0tThuW","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/sqlite/keys/SHARED_KEY-rlKayw","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/postgres/document-UVdKH5","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/postgres/keys/ALPHA-vp4tUM","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/postgres/keys/SHARED_KEY-zIrtTy","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/postgres/adopted-VOrxvq","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/postgres/moved-document-2ax87C","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/ambient/sqlite/role-document-Thav9j","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/ambient/postgres/role-document-aBsuCP","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/assume-role/sqlite/role-document-DCMBiI","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/assume-role/postgres/role-document-VQT7uw","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/web-identity/sqlite/role-document-bJoj8z","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/web-identity/postgres/role-document-lIb6is","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/ambient/iam-canary-nutVnE","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/sqlite/version-before-adopt-UaNxbd","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/postgres/version-before-adopt-ZTfDqQ","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/sqlite/version-after-adopt-uxvHij","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/postgres/version-after-adopt-nNviQQ","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/sqlite/replacement-after-adopt-6ylIP6","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/sqlite/replacement-after-adopt-a4P6Ur","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/postgres/replacement-after-adopt-LTTzsr","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/postgres/replacement-after-adopt-3IePzU","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/sqlite/promoted-finish-loss-fwqaWV","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/postgres/promoted-finish-loss-LE2sWp","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/sqlite/promoted-marker-refusal-PFXzy2","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/postgres/promoted-marker-refusal-nmcfvj"],"verified_absent_arns":["arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/web-identity/sqlite/role-document-bJoj8z","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/sqlite/promoted-marker-refusal-PFXzy2","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/ambient/postgres/role-document-aBsuCP","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/postgres/version-before-adopt-ZTfDqQ","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/postgres/version-after-adopt-nNviQQ","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/postgres/promoted-finish-loss-LE2sWp","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/ambient/iam-canary-nutVnE","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/assume-role/sqlite/role-document-DCMBiI","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/sqlite/document-0cMXky","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/ambient/sqlite/role-document-Thav9j","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/sqlite/version-after-adopt-uxvHij","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/postgres/replacement-after-adopt-3IePzU","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/sqlite/keys/SHARED_KEY-rlKayw","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/sqlite/version-before-adopt-UaNxbd","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/web-identity/postgres/role-document-lIb6is","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/postgres/keys/ALPHA-vp4tUM","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/postgres/adopted-VOrxvq","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/postgres/moved-document-2ax87C","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/sqlite/replacement-after-adopt-6ylIP6","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/sqlite/replacement-after-adopt-a4P6Ur","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/postgres/document-UVdKH5","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/sqlite/keys/ALPHA-0tThuW","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/assume-role/postgres/role-document-VQT7uw","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/postgres/replacement-after-adopt-LTTzsr","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/sqlite/promoted-finish-loss-fwqaWV","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/postgres/keys/SHARED_KEY-zIrtTy","arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261026c07/static/postgres/promoted-marker-refusal-nmcfvj"],"unresolved_names":[],"historical_cleanup":{"arn":null,"status":"not-requested","delete_attempts":0,"delete_response_exact_arn":false,"metadata_verified":false,"absence_checks":0,"verified_absent":false},"tests":{"TestAWSSecretsManagerRealSmoke/preflight-static":"pass","TestAWSSecretsManagerRealSmoke/preflight-ambient":"pass","TestAWSSecretsManagerRealSmoke/preflight-assume-role":"pass","TestAWSSecretsManagerRealSmoke/preflight-web-identity":"pass","TestAWSSecretsManagerRealSmoke/preflight-kms-denied":"pass","TestAWSSecretsManagerRealSmoke/auth-boundaries/expired-static":"pass","TestAWSSecretsManagerRealSmoke/auth-boundaries/expired-assume-role":"pass","TestAWSSecretsManagerRealSmoke/auth-boundaries/expired-ambient":"pass","TestAWSSecretsManagerRealSmoke/auth-boundaries/expired-web-identity":"pass","TestAWSSecretsManagerRealSmoke/auth-boundaries/configured-identity-refusal":"pass","TestAWSSecretsManagerRealSmoke/auth-boundaries":"pass","TestAWSSecretsManagerRealSmoke/lifecycle/sqlite":"fail","TestAWSSecretsManagerRealSmoke/lifecycle/postgres":"pass","TestAWSSecretsManagerRealSmoke/lifecycle":"fail","TestAWSSecretsManagerRealSmoke/role-auth/ambient/sqlite":"pass","TestAWSSecretsManagerRealSmoke/role-auth/ambient/postgres":"pass","TestAWSSecretsManagerRealSmoke/role-auth/ambient":"pass","TestAWSSecretsManagerRealSmoke/role-auth/assume-role/sqlite":"fail","TestAWSSecretsManagerRealSmoke/role-auth/assume-role/postgres":"pass","TestAWSSecretsManagerRealSmoke/role-auth/assume-role":"fail","TestAWSSecretsManagerRealSmoke/role-auth/web-identity/sqlite":"fail","TestAWSSecretsManagerRealSmoke/role-auth/web-identity/postgres":"pass","TestAWSSecretsManagerRealSmoke/role-auth/web-identity":"fail","TestAWSSecretsManagerRealSmoke/role-auth":"fail","TestAWSSecretsManagerRealSmoke/kms-denial/sqlite":"pass","TestAWSSecretsManagerRealSmoke/kms-denial/postgres":"pass","TestAWSSecretsManagerRealSmoke/kms-denial":"pass","TestAWSSecretsManagerRealSmoke/iam-denial":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness/version-before-adopt/sqlite":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness/version-before-adopt/postgres":"fail","TestAWSSecretsManagerRealSmoke/adoption-witness/version-before-adopt":"fail","TestAWSSecretsManagerRealSmoke/adoption-witness/version-after-adopt/sqlite":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness/version-after-adopt/postgres":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness/version-after-adopt":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness/replacement-after-adopt/sqlite":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness/replacement-after-adopt/postgres":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness/replacement-after-adopt":"pass","TestAWSSecretsManagerRealSmoke/adoption-witness":"fail","TestAWSSecretsManagerRealSmoke/promoted-replay/finish-loss/sqlite":"pass","TestAWSSecretsManagerRealSmoke/promoted-replay/finish-loss/postgres":"pass","TestAWSSecretsManagerRealSmoke/promoted-replay/finish-loss":"pass","TestAWSSecretsManagerRealSmoke/promoted-replay/marker-refusal/sqlite":"pass","TestAWSSecretsManagerRealSmoke/promoted-replay/marker-refusal/postgres":"pass","TestAWSSecretsManagerRealSmoke/promoted-replay/marker-refusal":"pass","TestAWSSecretsManagerRealSmoke/promoted-replay":"pass","TestAWSSecretsManagerRealSmoke":"fail"},"maximum_create_reservations":32,"creation_reservations":["hikyo-e2e-20261026c07/static/sqlite/document","hikyo-e2e-20261026c07/static/sqlite/keys/ALPHA","hikyo-e2e-20261026c07/static/sqlite/keys/SHARED_KEY","hikyo-e2e-20261026c07/static/postgres/document","hikyo-e2e-20261026c07/static/postgres/keys/ALPHA","hikyo-e2e-20261026c07/static/postgres/keys/SHARED_KEY","hikyo-e2e-20261026c07/static/postgres/adopted","hikyo-e2e-20261026c07/static/postgres/moved-document","hikyo-e2e-20261026c07/ambient/sqlite/role-document","hikyo-e2e-20261026c07/ambient/postgres/role-document","hikyo-e2e-20261026c07/assume-role/sqlite/role-document","hikyo-e2e-20261026c07/assume-role/postgres/role-document","hikyo-e2e-20261026c07/web-identity/sqlite/role-document","hikyo-e2e-20261026c07/web-identity/postgres/role-document","hikyo-e2e-20261026c07/kms-denied/sqlite/denied-document","hikyo-e2e-20261026c07/kms-denied/postgres/denied-document","hikyo-e2e-20261026c07/ambient/iam-canary","hikyo-e2e-20261026c07/static/sqlite/version-before-adopt","hikyo-e2e-20261026c07/static/postgres/version-before-adopt","hikyo-e2e-20261026c07/static/sqlite/version-after-adopt","hikyo-e2e-20261026c07/static/postgres/version-after-adopt","hikyo-e2e-20261026c07/static/sqlite/replacement-after-adopt","hikyo-e2e-20261026c07/static/sqlite/replacement-after-adopt","hikyo-e2e-20261026c07/static/postgres/replacement-after-adopt","hikyo-e2e-20261026c07/static/postgres/replacement-after-adopt","hikyo-e2e-20261026c07/static/sqlite/promoted-finish-loss","hikyo-e2e-20261026c07/static/postgres/promoted-finish-loss","hikyo-e2e-20261026c07/static/sqlite/promoted-marker-refusal","hikyo-e2e-20261026c07/static/postgres/promoted-marker-refusal"],"complete_creation_ledger":false,"diagnostics":[{"test":"TestAWSSecretsManagerRealSmoke/lifecycle/sqlite","operation":"UpdateSecretVersionStage","code":"InvalidParameterException"},{"test":"TestAWSSecretsManagerRealSmoke/lifecycle/sqlite","operation":"TagResource","code":"ThrottlingException"},{"test":"TestAWSSecretsManagerRealSmoke/lifecycle/postgres","operation":"UpdateSecretVersionStage","code":"ThrottlingException"},{"test":"TestAWSSecretsManagerRealSmoke/lifecycle/postgres","operation":"UpdateSecretVersionStage","code":"InvalidParameterException"},{"test":"TestAWSSecretsManagerRealSmoke/kms-denial/sqlite","operation":"CreateSecret","code":"AccessDeniedException"},{"test":"TestAWSSecretsManagerRealSmoke/kms-denial/postgres","operation":"CreateSecret","code":"AccessDeniedException"}],"diagnostics_truncated":false}
```

## HOSTED-EVIDENCE-RECEIPT.json

Original raw SHA256 `178ca1e06c0adad22146d127c257c629d20cd72de49942cfac2a5d64a5bb02f0`.

```json
{
  "at": "2026-10-26T20:22:49.006405+00:00",
  "runId": "40390662284",
  "attempt": 1,
  "runConclusion": "failure",
  "artifact": {
    "id": 12685683863,
    "node_id": "MDg6QXJ0aWZhY3QxMjY4NTY4Mzg2Mw==",
    "name": "aws-158-40390662284-327dec159d680fab7b33a251ab46a2d1c16e9fa1",
    "size_in_bytes": 1631,
    "url": "https://api.github.com/repos/Hikyo-Org/Hikyo/actions/artifacts/12685683863",
    "archive_download_url": "https://api.github.com/repos/Hikyo-Org/Hikyo/actions/artifacts/12685683863/zip",
    "expired": false,
    "digest": "sha256:7f6f38850bfdbf2b7cfa1ba34397f0cc33221a29c93d1a4f64d13a3667fffa2e",
    "created_at": "2026-10-26T20:22:25Z",
    "updated_at": "2026-10-26T20:22:25Z",
    "expires_at": "2026-11-09T20:22:24Z",
    "workflow_run": {
      "id": 40390662284,
      "repository_id": 1316165429,
      "head_repository_id": 1316165429,
      "head_branch": "main",
      "head_sha": "a25091501268b83ffff92aa558fce3207c868544"
    }
  },
  "artifactZipSHA256": "7f6f38850bfdbf2b7cfa1ba34397f0cc33221a29c93d1a4f64d13a3667fffa2e",
  "evidenceSHA256": "63323f856fd4913874a3bdc26bedb116a2ec4d13ded1ba604851a00f90893bb3",
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
    "TestAWSSecretsManagerRealSmoke/role-auth/assume-role/sqlite": "fail",
    "TestAWSSecretsManagerRealSmoke/role-auth/assume-role/postgres": "pass",
    "TestAWSSecretsManagerRealSmoke/role-auth/assume-role": "fail",
    "TestAWSSecretsManagerRealSmoke/role-auth/web-identity/sqlite": "fail",
    "TestAWSSecretsManagerRealSmoke/role-auth/web-identity/postgres": "pass",
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
    "TestAWSSecretsManagerRealSmoke/promoted-replay/marker-refusal/sqlite": "pass",
    "TestAWSSecretsManagerRealSmoke/promoted-replay/marker-refusal/postgres": "pass",
    "TestAWSSecretsManagerRealSmoke/promoted-replay/marker-refusal": "pass",
    "TestAWSSecretsManagerRealSmoke/promoted-replay": "pass",
    "TestAWSSecretsManagerRealSmoke": "fail"
  },
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

Original raw SHA256 `dbb506f841f18dee20d0e62db3d450489714db6b3f223524ab9b64a546ed39f8`.

```json
{
  "at": "2026-10-26T20:50:33.706322+00:00",
  "status": "C07_CLEANUP_VERIFIED_ACCEPTANCE_FAILED_NO_FURTHER_EXECUTION_AUTHORITY",
  "hostedRunId": "40390662284",
  "hostedAttempt": 1,
  "acceptanceStatus": "FAILED_4_LEAF_CASES_35_PASS_11_FAIL_RECORDS",
  "recordedArns": 27,
  "creationReservationsAttempted": 29,
  "lifetimeReservationsCharged": 208,
  "unusedSlotsClosedWithoutRefund": 3,
  "allKnownResourceAndReservedNameTargetsStrictNotFound": 29,
  "completePlannedDeletionNamespaceInventoryCount": 0,
  "ownedRolesAbsent": 8,
  "ownedOIDCProviderAbsent": true,
  "ownedBoundaryAbsent": true,
  "setupDefault": "v5",
  "setupV5Expired": true,
  "preservedSetupVersions": [
    "v2",
    "v3",
    "v4",
    "v5",
    "v7"
  ],
  "setupUserMFAAndSoleAttachmentPreserved": true,
  "setupAccessKeysInlinePoliciesGroups": 0,
  "protectedEnvironmentPreserved": true,
  "executionVariableCount": 0,
  "newKeyState": "PendingDeletion",
  "newKeyDeletionDateUTC": "2026-11-02T20:45:47.974Z",
  "physicalKeyDeletionClaimed": false,
  "allPriorKeySchedulesUnchanged": true,
  "newKeyCreationsForRenewal": 0,
  "newKeyTotalAcrossC05C06C07": 3,
  "keyCancellationCalls": 0,
  "grossBeforeCreditsUSD": "0.05",
  "billableUSD": "0.00",
  "taxUSD": "0.00",
  "allOriginalBudgetReserveFloorsRetained": true,
  "usageForecastUSD": "2.85",
  "usageCeilingUSD": "3.00",
  "grossAllReservesUSD": "3.85",
  "billableAllReservesUSD": "3.80",
  "independentCLEAN": false,
  "reviewRounds": "4/4 consumed",
  "fullCurrentPairCI": "UNESTABLISHED",
  "PR969Merged": false,
  "issue158Closed": false,
  "github": {
    "environment": {
      "id": 24756660811,
      "node_id": "EN_kwDOTnMXNc8AAAAFw5yqSw",
      "name": "hikyo-158-aws-sandbox",
      "url": "https://api.github.com/repos/Hikyo-Org/Hikyo/environments/hikyo-158-aws-sandbox",
      "html_url": "https://github.com/Hikyo-Org/Hikyo/deployments/activity_log?environments_filter=hikyo-158-aws-sandbox",
      "created_at": "2026-10-19T22:23:34Z",
      "updated_at": "2026-10-19T22:23:34Z",
      "can_admins_bypass": false,
      "protection_rules": [
        {
          "id": 69384094,
          "node_id": "GA_kwDOTnMXNc4EIree",
          "type": "required_reviewers",
          "prevent_self_review": false,
          "reviewers": [
            {
              "type": "User",
              "reviewer": {
                "login": "Dunky13",
                "id": 991668,
                "node_id": "MDQ6VXNlcjk5MTY2OA==",
                "avatar_url": "https://avatars.githubusercontent.com/u/991668?v=4",
                "gravatar_id": "",
                "url": "https://api.github.com/users/Dunky13",
                "html_url": "https://github.com/Dunky13",
                "followers_url": "https://api.github.com/users/Dunky13/followers",
                "following_url": "https://api.github.com/users/Dunky13/following{/other_user}",
                "gists_url": "https://api.github.com/users/Dunky13/gists{/gist_id}",
                "starred_url": "https://api.github.com/users/Dunky13/starred{/owner}{/repo}",
                "subscriptions_url": "https://api.github.com/users/Dunky13/subscriptions",
                "organizations_url": "https://api.github.com/users/Dunky13/orgs",
                "repos_url": "https://api.github.com/users/Dunky13/repos",
                "events_url": "https://api.github.com/users/Dunky13/events{/privacy}",
                "received_events_url": "https://api.github.com/users/Dunky13/received_events",
                "type": "User",
                "user_view_type": "public",
                "site_admin": false
              }
            }
          ]
        },
        {
          "id": 69384095,
          "node_id": "GA_kwDOTnMXNc4EIref",
          "type": "branch_policy"
        }
      ],
      "deployment_branch_policy": {
        "protected_branches": false,
        "custom_branch_policies": true
      }
    },
    "variables": {
      "variables": [],
      "total_count": 0
    },
    "branchPolicies": {
      "total_count": 1,
      "branch_policies": [
        {
          "id": 63637093,
          "node_id": "MDE2OkdhdGVCcmFuY2hQb2xpY3k2MzYzNzA5Mw==",
          "name": "main",
          "type": "branch"
        }
      ]
    },
    "OIDC": {
      "use_default": true,
      "use_immutable_subject": true,
      "sub_claim_prefix": "repo:Hikyo-Org@316726515/Hikyo@1316165429"
    }
  },
  "nativeReceipts": {
    "c07-renewal-final-iam-reconciliation": "3935d18f8672a8269ec797418fe3b4fa29d83baa2b2b24fc22078647669d9b9d",
    "c07-renewal-final-billing": "a175d2187af7e3a8348f7d60bbdfa5a9c9517b627f0d525210223d0529e3c819",
    "c07-renewal-final-secret-inventory-3": "fdff64769f2ee1d77f24c11e6195fc8da3cf26356f181ee5e10c0ae709193fb6",
    "c07-renewal-final-kms-reconciliation": "015216a08ddd45b3afa3a2fbdb9e0232052da09e19ba98fbb2795cee2352064c"
  }
}
```
