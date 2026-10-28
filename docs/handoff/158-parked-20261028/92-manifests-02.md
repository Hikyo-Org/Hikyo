## UNATTENDED-AUTHORITY-DISPOSITION.json

Original raw SHA256 `f4667597e4ae2c248b67ef0c4503da4e87af7ddf1b94d83831247fe98c815425`.

```json
{
  "at": "2026-10-28T05:52:07.781022+00:00",
  "recordedBy": "agent",
  "authority": "EXISTING_C12_SCOPE_WITH_LATER_OWNER_UNATTENDED_DELEGATION",
  "ownerInstructionPosition": 3902,
  "ownerInstructionSHA256": "ac7ce14c0fd3412fca2e0270dc67c7bb13f53b553106e080cf70c0e078b9229d",
  "claimSHA256": "fea113e1367f578b3a0626068532d242826c597edf6f995647f3bcf9e9725e20",
  "notAdditionalResourceOrBudgetGrant": true,
  "ownerUnavailable": true,
  "humanReadyOrExactHashReviewClaimed": false,
  "delegatedOperationalDecisions": [
    "One initial fixed c12 start after fresh qualified operator readiness; no shift or replacement window",
    "Accept only exact bound data independently rederived from the frozen18templates and verified actual key/window/namespace",
    "Release only the exact sole c12 protected deployment after source/MFA/fixture/nine-binding/once/budget gates",
    "Complete ordinary final application queue/merge only after genuine all46acceptance, independent cleanup and mandatory current-base CI; no admin bypass"
  ],
  "technicalGatesRetained": [
    "Account099256073061/eu-central-1",
    "One eighth key and exact c12alias; no cancellation; all old schedules retained",
    "32new/336lifetime charged slots; one hosted attempt; no retry/refund/redispatch/reassignment",
    "USD7.15usage/overalltargetbelow9; fresh complete billing/prices/inventory",
    "Exact18frozen template fingerprints; explicit value-read denials",
    "Official setup-user credential provider and real identity/MFA/sentinel checks; root credentials never substitute",
    "Fixed provisioning55m/ordinary2h/cleanup4h after oneT0",
    "5/5reviews; no independent CLEAN or review6"
  ],
  "currentLiveState": "HOLD_OWNER_IAM_ROOT_AUTHENTICATION_NOT_REESTABLISHED",
  "AWSResourceMutations": 0
}
```

## UNATTENDED-QUALIFICATION.json

Original raw SHA256 `86e89e9e3f98d9c1e921f32df02f48ef198aec58e0d53edcd45efe117412a56c`.

```json
{
  "at": "2026-10-28T05:52:08.451884+00:00",
  "passed": true,
  "exitCode": 0,
  "sourcePins": {
    "unattended_c12.py": "1acdd3f086e6575488dc9a8bdcc7b2bf40c8e5c267ffc3f8b155372c1d25c0d2",
    "test_unattended_c12.py": "56d593a01399172c2f9d2f230533cd2e8791a56cd79784e64d847d1578bec67e",
    "setup_probes_existing_provider_once.py": "e1f336e92ec21dc40424f97d50d67127c21990223937506cebdc767a7e954d7a",
    "qualify_unattended.py": "8b4b39477647a0067a8647c6e76db83577413f528a4c90c6415dc1e4908d8812",
    "UNATTENDED-SOURCE-DERIVATION.json": "618e8cae4cd535176b69a632964302ed8dbfeb47c0818103ca2afba9ecbb2474",
    "UNATTENDED-AUTHORITY-DISPOSITION.json": "f4667597e4ae2c248b67ef0c4503da4e87af7ddf1b94d83831247fe98c815425",
    "OWNER-UNATTENDED-REQUEST.json": "ac7ce14c0fd3412fca2e0270dc67c7bb13f53b553106e080cf70c0e078b9229d",
    "TYPED-OFFICIAL-PROVIDER-RESULT-02.json": "66414269660da1cfa0e40fde282b8c96e5910d73a3f50dae463ae58598638576"
  },
  "logSHA256": "3923049090f9c9d9b5e515cd033a2b462af4cf5ba2c35f926b7c75b22b40c256",
  "original104PinsPassed": true,
  "sourceUnchanged": true,
  "temporaryConfigurationRemoved": true,
  "AWSCalls": 0,
  "formalReview": false,
  "independentCLEAN": false,
  "review6": false,
  "liveOwnerAdmission": "HOLD_NO_WINDOW_OR_RESOURCE"
}
```

