# AWS #158 / #844 acceptance source matrix

## Current evidence and subsequent source reconciliation

R5 completed independent Sol-high SOURCE_SOUND plus Astra owner verification on
its exact50-path inventory `b8c28d1880be2521dc52423eb65889961d131ea753245071f346736e196e756e`.
A later granted provider run passed3 narrow tests and57 full-package tests with
zero failures/skips at63 source inputs, manifest
`1a24f483191de01a122de50481003f2f67021617dbbbf2b4da2be8320f9c6471`.
That includes the controlled same-client cache refresh/refusal test. Original
provider56 and both first failures remain historical evidence. No result proves
real STS/projected-token refresh, effective IAM or database revocation fences.

The source-completeness pass after R5 identified additional surface/integration
work. Its new drafts are outside R5's verdict and outside provider57 execution:
real authenticated API/CLI adapter ceremonies, missing CLI plan/adopt/revoke
cases, AWS-specific development-fake metadata needed for successful browser
adoption, and durable real-provider expired-input/current-credential fences.
Their final source inventory and independent review must be recorded separately.
Do not broaden R5 to these edits. All new source compilation/execution is held.

The existing browser destination editor deliberately locks routing and directs
moves to the CLI ceremony. This is the current product contract, not a missing
editable destination field. The generic fake previously lacked AWS metadata
witnesses and could only prove adoption refusal. A successful AWS adoption flow
must use real module-produced witnesses over a controlled AWS metadata peer;
a nonempty invented witness is not a substitute.


