# Configuration rollout authority: enrollment, rotation and retirement

This runbook covers the authority that signs managed configuration rollout
commands and the controller that trusts it. It adds an opt-in strict profile
with ML-DSA-65 signatures. Existing enrollments are unchanged: without the
`authority` object in the enrollment they keep one Ed25519 key and the v1
command bytes, byte for byte.

Everything here was checked against the code at the candidate named in the
[handoff](../handoff/1131-rollout-trust.md). The commands that touch a real
cluster were **not run** (NOTRUN): the tests drive the real producer, mailbox
and controller against an instrumented fake Kubernetes API. Rehearse every
cluster step on a disposable cluster first.

## What the strict profile does and does not claim

Claim, in the [template](../security/pqc/qualification.md#claim-statement-template)
form: rollout command, server to controller, same build on both sides.
Confidentiality: not applicable, commands carry no configuration values.
Authenticity: dual (Ed25519 and ML-DSA-65, both required) or PQ-only (ML-DSA-65
required), with the trust anchor installed by you. Exceptions: E-11, E-14, E-15.

- The controller derives the required signature set from its own enrollment,
  never from the command. A command with a missing, extra, duplicate,
  reordered or other-algorithm signature is refused before any Kubernetes call.
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
2. Pause configuration Apply. Wait until no rollout is `applying` or `partial`
   and the last controller response is terminal. Check the controller journal:

   ```sh
   kubectl -n "$NS" get secret $ROLLOUT-journal \
     -o jsonpath='{.data.journal\.json}' | base64 -d \
     | jq '{action: .accepted.command.action, sequence: .accepted.command.sequence, outcome: .response.outcome, phase: .response.receipt.phase}'
   ```

   Proceed when the outcome is `complete` and the phase is `applied` or
   `restored`, or the accepted action is `prepare` (a staged plan, which a later
   prepare replaces). A rollout still open (phase `config-written`,
   `rollout-requested`, `rollout-ready` or `restoring`) must finish or be
   restored first.
3. Keep the enrollment id. Changing `rollout.enrollmentID` is not supported by
   this procedure (see [Gaps](#gaps-and-registered-residual-risk)).

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

   Keep the existing Ed25519 key. To rotate it at the same time, generate a new
   one with `openssl genpkey -algorithm ED25519 -out authority-ed25519.key` and
   `openssl pkey -in authority-ed25519.key -pubout -out authority-ed25519.pub`.
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
   ids must be in `required` order:

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
   a malformed id, and renders nothing different for the default values.
5. The enrollment ConfigMap is immutable, so it cannot be updated in place.
   Delete it and render it again, then restart both consumers. The controller
   StatefulSet updates with `OnDelete`, so delete its Pod:

   ```sh
   kubectl -n "$NS" delete configmap $ROLLOUT-enrollment
   helm upgrade RELEASE CHART -f values.yaml
   kubectl -n "$NS" rollout restart deployment/$FULLNAME
   kubectl -n "$NS" delete pod $ROLLOUT-0
   ```

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

   Expected: `generation`, `required` and `key_ids` equal to your values. Then
   resume Apply. After the next accepted command, the journal shows the strict
   envelope:

   ```sh
   kubectl -n "$NS" get secret $ROLLOUT-journal \
     -o jsonpath='{.data.journal\.json}' | base64 -d \
     | jq '{generation: .accepted.command.authority.generation, algorithms: [.accepted.signatures[].algorithm]}'
   ```

   Expected: your generation and `["ed25519","mldsa65"]`.

What happens to the retained plan: the new controller anchors the journal's last
accepted command. A pre-enrollment v1 command anchors with the Ed25519 key
(which stays required in the dual window), so **Restore of the last plan keeps
working** and new commands must be strict. A v1 command still queued in the
mailbox is refused, and the server cannot renew a command signed under the old
regime. A decision that was pending at the switch needs a fresh MFA decision.

## Move to PQ-only (retire the classical key)

Set `required: [mldsa65]`, one key id, `generation` above the previous, a public
file with the ML-DSA-65 block only and a signer file with the ML-DSA-65 block
only. Repeat steps 5 and 6. The retained plan still anchors, through its
ML-DSA-65 signature. After the new generation is confirmed live, destroy the
Ed25519 private key everywhere it was held. Signatures made with it remain in
history and are forgeable after a quantum break (E-12).

Going back (PQ-only or dual to compatibility) is refused by the floor. See
[Rollback](#rollback).

## Rotate a key

Always raise `generation`. Never reuse a generation with different keys: the
controller refuses a key set that differs at the same generation.

| Rotation | Retained plan | What to do |
| --- | --- | --- |
| Ed25519 key, ML-DSA key unchanged | Kept: the ML-DSA signature anchors it | Steps 1 to 6 with new ids and a higher generation |
| ML-DSA-65 key replaced | **Not anchorable**: the controller exits at start | Finish or restore any open rollout, then follow [Replace the ML-DSA key](#replace-the-ml-dsa-key) |
| Both | Same as ML-DSA | Same as ML-DSA |

### Replace the ML-DSA key

There is no mechanism that lets the old key vouch for the new one. The
journal's last accepted command is signed by the old ML-DSA key, so a controller
holding only the new key cannot anchor it and exits with
`authority trust state refused`, writing nothing. To proceed you give up the
retained plan:

1. Complete or restore every open rollout first, so that you do not need a
   Restore later. Note the last response.
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

Consequence: Restore of any plan from before this step is no longer available
from the controller. Restore the Deployment by hand from the receipt if you must.

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

The refusal is visible: the controller exits with
`authority trust state refused`, and per-command reconciliation returns the same
refusal if the floor rose under a running process. No Kubernetes object is
written. Fix the enrollment, do not clear the floor.

Clearing the floor is possible and is **break-glass**: deleting `trust.json`
(or emptying the whole journal Secret) makes the controller forget it, and a
stale enrollment is then accepted again. That is the registered residual E-14:
journal write is authority over the floor. Do it only on the owner's recorded
decision, and expect to re-enroll afterwards.

An older controller image reading a journal that holds a strict command refuses
it (unknown fields), so rolling the image back after the first strict command
fails closed. It is not a supported rollback.

## Restore, takeover and restart

- A restart or Pod replacement runs the same start-up check: floor, then
  journal anchor, then commands. A replacement Pod with a new UID takes custody
  through the existing lease epoch first, and its floor write carries that
  epoch, as the admission policy requires.
- After restoring a namespace from backup, the journal Secret comes back with
  its **old** floor. If the enrollment ConfigMap came back too, both are
  consistent and old. If you re-installed a newer enrollment, the floor rises.
  A restore never lowers a floor that is still in the journal, but a restore of
  the journal itself restores its older floor. Re-read `trust.json` after every
  restore and re-enroll to the intended generation before resuming Apply.
- Restoring the Hikyo database changes the recovery incarnation. The enrollment
  pins the incarnation, so after a database restore you re-enroll with the new
  incarnation anyway; take that chance to confirm the generation.

## Gaps and registered residual risk

- **E-14, journal write is floor write.** The floor, the accepted sequence and
  the retained plan live in a Secret that anyone with journal write can alter.
  The strict profile adds no external anchor.
- **Key replacement drops the plan.** An ML-DSA key or enrollment id change
  with a retained plan cannot be anchored in band. The controller refuses
  visibly and you clear `journal.json` yourself. No re-anchoring mechanism was
  built because the register did not decide one.
- **Enrollment id changes are not covered.** The server keeps rollout rows
  keyed by enrollment id and a per-enrollment sequence. Keep the id.
- **Pending commands do not cross a rollover.** A signed command persisted by
  the server before the switch is refused by the new controller and not renewed
  by the new server. Make a fresh decision. The same holds when you edit the
  enrolled target (for example a new source alias) at an unchanged generation:
  the signed binding includes a digest of the target, so commands signed for the
  old target are refused. The retained journal entry still anchors, but the
  existing target check on the stored plan still applies as it did before.
- **Anchor delivery is classical.** See the claim above.
- **Classical history.** Pre-strict commands, receipts and audit rows stay
  forgeable after a quantum break (E-12).
- **Rehearsal and rollout on a real cluster are NOTRUN.** The kind rehearsal
  script still generates only an Ed25519 authority.
