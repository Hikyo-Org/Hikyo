# #162 frozen review scope

Baseline commit: `d02242217502de6f23f528396be572072c34c085`.
Baseline tree: `13c4558401d826c74d6979916d2ac3d2c29c1fdf`.

The table binds the current review input by full Git blob IDs. The eventual
signed PR commit and its tree are recorded by the review receipt; it must
contain these exact blobs. Evidence-only handoff updates do not silently
extend review credit to changed source. The full PR diff is reviewed separately.

Covered regions: the complete Vault client/module/credential package and its
fixtures; Vault registration, addressing, import-loop, API/CLI/browser consumers;
and shared outbox/custody/adoption/health paths that govern Vault. Other provider
implementations and unrelated portions of the shared ADRs are context, not a new
release-membership decision. ADR review covers the Vault banner/row and linked
KV v2 contract. API review covers Vault provider and adapter lifecycle shapes.

## Authored portions

Line provenance is navigation for review, not a claim of exclusive authorship.
Current edits are OpenAI. Original #823 PR-head blame is used only where its
path blob equals the merge blob; otherwise squash-origin lines stay mixed or
unknown. Claude coauthor trailers identify Anthropic portions. Native OpenAI
edit/commit records identify later #823 repairs, #844 and #1071; a human Git
author alone never identifies the model. Moves, shared historical material and
unbound commits remain conservative. Counts include comments and blank lines.

OpenAI review of Anthropic portions is distinct from same-provider review of
OpenAI portions. Unknown portions retain their label. The owner removed the
mandatory cross-model gate on October 29; independent final-byte review covers
the entire named scope without inventing cross-provider execution. Current
policy, findings and dispositions are in the [closeout packet](./162-vault-1.0-closeout.md).

