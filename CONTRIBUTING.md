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

## Developer Certificate of Origin

Every commit in a pull request must carry both a Developer Certificate of Origin
(DCO) sign-off and a cryptographic signature that GitHub reports as **Verified**.
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
printf '%s %s\n' "$(git config user.email)" "$(cat "$hikyo_signing_key.pub")" >> "$hikyo_allowed_signers"
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
as the checker base. The hook uses `origin/main` even when `origin` points to
your fork; this explicit fetch updates that local reference from Hikyo:

```sh
scripts/git/install-hooks.sh
git fetch https://github.com/Hikyo-Org/Hikyo.git refs/heads/main:refs/remotes/origin/main
```

After creating your contribution with `git commit -S -s`, run:

```sh
git log -1 --format='%h %G? %s'
scripts/ci/check-dco.sh origin/main HEAD
scripts/ci/check-commit-signatures.sh origin/main HEAD
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
until then, `ci-required` reports the author as not vouched and the pull
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
