# Contributing to Hikyo

Open an issue and get maintainer agreement before starting a large change.
Security vulnerabilities are the exception: never open them as public issues.
Report them privately through the [security policy](./SECURITY.md).

## Developer Certificate of Origin

Every commit in a pull request must carry a Developer Certificate of Origin
(DCO) sign-off. Add it with:

```sh
git commit -s
```

The sign-off certifies the [Developer Certificate of Origin 1.1](https://developercertificate.org/).
CI checks the pull request's commit history; a sign-off added only to a squash
message does not satisfy the gate. Hikyo uses the DCO, never a Contributor
License Agreement (CLA), so contributors retain their copyright.

## Pull requests from forks

Hikyo uses [vouch](https://github.com/mitchellh/vouch). CI validates a fork's
pull request only after its author is listed in
[`.github/VOUCHED.td`](https://github.com/Hikyo-Org/Hikyo/blob/main/.github/VOUCHED.td);
until then, `ci-required` reports the author as not vouched and the pull
request stays open. A maintainer vouches for you, usually after you have opened
an issue about the change, and a maintainer approves each workflow run from a
fork. CI runs fork code without secrets. A fork pull
request that changes anything under `.github/` cannot pass CI; a maintainer
lands such changes from a branch in this repository.

## Security-sensitive contributions

Contributions touching cryptography, authentication, deployment adapters, or
delivery paths require maintainer security review. Maintainer-authored changes
use adversarial cross-model review until a second maintainer exists; this is a
compensating check, not independent human review.

Do not report vulnerabilities in public issues. Use the private channels in the
[security policy](./SECURITY.md).

## Design decisions

The locked architecture decision records live in [`docs/adr/`](./docs/adr/README.md)
and the build-ready specification set in [`docs/spec/`](./docs/spec/README.md).
Code comments cite them by file stem ("the encryption-model ADR" is
`docs/adr/encryption-model.md`); `docs/adr/README.md` maps every short name to
its file. A change that contradicts a locked ADR reopens it under the
[amendment procedure](./GOVERNANCE.md#amendment-procedure) rather than silently
diverging. See the [OSS mechanics ADR](./docs/adr/oss-mechanics.md), section
Governance, for the full mechanism.

## Local verification

Before you request review, run the checks for what you touched:

- Go changes: `go test ./<changed-package>/...`, plus `go test ./...` for
  anything cross-cutting. Add or update tests for the behaviour you change.
  There is no numeric coverage gate, so reviewers, not an automated threshold,
  judge whether the tests cover the change.
- `web/` changes: `node --run typecheck` and `node --run test` in `web/`.
- Run the formatter and linters so the `lint` gate does not bounce the pull
  request.

You do not need to reproduce the full release gate locally; CI is the source of
truth for that.

## Continuous integration

Every pull request runs the complete gate. The aggregate `ci-required` check
stays red, and blocks merge, until all of these pass:

- `lint`, `test`, and the sharded isolation suites;
- `race`, the race detector over every package except `./internal/isolation/`
  (that suite runs race-instrumented on the weekly `race-isolation` workflow);
- `fuzz` (see below);
- `govulncheck` for known vulnerabilities, plus the supply-chain, web, compose,
  and Kubernetes end-to-end checks.

You do not run these by hand as a rule; open the pull request and let CI report.

### Fuzzing

Fuzzing feeds a function randomised inputs to surface crashes and panics that
example-based tests miss. CI runs a bounded fuzz pass over every `Fuzz*` target
on every pull request, so you never have to fuzz manually. To reproduce or
extend one target locally:

```sh
go test -run='^$' -fuzz='^FuzzParseHeader$' -fuzztime=30s ./internal/crypto/
```

When fuzzing finds a failure, CI keeps the minimised input for 30 days and
replays it against the pull request's trusted base. A finding that does not
reproduce on the base is added to the pull request with its replay command; one
that also fails on the base opens or updates a standalone bug issue. Either way,
`fuzz` and `ci-required` stay red until it is fixed. Commit the minimised input
with the fix so `go test ./...` keeps it as a regression case.
