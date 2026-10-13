# Isolated OpenAEV assertions

This layer builds on the reusable core in PR #905 and adds the complete
existing authorization, adversarial, bounded-request, resource-guard and
native audit/alert/inbox detection suite. It has no automatic lab provisioning
or release publisher. PR #903 supplies the fresh hosted lab and coordinator.

Install `requirements-check.lock` into a Python 3.11+ virtual environment,
then run `scripts/openaev/check.sh` with that environment on PATH.
The check performs Ruff, mypy and all 269 combined core/suite unit tests.
These unit tests are distinct from the 313 assertions against a live release.

The core preserves sanitized reporting diagnostics, requires native node identity,
and collects the correct idle or active pod boundary. See
[the core handoff](../../docs/handoff/core.md) for its validation evidence.

The isolated lifecycle accepts the same authenticated release identity grammar as
the release helper, including stable, RC, early core prerelease, and nightly
versions. It retains the exact version, full source SHA, and pinned image digest.

Review regressions reject nested fixture key names in denial/current responses,
secret metadata in conditional responses, ordinary background collector crashes, truncated native HTTP responses,
and incomplete administrator resume state. Native administrator exec has a
30-second bound and sanitized failures. No original assertion was removed.
