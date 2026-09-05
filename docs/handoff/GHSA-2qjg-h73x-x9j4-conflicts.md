# GHSA-2qjg-h73x-x9j4 conflict resolution

Private delivery branch: `security/integration`, private PR #2.

Merged public main `a7c084200e5b61049114ce5b85d2cbd1a6061d47` into advisory head
`23b000ed820ae309652e7ce4a2dfde419d05f1a9` without rewriting prior commits.
Main contains the browser session-epoch security fix and conflicted with this
advisory in AuthProvider, its tests, and the approval browser fixtures.

The resolution preserves both sets of regression tests and main's cookie,
request, and cross-tab fences. Recovery-code delivery now consumes an exact,
one-shot whoami-verified remint record tied to the initiating transition.
It retires old sensitive operations and state while keeping the initiating
component mounted for its synchronous transfer. Authoritative capabilities
come from the verified whoami response. Unrelated session changes still replace
the QueryClient and component epoch. Peer-message fixtures use the structured
message protocol; foreign-remint rejection retires the originating surface,
so no callback is delivered to that retired surface.

Approval tests retain per-case policy restoration and exact reviewer-grant
cleanup. Grant writes and cleanup use the passkey-backed administrator page.
Browser context cleanup also covers failures while installing cookies.

Sensitivity review: the imported values.ts changes fence passkey results and
reconcile the OIDC popup session before completion. They introduce no mutation
payload caching or retained plaintext. Its reviewed inventory hash is refreshed.

Validation: Node 26.7.0; all 801 web tests pass, web typecheck and production
build pass, and both real Chromium cross-tab tests pass (BroadcastChannel and
storage fallback). Recovery-code regression tests cover delayed whoami with
both unchanged and rotated companion cookies. All four approval browser checks pass on each of desktop and mobile; the real
mobile recovery-code replacement and display-once flow also passes (11 browser
cases total including the two cross-tab cases). Backend source is unchanged
relative to the previously verified private integration candidate.

The advisory remains private and draft. Resolving and pushing this branch does
not merge or release the advisory. Hosted checks are unavailable on GitHub's
temporary private advisory fork.
