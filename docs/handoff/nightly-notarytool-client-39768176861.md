# Hosted notarization client failure

## Release state

[Nightly 39768176861](https://github.com/Hikyo-Org/Hikyo/actions/runs/39768176861)
tested merged source `70a34271ea9415946048866914b325321c5626d5`. All 47
validation jobs and artifact construction passed. Apple job 119809416922
imported the identity and signed the executable and HFS+ UDZO disk image, then
`notarytool submit` terminated with `Trace/BPT trap: 5` before returning an ID.
The arm64 receipt is Unknown, with no submission ID or diagnostic log. These
missing diagnostics do not mean zero warnings. Publication and container jobs
were skipped; candidate `0.0.1-nightly.20261022.87.g70a34271` was not released.

The Keychain repair in #1022 remains effective. The HFS+ change in #1023 fixed
the separately observed native image/preflight problem but did not prevent the
hosted crash. No protected credential was changed during this diagnosis.

## Hosted differential reproduction

The credential-free diagnostic branch generated an unregistered P-256 PKCS#8
key on the same hosted runner and compared its installed clients. It had no
checkout, protected environment, action dependency, or repository permissions.
Its bounded runner exception was validated through the existing runner-policy
owner. The diagnostic branch is separate from this production change.

[First probe 39775649406](https://github.com/Hikyo-Org/Hikyo/actions/runs/39775649406),
commit `a8edd3dea5ce16482a2432bb6c548aded7422cb5`, compared key encodings.
Xcode 16.4's notarytool 1.0.0 (38) crashed with exit 133 on canonical PEM,
both with and without its final newline. Xcode 26.3's notarytool 1.1.0 (39)
handled both encodings and returned HTTP 401, as expected for an unregistered
key. Literal newline corruption returned ordinary format errors.

[Second probe 39776004989](https://github.com/Hikyo-Org/Hikyo/actions/runs/39776004989),
commit `272ab75b984ee4b04f0e791cfa79a87031c11628`, used a newly generated valid
UUID, a ten-character dummy key ID, and a harmless HFS+ UDZO image. The key,
arguments, image, and host were held constant between client selections:

| Client | `history` | `submit` |
| --- | --- | --- |
| Xcode 16.4 / notarytool 1.0.0 (38) | SIGTRAP, exit 133 | SIGTRAP, exit 133 |
| Xcode 26.3 / notarytool 1.1.0 (39) | HTTP 401, exit 1 | HTTP 401, exit 1 |

These probes deliberately fail overall when a client crashes. Their failure is
expected evidence, not a failing production validation. The newer client reaches
ordinary authentication rejection on the same input. This isolates a client
compatibility failure before upload; it does not prove valid production-secret
authentication, an upload, or Accepted notarization.

## Production change

Both stable and nightly Apple jobs explicitly select the installed Xcode 26.3
through their job-level `DEVELOPER_DIR`. An initial step reports Xcode and
notarytool versions before the step that receives protected credentials.
Runner, protected environment, permissions, artifact identity, and downstream
signing, notarization, matching-log, stapling, Gatekeeper, and inventory gates
retain their existing requirements.

Existing owner tests for both workflows now require the tested client selection.
They fail against the previous workflows because the explicit selection is
absent. After the repair, local validation passed the focused Apple workflow and
fixture tests, runner policy, action-pin tests, and whitespace checks. Shared
fast-feedback passed Go formatting, owner-package compilation, and Actionlint
in resource-queue job 841.

## Remaining release proof

The user authorized commit, push, PR, merge, and a new nightly. After protected
merge validation, trigger the main-only nightly wrapper once. Verify the selected
client version, actual production authentication and both Accepted receipts with
matching public logs, final stapled DMGs, signed release inventory, publication,
and the container. HTTP 401 from the unregistered diagnostic key establishes
client behavior only; it is not release completion.
