# PQ staged rollout, abort and recovery runbook (NOT EXECUTABLE)

> **Not executable.** This runbook is part of the #1135 qualification packet for
> candidate `68b2673dea9eb7053edd77eca2e585146bb5ad2c`. It must not be followed
> on any production or shared system while the owner's open decisions in the
> [packet](../security/pqc/qualification-packet.md#release-and-blocked) stand.
> It authorizes no key generation, rotation, trust activation, provider
> mutation, release, publication or deployment. No step in it was run against a
> real environment: every step that needs one is marked **NOTRUN**. On
> 2026-11-04 the owner authorized merging each green child PR to `dev` and
> accepted publication in the next nightly; that is the maintainer security
> approval for the PR that merges under it, and it authorizes no run of this
> runbook. #1172 (Transit, migration 00084) did not merge with that round; it merges
> separately, and its migration 84 is roll-forward only and moves the rollback floor of every instance that upgrades. Independent adversarial review is required;
> cross-provider review is SKIPPED by explicit owner instruction (2026-11-02)
> and is not CLEAN. The surface runbooks it indexes carry their own caveats;
> this file adds order, gates and abort rules and does not replace them.

## Command validation

Commands below were checked against a `hikyo` binary and a
`hikyo-receiver-example` binary built from the candidate (Go 1.27.3
darwin/arm64). Two levels: **help** means the name and flags appear in the
binary's own usage text; **run** means it was executed once with throwaway input.
Nothing was executed against an instance, a cluster or a provider.

| Command | Level | Note |
| --- | --- | --- |
| `hikyo backup keygen [--hybrid] [--output-file PATH \| --dangerously-print]` | help; one **run** for step 1.2 | Group form only: `hikyo backup --help`. `hikyo backup keygen --help` and `hikyo help backup keygen` print the backup group usage and exit 0, never keygen-only help. The single run established the datastore requirement in step 1.2; its output was not kept and it was not repeated for this repair |
| `hikyo backup export --out DIR ((--recipient AGE-RECIPIENT)... \| --passphrase-file PATH)` | help | The CLI reads neither `HIKYO_BACKUP_RECIPIENTS` nor `HIKYO_BACKUP_DIR`; pass `--recipient` and `--out` |
| `hikyo backup upgrade-export`, `hikyo backup upgrade-drill` | help | The manual route with a hybrid recipient and a separately held hybrid identity; the refusal text for the automatic path names them. Both refuse without the installation trust context (public bundle, operator pin and state directory), as set out in [manual upgrades](manual-upgrades.md#populated-upgrade). Only `sudo hikyo upgrade` and the unattended upgrade refuse a hybrid managed policy (see Stage 1). No hybrid vault (automatic custody) drill exists |
| `hikyo restore run`, `status`, `reconcile --principal ID`, `drill --from ARCHIVE ...` | help | `hikyo restore --help` |
| `hikyo escrow verify --root-key-file FILE --assert-separate-custody` | help | Checks the recovered root key against the instance hierarchy; it needs the datastore and `HIKYO_ROOT_KEY_FILE`. It concerns the root key escrow, not the backup identity |
| `hikyo config-rollout --authority-public-key PATH --print-authority-ids` | **run** | Throwaway Ed25519 and seed-only ML-DSA-65 public keys made with OpenSSL 4.0.3; printed `ed25519-sha256:` and `mldsa65-sha256:` ids, exit 0, no cluster contacted. Keys deleted |
| `openssl genpkey -algorithm ML-DSA-65 -provparam ml-dsa.output_formats=seed-only` | **run** | OpenSSL 4.0.3 only; produced a key and its public key. A different OpenSSL may not support it |
| `hikyo server` flags `--config-rollout-enrollment`, `--config-rollout-signing-key`, `--root-key-file`, `--tls-cert-file`, `--tls-key-file` | help | `hikyo server --help` |
| `hikyo instance-config status`, `adopt`, `apply --revision N --expected-generation N` | help | |
| `hikyo values set KEY (--stdin \| --value-file PATH)`, `hikyo values publish --versions <id,id> [--env <env>]` | help | That the owner configuration project is reached this way was not run |
| `hikyo -vv <verb>` | help | Verbosity before other options |
| `hikyo adapter plan\|sync\|test --target <target>` | help | |
| `hikyo-receiver-example keygen -protocol 2 -dir DIR`, `sender-keygen -dir DIR [-with-ed25519]`, `pubkey -pem FILE`, `fingerprint -protocol 2 -origin -recipient -ack-key -ack-key-mldsa65 -generation` | help | Per-subcommand `-h`. The tool is not shipped in a release; build it from source |
| `hikyo transit key create --algorithm ...` | help | Lists only `xchacha20-poly1305\|ed25519\|hmac-sha256`: ML-DSA is unreachable |
| `hikyo pki issuer create-root --key-algorithm ALG` | help | ALG is open text; the server refuses `mldsa65` (isolation test) |
| TLS profile selection | none | No `--tls-profile` flag exists in any `hikyo` help. Server: `HIKYO_TLS_PROFILE` through managed configuration. CLI: `tls_profile` in a provisioned trust bundle |
| `kubectl`, `helm`, `docker`, host service commands | not validated | Cited from the surface runbooks only; all NOTRUN |

Found while validating: `docs/operations/pqc-release-signing-runbook.md` correctly
contains no command; no PQ signing tool exists.

## Prerequisites (all must hold before Stage 1 on any environment)

| Prerequisite | State at the candidate |
| --- | --- |
| A named non-production environment and a written resource-specific authorization | None exists. HOLD |
| Maintainer security approval for each child being enabled, independent adversarial review of its final head, and the child merged | The owner's 2026-11-04 merge authorization is the security approval for each PR that merges under it; #1172 was held back from that authorization and needs its own approval record (its handoff says security approval: not given). It covers merging, not enabling a feature anywhere. Independent review of each final head is still required. HOLD for any enablement |
| Owner decisions in the packet (DEC-17 thresholds, key custody, peers and expiry for exceptions, and the other open items listed in the packet's "Release and blocked") | Open. HOLD. Decided on 2026-11-04: `backup-receipt/v2` kept as built with the E-19 gap registered; the stale-node fence accepted as recorded limit E-22 (strict is per process; the fence stays #1127 work); the `/api/v1` freeze, option A |
| Every node, receiver and restore host runs a build that contains the readers (the release with these changes) | No such release exists. NOTRUN |
| Frozen legacy fixtures pass on first-party nodes | Passed locally by children; NOTRUN on any real node |
| Backup of the datastore taken and a restore drill passed before any floor-moving upgrade (migration 84 and runtimeconfig migration 6 are roll-forward; migration 84 is #1172, which merges separately from that round) | NOTRUN |
| Operator attests the readers and verifiers Hikyo cannot observe (E-17) | Required; cannot be measured |

## Protected secret loading

Applies to every key below. None of it was run with real material.

- Generate on the operator or custodian host, with `umask 077`, writing to files
  with `--output-file` or the surface tool's `-dir`. Never `--dangerously-print`
  into evidence, never a key in argv, a log, an audit event, a receipt or an
  error. Never paste a private key into chat or a ticket.
- Pass secrets to Hikyo by file (`--identity-file`, `--root-key-file`,
  `--value-file`, `--key-file`, `--config-rollout-signing-key`), mode `0600` or
  stricter; the server refuses group- or world-readable webhook key files.
- Imported keys are not accepted for new strict material unless generated by the
  maintained tool (DEC-12). The code does not enforce this for webhook keys, and
  the rollout and PKI guides generate ML-DSA seeds with OpenSSL, which that rule
  does not clearly allow (packet C-13: owner decision).
- Entropy is `crypto/rand` via the maintained tools. Do not hand-roll keys.

## Key escrow and retention

| Item | Rule | Source |
| --- | --- | --- |
| Backup identity (X25519 and hybrid) | Escrow in a failure domain separate from the root key and datastore. No command checks or records custody of this identity. `hikyo escrow verify --root-key-file FILE --assert-separate-custody` is the root key escrow check: it verifies the recovered root key against the hierarchy and records instance, incarnation, root epoch and the operator's assertion. Keep every identity that wrote a retained archive; keep the X25519 identity for archives written before the switch | #1126, migration |
| `operator.age` custody record | Holds backup identity, P-256 attestation key and root escrow, sealed by age-scrypt at a 16-byte file key (category 1). Old copies hold a live `root_escrow`; retiring them needs root rotation. Root rotation does not re-seal it (#1154) | migration |
| Webhook receiver identity and sender keys | Do not destroy the receiver's old identity while retained ciphertext needs it; sender pair rotates as a unit; never restore a retired pair | #1128 |
| Rollout Ed25519 and ML-DSA keys | Keep the Ed25519 key until the PQ-only generation is confirmed, then destroy it; same custody for both; retired classical signatures stay forgeable after a quantum break (E-12) | #1131 |
| Any PKI ML-DSA issuer, CRL or key | Never delete to roll back; PKI retire destroys only an eligible key. (Transit retire stops signing and retired keys still verify; see the Transit operations note.) | #1133, #1129 |
| Retirement | Only when every eligibility criterion holds and the owner records a decision with an evidence reference. A destroyed key erases nothing already written. **No retirement drill was run** | migration |

## Staged order

Order follows the program design and the migration ladder: readers first, then
confidentiality of long-lived material, then protocols, then signatures. Each
stage repeats the cohorts **disposable fixture, then named non-production, then
(separately authorized) production**. Only the first cohort has any local
evidence, and no cohort was exercised for real. A step starts only when the
previous gate is recorded.

### Stage 0: decide (open decisions HOLD)

Gate: owner and maintainer records for the open decisions; no HOLD on the
surface. Abort: nothing deployed. Decided on 2026-11-04: the rollback policy
for the moved floor in what merges (runtimeconfig migration 6) is accepted for the next
nightly, and #1172's migration 84 merges separately and moves a second floor; the stale-node
fence is a recorded limit (E-22). The packet's "Release and blocked" lists what
stays open.

### Stage 1: hybrid backups (#1126)

| Step | Command or action | Observable success | Abort or rollback | Status |
| --- | --- | --- | --- | --- |
| 1.1 Readers | Deploy the reader-capable build to every node and every host that may restore | Legacy fixture restores; `hikyo restore drill` passes with an X25519 archive | Restore the pre-upgrade backup (the candidate carries roll-forward floors: migration 84, which is #1172 and merges separately, and runtimeconfig migration 6; whether an old binary refuses either was not run, so the abort is the restore either way) | NOTRUN |
| 1.2 Key | `hikyo backup keygen --hybrid --output-file PATH` (a "server host only" group: it fails with `no datastore configured` until `HIKYO_DB` or `--dev` is set, and with `HIKYO_DB` set it ran without creating a database; running it on the server host weakens the custody split with the root key and datastore, so set `HIKYO_DB` to `sqlite:PATH`, which need not exist on the custodian host); escrow the private half apart from the root key (no command records that). Separately, on the server host, `hikyo escrow verify --root-key-file FILE --assert-separate-custody` checks the recovered root key against the hierarchy; it needs `HIKYO_ROOT_KEY_FILE` as well as the datastore, and with only `HIKYO_DB` set it refuses and asks for `HIKYO_ROOT_KEY_FILE` | Public recipient (`age1pq1...`, 1959 characters) recorded; escrow drill restores a fixture; `escrow verify` prints `Escrow root verified against the current hierarchy.` and records the instance, incarnation and root epoch, not the backup identity | Destroy only never-used test keys | NOTRUN |
| 1.3 Configure | Set `HIKYO_BACKUP_RECIPIENTS` to hybrid recipients only (at most 32, no mix, 65,536-byte managed cap) through managed configuration | Over-bound or mixed set refused before output | Publish X25519 recipients again | NOTRUN |
| 1.4 Write and prove | `hikyo backup export --out DIR --recipient <hybrid recipient>` (the CLI does not read the managed set from step 1.3); then `hikyo restore drill --from ARCHIVE --identity-file PATH --root-key-file PATH --principal ID --project ORG/PROJECT --target-sqlite PATH` (or `--target-postgres-dsn-file PATH`) `-o json`. A plain `hikyo backup export` emits **no receipt**: only an upgrade export builds one, so for `backup-receipt/v2` use the manual route in [manual upgrades](manual-upgrades.md#populated-upgrade): `hikyo backup upgrade-export --out DIR --recipient <hybrid recipient>`, run with the instance's public bundle, operator pin and state directory (`HIKYO_UPGRADE_BUNDLE`, `HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY`, `HIKYO_UPGRADE_STATE_DIR`); it refuses without that installation trust context (the requirement that every export path emits a receipt is unmet, packet E-19) | Archive written; drill passes; truncated archive refused; for the receipt, `upgrade-export` prints `ciphertext: PATH` and `receipt: PATH`, and the receipt file's `format` field is `backup-receipt/v2` | Stop exporting hybrid; keep both identities | NOTRUN |
| 1.5 Constraint | `sudo hikyo upgrade` and the unattended container upgrade **refuse** a hybrid managed policy (custody is X25519). Do not flip the policy to hybrid while an upgrade is in progress: no recovery exists for a source frozen at `backup-preparing` whose policy became hybrid | Use the manual route or restore the X25519 policy first | | NOTRUN |

No strict backup stage exists: the profile selector is NOT BUILT and archive
provenance is HOLD (E-19). Restored credentials are inert until the operator
reconciles each principal with `hikyo restore reconcile --principal ID`.

### Stage 2: strict TLS on the native listener and owned clients (#1127)

| Step | Action | Observable success | Abort or rollback | Status |
| --- | --- | --- | --- | --- |
| 2.1 Inventory | List every client of the listener; confirm each offers `X25519MLKEM768` (a browser or tool that does not will be refused) | Written list | | NOTRUN |
| 2.2 Readers | Deploy the build; the offline gate runs runtimeconfig migration 6 and records `compat` | `hikyo instance-config status` shows the revision | Binary rollback is a backup restore, not an in-place downgrade | NOTRUN |
| 2.3 Select | Publish `HIKYO_TLS_PROFILE=strict` as a managed value, then `hikyo instance-config apply --revision N --expected-generation N`. Needs a TLS certificate pair; refused on plaintext listeners | Every node reports the new generation active | Publish and apply `compat` | NOTRUN |
| 2.4 Fence caveat | A node behind the desired generation keeps its old profile and is only reported pending. Strict is per process: the owner accepted this as recorded limit E-22 on 2026-11-04 and the fence stays #1127 work. **No fleet claim is possible until a fence exists** | Per-node statement only | | Recorded limit |
| 2.5 Observe | `hikyo -vv <verb>` shows the TLS line with `tls1.3` and `x25519mlkem768`; a classical-only client is refused | Observed line; refusal | | NOTRUN |

CLI strict needs a provisioned trust bundle with `tls_profile`; a CLI older than
the closed-set trust-store decoder silently reads it as `compat`. Strict gives
confidentiality only; authentication stays classical (E-20). No external hop
(adapters, federation, mail, database, update, pairing) is reached.

### Stage 3: sealed webhook protocol 2 (#1128), receiver first

Follow [the migration guide](sealed-webhook-v2-migration.md) steps 1 to 7. Gates:
receiver keys made with `hikyo-receiver-example keygen -protocol 2 -dir DIR` and
the ML-DSA sender key with `sender-keygen -dir DIR` (existing Ed25519 key kept);
the sender pair is pinned and the hybrid members added together; the
fingerprint is computed with `fingerprint -protocol 2 ...` and read over an
independent channel; the registry entry changes in place with
`max_response_bytes` at least 5120; `hikyo adapter test --target <target>`
proves the responder holds both acknowledgement keys, **not** the hybrid
identity. Observable success: only `/hikyo/v2/sync` for the new target in the
receiver log, a stripped message refused with no acknowledgement, a replay
answered `already_applied`. Abort and rollback: restore the v1 registry entry
(a downgrade; no floor exists, R-1128-B) and remove
`signing_key_file_mldsa65`. The classical write path stays open until v1 is
retired on the receiver, a separate recorded decision (R-1128-A). The reference
receiver is not production software. **NOTRUN against any real receiver.**

### Stage 4: rollout authority (#1131), disposable cluster only

Follow [config-rollout-authority](config-rollout-authority.md). Gates: enroll
only with no open job and a terminal target state; keys as seed-only ML-DSA-65
beside the Ed25519 key; record ids with `hikyo config-rollout --authority-public-key authority.pub --print-authority-ids`;
the enrollment is installed out of band by the operator (DEC-20 c); a strict
command must be accepted before moving to PQ-only. Observable success: the
controller starts, raises the floor in the journal Secret `trust.json`, accepts a
strict command and refuses a v1 command, a stripped signature and a stale
enrollment with no change to the target; a refusing start still writes the custody
Lease and start-up state, and the floor is raised before commands are read. Abort: do not start a new Apply during a
switch; recovery from an unanchorable journal is clearing `journal.json` (and
`command.json` after a database restore), a break-glass act. The kind
rehearsal, every `kubectl` and `helm` step and the real cluster are **NOTRUN**.

### Stages not executable at the candidate

| Surface | Why there is no stage |
| --- | --- |
| Transit ML-DSA (#1129, PR #1172) | Not in what merged in that round. #1172 carries the mechanism, the public surface (API enum and clients from #1212, CLI help, WebUI option) and migration 00084, which moves the rollback floor for every upgrading instance whether or not a key is created. After it merges an authorized caller can create an `mldsa65` Transit key; no key is created and no signer enabled by the merge. Open there: restore-hold, instance reencrypt, multi-node takeover, release-binary refusal against a migration-84 database, Wycheproof vectors and owner thresholds (NOTRUN, see the handoff) |
| Private PKI ML-DSA (#1133) | `MLDSAEnabled` is false in production wiring |
| Release trust (#1132) | No signer, no root, no caller of the verifiers on any shipping path; custody of the ML-DSA recovery key is not named. See the [signing runbook](pqc-release-signing-runbook.md), also not executable |
| Pairing (#1130) | HOLD; no PQ protocol. The classical pairing and revocation drill is NOTRUN |
| External routes (#1134) | No selector reaches them; nothing to enable |

## Abort criteria (any one stops the stage)

A failed or missing mandatory owner decision; an unknown algorithm, version or
trust generation; missing custody or restore proof; lost audit closure; an
unsafe downgrade (a classical fallback under a PQ policy); a budget exceeded once
the owner sets thresholds (none exist, so this criterion cannot be evaluated);
an unexplained change in a consumer; a node behind the desired generation on a
strict surface; a restore that fails the drill. Preserve existing recovery and
maintenance fences, reconcile the exact failed stage, never clear a hold,
reactivate revoked authority or fall back to classical behavior to finish.

## Incident recovery

| Event | Action | Status |
| --- | --- | --- |
| Restore needed | `hikyo restore run --from ARCHIVE --identity-file PATH`, then `hikyo restore status` and `hikyo restore reconcile --principal ID` one principal at a time; machine credentials are re-minted | NOTRUN with a hybrid archive on a real host |
| Backup or receiver identity compromised | New identity and generation, fresh PQ archive; old identity still opens historical copies | NOTRUN |
| Signing key compromised | Per the surface's existing rule (PKI CRL, Transit compromise, webhook generation change, rollout re-enrollment with a higher generation) | NOTRUN |
| Lost ML-DSA recovery key (E-18b) | No PQ-signed reinstall exists; recovery is an operator-installed root from an independently verified source | HOLD |
| Authority broken by a quantum adversary | Remote classical-authenticated updates are untrusted; offline reinstall | NOTRUN |
| Credentials that crossed classical hops before strict | Treat as exposed; rotate long-lived ones after the surface is strict; a rotated credential re-sent over a compat hop is exposed again | NOTRUN |

## Observable success, summarised

Per stage the gate is an observed artifact: a passing drill with the receipt
schema, a TLS line with the named group plus a refused classical client, a
receiver log showing only the v2 path plus refusals, a controller journal with
the raised floor plus refusals that leave the target unchanged. None was observed in a real
environment. A hosted job or a local test is not that observation.