## PRICE-RECONCILIATION.json

Original raw SHA256 `aae42b3c82869139de144e24839970f32e09212a372ed13d9b5e087715253ae8`.

```json
{
  "at": "2026-10-28T05:36:43.765790+00:00",
  "region": "eu-central-1",
  "sources": [
    {
      "service": "awskms",
      "url": "https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/awskms/current/eu-central-1/index.json",
      "sha256": "0aa773d6fa48be9cdf1a79ff46beb9c8135f36cfe4245c541ef34c575c178cbe",
      "publicationDate": "2026-10-26T20:18:14Z",
      "rates": [
        {
          "sku": "PK7D6SUW8TP3XWZU",
          "usageType": "eu-central-1-KMS-Keys",
          "unit": "Keys",
          "USD": "1.0000000000",
          "description": "$1 per customer managed KMS key version in EU (Frankfurt)"
        },
        {
          "sku": "4H6XVYZMMD8MKVZD",
          "usageType": "eu-central-1-KMS-Requests",
          "unit": "Requests",
          "USD": "0.0000030000",
          "description": "$0.03 per 10000 KMS requests in EU (Frankfurt)"
        }
      ]
    },
    {
      "service": "AWSSecretsManager",
      "url": "https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/AWSSecretsManager/current/eu-central-1/index.json",
      "sha256": "675e1539f309b43af9c1a265d310110299ff889983ed27089a8dc35675937c1d",
      "publicationDate": "2026-09-11T12:46:10Z",
      "rates": [
        {
          "sku": "PCHWRK792Y74NHFN",
          "usageType": "EUC1-AWSSecretsManagerAPIRequest",
          "unit": "API Requests",
          "USD": "0.0000050000",
          "description": "$0.05 per 10000 API Requests"
        },
        {
          "sku": "N7A3USAT9EUMXNZ4",
          "usageType": "EUC1-AWSSecretsManager-Secrets",
          "unit": "Secrets",
          "USD": "0.4000000000",
          "description": "$0.40 per Secret"
        }
      ]
    }
  ],
  "billingStillRequired": true,
  "reserveReduction": false
}
```

## PARKED.json

Original raw SHA256 `e696c0d81ed57813c20a6bf4bfccea561ddaa30c254544ebfce7c67702a33f34`.

```json
{
  "at": "2026-10-28T07:42:48.197329+00:00",
  "status": "PARKED_BY_EXPLICIT_OWNER_REQUEST",
  "ownerInstructionAt": "2026-10-28T07:27:20.148Z",
  "ownerInstructionSHA256": "ca51d518fa89e2da21eafcc7a68a895b2fc737786e090d160a5c1ec9fc2a1cac",
  "ownerInstructionSummary": "Document everything into separate issue158comments, commit open changes, stop work for now; a later fresh agent resumes.",
  "supersedesUnattendedExecutionForNow": true,
  "c12ClockStarted": false,
  "c12AWSResourcesCreated": 0,
  "c12HostedDispatches": 0,
  "unusedChargedC12Slots": 32,
  "lifetimeChargedReservations": 336,
  "chargedSlotsRefunded": 0,
  "claimPreserved": "fea113e1367f578b3a0626068532d242826c597edf6f995647f3bcf9e9725e20",
  "newCloudOrMergeAuthority": false,
  "reminderEnabled": false,
  "newOwnerDirectionBeforeResume": true,
  "allPriorSchedulesAndReservesPreserved": true
}
```