This is the whole-owner source candidate, not final feature acceptance. Live
#158 still has its original acceptance body and v1.1 milestone. Owner comment
[6220607066](https://github.com/Hikyo-Org/Hikyo/issues/158#issuecomment-6220607066)
records conditional 1.0 relevance; it is not a release or cloud-write grant.

The explicit October 16 route B approval covers BOTH the original Anthropic AWS
amendment and the OpenAI #844 concurrency/IAM/replay/custody delta: independent
GPT-6.1 Sol high substantive review plus Astra-high verification. No additional
quota-gated formal run is owed. Historical formal SKIP and provider provenance
remain preserved. Original R1 consent race and fixture deadline findings, R2
promoted-replay regression and their R3 source repairs remain part of review.

| Accepted behavior | Current production/source and acceptance draft | Evidence boundary / remaining work |
|---|---|---|
| Browser, CLI and API configuration | Existing provider registration, AWS fields, CLI adapters and API handlers; authenticated `aws_authenticated_surface_test.go`, CLI/server seam tests and registered `aws-adapters.flows.ts` | Authenticated integration and rendered browser drafts remain uncompiled/unexecuted. Browser uses the actual AWS module with dev-only simulated auth/API; API/CLI fixture uses controlled AWS HTTP. Both-engine full product runs remain mandatory. |
| Four auth modes, opt-in, bounded role duration and protected static input | `awssm/auth.go`, client constructor tests; real role/auth-boundary tests; CLI stdin and browser password-state tests | Provider57 covers the controlled local cache/refusal tests; original provider56 remains separate. Real STS expiry/negative/fresh-client recovery sources are new; four authentic expired sidecars and secure projection are missing. |
| No production secret-value reads | Closed awssm API/operation/source pins; metadata-only module; separate synthetic oracle | Existing provider closure tests passed. Real effective IAM deny/positive reader controls remain unrun. Oracle credentials never enter product writer or process-replay child. |
| Create/update/staging and per-key outcomes | `awssm/module.go`, real SQL-backed lifecycle, partial-conflict case and sibling convergence | Real suite must verify deterministic JSON, UTF-8/newlines, atomic per-key outcomes and failed target state on both engines. |
| Concurrency and consent | Conditional AWSCURRENT promotion; canonical complete-ARN/current-version witness persisted by migration80 and both query sources | Provider race/adoption cases passed at earlier input set. Both-engine/real version-before-Adopt, version-after-Adopt and replacement-after-Adopt sources remain uncompiled. |
| Durable replay and custody | Stable job token, pre-Prepare witness check, exact-job current-marker repair; real promoted-replay and cold-process lifecycle sources | New process fixture exits after accepted PutSecretValue before Finish, then reopens original signed store/root in a second process. Exact job, unfinished intent, unknown closure and one new AWS version must be verified. |
| Destination move and retain/prune | Existing service route-move protocol; new real activation loader and per-key-to-JSON move; retained two-target adapter teardown | Both engines must prove old-name scrub before generation/destination activation, source delivery at new name, no remote delete on retain, and released custody. Role/adoption/source-unset cases retain prune coverage. |
| Pause/resume, source deletion/restore and teardown | Existing provider behavior; real lifecycle; browser persisted pause and retain/prune request choice | Real AWS recovery window, restore and exact-ARN asynchronous final cleanup remain unrun. Browser request acceptance is not remote cleanup. |
| KMS, throttling, expired auth and identity movement | Existing failure classes/retry tests; real KMS-negative actor, STS expiry and configured identity refusal drafts | Provider-local failure classification passed. Controlled throttling is labeled injected, never manufactured by flooding AWS. KMS/IAM effective permissions remain unverified. |
| Least-privilege and cleanup | Separate five writers/source/oracle/cleanup; expiring exact namespace/KMS templates; expanded setup/retirement plan | Root MFA, account plan, non-root setup identity, region, secure injection, resource/spend/destructive grants and final effective policies are still missing. No cloud execution authorized. |
| Resource bound | Shared32 cumulative create reservations, persistent single-use marker, child create refusal | Now31 nominal creates:10 lifecycle,6 role,2 KMS,1 IAM canary,8 witness,4 promoted. Failed/retried calls consume the one remaining allowance. These local guards remain unrun. |

The first provider-only validation was exact and narrow: direct Go1.27.2,
bounded/offline, repaired plan and full awssm package passed with56 top-level
passes and zero failures/skips. The later63-input provider run passed57, including
the new controlled cache test; these are separate recorded source identities. Preserve the first launcher checksum-selection failure and
the subsequent55-pass/one-stale-expectation failure. No result applies to later
source files unless rerun at their pinned candidate.

## Post-R5 source completion and development boundary

The newly drafted `TestAWSAuthenticatedAPIAndCLILifecycle` exercises eight
API/CLI, JSON/per-key and engine combinations through the real TLS router,
cookie/CSRF checks, passkey single-decision ceremonies, CLI browser handoff and
bearer rotation, sealed credential custody, SQL service and durable worker.
It covers configuration, plan/adopt, replacement, queued-job revocation and
fresh recovery, pause/resume, asynchronous moves and retain/prune. The AWS HTTP
peer and software authenticator are controlled. The app-private construction
error mapper is mirrored for this fixture's injected trust roots; this does not
prove production factory wiring or a rendered browser. Disabled workload
opt-in cases use canonical AWS origins and refuse before credential discovery;
an emulator endpoint would instead prove a different origin refusal.

`aws-adapters.flows.ts` drafts rendered desktop/mobile adoption with actual
`awssm.Module` metadata witnesses and journal decisions, then convergence,
reload and consumed-consent refusal. Its new in-memory AWS peer in
`internal/app/adapter_fake_aws_dev.go` simulates authentication and remote AWS:
stable complete ARN, exact-ARN lookup, version/token replay, conditional current
promotion, scheduled deletion and retained namespace state. Only the explicit
development fixture marker seeds unowned synthetic values. It never fabricates
a witness or overwrites an existing name. Namespaces bind origin/region/account;
kind/KMS changes do not create a second namespace for the same AWS name.

The full registration boundary is part of source review: startup/node activation
in `internal/config/managed_dev.go` rejects overrides outside development mode;
`internal/app/generation.go` starts with the real factory and substitutes only
when both Dev and DevAdapterFakeProvider are true, wiring that same choice to
service and worker. `internal/app/owner_runtime.go` checks switches before
prepare and after drain and forbids nondevelopment/HA/stateful switches.
`hasState` includes AWS scheduled-deleted resources. Other providers retain the
existing generic fake; new countercontrols cover their unchanged behavior and
the production factory's refusal to treat a fixture marker as a fallback.
No new production fallback or configuration switch was added. Browser routing
remains CLI-directed; the new assertions prove those fields stay disabled.

`aws_real_credential_fence_test.go` adds genuine expired static-descriptor
failure through a durable job and local database credential revoke after AWS
accepted a staged Put. Expected exact-job supersession is captured explicitly;
generic drain remains strict. The fixture pins the original Owned ledger id,
tenant/target/name, ARN/current version/value, settled failed effect and released
provider fence before fresh-credential recovery. An independent narrow review
found and repaired the initial incorrect generic-drain expectation and weak
custody/version assertion. This does not claim AWS revocation of an issued STS
session. These cases reuse an existing resource and add zero create reservations:
the original31 nominal creates and32 cumulative cap remain unchanged.

No remaining named product surface or real-provider acceptance case is knowingly
left as an undrafted plan after this source pass. This is an implementation
inventory assertion subject to the new full candidate review, not SOURCE_SOUND
or runtime acceptance. All post-R5 drafts need formatting, generated bindings
where applicable, compilation and allocated execution. Secure delivery and
sandbox setup remain external prerequisites as detailed below.

## Explicit unfinished work

1. Review all current source drafts together, resolve independently confirmed
   findings, format/compile them in an allocated lane, and integrate signed
   migrations76-79 before generating the canonical migration80 bindings. Do not
   copy unsigned sibling drafts or hand-edit generated SQL bindings.
2. Run both-engine controlled durable tests and the real sandbox suite. The new
   auth tests cover fresh-client reconstruction after expired inputs, not
   automatic refresh of the same client's cached role session. That additional
   refresh case needs a bounded long-lived session/projection design and remains
   uncovered; it is not silently counted as demonstrated auth recovery.
3. Execute actual authenticated API/CLI and desktop/mobile flows: create,
   inspection, credential replacement, plan/adopt, pause/resume, move,
   retain/prune, error paths and response/audit secrecy. Prebuild OIDC only in
   the allocated Go lane before web tests. Existing shared tests and new seam
   tests cannot substitute for these runs.
4. Produce real provider identity, version, lifecycle, deny and independent
   cleanup evidence, then final candidate review/CI, signed+DCO commits,
   GitHub Verified signatures, PR review and verified main merge. Parent owns
   PR registration/watch; #79 consumes proof without duplicating AWS execution.

Cloudflare, other ADRs, unrelated #844 changes, global quota/provider policy,
production deployment and release are outside this disposition. The same #158
owner retains responsibility through the complete endpoint.

## Credential lifetime and revocation disposition

Accepted scope is live #158 acceptance criterion 2 (bounded role sessions) and
criterion 5 (expired credentials fail loudly without blind overwrite or false
success). The inherited contract remains `deployment-adapter.md` lines 248 and
270 (credential revoke destroys custody; running jobs fail their next provider
call) and `permission-model.md` lines 222-224 (current authorization at each
sensitive step, no authorization cache). This does not declare immediate AWS
revocation of an already issued STS session when its source token expires.

Production lifetime trace, independently checked by the owner:

- `internal/app/adapter_provider.go:64` constructs a fresh AWS client for each
  module lease. `internal/app/adapter_loader.go:121` constructs that lease on
  each Load; lines 165-177 clear plaintext and release it.
- `internal/adapter/outbox.go:15` bounds an attempt to two minutes. Lines
  246-269 load, synchronously Sync and release on each loop, including before
  rate-limit waits. Explicit newly assumed sessions last 900-3600 seconds
  (`awssm/auth.go:146`, `:289`, `:302`). Ambient sessions may already be near
  expiration; the attempt bound alone does not prove their refresh behavior.
- `awssm/client.go:210` Forget drops credential/STS references and closes idle
  transports. It does not promise memory zeroization of immutable SDK strings
  or revoke credentials at AWS. No module cache survives between these leases.
- Each signed request calls Retrieve (`awssm/client.go:284`). Pinned AWS SDK
  v1.47.3 `aws/credential_cache.go:95` returns unexpired cached credentials or
  retrieves again after expiry. Static descriptors do not carry an expiration
  timestamp; actual AWS rejection is essential. STS provider expiry metadata
  drives SDK role refresh. This is source evidence, not measured live refresh.
- `internal/store/adapter_runtime.go:464` checks current authority and exact
  generation/job/lease. `internal/store/repos_adapters.go:683` clears the stored
  credential, retires queued/running jobs and bumps generation atomically;
  both `queries/{sqlite,postgres}/adapter_repository.sql` contain these
  predicates. `awssm/module.go:516` gates each subsequent mutation. Already
  transmitted requests cannot be recalled; existing provider fencing and
  custody/unknown-outcome handling still apply.
- Expired/rejected credentials map to ErrProviderAuth and the worker terminal
  failure path (`awssm/client.go:334`, `:384`; `outbox.go:298`). Neither SDK
  credential caching nor fixture reconstruction replaces Hikyo authorization.

Fresh-client real auth drafts prove only the intended future sequence: genuine
expired inputs receive expiry-specific STS refusal, the product reports auth
failure, and reconstruction with approved fresh inputs resolves the same
identity. They do not prove automatic same-client role-cache refresh, immediate
AWS STS-session revocation, or current-authority fences against a real database.
The existing provider `TestRevocationStopsEverySubsequentAWSRequest` passed at
the recorded provider56 source identity using controlled gates. The both-engine
`TestAdapterAWSSecretsManagerLifecycle/revocation-after-create-stops-plaintext`
uses actual database grant removal; it and credential-retirement integration
coverage still await execution. Same-client long-lived refresh is an explicit
coverage limitation, not an additional accepted requirement or a waiver of the
revocation/expiry requirements above.

## R4 fixture repairs and remaining runtime boundary

Independent whole-owner R4 found three source defects in the cold-process draft:
an undefined sandbox binding, omitted original creation reservations during
custody import, and an incorrect Dispatched expectation for an already-Owned
resource. The owner confirmed and repaired all three. Import now validates the
original bounded reservations, counts historical replacement ARNs against them,
requires exact current identities and forbids any child create. Crash checks
retain the exact original Owned ledger row plus the exact same-job/name
unfinished intent. Production custody transitions were not changed.

The hard-exit scenario concerns the worker child. Parent-supervisor hard death
is not demonstrated cleanup recovery: the exact ARN registry is in memory and
private output, while the persistent single-use marker only prevents namespace
budget reset. Operator reconciliation from retained evidence is still required
before further cleanup or a new approved namespace; do not claim a durable
parent cleanup journal.

The subsequently executed `TestClientRefreshesExpiredCachedCredentials` strengthens the
same-client boundary without waiting for real STS expiry: the actual SDK cache
uses a controlled early-expiry window, and one client must obtain new synthetic
signing credentials or refuse before its next HTTP request when refresh fails.
HTTP is replaced in memory; it cannot establish real STS issuance, projection,
IAM revocation or cloud behavior. This test passed in the later provider57 run and is not part of the earlier56 pass.

## Secure input implementation versus unavailable delivery capability

`internal/isolation/aws_real_fixture_test.go` implements the file intake:
`readAWSProtectedFile` refuses Windows, symlinks/nonregular files, group/world
permissions, changed inode during open and inputs over32KiB. Strict manifest
and descriptor decoding, exact actor/account/region checks, synthetic namespace
and explicit mutation approval run before mutations. `writer` resets ambient
credential variables, isolates shared config, disables IMDS/container sources
and binds each selected source to the separately approved role. Authentic expired
sidecars are shape/provenance preconditions, followed by actual STS expiry checks.
`aws_real_scope_test.go` drafts protected-input and scope failures. Parent
directories must be operator-trusted; none of this creates a secret store.

The available T3 request_secret tool returns an opaque one-use reference, never
plaintext. Its exposed consumers are webhook signature configuration; no callable
local-file projection/export or fixture credential consumer is available. A
repository script cannot resolve that app-owned reference, and inventing a bridge
or requesting a secret first would not solve this boundary. No such request was
made. This is an external delivery capability/authorization gap, not a missing
implementation of the existing protected-file loader.

An operator-projected protected file remains a supported fixture input form, but
its actual approved source, host path/custodian, token renewal, authentic expired
material and delivery session are not established. The proposed GitHub OIDC job
is an unselected setup option, not a deployed workflow or implemented projection.
It would require separately approved runner/environment/issuer/trust scope and
reviewed delivery code. Do not describe that option as ready or infer those
permissions from account signup. Account MFA, plan, non-root identity, region,
role/key resource grants and secure delivery must be resolved through the parent
before any real credential request or test write.
