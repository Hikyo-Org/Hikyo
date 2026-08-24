# Handoff: nightly publication GitHub App

Failed run:
https://github.com/Hikyo-Org/Hikyo/actions/runs/32705690965/job/97366200119.
Implementation base: `cf4bb563b3f98d1cde4856470afe02d408d7761a`.

## Failure and boundary

All six archives built and passed the independent version/commit identity
check. Publication then failed because the built-in `GITHUB_TOKEN` could not
create `v0.0.1-nightly.20260824.3.gcf4bb563` through the protected `v*` tag
ruleset. GitHub also rejects the built-in GitHub Actions integration as a
repository-ruleset bypass actor when that integration is not installed on the
organization.

Nightly publication now uses an organization-owned GitHub App installed only
on `Hikyo`. The app has repository `Contents: read and write`, no webhook, and
no event subscriptions. The workflow requests that one permission in a
short-lived installation token, creates the lightweight tag only after archive
identity passes, and creates the prerelease with `--verify-tag`. The built-in
workflow token is read-only.

## Recovery behavior

A retry accepts an existing nightly tag only when it still resolves to the
exact checked-out main commit. Any different object is fatal. If tag creation
succeeds but release creation fails, rerunning the same workflow run therefore
resumes publication without moving or replacing the immutable tag.

## Adjacent repository-policy repair

`configure-repository.sh` previously tried to broaden repository Actions to
`allowed_actions=all`. That conflicts with Hikyo-Org's selected-actions policy.
Repository configuration now preserves the organization-approved mode while
continuing to assert SHA pinning.
