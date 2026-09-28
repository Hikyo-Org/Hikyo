# PR #834: documentation conflict resolution

The parent feature PR is already merged. Reapply the remaining documentation onto main at `85c0d49c`; target main instead of the historical feature branch.

Keep the newer temporary-access eligibility, expiry, scheduler, and ceremony behavior. Retain missing repository, API, metrics, and web helper comments.

Application changes are comments only, verified from the complete diff. Full-source sensitivity review confirmed that temporaryAccess.ts still sends policy/request metadata only, and Ceremony.tsx keeps TOTP in useSensitiveState, clears submitted codes, binds passkeys to purpose/environment/keys, and refuses workspace emergency access. Refreshed only those two sensitivity-inventory hashes after the comment edits. Local validation passed: `go test ./internal/store/... ./internal/service -run "Access|Policy"`, gofmt, git diff --check, web typecheck, lint, and all 1,224 web unit tests (144 files). Cross-provider review skipped for this comment-only resolution; runtime and UI behavior are unchanged.

Before merge: publish the signed replacement commit with DCO sign-off, verify GitHub signature status, and require green CI on the exact PR head. Original bot commits lack both signing and DCO, so replacing their history requires a guarded force-with-lease push.
