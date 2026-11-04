# Configuration rollout authority: enrollment, rotation and retirement

This runbook covers the authority that signs managed configuration rollout
commands and the controller that trusts it. It adds an opt-in strict profile
with ML-DSA-65 signatures. Existing enrollments are unchanged: without the
`authority` object in the enrollment they keep one Ed25519 key and the v1
command bytes, byte for byte.

What was checked, at the candidate named in the
[handoff](../handoff/1131-rollout-trust.md): the controller, server and mailbox
behavior described here, driven by tests against an instrumented fake Kubernetes
API; the OpenSSL key-generation commands (by a test); `--print-authority-ids`;
the chart values and their refusals; and the `helm template` command in step 5
(run locally with the flags shown, rendering the right namespace). Every
`kubectl` command here, the Pod deletions and the enrollment ConfigMap
recreation were **not run** (NOTRUN) on a real cluster. Rehearse them on a
disposable cluster first.

## What the strict profile does and does not claim

Claim, in the [template](../security/pqc/qualification.md#claim-statement-template)
form: rollout command, server to controller, same build on both sides.
Confidentiality: not applicable, commands carry no configuration values.
Authenticity: dual (Ed25519 and ML-DSA-65, both required) or PQ-only (ML-DSA-65
required), with the trust anchor installed by you. Exceptions: E-11, E-14, E-15.

- The controller derives the required signature set from its own enrollment,
  never from the command. A command with a missing, extra, duplicate,
  reordered or other-algorithm signature is refused before any write: the
  controller reads the command and journal Secrets first, then refuses.
- Both signatures cover identical bytes that include the label
  `hikyo/config-rollout/command/v2`, the trust generation, the required set, the
  key ids and a digest of the enrolled target. A signature from one suite does
  not verify under another, and a command for one installation does not verify
  on another that shares a key.
- The anchor, meaning the enrollment record and the public key file, reaches the
  cluster through ordinary Kubernetes access. Nothing PQ protects how it got
  there (E-14). There is no in-band key transition: a command cannot announce a
  new key. Replacing a key is always your out-of-band re-enrollment (DEC-20 c).
- Anyone who can write the journal Secret can reset the generation floor (below)
  and the accepted sequence. Replay protection holds against actors without
  that write only.
- The controller image, the release that delivered it and the cluster API are
  separate surfaces. Until #1132 closes the release path, no document may say
  more than the claim above.

## Profiles

| Profile | Enrollment `authority` | `authority.pub` and signer file | Command bytes |
| --- | --- | --- | --- |
| Compatibility (default) | absent | one Ed25519 key | unchanged v1 |
| Strict, dual window | `required: [ed25519, mldsa65]` | both keys | v2, two signatures |
| Strict, PQ-only | `required: [mldsa65]` | the ML-DSA-65 key only | v2, one signature |

The sets are closed. ML-DSA-44 and ML-DSA-87, SLH-DSA, a second key of one
algorithm, an extra key beside the required ones, and Ed25519 as the only
required algorithm are all refused. The controller refuses to start if a key
file does not hold exactly the enrolled keys.

Record sizes: the ML-DSA-65 public key is 1952 bytes and its signature 3309
bytes. A signed dual command is about 5.7 KB against a 512 KiB record cap.
Measured on an Apple M2 Pro with Go 1.27.3 (local numbers, no budget set):
Ed25519 sign about 41 us and verify about 74 us; dual sign about 0.9 ms and
verify about 0.24 ms; PQ-only sign about 0.54 ms and verify about 0.2 ms.
Reproduce with:

```sh
go test -run '^$' -bench BenchmarkSignAndVerify -benchmem ./internal/configrollout/
```

## Read this first: what a re-enrollment cannot carry across

Re-enrollment is an out-of-band act and Hikyo has no in-band transition. These
limits follow from that and are registered in [Gaps](#gaps-and-registered-residual-risk):

- **An open rollout cannot cross it.** A rollout whose job is `applying` or
  `partial` holds a command signed under the old regime. After the switch the
  server cannot send, answer, renew or restore from that row (every one of those
  returns `invalid decision`), so the job cannot be completed or restored
  through Hikyo, and a boot with such a job is fatal unless sources are pending
  (G-2). Do not enroll with an open job.
- **Restore through Hikyo does not cross it either.** Restore signs a new
  decision on the stored row, which the new provider refuses (G-3). The
  controller's journal entry may still anchor, but nothing in Hikyo can use it.
- **The journal check is narrow.** Only the keys, the generation and the shape
  of the retained entry are checked, and the entry also has to match the
  enrollment's id, owner, incarnation and source aliases (G-1).

## Before you start

The commands below use these shell variables. `FULLNAME` is the chart's full
name (`kubectl -n "$NS" get deployment` shows it) and the rollout objects are
named from it by the chart:

```sh
NS=your-namespace
FULLNAME=your-release-hikyo
ROLLOUT="$FULLNAME-rollout"
```

1. Run a controller and server image that contains this support on **both**
   sides while the enrollment is still the compatibility profile. A controller
   or enrollment parser without it refuses the strict enrollment and the strict
   command because it rejects unknown fields. That refusal is the intended
   failure, and it is why readers come first.
2. Do not start a new Apply. There is no control that pauses Apply, so this is
   a rule for the people who can approve one. Wait until no rollout is
   `applying` or `partial` in Hikyo's status and the last controller response is
   terminal, and **re-check both immediately before step 5**. Check the
   controller journal:

   ```sh
   kubectl -n "$NS" get secret $ROLLOUT-journal \
     -o jsonpath='{.data.journal\.json}' | base64 -d \
     | jq '{action: .accepted.command.action, sequence: .accepted.command.sequence, outcome: .response.outcome, phase: .response.receipt.phase}'
   ```

   Proceed when the outcome is `complete` and the phase is `applied` or
   `restored`, or the accepted action is `prepare` (a staged plan, which a later
   prepare replaces). A rollout still open (phase `config-written`,
   `rollout-requested`, `rollout-ready` or `restoring`) must finish or be
   restored first, before the switch.
3. Keep the enrollment id. Changing `rollout.enrollmentID` is not supported (G-4).

## Enroll the strict profile (compatibility to dual)

1. Generate the ML-DSA-65 key. OpenSSL 3.5 or newer is needed, and the
   `seed-only` parameter is required: Hikyo accepts only the seed-only PKCS#8
   form, and OpenSSL's default form (seed plus expanded key) is refused.

   ```sh
   umask 077
   openssl genpkey -algorithm ML-DSA-65 \
     -provparam ml-dsa.output_formats=seed-only -out authority-mldsa65.key
   openssl pkey -in authority-mldsa65.key -pubout -out authority-mldsa65.pub
   ```

   **Keep the existing Ed25519 key in this step.** The controller's retained
   journal entry is still a v1 command, and it anchors only through the Ed25519
   key that signed it. A new Ed25519 key at this point makes the controller
   refuse to start (see [Rotate a key](#rotate-a-key)).
2. Build the two files the chart reads. Order does not matter.

   ```sh
   cat authority-ed25519.key authority-mldsa65.key > authority.key
   cat authority-ed25519.pub authority-mldsa65.pub > authority.pub
   ```

3. Read the key ids. They are algorithm-bearing and are what the enrollment
   pins:

   ```sh
   hikyo config-rollout --authority-public-key authority.pub --print-authority-ids
   ```

   Expected output, one line per key, for example:

   ```text
   ed25519 ed25519-sha256:<64 hex>
   mldsa65 mldsa65-sha256:<64 hex>
   ```

4. Replace the signer Secret content (`rollout.authorityExistingSecret`,
   `rollout.authorityKey`) with the combined `authority.key`, and set these
   values. `generation` must be higher than any earlier one (start at 1) and the
   ids must be in `required` order. The profile id `pqc-auth/1` is written into
   the enrollment by the chart and is signed with every command:

   ```yaml
   rollout:
     authorityPublicKey: |
       -----BEGIN PUBLIC KEY-----
       ...Ed25519...
       -----END PUBLIC KEY-----
       -----BEGIN PUBLIC KEY-----
       ...ML-DSA-65...
       -----END PUBLIC KEY-----
     authority:
       generation: 1
       required: [ed25519, mldsa65]
       keyIDs: ["ed25519-sha256:<64 hex>", "mldsa65-sha256:<64 hex>"]
   ```

   `helm template` refuses a wrong set, a wrong order, a mismatched id count or
   a malformed id, and renders nothing different for the default values. A
   strict render differs from the compatibility render only in the enrollment
   ConfigMap (compared on a fixture): the Deployment and the controller
   StatefulSet are rendered identically.
5. The enrollment ConfigMap is immutable, so it cannot be updated in place.
   Render the new one first and check it, then delete the old one, apply the
   new one and restart both consumers by deleting their Pods. Do **not** use
   `kubectl rollout restart`: it edits the Deployment's pod template, and the
   controller's Restore refuses a Deployment whose spec is neither the plan's
   before nor after state. Deleting Pods leaves the template alone.

   Render (both flags are required):

   ```sh
   KUBE_VERSION=$(kubectl version -o json | jq -r '.serverVersion.gitVersion')
   helm template RELEASE CHART -f values.yaml --namespace "$NS" \
     --kube-version "$KUBE_VERSION" -s templates/rollout-custody.yaml > rollout-custody.yaml
   ```

   Without `--kube-version` the chart refuses to render
   (`enrolled config rollout requires Kubernetes 1.36`); with only
   `--kube-version` it renders `target.namespace: default`, and applying that
   would install a wrong, immutable enrollment after the old one was deleted.
   Check the result before deleting anything. Expect exactly your namespace and
   generation (checked on a fixture with `--namespace ns --kube-version v1.36.1`):

   ```sh
   grep -o '\"namespace\":\"[^\]*\"' rollout-custody.yaml
   grep -o '\"generation\":[0-9]*' rollout-custody.yaml
   ```

   The file holds one ConfigMap and also the empty control Secrets and the
   Lease. Keep **only** the ConfigMap document (kind `ConfigMap`, name
   `$ROLLOUT-enrollment`) in `enrollment-configmap.yaml` and never apply the
   rest. Then:

   ```sh
   kubectl -n "$NS" delete configmap $ROLLOUT-enrollment
   kubectl -n "$NS" apply -f enrollment-configmap.yaml
   kubectl -n "$NS" delete pod -l app.kubernetes.io/name=hikyo,app.kubernetes.io/instance=RELEASE
   kubectl -n "$NS" delete pod $ROLLOUT-0
   ```

   `RELEASE` is the Helm release name; the server Pods carry the Deployment's
   two selector labels, which you can confirm with
   `kubectl -n "$NS" get pods --show-labels`. The `kubectl` commands are NOTRUN.

   **`helm upgrade` is not used here on purpose, and its effect on a live
   Deployment is unverified.** The chart renders the Deployment's arguments and
   ports, and Helm's three-way merge re-applies rendered fields over live ones,
   so an upgrade may revert fields a Hikyo rollout changed. That would also make
   Restore refuse. This was reasoned from the chart and Kubernetes' merge rules,
   not run on a cluster. Keep your values in step with the applied state before
   any later upgrade.

   A server whose signer file is not exactly the enrolled keys fails at boot
   with a conflict, and a controller whose public key file is not exactly the
   enrolled keys exits with
   `authority public key does not match the enrollment`.
6. Confirm enforcement. The new controller records the generation as a floor
   before it reads any command:

   ```sh
   kubectl -n "$NS" get secret $ROLLOUT-journal \
     -o jsonpath='{.data.trust\.json}' | base64 -d | jq .
   ```

   Expected: `profile` `pqc-auth/1`, and `generation`, `required` and `key_ids`
   equal to your values. Then Apply may resume. After the next accepted command,
   the journal shows the strict envelope:

   ```sh
   kubectl -n "$NS" get secret $ROLLOUT-journal \
     -o jsonpath='{.data.journal\.json}' | base64 -d \
     | jq '{generation: .accepted.command.authority.generation, algorithms: [.accepted.signatures[].algorithm]}'
   ```

   Expected: your generation and `["ed25519","mldsa65"]`. Until you see that,
   **no strict command has been accepted**, and the later steps that need one
   must wait.

What happens to the retained journal entry: the new controller anchors the last
accepted command. A pre-enrollment v1 command anchors with the Ed25519 key,
which stays required in the dual window. That keeps the controller starting and
keeps its stored plan. It does **not** make Restore available through Hikyo
(G-3). A v1 command that was queued in the mailbox and not yet accepted is
refused, and the server cannot renew a command signed under the old regime.
A v1 command that the controller had already accepted but not yet answered is
resumed by the dual controller from its journal (it needs no new authority),
which is why step 2 requires a terminal state.

## Move to PQ-only (retire the classical key)

Precondition: at least one **strict** command has been accepted since enrolling
(the step 6 check shows an `authority` object). A journal that still holds a v1
entry cannot move straight to PQ-only: the v1 entry anchors only through the
Ed25519 key, which PQ-only no longer requires, so the controller would refuse to
start.

Then set `required: [mldsa65]`, one key id, `generation` above the previous, a
public file with the ML-DSA-65 block only and a signer file with the ML-DSA-65
block only. Repeat steps 5 and 6. The retained strict entry still anchors,
through its ML-DSA-65 signature. After the new generation is confirmed live,
destroy the Ed25519 private key everywhere it was held. Signatures made with it
remain in history and are forgeable after a quantum break (E-12).

Going back (PQ-only or dual to compatibility) is refused by the floor. See
[Rollback](#rollback).

## Rotate a key

Always raise `generation`. Never reuse a generation with different keys: the
controller refuses a key set that differs at the same generation.

| Rotation | Retained entry | What to do |
| --- | --- | --- |
| Ed25519 key, ML-DSA key unchanged, **after** at least one strict command was accepted | Anchors through the ML-DSA signature | Steps 1 to 6 with new ids and a higher generation |
| Ed25519 key rotated while the journal still holds a v1 entry (including during the first enrollment) | **Not anchorable**: the controller exits at start | Accept one strict command first, or follow [Replace the ML-DSA key](#replace-the-ml-dsa-key) to clear the entry |
| ML-DSA-65 key replaced | **Not anchorable**: the controller exits at start | Finish or restore any open rollout, then follow [Replace the ML-DSA key](#replace-the-ml-dsa-key) |
| Both | Same as ML-DSA | Same as ML-DSA |

### Replace the ML-DSA key

There is no mechanism that lets the old key vouch for the new one. The
journal's last accepted command is signed by the old ML-DSA key, so a controller
holding only the new key cannot anchor it and exits with
`authority trust state refused`, writing nothing except the custody lease. To
proceed you give up the retained entry:

1. Complete every open rollout first (there is no Restore to rely on afterwards,
   see G-3). Note the last response.
2. Install the new enrollment and key files (steps 1 to 5) but stop before
   starting the controller if you can, or let it exit.
3. Remove only the plan key from the journal Secret. **Keep `trust.json`.**

   ```sh
   kubectl -n "$NS" patch secret $ROLLOUT-journal --type=json \
     -p '[{"op":"remove","path":"/data/journal.json"}]'
   ```

4. Start the controller. It records the new generation and accepts only
   commands signed under it. Commands signed under the old key fail the
   signature and the key-id binding, so emptying the accepted sequence does not
   reopen replay of old commands.

The same procedure applies when the journal entry no longer matches the
enrollment's incarnation, owner or source aliases (G-1). After a database
restore it needs `command.json` cleared too (see
[Restore, takeover and restart](#restore-takeover-and-restart)). It does **not**
work for an enrollment id change (G-4).

## Retire the enrollment or a key without replacing it

Destroying a private key while its public key is still enrolled leaves the
controller trusting a key that can no longer sign: configuration Apply stops
working, safely. Retire a key only by moving to a higher generation that no
longer requires it, which for the classical key means PQ-only. The ML-DSA key
cannot be dropped back to a classical-only profile.

## Rollback

The generation floor is stored in the journal Secret under `trust.json`, next
to `journal.json`, and the controller refuses an enrollment below it:

- a lower generation, which is what restoring an older enrollment ConfigMap or
  re-applying old Helm values produces;
- the compatibility profile once any strict generation was recorded;
- another key set at the same generation.

The refusal at start is visible: the controller exits with
`authority trust state refused`. Per-command reconciliation returns the same
refusal if the floor rose under a running process, but the controller loop
discards per-command errors (`custody.go`), so that one is **not** visible: the
mailbox simply gets no response. The refused start writes
nothing to any Secret or to the target. Its only write is the custody Lease,
whose epoch the existing admission policy requires. Fix the enrollment, do not
clear the floor.

A **malformed `trust.json`** also refuses a start, under either profile: the
base controller ignored that key, so this is a change for a compatibility
controller too. It needs journal write and it fails closed. Delete the key only
as the break-glass below.

Clearing the floor is possible and is **break-glass**: deleting `trust.json`
(or emptying the whole journal Secret) makes the controller forget it, and a
stale enrollment is then accepted again. That is the registered residual E-14:
journal write is authority over the floor. Do it only on the owner's recorded
decision, and expect to re-enroll afterwards.

**A new journal Secret has an empty floor.** The enrollment pins the journal
Secret's UID, so replacing that Secret means a new enrollment, and the new
Secret holds no `trust.json`. The first start under the enrollment you install
records that enrollment's generation. Nothing remembers a higher one. Keep your
own record of the highest generation you ever enrolled and install at least
that.

An older controller image reading a journal that holds a strict command refuses
it (unknown fields), so rolling the image back after the first strict command
fails closed. It is not a supported rollback.

## Restore, takeover and restart

- A restart or Pod replacement runs the same start-up check: floor, then
  journal anchor, then commands. A replacement Pod with a new UID takes custody
  through the existing lease epoch first, and its floor write carries that
  epoch, as the admission policy requires. The compatibility profile keeps its
  historical start and does not anchor at start; only a strict floor refuses it.
- After restoring a namespace from backup, the journal Secret comes back with
  its **old** floor. If the enrollment ConfigMap came back too, both are
  consistent and old. If you re-installed a newer enrollment, the floor rises.
  A restore never lowers a floor that is still in the journal, but a restore of
  the journal itself restores its older floor. Re-read `trust.json` after every
  restore and re-enroll to the intended generation before resuming Apply.
- Restoring the Hikyo database changes the recovery incarnation, and the
  enrollment pins the incarnation, so you must re-enroll with the new one. The
  site page asks for new object identities and no stale control messages
  (`configuration-rollouts`). Re-creating the control Secrets gives them new
  UIDs, which means a new enrollment and an empty floor (see Rollback). The
  in-place alternative below keeps the Secrets and removes the stale messages,
  which is what that sentence protects:
  1. Check the target first. If the receipt is **not** `applied` or `restored`
     the target is open and clearing keys is not enough (G-1):

     ```sh
     kubectl -n "$NS" get secret $ROLLOUT-receipt \
       -o jsonpath='{.data.receipt\.json}' | base64 -d | jq .phase
     ```

     Restore the target by hand from the receipt first (G-2, G-3).
  2. Re-enroll with the new incarnation (steps 4 and 5). Under the strict
     profile the controller then refuses to start with
     `authority trust state refused`, because its retained entry carries the
     old incarnation.
  3. Clear the stale journal entry (keep `trust.json`) **and the stale command**:

     ```sh
     kubectl -n "$NS" patch secret $ROLLOUT-journal --type=json \
       -p '[{"op":"remove","path":"/data/journal.json"}]'
     kubectl -n "$NS" patch secret $ROLLOUT-command --type=json \
       -p '[{"op":"remove","path":"/data/command.json"}]'
     ```

     The server's sequence counter is a database table, so the restore rolled
     it back, while the command Secret still holds the higher sequence. Without
     the second command the mailbox refuses every new command
     (`Mailbox.Send` conflicts when the stored sequence is not lower; tested).
     Replacing the ML-DSA key needs only `journal.json` cleared, because the
     counter keeps rising.

## Gaps and registered residual risk

Each gap names its residual risk, what the claim is limited to, and an owner
(Marc Went for all, on the owning ticket #1131 unless noted).

| ID | Gap | Residual risk | Claim limited to |
| --- | --- | --- | --- |
| G-1 | The journal anchor requires the retained entry to match the enrollment: the ML-DSA key (or, for a v1 entry, the Ed25519 key), the generation, and its shape, meaning enrollment id, owner, incarnation and any source alias the entry names. A changed incarnation, owner or removed alias, a replaced ML-DSA key, and an Ed25519 rotation over a v1 entry all fail. The controller refuses visibly at start and writes nothing but the lease. After a database restore the rolled-back sequence counter also needs `command.json` cleared. **If the target is open** (receipt not `applied` or `restored`), prepare and restore both return a conflict even with both keys cleared, the plan having gone with `journal.json`, and the controller loop discards that error, so nothing says so | Availability: the controller stays down until the operator clears `journal.json` (and `command.json` after a restore), which loses the stored plan; with an open target the rollout is blocked until the target is restored by hand (G-2, G-3) |  Plans survive only the transitions listed above |
| G-2 | An open job (`applying` or `partial`) cannot cross a re-enrollment. The new provider refuses `Send`, `Response`, `RenewCommand` and `DecisionCommand` on the old row; reconciliation clears the active runtime and returns the error, the job becomes `partial` with `transport_failed`, and `app.Boot` is fatal on that error unless sources are pending (source reading, `internal/app/app.go`; the service half is tested, the boot half is not run). Restore needs a `partial` job and fails the same way, and recovery refuses unresolved jobs. Once the floor is raised the old enrollment is refused too. A key swap before this change refused the same row, so the new parts are the floor closing the way back and the instructions here | A job stranded across a switch has no in-band way out: availability and a rollout in an unknown state | Enroll only with no open job; no claim that a rollout survives a switch |
| G-3 | Restore through Hikyo is not available across a re-enrollment, for a job started before it (see G-2). Restore also needs a `partial` job, which the procedure forbids crossing with, and `kubectl rollout restart` (or a Helm upgrade that re-applies rendered fields) changes the Deployment spec so the controller's Restore refuses | Restore of a pre-switch rollout is by hand from the receipt | The journal entry stays anchored; the plan is not restorable through Hikyo |
| G-4 | Enrollment id changes. Clearing `journal.json` is not enough: the command Secret still holds the previous id's command and the producer's mailbox refuses to replace it at any sequence (tested at 1 and 99); the server's per-enrollment sequence is keyed by id and restarts at 1; rows for the old id are refused (`self_config_deployment.go`, `_restore.go`, `_renew.go`). It fails closed | Availability only | Unsupported; keep the id |
| E-14 | The floor, accepted sequence and plan live in a Secret anyone with journal write can alter; the anchor reaches the cluster over classical access | Stale or forged state by an actor with journal write | As stated in the claim |
| | A command persisted before a switch is refused by the new controller and not renewed by the new server. Editing the enrolled target at an unchanged generation has the same effect on commands signed for the old target (the signed binding includes a digest of the target), while the retained journal entry still anchors | Fresh decision needed for decisions not yet started (not for open jobs, see G-2) | |
| | `helm upgrade` against a live Deployment changed by a rollout: whether it reverts executor-applied fields was not run | Possible silent revert | Unverified; use step 5 |
| | Classical history stays forgeable after a quantum break | E-12 | |
| | Rehearsal and rollout on a real cluster are NOTRUN. The kind rehearsal script still generates only an Ed25519 authority. The upgrade-source command path was not run under the strict profile | | |
