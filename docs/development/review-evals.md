# Review eval corpus

Regressions that reached `dev` and turned integration red. Each entry pairs the
change that introduced the failure with the change that fixed it, so a reviewer
(human or agent) can be replayed against the introducing diff to check whether
it would have caught the problem. Rerun the corpus when a review prompt, skill
or model changes, and keep the change only if it catches more without adding
noise on clean PRs.

Add an entry when a `Dev integration is red` issue closes. Record only facts
you verified from the run, the failing check and the fix; write `not traced`
for anything you did not establish.

| Red dev run | Failing check | Introduced by | Fixed by | Class |
| --- | --- | --- | --- | --- |
| [42310710033](https://github.com/Hikyo-Org/Hikyo/actions/runs/42310710033) at `526156110` | `e2e/demo/demo.spec.ts:351` "read-only details and filters work while management actions stay disabled" (desktop, mobile) | #1222 (`6ea1258bb`, renamed the credential hint "Developer principal:" to "Developer:") | #1249 (`0bdc360c8`) | Copy change without updating the e2e expectation that reads it |
| [43180572146](https://github.com/Hikyo-Org/Hikyo/actions/runs/43180572146) at `426bf59ce` | `validation / race shard (6)`: `api` `TestFreezeContractRefusesDirectionlessAndMisreadChanges`, the three `request_encoding_header_gains_*` subtests ("gate errored: … invalid character '}' after top-level value") | #1232 (`1d736c125`, `encoded` fixture helper has one `}` too many; the kin-openapi v0.152.0 loader ignored the trailing data), exposed by #1290 (`881efef4f`, kin-openapi v0.152.0 to v0.153.0, which rejects it; the only bump of the loader modules after the last green commit). Not traced: which change inside kin-openapi | PR for #1292 (#TBD) | Test fixture that is invalid input but passed only because a dependency was lenient; `-race` was not involved, and `test_core` never reached its test step |
| [43180572146](https://github.com/Hikyo-Org/Hikyo/actions/runs/43180572146) at `426bf59ce` | `validation / test_core` and `validation / web-go`: govulncheck binary mode, GO-2026-6859 (`net/http@go1.27.3`, fixed in go1.27.4) | Not a repository change: the advisory names `net/http@go1.27.3`, the toolchain `go.mod` pinned. Not traced: when the advisory was published | PR for #1292 (#TBD) | Toolchain pin (`go.mod`) behind a published fix; the failure arrives with no diff to review |