| Covered path | Git blob | Line origins (Anthropic / OpenAI / mixed or unknown) |
| --- | --- | --- |
| `api/openapi.yaml` | `9886c995f8689243121d6fc34f6f6169c36d2ffc` | 17983 / 88 / 7006 |
| `docs/adr/deployment-adapter.md` | `bdaa2e3ce60353b1ffe9b654b256b079bce6d9ad` | 7 / 26 / 273 |
| `docs/adr/mvp-boundary.md` | `686bdffe8d359197c3ba2b3c44a2d91084824935` | 24 / 1 / 256 |
| `docs/adr/vault-kv-adapter.md` | `450fc2f9c2e6e617126dfc29d6f97baec5790b5b` | 71 / 92 / 0 |
| `docs/site/src/content/docs/docs/deployment-adapters.mdx` | `bcb11cd555d854d5caa47305430d8f508e842199` | 373 / 36 / 264 |
| `internal/adapter/adapter.go` | `07f144836ee0dc01cc177edc3cc61295f52d6958` | 347 / 42 / 381 |
| `internal/adapter/health.go` | `95ebc1567f5de2f1688d3d0d917e887c85b8420e` | 0 / 1 / 91 |
| `internal/adapter/health_test.go` | `903d0fc2edd213e4d6b80230520b4e320aafc95f` | 0 / 1 / 73 |
| `internal/adapter/ledger.go` | `f328aa495bc27414da5de26fe7935ac9ee1a09a2` | 0 / 0 / 175 |
| `internal/adapter/origin.go` | `0a7576e626c3bdc091f8644a81b71608b793c0ac` | 0 / 144 / 0 |
| `internal/adapter/outbox.go` | `24c0ae1e6a290607427c50cab51f3f3ef315f36b` | 6 / 66 / 266 |
| `internal/adapter/provider.go` | `2d51fd2b68ee67c45efe55014f732220eab6edb5` | 8 / 24 / 53 |
| `internal/adapter/vaultkv/ambiguous_review_test.go` | `a8fdd35359a404e47a2f7ec21b77d2090244c741` | 0 / 79 / 0 |
| `internal/adapter/vaultkv/cleanup_context_test.go` | `b02298cde1ea882412c14e984ee40825bb41102a` | 0 / 101 / 0 |
| `internal/adapter/vaultkv/client.go` | `e7d0f88e962ef71f364333202bf243c739b0dccb` | 524 / 205 / 0 |
| `internal/adapter/vaultkv/client_test.go` | `08ca377e09bc5f1bf1054ac7488b6b139ae33b93` | 446 / 194 / 0 |
| `internal/adapter/vaultkv/contract_external_test.go` | `bd0f03ea5145efeb65f7dd82e60dc1c06b922933` | 354 / 285 / 0 |
| `internal/adapter/vaultkv/credential.go` | `3d76fef4dbd060605df610d51b8f773da408c1c1` | 154 / 3 / 0 |
| `internal/adapter/vaultkv/marker_custody_test.go` | `8cfcc179c47b7eed9e197e0c3634e87033fbb304` | 0 / 65 / 0 |
| `internal/adapter/vaultkv/module.go` | `6ba0782b0ccb5a64bed5cd1631e313aaf0a3a231` | 619 / 208 / 2 |
| `internal/adapter/vaultkv/module_test.go` | `51c9d114845e406fe573d3411bee16ba54a59b6d` | 764 / 151 / 0 |
| `internal/adapter/vaultkv/namespace_identity_test.go` | `7904db035117de00143c8866fdbe457548dcbcbc` | 0 / 77 / 0 |
| `internal/adapter/vaultkv/request_gate_test.go` | `88ab6ea91cefc0fff64dfa8ca8553e424a0a640f` | 0 / 131 / 0 |
| `internal/adapter/vaultkv/review_hold_test.go` | `91a35240aa02df7478cf11f568a54a40c804f750` | 0 / 58 / 0 |
| `internal/adapter/vaultkv/transport_error_test.go` | `efaba3878726b4c142733f615f6b5b6792170d0d` | 0 / 82 / 0 |
| `internal/app/adapter_loader.go` | `2e09cd858186765c56e66a1dd5e66c01b67471ed` | 5 / 5 / 169 |
| `internal/app/adapter_provider.go` | `ccfddd02b59e2013582d42be9816574705b3e740` | 80 / 45 / 60 |
| `internal/cli/adapters.go` | `e8bdac60a05aa56615602d808007b31eabc561fd` | 334 / 0 / 932 |
| `internal/cli/adapters_vaultkv_test.go` | `3a729ea9d73e6241cb7b83a0a14aded1aaa3e58a` | 99 / 61 / 0 |
| `internal/cli/import_loop.go` | `6a827391b2ff01f4d5492359e8b0e33091a936b0` | 70 / 60 / 0 |
| `internal/cli/import_wizard.go` | `fafedf1642bcf976e5bdea202a0df6567c9bb94a` | 434 / 6 / 54 |
| `internal/isolation/adapter_vaultkv_e2e_test.go` | `a5eceb0c23e57f26fb7399b28ccdddd3dee9bf14` | 280 / 548 / 0 |
| `internal/service/adapters.go` | `635a49b86f5617cfe51aaa6edddac1138011996e` | 410 / 154 / 1568 |
| `internal/service/adapters_vaultkv_test.go` | `d36c4b253dc244bead00f9bde5381815ac903090` | 70 / 7 / 0 |
| `internal/store/adapter_lifecycle_queries.go` | `c4efb4c6fa20cc62bbacdf9f4e64f445d40bfc86` | 0 / 14 / 111 |
| `internal/store/adapter_runtime.go` | `d1492fc450e0be9d1aa6b61b1040e208300555f4` | 80 / 209 / 1171 |
| `internal/store/adapter_runtime_queries.go` | `6eded2d436974b380d9e33fdd764f3e90d6710b2` | 0 / 116 / 904 |
| `internal/store/adapter_store_queries.go` | `012a3858cc270860107fd7b872a86e233ef79a7a` | 0 / 15 / 580 |
| `internal/store/pggen/adapter_repository.sql.go` | `fa5283ff1290c443f03492cd61986c4b38ef2075` | 0 / 236 / 2300 |
| `internal/store/pggen/adapter_runtime.sql.go` | `46231e37c3e6dce1961e6729873d96bda428ffe1` | 0 / 347 / 2739 |
| `internal/store/queries/postgres/adapter_repository.sql` | `d9b7a828e8456a9197247cbd4b59918ec6173a70` | 0 / 30 / 192 |
| `internal/store/queries/postgres/adapter_runtime.sql` | `086174b54446b43f37fd1b093bc778da8448f424` | 0 / 66 / 267 |
| `internal/store/queries/sqlite/adapter_repository.sql` | `89687d278fd764e386a433bee17a057a5b36e12d` | 0 / 30 / 192 |
| `internal/store/queries/sqlite/adapter_runtime.sql` | `68184d46b4cf875a25a791ea23b5f3a029ab7009` | 0 / 66 / 267 |
| `internal/store/repos_adapters.go` | `0b1218df91b3fa0e510fc196402d8f458c5a52e5` | 50 / 70 / 606 |
| `internal/store/repos_adapters_config.go` | `f5bc70732f6b94c6d253c0b586a93bb8f91e8d97` | 129 / 19 / 386 |
| `internal/store/repos_adapters_move.go` | `feefd717d864b1bd7e1317b82fc7c5335257778f` | 107 / 43 / 670 |
| `internal/store/sqlitegen/adapter_repository.sql.go` | `aa7dd3d788c91fe87dc5aca3c3ec5643cac2d68d` | 0 / 236 / 2347 |
| `internal/store/sqlitegen/adapter_runtime.sql.go` | `5dd0903aecb3f12dd288fd71175705dd2a4caf9c` | 0 / 350 / 2753 |
| `internal/store/vault_recovery_test.go` | `dca6154d8cc93444f46c8118c8630b69e4709d80` | 0 / 284 / 0 |
| `web/e2e/flows/machine-access.spec.ts` | `3056d72f4b2bc5d8732c6716b5367732be8d8dee` | 729 / 45 / 1435 |
| `web/src/api/adapters.ts` | `6461cfa75944aab857020500221cb247b1291d2e` | 43 / 2 / 509 |
| `web/src/routes/Adapters.test.tsx` | `86c7e5552e83a33a5a6c1328668c88e93a166598` | 328 / 20 / 74 |
| `web/src/routes/Adapters.tsx` | `37f908ac4d532a518ecf884ecbf7b781a4136334` | 823 / 46 / 1263 |
