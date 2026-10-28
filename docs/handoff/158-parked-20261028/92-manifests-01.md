# Preserved operator manifests and nonsecret reconstruction data

Archive only. Original raw SHA256 is listed before every normalized document. Any path normalization changes the displayed bytes; the old digest authenticates the original, not the transcription. Live statuses in these historical records are superseded by parking.

## OPERATOR-PREPARATION-MANIFEST.json

Original raw SHA256 `f9b09c093f9943be8fd209bc428bab758eef7305c018491b30748dafbbfc200f`.

```json
{
  "at": "2026-10-28T01:00:19.673559+00:00",
  "schema": "hikyo-158/c12-actual-claim-data-preparation/v1",
  "status": "PREPARED_NOT_EXECUTED_OR_READY",
  "claimSHA256": "fea113e1367f578b3a0626068532d242826c597edf6f995647f3bcf9e9725e20",
  "proposalCanonicalSHA256": "77f086009324260f41d6ca10e2b7a63cbc328fe4935f0865d80931665006b2d9",
  "files": {
    "admit_c12.py": {
      "origin": "<report-root>/completion-c12-proposal/admit_c12-DRAFT.py",
      "originSHA256": "a670e8864fe22472ee30aacada5e112c20801323e41efaf0a458e11ce34b6924",
      "sha256": "171e1aa7aaedb78788acadee1ec10103062ea3c6b99e668dc1b380a42ef2373b",
      "replacements": [
        {
          "from": "UNBOUND_APPROVED_C12_CLAIM_SHA256",
          "to": "fea113e1367f578b3a0626068532d242826c597edf6f995647f3bcf9e9725e20",
          "occurrences": 1
        }
      ]
    },
    "bind_console_data.py": {
      "origin": "<report-root>/completion-c11/bind_console_data.py",
      "originSHA256": "08e5b67fe1109403d58cc728254a7ef2642220c80b4cd3fcfbc8f13699d85cb2",
      "sha256": "fb82cd063ea40d0d99b877237490fc9db0889719265b5a251b207938b525c91e",
      "replacements": [
        {
          "from": "20261027c11",
          "to": "20261028c12",
          "occurrences": 1
        },
        {
          "from": "c11",
          "to": "c12",
          "occurrences": 9
        },
        {
          "from": "C11",
          "to": "C12",
          "occurrences": 2
        }
      ]
    },
    "bind_final_iam_cleanup.py": {
      "origin": "<report-root>/completion-c11/bind_final_iam_cleanup.py",
      "originSHA256": "aa9947b05da4083bca6c1d5633c83808d2a6e4c25ea12f367cfc69f23c98d81d",
      "sha256": "b02911517f3784935f973c7aa03272487bdaf78025cb89862c7100ce56661a06",
      "replacements": [
        {
          "from": "c11",
          "to": "c12",
          "occurrences": 3
        },
        {
          "from": "C11",
          "to": "C12",
          "occurrences": 1
        }
      ]
    },
    "bind_policies.py": {
      "origin": "<report-root>/completion-c11/bind_policies.py",
      "originSHA256": "fc2b8beef44c6a017451f68f3e4940d5f193ca2e271b121b47fdbc1676d5e5a0",
      "sha256": "fc2b8beef44c6a017451f68f3e4940d5f193ca2e271b121b47fdbc1676d5e5a0",
      "replacements": []
    },
    "bind_protected_vars.py": {
      "origin": "<report-root>/completion-c11/bind_protected_vars.py",
      "originSHA256": "0e69e9e92d86ddef57c6fe2522e84f7e08b5799ecc36212c02bc9cb81471ede3",
      "sha256": "1ba327f088da6abf253e8f18cd06e91a5b058f3714e7fa9576a81d390b353382",
      "replacements": [
        {
          "from": "20261027c11",
          "to": "20261028c12",
          "occurrences": 2
        },
        {
          "from": "c11",
          "to": "c12",
          "occurrences": 2
        },
        {
          "from": "361e02e3915eef840429d88589c39689f4a58dfb",
          "to": "c253ff9b1ccec7408f13366f8c5640c8c7cf3d0a",
          "occurrences": 2
        }
      ]
    },
    "bind_window_once.py": {
      "origin": "<report-root>/completion-c11/bind_window_once.py",
      "originSHA256": "f9ee6980d415f352e773c0ab318e746872df50f37659c2d4cb44e360bda66848",
      "sha256": "02df54767610ea66c371f4e6be00f14e0f96b3956fdc2c10db0b1a4a1cedaed9",
      "replacements": [
        {
          "from": "20261027c11",
          "to": "20261028c12",
          "occurrences": 1
        },
        {
          "from": "c11",
          "to": "c12",
          "occurrences": 2
        },
        {
          "from": "C11",
          "to": "C12",
          "occurrences": 1
        },
        {
          "from": "08a101286f0c30f97f0c97a6e8cc9671252eb39ee77149ab7ff323ecf0d6eec9",
          "to": "fea113e1367f578b3a0626068532d242826c597edf6f995647f3bcf9e9725e20",
          "occurrences": 1
        },
        {
          "from": "position']) is int and ready['position'] > 3186",
          "to": "position']) is int and ready['position'] > 3828",
          "occurrences": 1
        }
      ]
    },
    "dispatch_workflow_once.py": {
      "origin": "<report-root>/completion-c11/dispatch_workflow_once.py",
      "originSHA256": "b8e68847da201766aca40ff778703aa8e628af1522a50096d82ff4bd39a7115d",
      "sha256": "620002d755872481454080ced261facdf64c220b50dc3fb8a48a9022e41cd3e7",
      "replacements": [
        {
          "from": "c11",
          "to": "c12",
          "occurrences": 1
        },
        {
          "from": "C11",
          "to": "C12",
          "occurrences": 1
        },
        {
          "from": "361e02e3915eef840429d88589c39689f4a58dfb",
          "to": "c253ff9b1ccec7408f13366f8c5640c8c7cf3d0a",
          "occurrences": 1
        },
        {
          "from": "'newReservations':0,'assignedClosedC10PoolSlots':32",
          "to": "'newReservations':32",
          "occurrences": 1
        },
        {
          "from": "'lifetime':304",
          "to": "'lifetime':336",
          "occurrences": 1
        }
      ]
    },
    "fetch_hosted_terminal_evidence.py": {
      "origin": "<report-root>/completion-c11/fetch_hosted_terminal_evidence.py",
      "originSHA256": "2e2bacb2688ef3c0b40b9a08c5e1f8ecffbab46bf2f6a2423f2b38b492352eda",
      "sha256": "ab29d34a76c74c06454322065c19dfabce26ce4fd3043d0925b144a07aa82cdb",
      "replacements": [
        {
          "from": "20261027c11",
          "to": "20261028c12",
          "occurrences": 1
        },
        {
          "from": "c11",
          "to": "c12",
          "occurrences": 1
        },
        {
          "from": "361e02e3915eef840429d88589c39689f4a58dfb",
          "to": "c253ff9b1ccec7408f13366f8c5640c8c7cf3d0a",
          "occurrences": 2
        }
      ]
    },
    "key_once.py": {
      "origin": "<report-root>/completion-c11/key_once.py",
      "originSHA256": "28465958eeeeb71d42c4f5d90a813c6c39b52bf2220782ac84279bd06b57228b",
      "sha256": "5190a661bcc53ff625dbaa9837b66020fcac6fe7e71571abc857c6f6d21261a0",
      "replacements": [
        {
          "from": "20261027c11",
          "to": "20261028c12",
          "occurrences": 3
        },
        {
          "from": "c11",
          "to": "c12",
          "occurrences": 3
        },
        {
          "from": "C11",
          "to": "C12",
          "occurrences": 1
        }
      ]
    },
    "official_setup_login_once.py": {
      "origin": "<report-root>/completion-c11/official_setup_login_once.py",
      "originSHA256": "25b5c6887f4c829f268c12f57b9f2bab14f1a5bad627b7261bb15f7c90679fa5",
      "sha256": "10de1ea8befcbac5a21ea2d00b386b1a478871d3ea4328867fef4921e17b3fdb",
      "replacements": [
        {
          "from": "c11",
          "to": "c12",
          "occurrences": 3
        },
        {
          "from": "C11",
          "to": "C12",
          "occurrences": 1
        },
        {
          "from": "private-operator/setup-login-clean-c12",
          "to": "private-operator/setup-login-clean-c11",
          "occurrences": 1
        }
      ]
    },
    "prepare_iam_once.py": {
      "origin": "<report-root>/completion-c11/prepare_iam_once.py",
      "originSHA256": "0ff65a085fa1e46d6063e5b300a7dc6f88cb39a7759890a14cdeed236d62f908",
      "sha256": "f8aa31e47129e50849a462bc3b6d3ff7d65fa6369fbe01131dcfba1d235fcd62",
      "replacements": [
        {
          "from": "c11",
          "to": "c12",
          "occurrences": 5
        },
        {
          "from": "v10",
          "to": "v11",
          "occurrences": 4
        },
        {
          "from": "V10",
          "to": "V11",
          "occurrences": 2
        }
      ]
    },
    "provision_fixture_once.py": {
      "origin": "<report-root>/completion-c11/provision_fixture_once.py",
      "originSHA256": "e281171469e716d8548d7f897a854d7ba823f4b97edfa651a7a6e9dbf3078108",
      "sha256": "60e86d575d28802034a7c12d62607c88a32c550d354ab2d7c0385f146bc2d5d5",
      "replacements": [
        {
          "from": "c11",
          "to": "c12",
          "occurrences": 1
        },
        {
          "from": "C11",
          "to": "C12",
          "occurrences": 1
        },
        {
          "from": "'newReservations':0,'assignedClosedC10PoolSlots':32",
          "to": "'newReservations':32",
          "occurrences": 1
        },
        {
          "from": "'newReservationsCharged':0,'assignedClosedC10PoolSlots':32",
          "to": "'newReservationsCharged':32",
          "occurrences": 1
        },
        {
          "from": "'lifetime':304",
          "to": "'lifetime':336",
          "occurrences": 3
        }
      ]
    },
    "resolve_and_report.py": {
      "origin": "<report-root>/completion-c11/resolve_and_report.py",
      "originSHA256": "2c2672ecb4ccd67949db53907cd1640e5966205935cba3d92c7466bfb0425cab",
      "sha256": "2606151802ae653d17fe9b5864901cf0b00c2c90bc4acbd0c5ef58aa34399281",
      "replacements": [
        {
          "from": "20261027c11",
          "to": "20261028c12",
          "occurrences": 7
        },
        {
          "from": "c11",
          "to": "c12",
          "occurrences": 10
        },
        {
          "from": "v10",
          "to": "v11",
          "occurrences": 1
        },
        {
          "from": "'newReservationsCharged':0,'assignedClosedC10PoolSlots':32",
          "to": "'newReservationsCharged':32",
          "occurrences": 1
        },
        {
          "from": "'lifetimeReservations':304",
          "to": "'lifetimeReservations':336",
          "occurrences": 1
        },
        {
          "from": "repair-c09-diagnosis-20261027/SOURCE-MANIFEST.json",
          "to": "completion-c12-proposal/FROZEN-SOURCE-MANIFEST.json",
          "occurrences": 1
        },
        {
          "from": "All six prior bootstrap keys",
          "to": "All seven prior bootstrap keys",
          "occurrences": 1
        },
        {
          "from": "Maximum seven new keys",
          "to": "Maximum eight new keys",
          "occurrences": 1
        },
        {
          "from": "all six prior new-key deletion schedules",
          "to": "all seven prior new-key deletion schedules",
          "occurrences": 1
        },
        {
          "from": "reassigns 32 unused charged c10 slots with zero new charge, lifetime 304",
          "to": "adds 32 newly charged slots, lifetime 336",
          "occurrences": 1
        }
      ]
    },
    "retire_fixture_once.py": {
      "origin": "<report-root>/completion-c11/retire_fixture_once.py",
      "originSHA256": "b77c8d80ae00947257c00075262749554b5e39ca9506ddf00b946a4d1dcfa4b8",
      "sha256": "645e668b35c75e12d8e5d060aa8f0404abdc8ccc3c5cc15d829507adc5e134f6",
      "replacements": [
        {
          "from": "20261027c11",
          "to": "20261028c12",
          "occurrences": 1
        },
        {
          "from": "c11",
          "to": "c12",
          "occurrences": 1
        },
        {
          "from": "C11",
          "to": "C12",
          "occurrences": 1
        }
      ]
    },
    "retire_run_variables_once.py": {
      "origin": "<report-root>/completion-c11/retire_run_variables_once.py",
      "originSHA256": "0b928b5105b6442b54ff2faea07fcb8f7560b2fbf4fa4b1cd70fbfe6c9fea8e2",
      "sha256": "9f80d47d04e8bce8b49828fbea781da72f239178c439ee419d8894625020c5ce",
      "replacements": [
        {
          "from": "c11",
          "to": "c12",
          "occurrences": 1
        }
      ]
    },
    "setup_probes_after_login_once.py": {
      "origin": "<report-root>/completion-c11/setup_probes_after_login_once.py",
      "originSHA256": "31e70fa5e30111b2d811ad2f69d54d365e23fd682c1024dc18bcea7612839f9f",
      "sha256": "d909b2ea986afeb669dc9800b7ce592b3b579aa1930b159d71ee33da61224b1a",
      "replacements": [
        {
          "from": "20261027c11",
          "to": "20261028c12",
          "occurrences": 1
        },
        {
          "from": "c11",
          "to": "c12",
          "occurrences": 1
        }
      ]
    },
    "verify_github_preparation.py": {
      "origin": "<report-root>/completion-c12-proposal/verify_github_preparation.py",
      "originSHA256": "dc21e05b2dbcee66f27794590fc30354874f270bf63b5f6d57d595e9244b104a",
      "sha256": "dc21e05b2dbcee66f27794590fc30354874f270bf63b5f6d57d595e9244b104a",
      "replacements": []
    },
    "verify_iam_step.py": {
      "origin": "<report-root>/completion-c11/verify_iam_step.py",
      "originSHA256": "36e8e23332245ec954df0da81bdd3f723009883590ef98d71cff2d4478feb718",
      "sha256": "b1a2f161402c55c7c85a59b0f6561b6acd848ac1408f4e92492aa1a16970e59c",
      "replacements": [
        {
          "from": "c11",
          "to": "c12",
          "occurrences": 6
        },
        {
          "from": "v10",
          "to": "v11",
          "occurrences": 5
        },
        {
          "from": "V10",
          "to": "V11",
          "occurrences": 2
        }
      ]
    },
    "verify_secret_absence_once.py": {
      "origin": "<report-root>/completion-c11/verify_secret_absence_once.py",
      "originSHA256": "93f0229a09a9f6ffa8f0c9719c50141de0743278fd1930d1483330cbfe252aee",
      "sha256": "e21ed9b81e7de78e25152a0535c41b1571abee0837cc0297a07a32e612178a53",
      "replacements": [
        {
          "from": "20261027c11",
          "to": "20261028c12",
          "occurrences": 3
        },
        {
          "from": "c11",
          "to": "c12",
          "occurrences": 1
        },
        {
          "from": "361e02e3915eef840429d88589c39689f4a58dfb",
          "to": "c253ff9b1ccec7408f13366f8c5640c8c7cf3d0a",
          "occurrences": 1
        }
      ]
    },
    "test_bind_policies.py": {
      "origin": "<report-root>/completion-c11/test_bind_policies.py",
      "originSHA256": "88c6cd900c597d2706e809787e923b885f9a41713734984d3c3995cf64af184d",
      "sha256": "88c6cd900c597d2706e809787e923b885f9a41713734984d3c3995cf64af184d",
      "replacements": []
    },
    "test_kms_inventory.py": {
      "origin": "<report-root>/completion-c11/test_kms_inventory.py",
      "originSHA256": "c893616b92c0c96d22c070d3cf8d47ef04b821888fbae856d8e04457dadeea64",
      "sha256": "5ee98dc3a0f7201387add553502fbd2ed24ca3b556c9463d1c676d86a0a0d8eb",
      "replacements": [
        {
          "from": "c11",
          "to": "c12",
          "occurrences": 1
        }
      ]
    },
    "test_operator_bindings.py": {
      "origin": "<report-root>/completion-c11/test_operator_bindings.py",
      "originSHA256": "962a7d74f75b883df73400855c55b4645fbd5a99e68279b6c1328d37c718e85d",
      "sha256": "962a7d74f75b883df73400855c55b4645fbd5a99e68279b6c1328d37c718e85d",
      "replacements": []
    },
    "test_retire_fixture.py": {
      "origin": "<report-root>/completion-c11/test_retire_fixture.py",
      "originSHA256": "259a3cd2d84348a955046e9d03374c90e26a413871cdeb4bc6a111bd2c5b87db",
      "sha256": "89e2ae0b953602a8eb63c6939f7a7e6e3620b5a1cb779e6bf454b8d08185cb49",
      "replacements": [
        {
          "from": "20261027c11",
          "to": "20261028c12",
          "occurrences": 1
        },
        {
          "from": "c11",
          "to": "c12",
          "occurrences": 1
        },
        {
          "from": "C11",
          "to": "C12",
          "occurrences": 1
        }
      ]
    },
    "test_variable_retirement.py": {
      "origin": "<report-root>/completion-c11/test_variable_retirement.py",
      "originSHA256": "1ba699ed2758f1b2e9830a70a8ae6588dfec67fba3a658aa4183d73e2642ebd9",
      "sha256": "1ba699ed2758f1b2e9830a70a8ae6588dfec67fba3a658aa4183d73e2642ebd9",
      "replacements": []
    },
    "test_window_budget.py": {
      "origin": "<report-root>/completion-c11/test_window_budget.py",
      "originSHA256": "4dfa597c30013df6dffb33515ac7f4c967882324cc46b0b383dd33ef192ba665",
      "sha256": "e66afcbb202268fe007c826aece8afb77ef0b00f99178ae90b94f7a57006693b",
      "replacements": [
        {
          "from": "c11",
          "to": "c12",
          "occurrences": 6
        },
        {
          "from": "C11",
          "to": "C12",
          "occurrences": 1
        },
        {
          "from": "'position':3186",
          "to": "'position':3828",
          "occurrences": 1
        },
        {
          "from": "'6.17'",
          "to": "'7.00'",
          "occurrences": 1
        },
        {
          "from": "'7.17'",
          "to": "'8.00'",
          "occurrences": 1
        },
        {
          "from": "'7.12'",
          "to": "'7.95'",
          "occurrences": 1
        },
        {
          "from": "'6.30'",
          "to": "'7.15'",
          "occurrences": 1
        },
        {
          "from": "'0.18'",
          "to": "'0.20'",
          "occurrences": 1
        },
        {
          "from": "'0.19'",
          "to": "'0.21'",
          "occurrences": 1
        }
      ]
    },
    "test-iam-final-action.mjs": {
      "origin": "<report-root>/completion-c12-proposal/test-iam-final-action.mjs",
      "originSHA256": "4c3c9263da58d1a3b7345792f5863fa9f8cbffb28bc203cda45ec5a6141c42f5",
      "sha256": "4c3c9263da58d1a3b7345792f5863fa9f8cbffb28bc203cda45ec5a6141c42f5",
      "replacements": []
    },
    "iam-cleanup-DRAFT.js": {
      "origin": "<report-root>/completion-c11/iam-cleanup-DRAFT.js",
      "originSHA256": "9821a4d641b9147a4e5ded29f2314141eed95546c9aa80b671685a51c9ddecf0",
      "sha256": "9821a4d641b9147a4e5ded29f2314141eed95546c9aa80b671685a51c9ddecf0",
      "replacements": []
    },
    "iam-final-action-DRAFT.js": {
      "origin": "<report-root>/completion-c12-proposal/iam-final-action-DRAFT.js",
      "originSHA256": "da3406c6410c172550403c8d3c90675942a29ace27ca40957e7386624a3cc4c7",
      "sha256": "da3406c6410c172550403c8d3c90675942a29ace27ca40957e7386624a3cc4c7",
      "replacements": []
    },
    "key-finish-DRAFT.js": {
      "origin": "<report-root>/completion-c11/key-finish-DRAFT.js",
      "originSHA256": "79b75b4e03d44b8fe300a067379b2e284c82a8b423f111d2bb5dd325d00cada8",
      "sha256": "79b75b4e03d44b8fe300a067379b2e284c82a8b423f111d2bb5dd325d00cada8",
      "replacements": []
    },
    "retire-alias-once-DRAFT.js": {
      "origin": "<report-root>/completion-c11/retire-alias-once-DRAFT.js",
      "originSHA256": "a58515be3fd8bf71e8741a583cab8f3c193499473ba21e8c4278fae83de3a3ef",
      "sha256": "9888a1ea42df7fc8897bce3107d3c693ef2fe5ab7775d1da7aafe7a2e57068d7",
      "replacements": [
        {
          "from": "20261027c11",
          "to": "20261028c12",
          "occurrences": 3
        },
        {
          "from": "C11",
          "to": "C12",
          "occurrences": 2
        }
      ]
    },
    "schedule-key-once-DRAFT.js": {
      "origin": "<report-root>/completion-c11/schedule-key-once-DRAFT.js",
      "originSHA256": "fb7613c05724e56e6395a64392524301cd3fc1ddd622d2e8644bd969957981c9",
      "sha256": "4604820e3ca47c20b3edf880acd497e6fd81d983e730ad6c0ca8acec33d55c6c",
      "replacements": [
        {
          "from": "C11",
          "to": "C12",
          "occurrences": 3
        }
      ]
    },
    "c12-billing-admission.js": {
      "origin": "<report-root>/completion-c11/c11-billing-admission.js",
      "originSHA256": "4604f4fbb32c99c1d850bb05345e66f75aedd22ccdecc7cba6c70a8c1da2c3a0",
      "sha256": "4604f4fbb32c99c1d850bb05345e66f75aedd22ccdecc7cba6c70a8c1da2c3a0",
      "replacements": []
    },
    "c12-boundary-draft-DRAFT.js": {
      "origin": "<report-root>/completion-c11/c11-boundary-draft-DRAFT.js",
      "originSHA256": "ade2cab2d764315b7d3d222fa63259314c766245b10a2635a61c9770f3be370b",
      "sha256": "ade2cab2d764315b7d3d222fa63259314c766245b10a2635a61c9770f3be370b",
      "replacements": []
    },
    "c12-historical-secret-admission.js": {
      "origin": "<report-root>/completion-c11/c11-historical-secret-admission.js",
      "originSHA256": "6c51689b6d490efdeec4b0d6261a1aabe087c41450c7d9780580dc7ce5f0fe78",
      "sha256": "6c51689b6d490efdeec4b0d6261a1aabe087c41450c7d9780580dc7ce5f0fe78",
      "replacements": []
    },
    "c12-iam-admission.js": {
      "origin": "<report-root>/completion-c11/c11-iam-admission.js",
      "originSHA256": "646b47d171a91b379c8cfd8a995953941cc08b031249e3a7e86bae01c63ac93a",
      "sha256": "646b47d171a91b379c8cfd8a995953941cc08b031249e3a7e86bae01c63ac93a",
      "replacements": []
    },
    "c12-key-draft.js": {
      "origin": "<report-root>/completion-c11/c11-key-draft.js",
      "originSHA256": "ddee742540dd45ddd7ff7f9a44f8e298fd295ef8fb24916981db5f0b22909af8",
      "sha256": "23f8306192578a410e35c7d0c6681c19a5fe4a0d391ffe5400f30e46f9b10d2d",
      "replacements": [
        {
          "from": "20261027c11",
          "to": "20261028c12",
          "occurrences": 2
        }
      ]
    },
    "c12-key-readback-DRAFT.js": {
      "origin": "<report-root>/completion-c11/c11-key-readback-DRAFT.js",
      "originSHA256": "aaa19a1f3fa97ab6eaaa33a7cbc729c7edd875fdc7ff510621d9fd769a750bee",
      "sha256": "3e267852cf18aed175eddf1664866b61c8fdfed60f975093c085fd43389580f0",
      "replacements": [
        {
          "from": "20261027c11",
          "to": "20261028c12",
          "occurrences": 1
        },
        {
          "from": "C11",
          "to": "C12",
          "occurrences": 3
        }
      ]
    },
    "c12-kms-admission.js": {
      "origin": "<report-root>/completion-c12-proposal/c12-kms-admission-DRAFT.js",
      "originSHA256": "f4bb34b353bd9c42b03dc4af4a319267261e8cd011f502eda21a0bf382c00363",
      "sha256": "f4bb34b353bd9c42b03dc4af4a319267261e8cd011f502eda21a0bf382c00363",
      "replacements": []
    },
    "c12-kms-list-complete.js": {
      "origin": "<report-root>/completion-c11/c11-kms-list-complete.js",
      "originSHA256": "de95be93d13418d2582b4c41019859d052d4ff2bb809ea1976db24d559921cc9",
      "sha256": "de95be93d13418d2582b4c41019859d052d4ff2bb809ea1976db24d559921cc9",
      "replacements": []
    },
    "c12-secret-admission.js": {
      "origin": "<report-root>/completion-c11/c11-secret-admission.js",
      "originSHA256": "ec18f6fc1ef9db40b9ac6e921362b39ed257143ffe41cecc639ffab3ff7e2fb7",
      "sha256": "ec18f6fc1ef9db40b9ac6e921362b39ed257143ffe41cecc639ffab3ff7e2fb7",
      "replacements": []
    },
    "c12-setup-draft-DRAFT.js": {
      "origin": "<report-root>/completion-c11/c11-setup-draft-DRAFT.js",
      "originSHA256": "8399d2342f798821f5ca244393415622a902df0b8810a6c263c83cd2fe6dd679",
      "sha256": "8399d2342f798821f5ca244393415622a902df0b8810a6c263c83cd2fe6dd679",
      "replacements": []
    },
    "c12-setup-v11-archive.js": {
      "origin": "<report-root>/completion-c11/c11-setup-v10-archive.js",
      "originSHA256": "d6e1e64159c483b119a2dac1fc861b25872f596c1fe2e6247226135e8314767c",
      "sha256": "0d9fe1af299ddc7b56bd77036277896c4ac28897eb7aa2828ba24b0748d475d7",
      "replacements": [
        {
          "from": "v10",
          "to": "v11",
          "occurrences": 1
        },
        {
          "from": "Version 10",
          "to": "Version 11",
          "occurrences": 1
        }
      ]
    },
    "c12-v11-delete-draft.js": {
      "origin": "<report-root>/completion-c11/c11-v10-delete-draft.js",
      "originSHA256": "1c6789fa0ac9b961545977f74dc6c1a622afcae739845bdc6345fe511bce7848",
      "sha256": "4462e3ab41eb832028a3fd85c9ae769ca33f347f9214ed857742263303fe4d10",
      "replacements": [
        {
          "from": "v10",
          "to": "v11",
          "occurrences": 2
        },
        {
          "from": "Version 10",
          "to": "Version 11",
          "occurrences": 1
        },
        {
          "from": "version\\s*10",
          "to": "version\\s*11",
          "occurrences": 1
        }
      ]
    },
    "run_console_preflight.py": {
      "origin": "<report-root>/completion-c11/run_console_preflight.py",
      "originSHA256": "f0dab56c23eb53810350ee3f0e44d82a11104ded2ec62df4eaf98afc0cbc8f26",
      "sha256": "7b1774753a6030c63fb3f48eb6eff91742891016cec55eeff462a5399a644d2f",
      "replacements": [
        {
          "from": "c11",
          "to": "c12",
          "occurrences": 10
        },
        {
          "from": "v10",
          "to": "v11",
          "occurrences": 1
        }
      ]
    },
    "c12-owner-principal.js": {
      "origin": "<report-root>/completion-c11/c11-owner-principal.js",
      "originSHA256": "ab3c85482cb3faed9231554ac4325dff75d930fe02ae96e7ff20767aa3ecbc96",
      "sha256": "ab3c85482cb3faed9231554ac4325dff75d930fe02ae96e7ff20767aa3ecbc96",
      "replacements": []
    },
    "validate_operator.py": {
      "origin": "<report-root>/completion-c11/validate_operator.py",
      "originSHA256": "cad42f32f6ba3ca9f100b9ff478fcd408b105c7247d4e4df8a73572ce85d05dc",
      "sha256": "697df2c850591d4227383948bfe9b1397e7f3a5f868cdd9314f8e9fe0f9b6d98",
      "replacements": [
        {
          "from": "c11",
          "to": "c12",
          "occurrences": 1
        }
      ],
      "postPreparationDataBindings": [
        {
          "beforeSHA256": "318b06ef7b85e13e38c4d0b5fa5f156b185315fd54a30761ccf38e28cdab11f8",
          "replacements": [
            {
              "from": "OPERATOR-VALIDATION-01",
              "to": "OPERATOR-VALIDATION-02",
              "occurrences": 3
            }
          ],
          "scope": "Owner-approved c12 budget expectations; new evidence output filenames only"
        }
      ]
    },
    "aws-acceptance.yml": {
      "origin": "<report-root>/completion-c11/aws-acceptance.yml",
      "originSHA256": "0bb9e4fe28055d03ba7a055c3e7595aa316b3869d22eddd59bfafe2d54314b6a",
      "sha256": "0bb9e4fe28055d03ba7a055c3e7595aa316b3869d22eddd59bfafe2d54314b6a",
      "replacements": []
    },
    "SOURCE-PUBLICATION-CANDIDATE.json": {
      "origin": "<report-root>/completion-c12-proposal/SOURCE-PUBLICATION-CANDIDATE.json",
      "originSHA256": "74537a1c447f9a453d947a56aebd36c2e8ea8d0b4039c741c2cc6b029f6f318a",
      "sha256": "74537a1c447f9a453d947a56aebd36c2e8ea8d0b4039c741c2cc6b029f6f318a",
      "replacements": []
    },
    "SOURCE-PUBLICATION-RECEIPT.json": {
      "origin": "<report-root>/completion-c11/SOURCE-PUBLICATION-RECEIPT.json",
      "originSHA256": "c443d952439bd50388cc83b69f871b651c81e46cad3edabbdbfd95ab838821bf",
      "sha256": "c443d952439bd50388cc83b69f871b651c81e46cad3edabbdbfd95ab838821bf",
      "replacements": []
    },
    "EXPECTED-HOSTED-TESTS.json": {
      "origin": "<report-root>/completion-c11/EXPECTED-HOSTED-TESTS.json",
      "originSHA256": "77073c98fcae9511190378e8f66c02f8f7c19f8781fe979ed52fa40934644cbf",
      "sha256": "77073c98fcae9511190378e8f66c02f8f7c19f8781fe979ed52fa40934644cbf",
      "replacements": []
    },
    "pinned-runner.py": {
      "origin": "<report-root>/completion-c11/pinned-runner.py",
      "originSHA256": "00f8117346bd4d50aa5defe4968333822b388a727966141b6bcd9236f28ff3d0",
      "sha256": "00f8117346bd4d50aa5defe4968333822b388a727966141b6bcd9236f28ff3d0",
      "replacements": []
    },
    "BILLING-NATIVE-TABLE-PROFILE.json": {
      "origin": "<report-root>/completion-c11/BILLING-NATIVE-TABLE-PROFILE.json",
      "originSHA256": "9d6a3331e89f242d71d756af8713f86e75552346efb720ae1514d64d7f1ca02a",
      "sha256": "9d6a3331e89f242d71d756af8713f86e75552346efb720ae1514d64d7f1ca02a",
      "replacements": []
    },
    "prior_key_schedules.py": {
      "origin": "<report-root>/completion-c11/prior_key_schedules.py",
      "originSHA256": "5fc472cb06adb4afa74fc88f5993ac668479da8f4b88f98ce41aebb920f947fc",
      "sha256": "5fc472cb06adb4afa74fc88f5993ac668479da8f4b88f98ce41aebb920f947fc",
      "replacements": []
    },
    "test_prior_key_schedules.py": {
      "origin": "<report-root>/completion-c11/test_prior_key_schedules.py",
      "originSHA256": "82551cc18fa52150cebf5e050cbea6aa6f00cb60e04be87690390c58ba0d0618",
      "sha256": "82551cc18fa52150cebf5e050cbea6aa6f00cb60e04be87690390c58ba0d0618",
      "replacements": []
    },
    "normalize_native_billing.py": {
      "origin": "<report-root>/completion-c11/normalize_native_billing.py",
      "originSHA256": "9808fe3755cea9a4e419a76e2e0d19da9866bcbcbcdd09000cfb0ca4695ce60a",
      "sha256": "880bc918d94508804eb40d835c5e206cbf6575c193cc17e61e4462d8c34374ed",
      "replacements": [
        {
          "from": "c11",
          "to": "c12",
          "occurrences": 5
        }
      ]
    },
    "refresh_prices.py": {
      "origin": "<report-root>/completion-c11/refresh_prices.py",
      "originSHA256": "78da4f860eb413245fbf50f09421ce7f0e47d70c285a59cbc79340cb0f24830d",
      "sha256": "78da4f860eb413245fbf50f09421ce7f0e47d70c285a59cbc79340cb0f24830d",
      "replacements": []
    },
    "test_billing_proof.py": {
      "origin": "<report-root>/completion-c11/test_billing_proof.py",
      "originSHA256": "1d6d2d157d3a2c87d32eaa5f8d4d7ccbf838d6908f07b29b635fbfed2579dc5e",
      "sha256": "1c3108dae5665a65914aa3b42b9c33c3d0193ccdcd5cf3978e366afa136e06de",
      "replacements": [
        {
          "from": "c11",
          "to": "c12",
          "occurrences": 1
        }
      ],
      "postPreparationDataBindings": [
        {
          "beforeSHA256": "578fe15fc0cae6cd9986569d0b6ce1bac179dbedcc7bab2ecf1e9e0da6ccbc66",
          "replacements": [
            {
              "from": "'6.19'",
              "to": "'7.02'",
              "occurrences": 1
            },
            {
              "from": "'USD\\u00a00.18'",
              "to": "'USD\\u00a00.20'",
              "occurrences": 1
            },
            {
              "from": "'0.19'",
              "to": "'0.21'",
              "occurrences": 2
            }
          ],
          "scope": "Owner-approved c12 budget expectations; new evidence output filenames only"
        }
      ]
    },
    "BILLING-NATIVE-RED-FIXTURE.json": {
      "origin": "<report-root>/completion-c11/BILLING-NATIVE-RED-FIXTURE.json",
      "originSHA256": "f659c0a577cbd851a043ce2b5707f14ebbf538198413908755a661e9cd6ceea0",
      "sha256": "f659c0a577cbd851a043ce2b5707f14ebbf538198413908755a661e9cd6ceea0",
      "replacements": []
    },
    "BILLING-NORMALIZED-FIXTURE.json": {
      "origin": "<report-root>/completion-c11/BILLING-NORMALIZED-FIXTURE.json",
      "originSHA256": "31c672d510193911315547a73abad83d102e3af6aa26f842dbf26f49497d6a6c",
      "sha256": "31c672d510193911315547a73abad83d102e3af6aa26f842dbf26f49497d6a6c",
      "replacements": []
    },
    "c12-boundary-readback.js": {
      "origin": "<report-root>/completion-c11/c11-boundary-readback.js",
      "originSHA256": "ed7796ca6a53162c2a749a26aab9b64cede8a7fe3b08a3ae473889dcd26fd197",
      "sha256": "ed7796ca6a53162c2a749a26aab9b64cede8a7fe3b08a3ae473889dcd26fd197",
      "replacements": []
    },
    "c12-v11-deletion-readback.js": {
      "origin": "<report-root>/completion-c11/c11-v10-deletion-readback.js",
      "originSHA256": "646b47d171a91b379c8cfd8a995953941cc08b031249e3a7e86bae01c63ac93a",
      "sha256": "646b47d171a91b379c8cfd8a995953941cc08b031249e3a7e86bae01c63ac93a",
      "replacements": []
    },
    "c12-setup-readback.js": {
      "origin": "<report-root>/completion-c11/c11-setup-readback.js",
      "originSHA256": "646b47d171a91b379c8cfd8a995953941cc08b031249e3a7e86bae01c63ac93a",
      "sha256": "646b47d171a91b379c8cfd8a995953941cc08b031249e3a7e86bae01c63ac93a",
      "replacements": []
    },
    "c12-post-iam-retirement-readback.js": {
      "origin": "<report-root>/completion-c11/c11-post-iam-retirement-readback.js",
      "originSHA256": "646b47d171a91b379c8cfd8a995953941cc08b031249e3a7e86bae01c63ac93a",
      "sha256": "646b47d171a91b379c8cfd8a995953941cc08b031249e3a7e86bae01c63ac93a",
      "replacements": []
    },
    "c12-final-iam-reconciliation.js": {
      "origin": "<report-root>/completion-c11/c11-final-iam-reconciliation.js",
      "originSHA256": "646b47d171a91b379c8cfd8a995953941cc08b031249e3a7e86bae01c63ac93a",
      "sha256": "646b47d171a91b379c8cfd8a995953941cc08b031249e3a7e86bae01c63ac93a",
      "replacements": []
    },
    "c12-final-iam-closure.js": {
      "origin": "<report-root>/completion-c11/c11-final-iam-closure.js",
      "originSHA256": "646b47d171a91b379c8cfd8a995953941cc08b031249e3a7e86bae01c63ac93a",
      "sha256": "646b47d171a91b379c8cfd8a995953941cc08b031249e3a7e86bae01c63ac93a",
      "replacements": []
    },
    "c12-unused-boundary-readback.js": {
      "origin": "<report-root>/completion-c11/c11-unused-boundary-readback.js",
      "originSHA256": "ed7796ca6a53162c2a749a26aab9b64cede8a7fe3b08a3ae473889dcd26fd197",
      "sha256": "ed7796ca6a53162c2a749a26aab9b64cede8a7fe3b08a3ae473889dcd26fd197",
      "replacements": []
    },
    "c12-final-secret-closure.js": {
      "origin": "<report-root>/completion-c11/c11-final-secret-closure.js",
      "originSHA256": "ec18f6fc1ef9db40b9ac6e921362b39ed257143ffe41cecc639ffab3ff7e2fb7",
      "sha256": "ec18f6fc1ef9db40b9ac6e921362b39ed257143ffe41cecc639ffab3ff7e2fb7",
      "replacements": []
    },
    "c12-secret-pre-key-retirement.js": {
      "origin": "<report-root>/completion-c11/c11-secret-pre-key-retirement.js",
      "originSHA256": "ec18f6fc1ef9db40b9ac6e921362b39ed257143ffe41cecc639ffab3ff7e2fb7",
      "sha256": "ec18f6fc1ef9db40b9ac6e921362b39ed257143ffe41cecc639ffab3ff7e2fb7",
      "replacements": []
    },
    "c12-cleanup-deleteunusedboundary-draft.js": {
      "origin": "<report-root>/completion-c11/c11-cleanup-deleteunusedboundary-draft.js",
      "originSHA256": "f29964da36fa6b33882e187719151a3b5b80cc15878ef1735a8fc67d8629e7b6",
      "sha256": "f29964da36fa6b33882e187719151a3b5b80cc15878ef1735a8fc67d8629e7b6",
      "replacements": []
    },
    "verify_cleanup_native-DRAFT.py": {
      "origin": "<report-root>/completion-c11/verify_cleanup_native-DRAFT.py",
      "originSHA256": "a88521592e280e13846db39a829af29d7abb49d2a087bf1e728d0a6a747cc94a",
      "sha256": "4d695ad3eb9b07ee51166317e633e63befec6624fa1fc1f659f966e3ce636da3",
      "replacements": [
        {
          "from": "20261027c11",
          "to": "20261028c12",
          "occurrences": 1
        },
        {
          "from": "c11",
          "to": "c12",
          "occurrences": 9
        },
        {
          "from": "C11",
          "to": "C12",
          "occurrences": 1
        }
      ]
    },
    "c12-cleanup-restoresetupv5-DRAFT.js": {
      "origin": "<report-root>/completion-c11/c11-cleanup-restoresetupv5-DRAFT.js",
      "originSHA256": "e6af83600f9b4d618ea97aab0d4de524c51b20c0e7dbc1f8ecd9f7145ff14306",
      "sha256": "e6af83600f9b4d618ea97aab0d4de524c51b20c0e7dbc1f8ecd9f7145ff14306",
      "replacements": []
    },
    "c12-owner-access.js": {
      "origin": "<report-root>/private-operator/c10-owner-access.js",
      "originSHA256": "5d2a2f2667f149666449b20de6d432b52954d4752bf21bfd69deaec12781de7c",
      "sha256": "5d2a2f2667f149666449b20de6d432b52954d4752bf21bfd69deaec12781de7c",
      "replacements": []
    }
  },
  "newReservationsCharged": 32,
  "lifetimeReservations": 336,
  "AWSCalls": 0,
  "window": null,
  "reviewRounds": "5/5 consumed; owner disposition; no independent CLEAN or review6"
}
```

