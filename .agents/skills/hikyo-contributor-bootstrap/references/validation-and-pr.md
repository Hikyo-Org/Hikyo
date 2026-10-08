# Validation and contribution delivery

## Choose checks from the changed scope

Inspect current manifests, nearest rules, and relevant CI before running
commands. These examples match initial inspection; scripts own current flags.

- **Go/CLI:** test changed packages first with `go test ./<package>/...`; use
  `go test ./...` for cross-cutting work, and `go build ./...` plus `go vet ./...`
  as applicable. Embedded UI requires a SPA build and relevant ui-tagged checks.
  Gofmt changed handwritten files; `scripts/ci/check-go-imports.sh` pins imports.
- **Web:** after client/web installs, run `node --run typecheck`,
  `node --run lint`, and `node --run test` in `web/`. Build for integrated checks.
  Visual/interaction changes need affected states at desktop/mobile viewports
  and browser coverage, not just test counts. Check shared primitive siblings.
- **Generated client/API:** `pnpm --dir clients/ts run verify`; generated files
  are not hand-edited. Use current CI's pinned Go generators and inspect the
  full diff, including untracked artifacts. Retain API freeze/parity checks.
- **Docs site:** install its dependencies, then
  `pnpm --dir docs/site run verify`. Its tests build the site and run browser,
  PWA, and security checks; inspect browser prerequisites. Plain root Markdown
  does not automatically require site or application tests.
- **Shell/workflows:** use CI's pinned ShellCheck and
  `scripts/ci/run-go-tool.sh actionlint` for workflow changes. Go tool versions
  live in `scripts/ci/go-tool-modules.txt`; avoid random global replacements.
  Shell-only changes do not automatically need web tests.
- **Python:** apply only that module's Python requirements/checks, not
  TypeScript checks. In mixed work, scope each language's rules to its owner.

Inspect `git diff --check` and resulting status. Do not weaken tests, skips,
security settings, or linters to make bootstrap green.

## Preflight limits and empty-range trap

Read `scripts/dev/preflight.sh` and `scripts/dev/README.md`. The fast script
requires fnm; runs DCO, Go formatting/imports/build/vet, API freeze, client
generation/typecheck/tests, and web typecheck/lint. It omits web unit tests,
application Go tests, Playwright, PostgreSQL, all linters, and actual PR signature
verification. On untouched main, empty-range DCO fails. Execute relevant
bootstrap checks directly and report commit verification pending. Do not alter
preflight or invent a commit to get past that stage.

Once real contribution commits exist, broad contributions can run:

```sh
scripts/dev/preflight.sh
```

`--full` adds regeneration freshness and supply-chain fixtures needing more
tools, including chart/release prerequisites. It regenerates source and compares
with the checkout; legitimate uncommitted generated changes can trigger its
freshness gate. Investigate rather than resetting. Prefer focused checks for
narrow work instead of running every language/release fixture.

## Browser tests

With client/web dependencies installed, run from the root:

```sh
pnpm --dir web run e2e:install
pnpm --dir web run e2e
```

The install uses Playwright `--with-deps`; on Linux inspect system-package
privilege needs rather than silently escalating. Storybook browser tests are
separate: `pnpm --dir web run test-storybook` and
`pnpm --dir web run test-storybook:light` for changes affecting both themes.

The e2e script builds the SPA and invokes desktop/mobile separately. The harness
creates real Go instances and administrator state in temporary directories.
Optional `HIKYO_E2E_BINARY` must be an absolute path to a fresh ui-tagged binary
matching source/bundle; otherwise the harness builds it. Avoid inherited stale
binary overrides.

Do not substitute Vite tests, combine viewport projects in one invocation,
increase workers, or shard manually. Shared administrator/TOTP/passkey/cookie
state requires the repository isolation scheme. Inspect
`web/e2e/fixtures/instance.ts` for port overrides if defaults are occupied.
Partial flow selection is diagnosis, not proof of registry closure. Use native
product browser tools for manual QA when available; running the repo's automated
test script is separate from choosing another manual browser automation system.

## PostgreSQL and broader checks

SQLite success does not prove PostgreSQL behavior. Storage/transaction/migration
or isolation work requires a dedicated disposable PostgreSQL service and
relevant dual-engine tests. Inspect current CI for the PostgreSQL version,
permissions, and configuration. Some tests drop databases or tables; never use
production, shared, or personal data.

Provision a uniquely named test container on an unused loopback-bound port, or
use an explicitly designated disposable service. Docker is conditional, not a
prerequisite for normal web unit tests. Use test-only credentials, wait for
readiness, and set `HIKYO_TEST_POSTGRES_DSN` only in the test environment. Inspect
the harness for further variables. Record the container ID; clean up only task
resources, never all volumes.

Existing core wrapper:

```sh
HIKYO_TEST_POSTGRES_DSN="$hikyo_test_postgres_dsn" scripts/ci/test-core-packages.sh
```

It excludes `internal/isolation` and serializes `internal/app` to prevent
database-drop contention. Run affected isolation tests separately with the
disposable PostgreSQL setup. Do not add parallelism to accelerate bootstrap.
Race, fuzz, vulnerability, Kubernetes, no-egress, and release checks are distinct
evidence. Run what the contribution needs; authoritative exact-head CI proves
the complete remote result. `act` is not supported full-CI reproduction here.

## Commit, push, PR, and review

Proceed with delivery only through an authorized endpoint. Local bootstrap
alone does not authorize a commit, push, PR, or merge. For authorized changes:

1. Review the diff and accepted decisions, stage intended files, and use the
   current Conventional Commit style with `git commit -S -s`.
2. Refresh canonical main, verify DCO/signatures across the complete PR range,
   and finish relevant local checks. Preserve the real signing error on failure.
3. Push to the chosen writable remote with the installed hook, then confirm
   GitHub verification for every pushed PR commit. Local `G` and GitHub
   `verified: true` prove different stages.
4. Create/update the PR using the current template, stating problem, behavior,
   local evidence, and limitations. If T3 exposes PR linking, register it.
   Forks still require vouching/workflow approval and eligible changed paths.
5. Separate CI, review, merge, release, and deployment. Monitor only when
   requested/authorized with the available app-owned watcher. Retain human
   merge gates unless merge is explicitly authorized. Local/CI success does
   not prove production behavior.

Read current branch rules and workflow trust boundaries during delivery.
Required signatures on main, local signature checks, PR DCO checks, and CI
signature-checker fixtures are distinct controls. Do not execute untrusted PR
code inside credentialed bootstrap or CI workflows.
