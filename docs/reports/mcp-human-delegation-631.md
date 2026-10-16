# MCP human delegation necessity evidence (#631)

Status: **owner accepted no OAuth on 16 October 2026**. Source baseline:
`c4c626a105d0139fcc8a6f5444e585e7a4e0a761`. The owner selected
"A: Retain managed credentials (recommended)", accepting the operator lifecycle,
audit and offboarding limitations, revisit triggers and #651 remaining open.
This records that accepted decision, not client certification or merge proof.

Decision: **retain managed machine MCP; do not introduce OAuth for the
presently evidenced workflows**. A dedicated bounded automation identity is
available where an operator accepts credential custody and offboarding duties.
It is not human delegation. No concrete MCP configuration-read requirement for
human consent, current human authority or automatic employee termination was
found in the reviewed issues. That absence is not proof that nobody needs one.
The owner accepted this bounded disposition after reviewing the evidence.
Delivery status is tracked in the [issue handoff](../handoff/631-mcp-human-delegation.md).

## Scope and current boundary

The live [#631](https://github.com/Hikyo-Org/Hikyo/issues/631) accepts a no-OAuth
decision with revisit triggers. A protocol mismatch, an OAuth button or a
configuration example is not a necessity finding or a successful runtime call.
[#651](https://github.com/Hikyo-Org/Hikyo/issues/651) separately owns exact
candidate image, public HTTPS, deployment and named-client acceptance.

The issue's original five-tool/read-only wording is historical. Current
[registration](../../internal/app/generation.go) and the operative
[MCP ADR amendments](../adr/mcp-server.md) establish:

| Operator settings | Actual catalog |
| --- | --- |
| MCP disabled | No MCP endpoint |
| MCP enabled, write flag off | `hikyo_list_definitions`, `hikyo_list_environments`, `hikyo_inspect_configuration`, `hikyo_list_pending_changes`, `hikyo_list_revisions` |
| Both MCP and write flags on | Those five plus `hikyo_stage_change` and `hikyo_validate_change` |

Write without MCP is a configuration error. Both profiles use this same
registration-time boundary: `/mcp` pins `2026-07-28`; `/mcp/codex` pins
`2025-06-18`. The latter is a stateless lifecycle adapter, not another
authorization mechanism. See [read registry](../../internal/mcpserver/tools.go),
[write registry](../../internal/mcpserver/write_tools.go),
[profile constants](../../internal/mcpserver/codex.go), and
[configuration validation](../../internal/config/config.go).

This decision adds no tool or authority. It preserves the existing
[stage-and-validate decision](../adr/mcp-write.md), including generic staging
for secret-classified keys. It does not authorize exercising that write surface
in this investigation, or adding reveal, dedicated secret entry, approvals,
publish or delivery tools.

## The scoped configuration-read workflow

The concrete workflow is: an operator provisions a dedicated automation account
through `service.Identities.CreateServiceAccount` and `service.Grants.Create`;
a local client uses its managed bearer to list declarations for an explicit
project and inspect published configuration for an explicit environment.
The grant, not the client-supplied IDs, bounds authority. The response can
contain `config` plaintext and secret metadata, never secret plaintext.
The operator can remove the grant or revoke the credential before the next
request. This is an operator-managed read, not a human-consent ceremony.

There is real historical execution evidence, not just an example:

- The [corrected supported-grant record](1.0/mcp-supported-grant-proof.json)
  identifies source `987e0a4f00735e0697b0c841a825aa084669d7ca`, local image
  `sha256:b8bc752cb48adb6d8ed71278ae7dffc988db92c410237edbd4ca24e4a8bd68e6`,
  UTC `2026-09-05T10:16:38.512752+00:00`, two PostgreSQL processes and verified
  private-CA HTTPS. Official Go SDK `v1.7.0` called all five production tools;
  configuration and revision results were nonempty and the secret canary was
  absent. Same-bearer grant removal/restoration and first post-commit revocation
  refusal on both processes were recorded. These historical refusals predate
  the later uniform HTTP 401 amendment and do not prove today's wire status.
- This corrected automation fixture supersedes the original rehearsal's
  unsupported raw-SQL workload project grant. The original Inspector/Claude
  observations remain historical transport evidence, not supported-provisioning
  acceptance. Neither report is a published candidate RepoDigest or public
  deployment proof.
- The later [#781 native-client record](mcp-codex-781.md) records actual Codex
  `0.155.0` and the T3 client library reading a nonempty definition through
  `/mcp/codex` on SQLite and PostgreSQL, with seven tools discovered. It also
  tested validate/stage under that separately authorized task. It is not a
  current-version replay, a configuration-value read, a desktop click-through
  or provider-hosted execution.

No fresh server, Go build, browser or heavy suite was run for this initial
inventory: the owner reserved the 16 GB host for other workers. Current-source
runtime confirmation remains unverified for that precise reason. Historical
proof is retained with its exact scope rather than relabeled as current.

## Exact client and execution matrix

All rows below have **no automatic Hikyo human sign-in**: the current server
admits machine credentials only. A client's generic OAuth facility cannot make
Hikyo an authorization server. `M` below means the machine lifecycle described
in the next section, including no verified human attribution or automatic
employee termination.

| Client, version and protocol profile | Execution and managed bearer | Evidence or precise limitation | Custody and operator work | Attribution / termination |
| --- | --- | --- | --- | --- |
| Go SDK `v1.7.0`, `/mcp`, `2026-07-28` | Local actual production handlers over private-CA HTTPS; bearer passed | Historical corrected fixture: nonempty configuration read and five tools passed; exact source/image above | Local launcher and HTTP client hold token; operator provisions, rotates and revokes | M; actual grant/revocation behavior recorded for that historical source |
| Go SDK `v1.8.0`, `/mcp`, `2026-07-28` | Current `go.mod` pin; transport injects bearer | Current [interop test](../../internal/mcpserver/client_interop_test.go) uses a fake `echo` tool, not proof of a real configuration read. Production manual probe exists; not rerun during resource hold | Local probe process; synthetic fixture needed for fresh proof | M; current-source inference, no fresh lifecycle replay |
| Inspector `2.5.0`, Modern `2026-07-28` | Cached installed package; transient header | Historical actual definitions call passed on original qualified fixture. No corrected-fixture or current-source run claimed | Local Inspector and proxy process see token; transient in-process header avoids command arguments and saved catalog; clear session afterward | M; no human sign-in test |
| Codex `0.155.0`, `/mcp/codex`, `2025-06-18` | Actual local `codex app-server`; environment token reference | Historical #781 both-engine native definition-read pass; source test is [TestMCPCodexNative](../../internal/isolation/mcp_codex_native_test.go) | Isolated Codex home stores variable name only; subprocess receives token; operator maintains credential | M; #781 tested denials/revocation separately from successful unaudited read |
| Codex `0.160.1`, installed on 16 October | Local CLI/app-server; installed help exposes `--bearer-token-env-var` | Exact source pin below retains legacy `2025-06-18` and opt-in modern `2026-07-28`; actual selected/negotiated profile and scoped tool result unverified until replay. Older `0.153.2` transport failure is not evidence about this version | No Hikyo entry in inspected standard Codex config and no inherited Hikyo MCP token/URL. Use an isolated synthetic fixture when capacity is granted; do not modify the user's config | M; generic `mcp login` support is not Hikyo sign-in |
| T3 installed `0.0.46-nightly.20261011.2955`, native Codex `0.160.1` | Local desktop launches Codex; provider-hosted model is distinct from local MCP transport | Installed app metadata/settings inspected. Historical T3 source `97333813f85381313b41eb98fe859ed41c195b74` with Codex `0.155.0` passed through its actual client library; current desktop execution unverified, no browser launched | Provider process must receive the token; an unrelated shell export is insufficient. No Hikyo tool connector exposed in this agent's callable catalog | M; tool output may subsequently reach the model provider, even when HTTP MCP execution is local |
| Claude Code `2.1.261` historical; `2.1.292` installed | Local CLI; documented environment-expanded headers | Historical `2.1.261` legacy initialize was refused by modern `/mcp`; no authorized tool call. Current `2.1.292` protocol and runtime result unverified. No Claude/provider execution authorized in this task; version inspection only | No Hikyo entry found in standard Claude config; header channel documented, not executed | M; refusal is protocol evidence, not OAuth necessity |
| OpenAI Responses remote MCP; local Python SDK `2.37.0` | Provider-hosted remote execution; request-time `authorization` documented | Hosted build/wire profile not disclosed or measured. No authorized API credential in this process, no approved public synthetic endpoint/custody or provider request; installed SDK is not hosted-client proof | OpenAI receives bearer and returned configuration. Application supplies token each request; distinct recipient approval required before sending it | M at Hikyo; an API approval prompt is not Hikyo consent or human identity |

Inventory was read-only. It inspected installed binary versions/help, cached
Inspector package metadata, application version metadata, field-only standard
Codex/Claude/Cursor MCP configuration, T3 settings and SQLite schema, and the
available connector/tool catalog. No matching Hikyo configuration or callable
Hikyo connector was found in that bounded inventory. That is not a claim that
no credential exists elsewhere. No token value, keychain secret, provider
credential, tenant body or private settings dump is included here. No secret
request is needed to prepare this decision.

## Authority, custody and termination

`M`: [machine authentication](../../internal/authz/machine.go) resolves the
credential, service account and current epoch, and checks credential liveness.
The returned actor is the service-account principal. The credential's
`CreatedBy` metadata is not consulted as live human authority. The MCP
[registry](../../internal/mcpserver/registry.go) admits exactly the machine
artifact class; [admission](../../internal/service/mcp_admission.go) and the
page service independently authorize current grants in their transactions.

Successful reads have the locked `audited:none` disposition. Denials and the
optional stage/validate operations have their existing audit dispositions;
write events identify the service-account actor. A human-looking account name
does not supply per-call human attribution or consent. Source therefore does
not establish automatic employee disable/removal, IdP logout/session expiry,
or human membership loss as termination of the machine bearer. An operator
must revoke it, remove its grants or delete its account; bounded expiry and
epoch invalidation also terminate authority. Revocation stops future reads,
not copies already received by a local process or provider.

| Option | What it solves | What it does not solve / burden |
| --- | --- | --- |
| Retain existing managed machine identity | Existing automation and recorded client reads; no new protocol/storage | Reusing a broadly granted account inherits all its grants; shared identity obscures operator attribution. Rotation, recipient custody and employee offboarding remain operational work |
| Dedicated bounded operator-managed automation identity | Narrow project/environment grants and explicit short lifetime; independent revocation and separation from unrelated automation | Credentials do not carry independent scopes: every credential of the account inherits its current grants. Requires operator provisioning, inventory, rotation and offboarding. Still no human consent/current human authority; naming is not attribution |
| Embedded human delegation | Could bind consent, selected scope and current human authority with automatic termination under a reviewed lifecycle | New product and security contract, not a demonstrated need yet. Requires a reviewed ADR, durable multi-replica state, client interoperability and operations. OAuth and package choice are subsequent decisions |

The accepted [developer credential](../adr/developer-credentials.md) solves
non-TTY `hikyo run` delivery, not MCP inspection. It has a distinct artifact
class and is restricted to `delivery.fetch` at one environment by
[authorization](../../internal/authz/authorize.go) and
[artifact admission](../../internal/authz/artifact_admission.go). Reusing it for
MCP would change the locked authority, even without OAuth. The #807 waiver
cannot be applied to #631. The now-closed #613/#617 human-auth work supplies
identity context; it does not grant an upstream IdP login MCP authority.

## Necessity disposition and revisit triggers

No reviewed issue supplies a concrete unmet MCP read workflow beyond the
operator-managed case above. The smallest **candidate** unmet workflow is:
an employee connects a named client, consents to configuration reads in one
environment under their current human access, and the next call fails on
employee removal without an operator finding and revoking a separate bearer.
Machine credentials cannot meet that contract. It is a candidate decision
prompt, not an invented or already-approved business requirement.

Accepted owner choice: no OAuth for the present supported workflows, preserving
current boundaries with these explicit revisit triggers:

1. A named user/customer requires MCP reads with verified human consent,
   current human scope, attribution or centrally coupled offboarding, and the
   dedicated operator-managed option is insufficient.
2. An explicitly supported exact client cannot safely consume a managed bearer
   after its authentication, protocol, endpoint and custody requirements are
   separately evidenced. Mere legacy-version refusal is not this trigger.
3. An approved provider-hosted workflow requires per-user delegation rather
   than a recipient-approved bounded machine credential.
4. An explicitly approved release claim promises automatic human MCP sign-in.

Under that choice, package evaluation, OAuth spikes and implementation tickets
are inapplicable, not unfinished implementation. #651 retains all its own
unverified client/deployment gates. No support claim is expanded by this record.

If a revisit establishes and the owner accepts the candidate human need, do not proceed directly
to a library or OAuth implementation. First prepare the #631 ADR covering all
of its acceptance controls: existing identity/IdP integration; current per-call
authority; exact resource/audience and issuer binding; consent and attenuation;
registration, exact redirects and PKCE; code/refresh lifecycle and races;
theft/replay; revocation and human audit; transactional both-engine and
multi-replica storage; account, membership, grant and IdP-session consequences;
and SSRF/token-passthrough exclusions. Qualify the applicable locked-decision
[amendment review gate](../adr/oss-mechanics.md#governance--honest-bdfl-with-continuity)
before dependent work. Claude subscription review is unavailable; no waiver
or paid substitute is inferred. Only after positive necessity may maintained
embedded Go packages be evaluated, and only after the ADR locks may bounded
implementation tickets be published.

## Primary external evidence inspected

- Installed `codex-cli 0.160.1` help confirms bearer environment references and
  separate OAuth commands. Its upstream `rust-v0.160.1` tag resolves to
  [`d27764b82f7118f674371e6d6e76271d9d606edb`](https://github.com/openai/codex/tree/d27764b82f7118f674371e6d6e76271d9d606edb).
  [Protocol mode source](https://github.com/openai/codex/blob/d27764b82f7118f674371e6d6e76271d9d606edb/codex-rs/rmcp-client/src/protocol_mode.rs)
  distinguishes legacy and modern lifecycles; the
  [client source](https://github.com/openai/codex/blob/d27764b82f7118f674371e6d6e76271d9d606edb/codex-rs/rmcp-client/src/rmcp_client.rs)
  uses a supplied bearer without resolving OAuth credentials. This is pinned
  source evidence, not a negotiated-wire observation.
- [Codex MCP documentation](https://developers.openai.com/codex/mcp/) documents
  environment-loaded bearer configuration separately from OAuth. Read on
  16 October 2026; public docs may change independently of the installed binary.
- [Responses MCP documentation](https://developers.openai.com/api/docs/guides/tools-connectors-mcp)
  documents request-time authorization and tool filtering. It says the
  authorization value is not retained in the Response and must be supplied
  on every request. This does not eliminate provider custody during execution.
- [Claude Code MCP documentation](https://code.claude.com/docs/en/mcp) documents
  header-based authentication and OAuth as separate facilities; neither is
  a successful Hikyo call.
- [MCP 2025-11-25 authorization specification](https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization)
  makes authorization optional and recommends its framework for HTTP when
  supported. It does not prove a Hikyo human workflow or supersede Hikyo's
  pinned transport profiles.

## Acceptance and delivery ledger

| #631 requirement | Present evidence / remaining gate |
| --- | --- |
| Exact clients, real scoped read, custody and lifecycle matrix | Above; current-version execution limitations explicit, historical source/image proof linked |
| Minimum unmet workflow and comparison | Candidate distinguished from demonstrated demand; owner accepted no OAuth with the stated limitations and revisit triggers |
| Human-delegation ADR | Conditional on a positive decision; none claimed locked |
| Go package evaluation and spikes | Not started; positive necessity prerequisite intentionally preserved |
| Bounded implementation tickets | Not published; dependent on locked positive ADR |
| Honest completion | Evidence and decision accepted; issue remains open until documentation delivery passes review, signed/DCO checks and verified main merge |

Independent Sol source review was checked against current resolver, admission,
registry and ADR source by the Astra owner. It is not runtime proof or a
cross-provider review. Resource-constrained inventory did not run tests.
Fresh Codex runtime remains queued separately; its absence is explicit and
does not turn the historical runs into current proof. The complete offline HTML
report and handoff were previewed at widths 390 and 1280: no document-width
overflow, four tables, two sections, zero external assets and no console errors.
No release or production deployment is authorized. The handoff records PR and
merge gates separately from this accepted necessity decision.
