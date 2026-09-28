# PR #833: documentation conflict resolution

The parent feature PR is already merged. Reapply the remaining documentation onto main at `85c0d49c`; target main instead of the historical feature branch.

Keep the newer PKI trust-anchor, settlement, pagination, and rate-limit contracts. Add missing PKCS#8 decoding and issuer-selection outcomes; correct VerifyCA parameter and CRL-number wording.

Application changes are comments only, verified from the complete diff. Local validation passed: `go test ./internal/pki`, gofmt, and git diff --check. Cross-provider review skipped for this comment-only resolution; runtime and UI behavior are unchanged.

Before merge: publish the signed replacement commit with DCO sign-off, verify GitHub signature status, and require green CI on the exact PR head. Original bot commits lack both signing and DCO, so replacing their history requires a guarded force-with-lease push.
