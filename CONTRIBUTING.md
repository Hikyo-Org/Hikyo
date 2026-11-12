# Contributing to Hikyo

Open an issue and get maintainer agreement before starting a large change.
Security vulnerabilities are the exception: never open them as public issues.
Report them privately through the [security policy](./SECURITY.md).

## Setup with a coding agent

The repository includes a
[contributor bootstrap skill](./.agents/skills/hikyo-contributor-bootstrap/SKILL.md)
for signing, pinned dependencies, local builds/tests, and PR readiness. From a
checkout of this repository, invoke it with:

- Codex: `$hikyo-contributor-bootstrap`
- Claude Code: `/hikyo-contributor-bootstrap`

No separate skill installation is needed. The shared instructions live under
`.agents/skills/`; `.claude/skills/` links to the same copy. The skill asks
whether you also want a local Docker or source-built instance to explore, and
continues independent contributor setup while you decide. It preserves existing
signing configuration and does not push, merge, or deploy without authorization.

## Finding the right CI workflow

Start with the [CI guide](./docs/development/ci.md). It explains the required PR
checks, the maintenance and release-acceptance menus, and which workflows are
internal helpers. Feature PRs target `dev` and require automatic `ci-feedback`
plus trusted `ci-dev-required` admission. Merged dev commits request complete
background integration validation. Frozen release candidates target `main` and
use its mandatory queue; `ci-queue-required` requires full current-base proof
and, once `dev` exists, refuses feature PRs opened against main. GitHub and
`gh pr create` default to main, so pass `--base dev`.
See the [branching guide](docs/development/branching.md) for commands, protected
activation and the nightly promotion from dev to main.

## Developer Certificate of Origin

