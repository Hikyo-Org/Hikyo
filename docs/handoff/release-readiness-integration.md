# Release-readiness integration

This integration preserves the signed histories of eight reviewed source PRs on
main at `6bbb77968fad2c01eadd2cdc2210988ef94e97da`, after #1082 merged.
It excludes #969 and introduces no release, deployment, notification or live-provider
authorization. Keep the original PRs open until the protected integration merge.

| Source PR | Frozen head |
| --- | --- |
| [#1074](https://github.com/Hikyo-Org/Hikyo/pull/1074) | `ec7a2cb4d99e1aedd2bec8bb8c6693991d58df8a` |
| [#1076](https://github.com/Hikyo-Org/Hikyo/pull/1076) | `8e6611650012efe76f812e6c1755ef29b06ef5ee` |
| [#1078](https://github.com/Hikyo-Org/Hikyo/pull/1078) | `e2796392ab9f83097484d8e31be66a8f1d673c4f` |
| [#1079](https://github.com/Hikyo-Org/Hikyo/pull/1079) | `451bbf9f553052fa09dc0a1bad559e078035c22e` |
| [#1083](https://github.com/Hikyo-Org/Hikyo/pull/1083) | `e431db23a5d8257e679c394497915705015bdd2f` |
| [#1084](https://github.com/Hikyo-Org/Hikyo/pull/1084) | `f5faf05fcb61e69327db3bf492c5a0c9ca0d40cc` |
| [#1085](https://github.com/Hikyo-Org/Hikyo/pull/1085) | `c98163f224e71e482147efee5d26a3ac32f69b0c` |
| [#1086](https://github.com/Hikyo-Org/Hikyo/pull/1086) | `5d3598fe6c1d05399198c09f0cba2957adcc5aa8` |

## Integration behavior

The only manual conflict resolution is in `internal/server/browser.go`.
CSRF verification retains the refusal artifact-probe context from #1074 and
the explicit pre-auth overload response from #1084. Both admission and audit
imports remain. Nested credential reads share the same request receipt.
Every source head is an ancestor of integration runtime revision
`5e4d6b773b245c7b04811de5696c2eb79220c300`.

## Validation and limits

- Standards and Spec integration reviews found no actionable issue on that
  runtime revision against the stated base. Cross-provider review is SKIPPED
  because the approved subscription is unavailable. The inherited no-new-paid-usage
  boundaries also keep an additional Jev advisory NOTRUN.
- Complete-diff fast feedback passed: Go compilation and formatting, workflow
  and shell checks, read-only docs checks, 17 Python receiver tests, web typecheck,
  lint, and all 217 web unit files / 2,035 tests.
- Audit, service, server, authz, qualification and artifact-harness owner suites
  passed on the combined runtime revision.
- The combined refusal, pre-authentication, containment and inventory regressions
  passed on SQLite and an exclusively owned disposable PostgreSQL 18 fixture.
  The isolation batch completed in 62.278 seconds; no datastore leg was skipped.
- The complete alert-route suite passed with `HIKYO_ALERT_COLLECTOR_SCRIPT`
  naming this tree's receiver, so producer compatibility ran rather than skipped.
  CI coverage-conservation consumer regressions also passed.
- Source checks and local tests are separate from fresh hosted feedback,
  repository invariants and full current-base merge-group proof. The protected
  queue remains mandatory, with no timeout, threshold or check reduction.

## Visual evidence

The storage banner source and component regression are byte-identical to
#1086 at its frozen head. The original matched desktop/mobile captures use
the same synthetic prototype state; the before image reconstructs the prior
diagnostic message. Existing #1085 images show the terminal revocation consumer.
The original #1086 screenshot-upload exception was expressly scoped to that PR;
the integration must obtain its own evidence disposition before merge.
Do not commit local screenshots or HTML companions.

## Completion contract

Merge this integration through current-base full validation. Confirm every
frozen source head is an ancestor of the merged main commit, then reconcile
the original PR states. Never close originals before that proof. Preserve
each source handoff's HOLD/NOTRUN boundaries: full #1039 rehearsal, actual
collector and external alert delivery, final-candidate GitHub provider acceptance
and integrated 1.0 release qualification remain distinct work.

Successful credential reset terminally revokes existing developer custody.
The refusal-window migration also creates durable evidence. A source rollback
does not restore revoked credentials and must not discard retained evidence.
