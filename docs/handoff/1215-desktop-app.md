# #1215: Hikyo desktop app (macOS), handoff

Status: **implementation PRs open, none merged.** The owner authorized building the plan on
2026-11-19 ("go ahead, build everything"). That covers plan PRs 1, 2, 3, 4 and 6. It does not
cover merging, the release lane (plan PR 5), the `Hikyo-Org/hikyo-app` repository, a signing
identity, release or deployment. Decisions come from the [ADR](../adr/desktop-app.md) (D1 to
D15) and the [plan](../superpowers/plans/2026-11-04-desktop-app.md). Wire contract:
[desktop shell protocol](../spec/desktop-shell-protocol.md). Refs #1215; no PR closes it.

## Pull requests

| PR | Plan | Branch | Base | State |
|---|---|---|---|---|
| [#1462](https://github.com/Hikyo-Org/Hikyo/pull/1462) | 4 (CI definitions) | `hotfix/1215-desktop-ci-definitions` | `main` | green; touches trusted `scripts/ci/source-proof/inventory.go` |
| [#1454](https://github.com/Hikyo-Org/Hikyo/pull/1454) | 2 (client packages) | `dunky13/desktop-login-trust-packages` | `dev` | green |
| [#1458](https://github.com/Hikyo-Org/Hikyo/pull/1458) | 1 (server and SPA app mode) | `dunky13/desktop-app-mode-1` | `dev` | green before this handoff commit |
| [#1459](https://github.com/Hikyo-Org/Hikyo/pull/1459) | 3 (shell) | `dunky13/desktop-shell` | #1454 | `feedback (go)` red until #1462 is on `dev` |
| [#1461](https://github.com/Hikyo-Org/Hikyo/pull/1461) | 4 (macOS CI) | `dunky13/desktop-ci` | #1459 | same as #1459 |
| [#1460](https://github.com/Hikyo-Org/Hikyo/pull/1460) | 6 (public docs), draft | `dunky13/desktop-docs` | #1459 | docs verify passes locally |

Merge order: #1462 to `main`, then the `main` to `dev` backmerge (the root Go lane runs
`go test ./desktop/...` and needs the nested-module routing from #1462), then #1454, #1458,
#1459, #1461, and #1460 last (mark it ready first). Stacked bases are updated with merge
commits, never force-pushed.

After the backmerge, #1461 still owes two follow-ups: re-add the strict `desktop/`
golangci pass, and re-check the desktop-compile and docs-impact plan expectations.

## Amendment banners and tickets

| ADR banner | Operative when | Ticket |
|---|---|---|
| api-cli-surface.md | #1458 merges; first-contact clause waits for #1459 | #25 stays open |
| system-architecture.md | #1459 merges | close #22 |
| human-auth.md | #1459 merges | close #16 |
| threat-model.md | #1459 merges | close #8 |
| oss-mechanics.md | plan PR 5 merges (not built) | #33 stays open |

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
- Screenshots: captured locally, not uploaded. Hosting them on the public PRs waits for the owner.

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
7. `key_ids` is capped at 500 per reauth (the handoff's limit); larger units are refused by the
   SPA with "Continue in browser".

## Remaining work

1. Owner: review and merge #1462 (includes the paid `macos-15` runner), then the backmerge.
2. Owner: merge decisions for #1454, #1458, #1459, #1461, #1460.
3. Owner: create `Hikyo-Org/hikyo-app` and the Apple Developer ID signing and notarization
   identity, then build plan PR 5 (release lane) and its "sole exception" policy sentence.
4. Hand acceptance on a signed, notarized build: first launch from Downloads, move to
   Applications, relaunch, connections still open; plus the plan's acceptance list.