Every commit in a pull request must carry a Developer Certificate of Origin
(DCO) sign-off. Dependabot's commits are the only exemption, including when a
canonical `release/*` promotion or `main` backmerge carries them. It must also carry a cryptographic signature that GitHub reports
as **Verified**.
After completing the [signing setup](#commit-signing-setup), commit with:

```sh
git commit -S -s
```

The sign-off certifies the [Developer Certificate of Origin 1.1](https://developercertificate.org/).
CI checks the pull request's commit history; a sign-off added only to a squash
message does not satisfy the gate. Hikyo uses the DCO, never a Contributor
License Agreement (CLA), so contributors retain their copyright.

Lowercase `-s` (`--signoff`) adds the DCO trailer. Uppercase `-S` (`--gpg-sign`)
creates a cryptographic signature using your configured signing key. With
`commit.gpgsign=true`, `git commit -s` also signs cryptographically, but the
explicit `-S -s` example makes both requirements visible. Neither flag configures
a signing key for you.

## Commit signing setup

Run the commands below from the repository root in a POSIX-compatible shell
(including Bash or Zsh). Git configuration commands use `--local` so they apply
to this clone. If your signatures already pass local and GitHub verification,
keep your existing key and configuration and proceed to
[verification](#verify-before-pushing).

### Identity and signing key

Set your actual name and an email verified on your GitHub account, including a
GitHub-provided private noreply address if preferred:

```sh
git config --local user.name "Your Name"
git config --local user.email "your-verified-email@example.com"
```

For SSH signing, use Git 2.34 or newer and an existing SSH key, or follow
GitHub's [key generation and ssh-agent setup guide](https://docs.github.com/en/authentication/connecting-to-github-with-ssh/generating-a-new-ssh-key-and-adding-it-to-the-ssh-agent).
Adjust the key path below to your key. `ssh-add` needs a running SSH agent and
may prompt for your key's passphrase:

```sh
hikyo_signing_key="$HOME/.ssh/id_ed25519"
ssh-add "$hikyo_signing_key"
git config --local gpg.format ssh
git config --local user.signingkey "$hikyo_signing_key.pub"
git config --local commit.gpgsign true
```

Add the public key to GitHub as a **Signing Key**, following
[GitHub's key registration guide](https://docs.github.com/en/authentication/connecting-to-github-with-ssh/adding-a-new-ssh-key-to-your-github-account).
An existing Authentication Key must also be registered as a Signing Key.

### Local SSH verification

GitHub key registration does not configure local Git verification. The pre-push
checker requires a locally trusted signature (`G`), so configure an
[allowed-signers file](https://git-scm.com/docs/git-config#Documentation/git-config.txt-gpgsshallowedSignersFile).
Continuing in the same shell, add your public key to a file inside Git's metadata
directory, outside tracked source files:

```sh
hikyo_allowed_signers="$(git rev-parse --absolute-git-dir)/hikyo-allowed-signers"
hikyo_signer="$(git config --local user.email) $(cat "$hikyo_signing_key.pub")"
if ! grep -Fqx -- "$hikyo_signer" "$hikyo_allowed_signers" 2>/dev/null; then
  printf '%s\n' "$hikyo_signer" >> "$hikyo_allowed_signers"
fi
git config --local gpg.ssh.allowedSignersFile "$hikyo_allowed_signers"
```

If you already maintain an allowed-signers file, retain it and add your public
key there instead. For commits from other contributors in your branch, verify
their key identities before adding their public keys to your local trust file.

### Other signing formats

You can keep an existing GPG or S/MIME signing setup. GitHub documents
[generating a GPG key](https://docs.github.com/en/authentication/managing-commit-signature-verification/generating-a-new-gpg-key),
[adding a GPG key to GitHub](https://docs.github.com/en/authentication/managing-commit-signature-verification/adding-a-gpg-key-to-your-github-account),
and [configuring Git for GPG, SSH, or X.509 signing](https://docs.github.com/en/authentication/managing-commit-signature-verification/telling-git-about-your-signing-key).
Keep `commit.gpgsign=true`. The local checker requires status `G`; a valid GPG
signature with unknown local trust (`U`) does not pass. Establish local trust
only after verifying the signing key's identity.

### Verify before pushing

Install the hook once per clone and fetch the canonical repository's main branch
as the checker base. The dedicated `hikyo-upstream/main` reference keeps canonical
history separate from your fork's `origin/main`, so ordinary fork fetches cannot
replace the comparison base:

```sh
scripts/git/install-hooks.sh
git fetch https://github.com/Hikyo-Org/Hikyo.git refs/heads/main:refs/remotes/hikyo-upstream/main
```

Once `dev` exists, fetch it too with
`refs/heads/dev:refs/remotes/hikyo-upstream/dev`. On a branch cut from `dev`, the hook
then checks only your commits instead of everyone's since `main`. The manual checks
below use `hikyo-upstream/main`; on a branch cut from `dev`, pass `hikyo-upstream/dev`
to both instead.

After creating your contribution with `git commit -S -s`, run:

```sh
git log -1 --format='%h %G? %s'
scripts/ci/check-dco.sh hikyo-upstream/main HEAD
scripts/ci/check-commit-signatures.sh hikyo-upstream/main HEAD
```

Expect `G` in the log and both checks to report success for the complete PR
range. Re-run the checks after amending or rebasing. If signing or verification
fails, keep signing enabled and resolve the reported key, agent, or trust error;
do not bypass the hook. After pushing, confirm **Verified** for every PR commit
on GitHub. Local verification and
[GitHub verification](https://docs.github.com/en/authentication/managing-commit-signature-verification/about-commit-signature-verification)
are separate checks, and both must pass.

## Pull requests from forks

Hikyo uses [vouch](https://github.com/mitchellh/vouch). CI validates a fork's
pull request only after its author is listed in
[`.github/VOUCHED.td`](https://github.com/Hikyo-Org/Hikyo/blob/main/.github/VOUCHED.td);
until then, `ci-queue-required` reports the author as not vouched and the pull
request stays open. A maintainer vouches for you, usually after you have opened
an issue about the change, and a maintainer approves each workflow run from a
fork. CI runs fork code without secrets. A fork pull
request that changes anything under `.github/` cannot pass CI; a maintainer
lands such changes from a branch in this repository.

## Security-sensitive contributions

Contributions touching cryptography, authentication, deployment adapters, or
delivery paths require maintainer security review. Maintainer-authored changes
also require adversarial security review. Automated review is a compensating
check and does not constitute independent human review.

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
- `web/` changes: `node --run typecheck` and `node --run test` in `web/`. Also
  run the affected Playwright flow specs (`web/e2e/flows/*.spec.ts`, see the
  browser flow tests below) and list them in the PR Evidence. The pre-push hook
  runs the Storybook suite for you.
- Run the formatter and linters so the `lint` gate does not bounce the pull
  request.

User-visible changes (a CLI command or flag, an `/api/v1` operation or field, a
server setting) update the matching page under
`docs/site/src/content/docs/docs/` in the same pull request. The `docs-impact`
feedback check enforces this for `internal/cli/testdata/help.txt`,
`api/openapi.yaml` and `internal/config/variables.go`; if no docs change is
needed, add a `Docs-Impact: none - <reason>` trailer to a commit, in the final
trailer block next to `Signed-off-by`.

You do not need to reproduce the full release gate locally; CI is the source of
truth for that.

For browser flow tests, run `pnpm --dir web run e2e:install`, then
`pnpm --dir web run e2e` from the repository root. Local development can leave
`HIKYO_E2E_MAIL_BINARY` unset: the fixture builds its TLS SMTP helper from the
current checkout using Go. To use a prebuilt helper instead, set it to the
absolute path of a fresh binary built with
`go build -o ./mail-sink ./internal/mailtest/cmd`. When `CI` is present, the
fixture requires a non-empty `HIKYO_E2E_MAIL_BINARY` before creating temporary
files or starting a process. The browser workflow supplies this binary from its
same-run app artifact; missing configuration fails instead of compiling locally.

For optional feedback using the same package checks as the fast CI workflow:

```sh
scripts/ci/fast-feedback.sh
scripts/ci/fast-feedback.sh --working-tree # explicitly include local edits
```

The default comparison base is `hikyo-upstream/main`. Set `FAST_FEEDBACK_BASE`
and `FAST_FEEDBACK_HEAD` to test an explicit base/revision pair in a clean
checkout of that revision. `--plan` prints the selected domains without running
checks. Prepare the pinned tools and frozen client/web dependencies first; this
command does not install Node dependencies, generate files, or fix source. Go
checks can download modules/toolchains and populate normal Go caches. See
[the local feedback contract](scripts/dev/README.md)
for the checks and limits. The signed-commit pre-push hook remains installed.
The local feedback helper is optional and does not authorize merge. Hosted
useful feedback must pass before queue admission.

The pre-push hook also runs `scripts/ci/storybook-suite.sh` when a push changes
`web/`, `clients/ts/`, `.nvmrc`, the root `package.json`, that script or
`docs/site/scripts/test-csp.mjs`. If the suite already passed in this clone for
the exact pushed bytes and file modes of those inputs, the hook accepts the push
without looking at the checkout. Otherwise it runs the suite (about 4 minutes) in
the current checkout, which must hold exactly the pushed inputs: the same
committed versions, no modified or untracked files under them, and no `.env*`
files under `web/` or `clients/ts/`. The checkout need not be the pushed branch,
and an accepted push does not mean the current checkout is clean. The suite
needs the pinned Chromium: run `pnpm --dir web run e2e:install` once. Set
`HIKYO_PREPUSH_STORYBOOK=skip` to skip only that suite for a WIP push; CI still
runs it, and signature checks cannot be skipped.

The hook also runs `scripts/ci/golangci-lint-new.sh` when a push changes Go code
(the first run builds the pinned golangci-lint; later runs take well under a
minute). It applies the rules in `.golangci.yml`, chosen from findings that kept
recurring in review (unchecked errors, `==` on errors, `http.Get` without a
context, ignored `json.Marshal` errors, staticcheck
`SA*` bugs except `SA4006` and `SA4009`, plus `QF1001` and `QF1003` (not its style checks), unsafe integer narrowing, subprocess
arguments, file modes), and reports only issues on lines you changed since the
merge base, so the existing baseline of those rules never blocks you. A second strict
pass applies `.golangci-strict.yml` (wastedassign, unused, staticcheck `SA4006` and
`SA4009`) to every whole changed package (a moved file counts for both packages),
since those rules hold at zero across the repo. It lints a temporary worktree at the pushed commit, so any branch
can be pushed from any worktree, and uncommitted files and uncommitted local edits to
`.golangci.yml` and `.golangci-strict.yml` are ignored (the pushed commit's configs, pin and script apply); fetch `hikyo-upstream` first, as
a stale base widens "new code". Only the default build for your platform is
linted (files for another OS, or tagged `ui`, `k8se2e` or `floor*`, are not), and
type errors are always reported in full. Run it by hand with
`scripts/ci/golangci-lint-new.sh hikyo-upstream/main`.
Set `HIKYO_PREPUSH_GOLANGCI=skip` to skip it for a WIP push; the advisory
`ci-advisory-golangci` check still runs it on the pull request.

## Continuous integration

Each pushed commit receives fast `ci-feedback` using selected package checks.
Its four-minute runner timeout includes setup; shared-runner queue time is extra.
Application Go, tooling Go and frontend checks have independent runners with
the same deadline. All three lanes must succeed before queue admission.
This feedback does not authorize merge or replace any full-suite coverage.

When a feature is ready, merge its reviewed PR into dev after
`ci-dev-required` succeeds. Full dev CI finishes one active run and coalesces
pending work. Repair red integration before release promotion.

For a frozen release candidate targeting main, a maintainer adds its reviewed
PR to the mandatory merge queue. The trusted `ci-queue-required` check admits the exact head only
with successful useful feedback, valid signatures/DCO, and current review and
workflow authority. Admission alone cannot merge a PR. The queue runs the
complete suite on its immutable merge group against current main; every rebuilt
or reordered group needs fresh full proof. The group remains blocked until it passes all mandatory domains, including:

- `lint`, `test`, and the sharded isolation suites;
- `race`, the race detector over every package except `./internal/isolation/`
  (that suite runs race-instrumented in the `reliability` workflow's `race` suite,
  scheduled Mondays at 04:00 UTC or started manually with `suite=race`);
- `fuzz` (see below);
- `govulncheck` for known vulnerabilities, plus the supply-chain, web, compose,
  and Kubernetes end-to-end checks.

You do not need to run the complete suite locally. Follow automatic feedback,
resolve review findings, and request maintainer queue admission. See the
[CI guide](./docs/development/ci.md) for queue proof.

### Fuzzing

Fuzzing feeds a function randomised inputs to surface crashes and panics that
example-based tests miss. CI runs a bounded fuzz pass over every `Fuzz*` target
on every admitted full candidate, so you never have to fuzz manually. To reproduce or
extend one target locally:

```sh
go test -run='^$' -fuzz='^FuzzParseHeader$' -fuzztime=30s ./internal/crypto/
```

When fuzzing finds a failure, CI keeps the minimised input for 30 days and
replays it against the pull request's trusted base. A finding that does not
reproduce on the base is added to the pull request with its replay command; one
that also fails on the base opens or updates a standalone bug issue. Either way,
`fuzz` and the required full-proof check stay red until it is fixed. Commit the minimised input
with the fix so `go test ./...` keeps it as a regression case.
