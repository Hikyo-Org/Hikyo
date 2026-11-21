# #1215: Hikyo desktop app (macOS), handoff

Status: **plan PRs 1, 2, 3, 4 and 6 merged to `dev`; plan PR 5 (release lane) is implemented and
awaits review.** The owner authorized building the plan on 2026-11-19 ("go ahead, build
everything") and merging the stack in order on 2026-11-20. Release and deployment are not
authorized: nothing in the release lane has run against GitHub or Apple. On 2026-11-20 the owner also chose to release the app from this repository as
`desktop-v<version>` releases instead of a separate `Hikyo-Org/hikyo-app` repository. Decisions come from
the [ADR](../adr/desktop-app.md) (D1 to D15) and the
[plan](../superpowers/plans/2026-11-04-desktop-app.md). Wire contract:
[desktop shell protocol](../spec/desktop-shell-protocol.md). Refs #1215; no PR closes it.

## Pull requests

| PR | Plan | Base | Merge commit |
|---|---|---|---|
| [#1462](https://github.com/Hikyo-Org/Hikyo/pull/1462) | 4 (CI definitions, `hotfix/`) | `main` | `7b8d95dad` |
| [#1454](https://github.com/Hikyo-Org/Hikyo/pull/1454) | 2 (client packages) | `dev` | `1c09434d1` |
| [#1458](https://github.com/Hikyo-Org/Hikyo/pull/1458) | 1 (server and SPA app mode) | `dev` | `5ec2fe30d` |
| [#1466](https://github.com/Hikyo-Org/Hikyo/pull/1466) | `main` to `dev` backmerge | `dev` | `bf4b856d6` |
| [#1459](https://github.com/Hikyo-Org/Hikyo/pull/1459) | 3 (shell) | `dev` | `d6452c260` |
| [#1461](https://github.com/Hikyo-Org/Hikyo/pull/1461) | 4 (macOS CI, strict lint on `desktop/`) | `dev` | `51ecb6e6d` |
| [#1460](https://github.com/Hikyo-Org/Hikyo/pull/1460) | 6 (public docs) | `dev` | this PR |

Late fixes folded into the stack from review: each connection's lifecycle has one owner and
moves forward only (removal records a pending wipe first and waits for Wails to drop the window);
a finished WebKit download is published off the main thread, and quitting waits for a download
being saved (`OnShutdown`); the data directory lock is held for the whole run; remote cards offer
"Continue in browser" in the app.

## Amendment banners and tickets

| ADR banner | Operative | Ticket |
|---|---|---|
| api-cli-surface.md | yes (#1458, #1459) | #25 stays open for its other reasons |
| system-architecture.md | yes (#1459) | #22 closed 2026-11-20 |
| human-auth.md | yes (#1459) | #16 closed 2026-11-20 |
| threat-model.md | yes (#1459) | #8 closed 2026-11-20 |
| oss-mechanics.md | when plan PR 5 merges (banner is owner-locked and unchanged here) | #33 closes when plan PR 5 merges |

## Evidence

- Local: Go race tests for `desktop/...`, `probe/run.sh`, the driven WKWebView lane
  (`desktop/probes/driven`), vitest and the sensitive-inventory pin, golangci-lint on new code,
  docs-site verify.
- End to end on a packaged `.app`: add with pin, capability refusal, loopback sign-in, reveal,
  TOTP step-up, save-panel download, open-in-browser refusals, removal revoking the session.
- Reviews: spec and security reviews, three fix rounds on the shell. The final reviewer found no
  path where the bearer reaches an origin other than the keychain item's own binding, and no way
  for an item from a different signer, a different build or an any-app access list to be accepted.
  The reviewer did not review the new keychain tests. Cross-provider review was not run and is
  not CLEAN.
- Keychain access check (round 3): items match this app by its code-signing designated
  requirement, not its path. Verified locally with copies of the test binary at another path and
  with a Developer ID signed build (opt-in tests `HIKYO_DESKTOP_KEYCHAIN_TEST=1` and
  `HIKYO_DESKTOP_SIGN_IDENTITY`). CI runs neither. A packaged `.app` was not run from two
  locations.
- Screenshots: captured locally. The owner chose to upload them to #1458, #1459 and #1460; the
  upload waits on a browser signed in to GitHub.
- Not exercised by hand: a second app instance against the data directory lock, and quitting
  during a slow (for example exFAT) download copy.

## Residual risks

1. **Keychain access check and moved apps.** Round 3 confirmed that the path-based check refused
   a moved app's items and replaced it with a code-requirement match. The requirement lookup uses
   `SecTrustedApplicationCopyRequirement`, declared only in a private header and weak-imported, so
   a macOS without it refuses every item rather than failing to launch. An ad-hoc dev build matches
   only its identical binary, so every rebuild asks for sign-in again. Verify on a signed,
   notarized build before any release.
2. Sign-in over a refused item replaces the keychain item but does not revoke the old session; it
   stays live on the server until it expires or is revoked there.
3. Sign-in after a keychain read failure that is not a trust refusal goes to the stored record's
   origin, after a confirmation that names it.
4. WebKit counts a script-dispatched click as a user gesture (no public API to tell them apart);
   downloads use the private `_downloadAttribute`.
5. A crash between the keychain write and the connection record write can leave an orphan
   keychain item. Cleanup of such an item was not tested.
6. The download save panel has a timeout; behaviour at the timeout was not exercised by hand.
7. `key_ids` is capped at 500 per reauth (the owner kept this limit on 2026-11-20); larger units
   are refused by the SPA with "Continue in browser".
8. [#1469](https://github.com/Hikyo-Org/Hikyo/issues/1469): three review findings on `main`'s
   same-name check refusal (PR gate). Not desktop code, but its major finding is a PR gate gap and
   needs a `hotfix/` to `main`.

## Release lane (plan PR 5)

Owner decisions of 2026-11-21: the desktop jobs run inside the stable tag's `release.yml` push run,
so a desktop failure fails the run and blocks publication of the CLI release until the failed jobs
are rerun. Hand acceptance uses the staged `desktop-v<version>` draft from the first stable tag
before it is published; there is no extra preview or dispatch signing path.

What exists:

- `release.yml`: `build-artifacts` exports the channel from `release-channel.sh`. For stable
  versions only (never 0.x, `-rc` or nightly), `desktop-app` (macOS 15, matrix arm64 and x86_64,
  environment `apple-signing`) builds, signs (hardened runtime), notarizes and staples the app,
  builds and signs the DMG, notarizes and staples it, then verifies it. `desktop-draft` signs
  `desktop-inventory.json` with the stable workflow's keyless identity and stages the
  `desktop-v<version>` draft. `build-signed-draft` depends on neither. GoReleaser's tag is pinned
  with `GORELEASER_CURRENT_TAG`; nightly and CI use `--snapshot`, which needs no pin.
- `scripts/release/`: `build-desktop-app.sh`, `stage-desktop-draft.sh`, `publish-desktop-draft.sh`
  and `check-desktop-rulesets.sh`. `verify-desktop` in `scripts/release/stable` checks the bundle
  against the pinned trust root and exact identity, then the closed file set and each DMG's size
  and SHA-256. `stable-ceremony.sh review` and `publish` run the desktop check
  (`publish-desktop-draft.sh --check-only`) first, and `publish` runs the publisher after
  `publish-stable-draft.sh` for stable versions. The legacy `--offline` path prints
  the follow-up command instead. Artifacts are named by run attempt, and each image's digest is
  a job output that `stage-desktop-draft.sh` re-checks.
- `latest-stable-version.sh` and `security-release.yml` skip `desktop-v*`.
- `release/repository/desktop-tag-creation.json` and `desktop-tag-immutability.json`, upserted and
  read back by `configure-repository.sh`. **They are not applied.** The owner runs
  `scripts/release/configure-repository.sh Hikyo-Org/Hikyo` once before the first stable tag; the
  publisher refuses to run without both active.
- `desktop/build/darwin/entitlements.plist`: an empty dict (hardened runtime, not sandboxed).

Never exercised, because they need a real stable tag, Developer ID secrets, Apple and GitHub: the
macOS jobs end to end (the x86_64 build is cross-compiled on an arm64 runner and never launched),
Apple notarization of an app and a DMG, `spctl` and `stapler` on those bytes, keyless signing of the
inventory, creating the draft through the releases API without a tag, publishing a draft that creates the
`desktop-v<version>` tag (and the creation ruleset admitting the publisher), the post-publication
latest and tag checks, and the rerun path over an earlier draft. Exercised locally: the shell
fixtures with fake `gh`, `go` and `cosign`, and the Go verifier with real fixture signatures.

Hand acceptance, after the first stable tag run: download the staged draft's DMG, verify it as the
[desktop app page](../site/src/content/docs/docs/desktop-app.mdx) describes, open it as a
quarantined download, and run the plan's acceptance list. Publish with
`scripts/release/ceremony.sh publish` only after that.

## Remaining work

1. Owner: run `configure-repository.sh` for the desktop rulesets before the first stable tag. Decide
   whether one `apple-signing` approval per matrix job is acceptable.
2. Review plan PR 5. Close #33 when it merges.
3. Hand acceptance on the staged `desktop-v<version>` draft: first launch from Downloads, move to
   Applications, relaunch, connections still open; plus the plan's acceptance list and the two
   unexercised cases under Evidence.
4. Fix [#1469](https://github.com/Hikyo-Org/Hikyo/issues/1469) through a `hotfix/` to `main`.
5. Upload the screenshot sets to #1458, #1459 and #1460.
