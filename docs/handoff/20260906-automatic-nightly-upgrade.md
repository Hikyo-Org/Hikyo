# Automatic systemd nightly upgrades

## Operator request and scope

The operator approved `sudo hikyo upgrade` on the Hikyo server, including locally
stored encrypted operator keys. The locked signed-upgrade ADR records this
explicit nightly custody exception. Linux systemd with one local SQLite database
is supported. Stable, PostgreSQL, Compose, Kubernetes and remote fleet apply keep
their existing manual procedures.

The coordinator is new work after nightly 26. Existing binaries cannot gain the
command without a first-use bootstrap. Once this change is published, the
Upgrades docs provide one HTTPS bootstrap command; it verifies the downloaded
coordinator before running it, without replacing the working server first.
If an interrupted first upgrade leaves an intermediate binary without the new
command, rerun that same bootstrap command. Subsequent upgrades use the CLI.

## Implementation map

- `internal/app/automatic_*.go`: exact signed release selection, bounded route
  discovery, authenticated target coordinator handoff, proof preparation and
  durable per-hop reconciliation. Existing database inspection and admission
  remain authoritative; no version comparison authorizes schema changes.
- `internal/hostupgrade`: root-owned deployment config, systemd preflight,
  durable startup fence, full writer stop, runtime UID and root credential
  isolation, atomic executable replacement and exact process/readiness proof.
- `internal/upgradecustody`: age-encrypted, installation-bound backup identity,
  independent attestation signer and root escrow. Root-only vault defaults to
  `/etc/hikyo/upgrade-keys/operator.age`; the passphrase is interactive and is
  never persisted. Runtime receives only the public operator pin and proof.
- `internal/selfupdate` and `internal/upgradeassembly`: reuse complete nightly
  verification and atomic public bundle assembly. Cached assets are rechecked
  against immutable API inventory and current signed trust. Route discovery
  stops once it proves the installed source's route, including exact legacy
  bridges. The old notification flow still stages without applying remotely.
- `install/upgrade-nightly.sh`: first-use verification through pinned Cosign,
  complete OIDC certificate policy and pinned recovery-key authorization of the
  current catalog/policy, including revocations and persisted trust floors.
  `docs/site/scripts/prepare-content.mjs` copies the canonical script to the
  published docs artifact. No private release key is in the script or binary.

## Durable state and interruption handling

Default config is `/etc/hikyo/upgrade.json`. Controller state and its operation
journal are under `/var/lib/hikyo-upgrader`; public runtime evidence lives under
`/var/lib/hikyo-upgrade`. Persistent installation custody is a separate runtime
directory at `/var/lib/hikyo-installation`.

One encrypted backup and signed restore-drill attestation bind the full route.
The scratch drill reconciles one existing eligible principal, mints and revokes
a credential through existing authorization, and never changes live authority.
Each target performs its existing migration and boot gates. Intermediate hops
stay in maintenance; only the final exact process and healthy database allow
removal of the systemd fence. Consumed proof is removed from restart config.

The journal distinguishes preparing, proved, write intent, schema applied,
healthy hop, complete and terminal recovery-required states. An unchanged source
can retry before writes; schema-applied and healthy admissions reconcile against
the exact original source, route and hop. Uncertain writes or failed candidate
health retain the fence and require explicit recovery. An already healthy DB
record is not falsified if external readiness fails: the terminal host journal
and startup fence preserve that additional failure. No automatic old-binary or
database rollback occurs.

## Validation and delivery limits

`TestAutomaticUpgradePackagedNightlyRoute` builds production-stamped executable
fixtures with real signed declarations. It proves a populated legacy bridge and
two nightly hops, actual encrypted CLI export, automatic scratch proof, real
migrations, HTTP maintenance/readiness, protected-value readability and a restart
without consumed evidence. It also proves post-migration install interruption
resume and terminal refusal after actual wrong-root startup. Normal and race
runs passed. Only service lifecycle is substituted in this packaged test.

The relevant Go packages, Go vet, bootstrap refusal tests, root Linux adapter
tests, ShellCheck, and docs typecheck/build/PWA browser checks passed locally.
Additional real-systemd acceptance exercises the actual generated unit and
drop-ins separately from the packaged database test.

No live installation was changed. Local tests do not mean a nightly containing
this feature has been published; check the eventual PR, merge and signed nightly
publication before giving the operator the bootstrap command as available.
Local encrypted custody does not provide an off-host disaster recovery copy.

## Continuation on 2026-09-06 (second session)

The first session ended at the real-systemd acceptance fix. The second session
rebased onto main after #686, found main red, and fixed it first:

- PR #691 `fix(config): accept ephemeral listeners for production nodes`. #686
  rejected port 0 outside development mode without a test or rationale, which
  broke `TestPackagedNightlyReleaseUpgrade` and
  `TestNightlyAssemblyAuthenticatesCompleteDownload` on main. This PR is the
  stacked base of the feature PR.

Adversarial review of the feature produced two fixes folded into this branch:

- `fix(release): enforce current nightly policy revocations online`. Revocation
  was judged only against the policy copy bundled inside each immutable release,
  so a single nightly could not be revoked without refusing every nightly of its
  policy period. Online verifiers now also fetch the live
  `release/trust/nightly/policy.json`, require a catalog-authorized digest, and
  refuse manifests it revokes. The offline boot gate is unchanged. The runbook
  gained a "Revoke one published nightly" section.
- `fix(upgrade): prune stale public evidence and report unlock failures
  alongside outcomes`. Completed runs now remove earlier `bundle-*`,
  `evidence-*` and `backup-*` directories the journal no longer references. The
  lock release error no longer hides a migration failure.

Verified locally after the rebase: Go tests for app, hostupgrade, selfupdate,
releasetrust, upgradecustody, upgradeassembly, upgradebundle, upgradegate,
updatecheck, upgradecompat, store/upgrade, service, devupgrade, config, the
release and CI script packages; both Docker acceptance scripts (root adapter and
real systemd); bootstrap shell tests; ShellCheck; actionlint; docs check, build
and verify-docs.

Open items for the human: the standing Codex review of the Claude-authored fixes
could not run (Codex usage limit until 2026-09-13); a fallback reviewer model
needs Marc's choice. Merge of #691 and of the feature PR is gated on CI green plus
that review. The server on nightly 23 still cannot upgrade until a nightly
containing this feature is published; then the bootstrap command in the Upgrades
docs applies.