## PREPARATION-PINS.console-qualified.json

Original raw SHA256 `4e912899bf22b6310f720250d4e45abe5f82314e9150f4c727b4142c47e4cb42`.

```json
{
  "at": "2026-10-28T01:06:27.107728+00:00",
  "status": "ACTUAL_CLAIM_AND_BOTH_PRIVATE_CONSOLES_QUALIFIED_WINDOW_UNBOUND",
  "files": {
    "admit_c12.py": "171e1aa7aaedb78788acadee1ec10103062ea3c6b99e668dc1b380a42ef2373b",
    "bind_console_data.py": "fb82cd063ea40d0d99b877237490fc9db0889719265b5a251b207938b525c91e",
    "bind_final_iam_cleanup.py": "b02911517f3784935f973c7aa03272487bdaf78025cb89862c7100ce56661a06",
    "bind_policies.py": "fc2b8beef44c6a017451f68f3e4940d5f193ca2e271b121b47fdbc1676d5e5a0",
    "bind_protected_vars.py": "1ba327f088da6abf253e8f18cd06e91a5b058f3714e7fa9576a81d390b353382",
    "bind_window_once.py": "02df54767610ea66c371f4e6be00f14e0f96b3956fdc2c10db0b1a4a1cedaed9",
    "dispatch_workflow_once.py": "620002d755872481454080ced261facdf64c220b50dc3fb8a48a9022e41cd3e7",
    "fetch_hosted_terminal_evidence.py": "ab29d34a76c74c06454322065c19dfabce26ce4fd3043d0925b144a07aa82cdb",
    "key_once.py": "5190a661bcc53ff625dbaa9837b66020fcac6fe7e71571abc857c6f6d21261a0",
    "official_setup_login_once.py": "10de1ea8befcbac5a21ea2d00b386b1a478871d3ea4328867fef4921e17b3fdb",
    "prepare_iam_once.py": "f8aa31e47129e50849a462bc3b6d3ff7d65fa6369fbe01131dcfba1d235fcd62",
    "provision_fixture_once.py": "60e86d575d28802034a7c12d62607c88a32c550d354ab2d7c0385f146bc2d5d5",
    "resolve_and_report.py": "2606151802ae653d17fe9b5864901cf0b00c2c90bc4acbd0c5ef58aa34399281",
    "retire_fixture_once.py": "645e668b35c75e12d8e5d060aa8f0404abdc8ccc3c5cc15d829507adc5e134f6",
    "retire_run_variables_once.py": "9f80d47d04e8bce8b49828fbea781da72f239178c439ee419d8894625020c5ce",
    "setup_probes_after_login_once.py": "d909b2ea986afeb669dc9800b7ce592b3b579aa1930b159d71ee33da61224b1a",
    "verify_github_preparation.py": "dc21e05b2dbcee66f27794590fc30354874f270bf63b5f6d57d595e9244b104a",
    "verify_iam_step.py": "b1a2f161402c55c7c85a59b0f6561b6acd848ac1408f4e92492aa1a16970e59c",
    "verify_secret_absence_once.py": "e21ed9b81e7de78e25152a0535c41b1571abee0837cc0297a07a32e612178a53",
    "test_bind_policies.py": "88c6cd900c597d2706e809787e923b885f9a41713734984d3c3995cf64af184d",
    "test_kms_inventory.py": "5ee98dc3a0f7201387add553502fbd2ed24ca3b556c9463d1c676d86a0a0d8eb",
    "test_operator_bindings.py": "962a7d74f75b883df73400855c55b4645fbd5a99e68279b6c1328d37c718e85d",
    "test_retire_fixture.py": "89e2ae0b953602a8eb63c6939f7a7e6e3620b5a1cb779e6bf454b8d08185cb49",
    "test_variable_retirement.py": "1ba699ed2758f1b2e9830a70a8ae6588dfec67fba3a658aa4183d73e2642ebd9",
    "test_window_budget.py": "e66afcbb202268fe007c826aece8afb77ef0b00f99178ae90b94f7a57006693b",
    "test-iam-final-action.mjs": "4c3c9263da58d1a3b7345792f5863fa9f8cbffb28bc203cda45ec5a6141c42f5",
    "iam-cleanup-DRAFT.js": "9821a4d641b9147a4e5ded29f2314141eed95546c9aa80b671685a51c9ddecf0",
    "iam-final-action-DRAFT.js": "da3406c6410c172550403c8d3c90675942a29ace27ca40957e7386624a3cc4c7",
    "key-finish-DRAFT.js": "79b75b4e03d44b8fe300a067379b2e284c82a8b423f111d2bb5dd325d00cada8",
    "retire-alias-once-DRAFT.js": "9888a1ea42df7fc8897bce3107d3c693ef2fe5ab7775d1da7aafe7a2e57068d7",
    "schedule-key-once-DRAFT.js": "4604820e3ca47c20b3edf880acd497e6fd81d983e730ad6c0ca8acec33d55c6c",
    "c12-billing-admission.js": "4604f4fbb32c99c1d850bb05345e66f75aedd22ccdecc7cba6c70a8c1da2c3a0",
    "c12-boundary-draft-DRAFT.js": "ade2cab2d764315b7d3d222fa63259314c766245b10a2635a61c9770f3be370b",
    "c12-historical-secret-admission.js": "6c51689b6d490efdeec4b0d6261a1aabe087c41450c7d9780580dc7ce5f0fe78",
    "c12-iam-admission.js": "646b47d171a91b379c8cfd8a995953941cc08b031249e3a7e86bae01c63ac93a",
    "c12-key-draft.js": "23f8306192578a410e35c7d0c6681c19a5fe4a0d391ffe5400f30e46f9b10d2d",
    "c12-key-readback-DRAFT.js": "3e267852cf18aed175eddf1664866b61c8fdfed60f975093c085fd43389580f0",
    "c12-kms-admission.js": "f4bb34b353bd9c42b03dc4af4a319267261e8cd011f502eda21a0bf382c00363",
    "c12-kms-list-complete.js": "de95be93d13418d2582b4c41019859d052d4ff2bb809ea1976db24d559921cc9",
    "c12-secret-admission.js": "ec18f6fc1ef9db40b9ac6e921362b39ed257143ffe41cecc639ffab3ff7e2fb7",
    "c12-setup-draft-DRAFT.js": "8399d2342f798821f5ca244393415622a902df0b8810a6c263c83cd2fe6dd679",
    "c12-setup-v11-archive.js": "0d9fe1af299ddc7b56bd77036277896c4ac28897eb7aa2828ba24b0748d475d7",
    "c12-v11-delete-draft.js": "4462e3ab41eb832028a3fd85c9ae769ca33f347f9214ed857742263303fe4d10",
    "run_console_preflight.py": "7b1774753a6030c63fb3f48eb6eff91742891016cec55eeff462a5399a644d2f",
    "c12-owner-principal.js": "ab3c85482cb3faed9231554ac4325dff75d930fe02ae96e7ff20767aa3ecbc96",
    "validate_operator.py": "697df2c850591d4227383948bfe9b1397e7f3a5f868cdd9314f8e9fe0f9b6d98",
    "aws-acceptance.yml": "0bb9e4fe28055d03ba7a055c3e7595aa316b3869d22eddd59bfafe2d54314b6a",
    "SOURCE-PUBLICATION-CANDIDATE.json": "74537a1c447f9a453d947a56aebd36c2e8ea8d0b4039c741c2cc6b029f6f318a",
    "SOURCE-PUBLICATION-RECEIPT.json": "c443d952439bd50388cc83b69f871b651c81e46cad3edabbdbfd95ab838821bf",
    "EXPECTED-HOSTED-TESTS.json": "77073c98fcae9511190378e8f66c02f8f7c19f8781fe979ed52fa40934644cbf",
    "pinned-runner.py": "00f8117346bd4d50aa5defe4968333822b388a727966141b6bcd9236f28ff3d0",
    "BILLING-NATIVE-TABLE-PROFILE.json": "9d6a3331e89f242d71d756af8713f86e75552346efb720ae1514d64d7f1ca02a",
    "prior_key_schedules.py": "5fc472cb06adb4afa74fc88f5993ac668479da8f4b88f98ce41aebb920f947fc",
    "test_prior_key_schedules.py": "82551cc18fa52150cebf5e050cbea6aa6f00cb60e04be87690390c58ba0d0618",
    "normalize_native_billing.py": "880bc918d94508804eb40d835c5e206cbf6575c193cc17e61e4462d8c34374ed",
    "refresh_prices.py": "78da4f860eb413245fbf50f09421ce7f0e47d70c285a59cbc79340cb0f24830d",
    "test_billing_proof.py": "1c3108dae5665a65914aa3b42b9c33c3d0193ccdcd5cf3978e366afa136e06de",
    "BILLING-NATIVE-RED-FIXTURE.json": "f659c0a577cbd851a043ce2b5707f14ebbf538198413908755a661e9cd6ceea0",
    "BILLING-NORMALIZED-FIXTURE.json": "31c672d510193911315547a73abad83d102e3af6aa26f842dbf26f49497d6a6c",
    "c12-boundary-readback.js": "ed7796ca6a53162c2a749a26aab9b64cede8a7fe3b08a3ae473889dcd26fd197",
    "c12-v11-deletion-readback.js": "646b47d171a91b379c8cfd8a995953941cc08b031249e3a7e86bae01c63ac93a",
    "c12-setup-readback.js": "646b47d171a91b379c8cfd8a995953941cc08b031249e3a7e86bae01c63ac93a",
    "c12-post-iam-retirement-readback.js": "646b47d171a91b379c8cfd8a995953941cc08b031249e3a7e86bae01c63ac93a",
    "c12-final-iam-reconciliation.js": "646b47d171a91b379c8cfd8a995953941cc08b031249e3a7e86bae01c63ac93a",
    "c12-final-iam-closure.js": "646b47d171a91b379c8cfd8a995953941cc08b031249e3a7e86bae01c63ac93a",
    "c12-unused-boundary-readback.js": "ed7796ca6a53162c2a749a26aab9b64cede8a7fe3b08a3ae473889dcd26fd197",
    "c12-final-secret-closure.js": "ec18f6fc1ef9db40b9ac6e921362b39ed257143ffe41cecc639ffab3ff7e2fb7",
    "c12-secret-pre-key-retirement.js": "ec18f6fc1ef9db40b9ac6e921362b39ed257143ffe41cecc639ffab3ff7e2fb7",
    "c12-cleanup-deleteunusedboundary-draft.js": "f29964da36fa6b33882e187719151a3b5b80cc15878ef1735a8fc67d8629e7b6",
    "verify_cleanup_native-DRAFT.py": "4d695ad3eb9b07ee51166317e633e63befec6624fa1fc1f659f966e3ce636da3",
    "c12-cleanup-restoresetupv5-DRAFT.js": "e6af83600f9b4d618ea97aab0d4de524c51b20c0e7dbc1f8ecd9f7145ff14306",
    "c12-owner-access.js": "5d2a2f2667f149666449b20de6d432b52954d4752bf21bfd69deaec12781de7c",
    "ACTIVE-CLAIM.json": "fea113e1367f578b3a0626068532d242826c597edf6f995647f3bcf9e9725e20",
    "OWNER-APPROVAL.json": "845b46c2c166cf66ea9d5bdd61cfdc8278af26c2656c54f5054a28f740e02e6b",
    "HASH-REPRESENTATION-NOTE.json": "0bfaea493deeb9661b893c46f749ac650ef5fcf5d3db67b4c5c8a6acb864f109",
    "OPERATOR-PREPARATION-MANIFEST.json": "f9b09c093f9943be8fd209bc428bab758eef7305c018491b30748dafbbfc200f",
    "OPERATOR-VALIDATION-01.json": "f7b2fc9ec6c0ef10304d6cddc41b1184394f9158f00b366e05b26e24acefa08a",
    "OPERATOR-VALIDATION-01.log": "10f720142dd868973b29b6ed3e43baeb6d77db198c053dcc3f1b653812b74931",
    "OPERATOR-VALIDATION-02.json": "ae0c7a3f0d11a1549391937da2602dc5de99173927f37fea4652f8c87e885ef5",
    "OPERATOR-VALIDATION-02.log": "26dc683a719406c28366c9c8207167bcdac96d53d5ff8536c68fa3bca2269a70",
    "POLICY-BINDING-EXAMPLE-QUALIFICATION.json": "ae02eb0fbe33d85ca3e0fb018da12f00ea40a554802923431570d613821c7c9e",
    "PRIVATE-SETUP-CONSOLE-PREFLIGHT-INTENT.once.json": "8b99f55a20dfee20711c8275e8b2a94bd957d171ec2c83b55aefdbfb4c5811e5",
    "PRIVATE-SETUP-CONSOLE-PREFLIGHT-LAUNCH.json": "f8d6ae7d0d180c4f58bf242d5382947b68ef0595c097beecfd4422419738d34e",
    "PRIVATE-SETUP-CONSOLE-PREFLIGHT-ATTESTATION.json": "712b56fd97ca3a1dfed9b04046e91a110a252a5a31c93860834b033ea331748c",
    "PREREQUISITE-INTERFACE-READBACK.json": "2663e9b909aa84b0aa3abe253b08596ca60f42f1bc3426a2509254a07b774050",
    "awskms-eu-central-1-prices.json": "0aa773d6fa48be9cdf1a79ff46beb9c8135f36cfe4245c541ef34c575c178cbe",
    "AWSSecretsManager-eu-central-1-prices.json": "675e1539f309b43af9c1a265d310110299ff889983ed27089a8dc35675937c1d",
    "ready-native-preflight-snapshot/OWNER-CONSOLE-PREFLIGHT.json": "0f08befd7d0e2a9084920e73b84b54e8bf6947b829839b118c7182f22a7258c3",
    "ready-native-preflight-snapshot/c12-key-draft.json": "6fb7b3fcb0ff1ef58aa008c256b2121cb47abb46dd48a61c12c730a57d05ccde",
    "ready-native-preflight-snapshot/PRICE-RECONCILIATION.json": "8e214fa34f27be806a1b85791f94ea07190f1a2b55bc008e0095f7897301ec0f",
    "ready-native-preflight-snapshot/GITHUB-FRESH-ADMISSION.json": "f5b3ea53c39dfd0319e3d586019a0d49be367777c88b994dc9c35ce0a2f19fba",
    "ready-native-preflight-snapshot/c12-owner-principal.json": "65a553ecede7e76c0670c488e9cb6531e4d77385e7b5f5665ff898b9560520dc",
    "ready-native-preflight-snapshot/c12-historical-secret-admission.json": "8213b6f868df59d70aacca7cb7ede917832951e5bc00cc5c6bd8439f5f2b5ed2",
    "ready-native-preflight-snapshot/c12-iam-admission.json": "b25dbe84b2dce8d13c3be32e92d2ebce4ee97c838252fdde0e29305cf432db3f",
    "ready-native-preflight-snapshot/BILLING-NORMALIZED.json": "f35d6f3c8086a6ed73b5c26582b0cd3e7f31203a8ff57e878a0d5c66b70b39e8",
    "ready-native-preflight-snapshot/c12-billing-admission.json": "235c731728ca2f22ec6d11b81983c24b54cf423ba3450ccae59ac192870fa030",
    "ready-native-preflight-snapshot/c12-setup-v11-archive.json": "4ef1ef2ee5f383455340769f4c3592947f9697cc7eec9e179589d8415df4957c",
    "ready-native-preflight-snapshot/CURRENT-HEAD-CI-PRE-READY.json": "51e716f8426a775b16c0da4dfc300e4e4c28f072feba730ed5e71100e1f11d5c",
    "ready-native-preflight-snapshot/c12-kms-admission.json": "7b28f56e8f084796ef3e2925d073f16936d7e83ea00e64cae77f5575c20aefed",
    "ready-native-preflight-snapshot/c12-secret-admission.json": "a87a78677f0459a2b95bb439e73c678a5a93fe12aef2733b89926ce0d80ed495",
    "ready-native-preflight-snapshot/c12-owner-access.json": "333fd5135479b46207a34cf4e5a6f077178d0fd6250442661497e7e35708ea2c",
    "ready-native-preflight-snapshot/ARCHIVED-SETUP-V11.json": "1811db44b591dcbb8dee4b96b59f20362becd88d1bf22331f3d5f97e057f6e07",
    "ready-native-preflight-snapshot/MAIN-CORRESPONDENCE.json": "57a93258f8cdb0c1990706d8459ddc0c8d9d73480d8e508c1626877bf0170857",
    "ready-native-preflight-snapshot/c12-kms-list-complete.json": "0214459f20fbf3174f7f9e28aa98288c2b8137e9e74d396d66569daae2814e9a"
  },
  "PythonTestsPassed": 46,
  "nativeIAMPositive": 4,
  "nativeIAMRefusal": 13,
  "additionalOwnerAccessSyntaxAndNativeProbePassed": true,
  "IAMConsoleOwnerAttestationPosition": 3875,
  "rootConsoleNativePrincipalPassed": true,
  "officialCLILoginStarted": false,
  "windowBound": false,
  "AWSWritesForwarded": 0,
  "reviewRounds": "5/5 consumed; owner disposition; no independent CLEAN or review6"
}
```

## UNATTENDED-SOURCE-DERIVATION.json

Original raw SHA256 `618e8cae4cd535176b69a632964302ed8dbfeb47c0818103ca2afba9ecbb2474`.

```json
{
  "at": "2026-10-28T05:40:36.239532+00:00",
  "ownerInstructionPosition": 3902,
  "originalProbeSourceSHA256": "d909b2ea986afeb669dc9800b7ce592b3b579aa1930b159d71ee33da61224b1a",
  "existingProviderProbeSHA256": "e1f336e92ec21dc40424f97d50d67127c21990223937506cebdc767a7e954d7a",
  "changedPrerequisite": "Use owner-delegated normal official credential provider after independently verified exact setup policy, instead of requiring a fresh interactive login command. Literal identity/ListRoles-MFA/sentinel calls, counts, refusals, cutoffs and once intent remain unchanged.",
  "sameC12Limits": true,
  "humanAvailabilityClaimed": false,
  "newReview": false,
  "AWSCalls": 0
}
```
