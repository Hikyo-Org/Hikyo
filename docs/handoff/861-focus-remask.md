# Issue 861: remask secrets on focus loss

Source: https://github.com/Hikyo-Org/Hikyo/issues/861

Implemented on `dunky13/issue-861`. Initially delivered as a local signed commit; the later CI repair request authorizes updating PR #866 and verifying its checks.

## Behavior

The matrix cell editor and Values page clear revealed plaintext and reveal announcements on window blur and document visibilitychange. A shared focus-generation hook rejects single and bulk reveal responses begun before focus loss, including responses arriving after focus returns. Returning to the page requires a fresh reveal.

Listeners are removed on unmount. Existing component-owned sensitive state, uncached disclosure operations, ceremony checks, clipboard operations, publishing and session retirement remain intact. No screenshot detection was added. Sensitivity inventory pins were refreshed only for the reviewed MatrixRowEditor and Values sources.

Browser verification also found a stale visible countdown after an idle editor. Both surfaces now synchronize their countdown clock with a successful reveal, preserving the ten-second deadline.

## Validation

- Web typechecking and lint: passed.
- Focus and sensitivity-inventory regression tests: 13 passed, including both events, both surfaces, listener cleanup, pending single/bulk responses, return-to-focus and the visible ten-second countdown after 45 seconds of idle time.
- Full web unit suite: 1,522 tests passed across 168 files.
- Standards review: no remaining findings after extracting shared event handling and strengthening the visible-countdown assertion.
- Spec review: no remaining findings.
- Isolated Chromium component check with synthetic API responses: Values reveal then blur cleared plaintext; matrix reveal then blur and visibilitychange cleared plaintext. This is component/browser proof, not backend end-to-end verification. Temporary fixture files and server were removed after verification.
- Graphify structural refresh: run after code changes; generated output stays untracked.

Initial full-suite failures were the required source-hash inventory check while the implementation and pins were still changing. No checks were weakened.

## Delivery

PR: https://github.com/Hikyo-Org/Hikyo/pull/866

## CI repair, 9 October 2026

Run 37908112958 failed `validation / web-go` in the release-shaped binary vulnerability scan. The embedded app was built with Go 1.27.0 and x/net v0.58.0. The scan reported twelve affected vulnerabilities with fixes in Go 1.27.2 and x/net v0.60.0. All eight desktop/mobile web shards passed on the original head.

`go.mod` now requires Go 1.27.2 and x/net v0.60.0. Go selected their required compatible crypto, sys, term, text, mod and sync versions; `go mod tidy` updated the checksums. CI and release workflows already derive their Go version from `go.mod`, so the repaired binary uses the patched standard library. No scan or CI gate was weakened.

Local repair evidence:

- `go mod verify`: all modules verified.
- SPA rebuild: passed.
- `go build -tags ui` with Go 1.27.2: passed.
- Pinned `govulncheck -mode=binary` on the freshly built UI binary: zero affected vulnerabilities. One advisory remains in required modules whose affected code is not called; this is the scanner's informational output and does not fail the gate.
- `go test -count=1 -tags ui ./api/... ./internal/server/... ./internal/webui/...`: passed on macOS.
- Standards and spec reviews of the repair: no findings.

New-head Linux CI remains the authoritative remote check. Merge, release and deployment are outside the repair authorization.


## Inline review follow-up, 9 October 2026

The notified CodeRabbit comment was a review-in-progress notice; the GitHub Actions comment offered an optional benchmark, which was not requested. Inspecting inline reviews found three concrete issues, now addressed:

- A popup reauthentication ceremony itself blurs the main window. Capture the focus generation after authorization and restored focus, immediately before sending the reveal. Authorization may precede popup closure, so an abort-aware wait holds the continuation until the document is visible and focused. Ceremony replacement, scope changes and unmount cancel the wait and remove its listeners.
- Pin the shared focus lifetime helper in the sensitivity inventory. Its definition now triggers the conservative source-review matcher, and its entire source hash is reviewed.
- A discarded reveal result now explains that focus was lost and asks for another reveal. Values records disclosure names in its local audit panel even when plaintext is discarded; it retains no discarded plaintext. Matrix reports the audited disclosure and remasking notice.

Validation:

- New regression tests fail before the repair and pass afterward. They cover popup authorization before focus restoration, single/bulk reveal continuation, waiting without dispatch while unfocused, unmount cancellation, listener cleanup and discarded-result feedback/audit metadata.
- Focused affected route tests: 35 passed.
- Full web unit suite: 1,533 tests passed across 168 files.
- Web typechecking and lint: passed.
- Standards and spec reviews: no remaining findings.
- Isolated Chromium component checks with synthetic API/popup responses and controlled focus state: both Values and Matrix waited while unfocused, revealed after focus restoration, and remasked on subsequent blur. This does not claim real identity-provider end-to-end verification. Temporary fixture files and server were removed.
- Graphify AST refresh completed without clustering; generated graph remains untracked.

Remote CI for the new head remains pending.
