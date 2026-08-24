# Handoff: first nightly before a stable release

Failure: https://github.com/Hikyo-Org/Hikyo/actions/runs/32702294800/job/97356242963.
Implementation base: `9cf0fecb156e7ac60520e77b2d94c790222e9a83`.

## Contract

- With no published stable release, the nightly identity starts at
  `0.0.1-nightly.<date>.<run>.g<sha>`.
- After a stable release exists, nightlies continue on the next minor line. A
  stable `1.0.0` therefore produces `1.1.0-nightly...`.
- Stable discovery walks every GitHub Releases page. Daily prereleases cannot
  push the latest stable beyond a first-page query.
- GitHub API, malformed stable tag, date, run-number, and commit failures remain
  fatal. Only a successful empty stable-release set activates bootstrap.

## Regression evidence

The nightly workflow fixture failed before implementation because the initial
version and empty-release path were absent. The shared formatter now pins exact
initial and next-minor identities while preserving all invalid-input refusals.
The release-page fixture covers empty, nightly-only, multi-page stable,
malformed JSON, wrong-shape, and malformed-tag inputs.
