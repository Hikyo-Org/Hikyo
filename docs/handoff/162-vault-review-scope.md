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
| `docs/adr/vault-kv-adapter.md` | `aa52918329d2a32ea2ab5102ada0d1753209a0b2` | 72 / 80 / 0 |
| `internal/adapter/adapter.go` | `07f144836ee0dc01cc177edc3cc61295f52d6958` | 347 / 42 / 381 |
| `internal/adapter/health.go` | `95ebc1567f5de2f1688d3d0d917e887c85b8420e` | 0 / 1 / 91 |
| `internal/adapter/health_test.go` | `903d0fc2edd213e4d6b80230520b4e320aafc95f` | 0 / 1 / 73 |
| `internal/adapter/ledger.go` | `f328aa495bc27414da5de26fe7935ac9ee1a09a2` | 0 / 0 / 175 |
| `internal/adapter/origin.go` | `0a7576e626c3bdc091f8644a81b71608b793c0ac` | 0 / 144 / 0 |
| `internal/adapter/outbox.go` | `24c0ae1e6a290607427c50cab51f3f3ef315f36b` | 6 / 66 / 266 |
| `internal/adapter/provider.go` | `2d51fd2b68ee67c45efe55014f732220eab6edb5` | 8 / 24 / 53 |
| `internal/adapter/vaultkv/ambiguous_review_test.go` | `a8fdd35359a404e47a2f7ec21b77d2090244c741` | 0 / 79 / 0 |
| `internal/adapter/vaultkv/cleanup_context_test.go` | `b02298cde1ea882412c14e984ee40825bb41102a` | 0 / 101 / 0 |
| `internal/adapter/vaultkv/client.go` | `6c2d9cff816b2c6182ef9574e1b1267b41c04770` | 533 / 141 / 0 |
| `internal/adapter/vaultkv/client_test.go` | `b928a822ff3382dfc16135f2c2b31c93e001cee9` | 457 / 167 / 0 |
| `internal/adapter/vaultkv/contract_external_test.go` | `9fa5c42cf07ee6427a8b107a8ceee0b65120be5d` | 354 / 267 / 0 |
| `internal/adapter/vaultkv/credential.go` | `3d76fef4dbd060605df610d51b8f773da408c1c1` | 154 / 3 / 0 |
| `internal/adapter/vaultkv/marker_custody_test.go` | `8cfcc179c47b7eed9e197e0c3634e87033fbb304` | 0 / 65 / 0 |
| `internal/adapter/vaultkv/module.go` | `b6741d26bafe426284689d809b2fcfb8bbb2a56f` | 620 / 204 / 2 |
| `internal/adapter/vaultkv/module_test.go` | `51c9d114845e406fe573d3411bee16ba54a59b6d` | 764 / 151 / 0 |
| `internal/adapter/vaultkv/request_gate_test.go` | `88ab6ea91cefc0fff64dfa8ca8553e424a0a640f` | 0 / 131 / 0 |
| `internal/adapter/vaultkv/review_hold_test.go` | `91a35240aa02df7478cf11f568a54a40c804f750` | 0 / 58 / 0 |
| `internal/adapter/vaultkv/transport_error_test.go` | `efaba3878726b4c142733f615f6b5b6792170d0d` | 0 / 82 / 0 |
| `internal/app/adapter_loader.go` | `2e09cd858186765c56e66a1dd5e66c01b67471ed` | 5 / 5 / 169 |
| `internal/app/adapter_provider.go` | `ccfddd02b59e2013582d42be9816574705b3e740` | 80 / 45 / 60 |
| `internal/cli/adapters.go` | `e8bdac60a05aa56615602d808007b31eabc561fd` | 334 / 0 / 932 |
| `internal/cli/adapters_vaultkv_test.go` | `3a729ea9d73e6241cb7b83a0a14aded1aaa3e58a` | 99 / 61 / 0 |
| `internal/cli/import_loop.go` | `6a827391b2ff01f4d5492359e8b0e33091a936b0` | 70 / 60 / 0 |
| `internal/cli/import_wizard.go` | `fafedf1642bcf976e5bdea202a0df6567c9bb94a` | 434 / 6 / 54 |
| `internal/isolation/adapter_vaultkv_e2e_test.go` | `d239883f881699b4f586e7a55ebc634ea55d85fe` | 280 / 512 / 0 |
| `internal/service/adapters.go` | `635a49b86f5617cfe51aaa6edddac1138011996e` | 410 / 154 / 1568 |
| `internal/service/adapters_vaultkv_test.go` | `d36c4b253dc244bead00f9bde5381815ac903090` | 70 / 7 / 0 |
| `internal/store/adapter_lifecycle_queries.go` | `c4efb4c6fa20cc62bbacdf9f4e64f445d40bfc86` | 0 / 14 / 111 |
| `internal/store/adapter_runtime.go` | `0f6f0d2b3ee6f4fa0b244d3d44603d9e40327424` | 80 / 203 / 1171 |
| `internal/store/adapter_runtime_queries.go` | `9a45fedd3d307027de03a32b51e5c7a9e6148326` | 0 / 106 / 906 |
| `internal/store/adapter_store_queries.go` | `012a3858cc270860107fd7b872a86e233ef79a7a` | 0 / 15 / 580 |
| `internal/store/pggen/adapter_repository.sql.go` | `fa5283ff1290c443f03492cd61986c4b38ef2075` | 0 / 236 / 2300 |
| `internal/store/pggen/adapter_runtime.sql.go` | `8553966155f90f390f7100b33d04de9b0e4dec93` | 0 / 338 / 2739 |
| `internal/store/queries/postgres/adapter_repository.sql` | `d9b7a828e8456a9197247cbd4b59918ec6173a70` | 0 / 30 / 192 |
| `internal/store/queries/postgres/adapter_runtime.sql` | `4ecbc100953429127d351b365c18568222755eff` | 0 / 63 / 267 |
| `internal/store/queries/sqlite/adapter_repository.sql` | `89687d278fd764e386a433bee17a057a5b36e12d` | 0 / 30 / 192 |
| `internal/store/queries/sqlite/adapter_runtime.sql` | `1c7efbefd98e3aa7c777851773964b7be4a90ad6` | 0 / 63 / 267 |
| `internal/store/repos_adapters.go` | `0b1218df91b3fa0e510fc196402d8f458c5a52e5` | 50 / 70 / 606 |
| `internal/store/repos_adapters_config.go` | `f5bc70732f6b94c6d253c0b586a93bb8f91e8d97` | 129 / 19 / 386 |
| `internal/store/repos_adapters_move.go` | `feefd717d864b1bd7e1317b82fc7c5335257778f` | 107 / 43 / 670 |
| `internal/store/sqlitegen/adapter_repository.sql.go` | `aa7dd3d788c91fe87dc5aca3c3ec5643cac2d68d` | 0 / 236 / 2347 |
| `internal/store/sqlitegen/adapter_runtime.sql.go` | `3171c06f5e9a1bb6f3973bf08356483a7b51ae14` | 0 / 341 / 2753 |
| `internal/store/vault_recovery_test.go` | `dca6154d8cc93444f46c8118c8630b69e4709d80` | 0 / 284 / 0 |
| `web/e2e/flows/machine-access.spec.ts` | `3056d72f4b2bc5d8732c6716b5367732be8d8dee` | 729 / 45 / 1435 |
| `web/src/api/adapters.ts` | `6461cfa75944aab857020500221cb247b1291d2e` | 43 / 2 / 509 |
| `web/src/routes/Adapters.test.tsx` | `86c7e5552e83a33a5a6c1328668c88e93a166598` | 328 / 20 / 74 |
| `web/src/routes/Adapters.tsx` | `37f908ac4d532a518ecf884ecbf7b781a4136334` | 823 / 46 / 1263 |
