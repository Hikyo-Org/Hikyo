# #1041 Helm network boundaries

PR: [#1043](https://github.com/Hikyo-Org/Hikyo/pull/1043).

## Scope and contract

Continue the original PR branch with signed history. The task branch remains
bound to its original worktree; pushes target the original PR ref explicitly.
No production policy is applied here. Real deployment acceptance belongs to
[#1055](https://github.com/Hikyo-Org/Hikyo/issues/1055).

The canonical owner is `chart/hikyo/templates/networkpolicy.yaml`, with inputs in
`chart/hikyo/values.yaml` and `chart/hikyo/values.schema.json`.

- Defaults preserve the old behavior: one Ingress-only policy allows TCP 8080
  from any source and imposes no egress restriction. Authentication still applies.
- Independent ingress and egress opt-ins replace that same policy's rules.
  There is no second broad chart rule that defeats the restriction.
- Restricted ingress admits listed peers and, by explicit default, this
  release's enabled operator. It never admits TCP 8081. Same-pod loopback
  collection remains separate from pod-IP operational access.
- A namespace and pod selector in one peer are ANDed; peers are ORed. A lone
  pod selector is release-namespace-local. Labels require Kubernetes RBAC
  protection; they do not authenticate a ServiceAccount.
- Restricted egress admits configured DNS, datastore, IdP, adapter, collector
  and extra-rule peers/ports. Empty destination lists emit no rule. Configured
  destinations and extra rules require explicit ports; extra rules also require
  nonempty peers. Named/ranged ports and selector expressions are validated.
- Standard NetworkPolicy does not implement an FQDN allowlist. Operators own
  real provider CIDRs or an enforcing CNI's FQDN policy, all additive policy
  interactions, address translation and both configured IP families.

The illustrative `chart/hikyo/examples/restricted-network.yaml` contains no
universal provider ranges. The guide covers mail, updates/artifact downloads,
Kubernetes/kubelet access, dynamic secrets, remotes and pending cleanup.
The separate operator's outbound Kubernetes API access is outside this server
policy's selector.

The [Kubernetes NetworkPolicy specification guide](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
documents additive rules, selector conjunction and CNI/address-translation
limits. The chart guide applies those rules to Hikyo's integration inventory.

## Validation and evidence

Policy/helper revision recorded by the completed CNI exercise:
`ce99168bbfda9bbe111ec9f265f6286ca23a0f9d`; retained application
binary and signed public fixture source: `8fb29fcfde45ad57ef14e705a7dea4c06e61f859` on main baseline
`24e198b32a3ffd8d4013207b2f974d76ec49be6f`.

| Evidence class | Result |
| --- | --- |
| Source/schema/render | PASS: legacy and independent opt-ins, operator-only and disabled-operator modes, example values, legacy reused values, valid named/ranged/IPv6 peer shapes, invalid and implicit-open rules |
| Chart hardening | PASS: existing exact hardening/RBAC/args/env assertions retained |
| Chart mutation fixtures | PASS on `ce99168bb`: existing and new RBAC/env/args/hardening/network mutations refused; classifier fixtures pass |
| Full-diff fast feedback | PASS on `6f4e5c690`; final handoff revision is checked again before push |
| Enforcing CNI/runtime | PASS: 28/28 HTTP outcomes, seven denials, DNS UDP/TCP, PostgreSQL, restricted reboot, port-forward and policy-only recovery |
| Functional synthetic provider | PASS on SQLite and PostgreSQL: value/sentinel readback, scrub deletion, released custody, credential revocation |
| Published-guide consumer | PASS: docs preparation regression, Astro build, OSS policy and PWA gates on pinned Node 26.12.1 |
| Hosted exact-head CI | PENDING after push |
| Cross-provider review | SKIPPED: Anthropic subscription unavailable; no paid fallback authorized |
| Maintainer security/adversarial review and merge | REQUIRED; merge authorized after CI/review gates |
| Production/deployed acceptance | HOLD under #1055; no production mutation |

### Initial completed isolated runtime identity

- Kubernetes v1.36.1, IPv4 kind fixture, Calico v3.33.0.
- Cluster UID: `d38e972a-51e4-4b70-9689-3765fbf884e2`.
- Server NetworkPolicy UID, unchanged across default/restricted/recovered:
  `c7e8696d-95af-460e-87ac-ce06aabc7b64`.
- Calico node image: `sha256:565d8db3200e443955557f8f28ff44ab8a4a456754bc74a19c4f2f3444af9b1c`.
- Candidate image index: `sha256:9e900e435ebf67b59ae01872026f7e229c385685ae356679d10698bfd67431c4`.
- Application binary: `sha256:492651497faada92157ff2c57ad28ecefde39aaeb5f0ba23a9f0eca8b92fa5ee`.
- Functional fixture image: `sha256:22987c5e5d7559f4e791e5f507de21484f0ab2a062a2028db39798ca783b21ef`.

The source patch recorded at exercise start is empty. Application Go source is
unchanged between the retained binary's revision and the policy/helper revision.
The final legacy-values fix changes fallback rendering only when the old values
map lacks `networkPolicy`; the populated default and restricted specs remain
covered by render assertions. Named/ranged ports and IPv6 are schema/render
proof only. No runtime IPv6, other-CNI or real-provider claim is made.

### Fixture repair evidence

The first cluster run installed Calico and booted PostgreSQL, but the server
refused `public upgrade artifact unavailable` before any network claim could
be made. The worker's restrictive umask had made public fixture directories
0700 and files 0600. The fixture generator now explicitly normalizes only its
public output tree to 0755/0644. Signed-artifact checks remain intact; focused
generation passes with an explicit umask 077. The first failure log, original
public bytes/modes and candidate hash are retained as local evidence.

The failed run's cluster, registry, candidate image and anonymous registry
volume were removed. Registry cleanup now removes its owned anonymous volume;
first-run volume ownership was verified against this task's exact image digest.

### TLS helper startup budget

The second run passed the public-file fix but its TLS init helper exceeded
180 seconds. A no-swap reproduction captured its Go stack in application-wide
JSON Schema package initialization before main, not in certificate staging.
At the same 50m CPU ceiling, a 32Mi run was still in initialization when
interrupted after 103 seconds; 64Mi completed in 78.02 seconds and 128Mi in
9.00 seconds. A swap-enabled 32Mi control completed in 6.96 seconds. These
measurements point to startup memory/cache pressure under the old ceiling. The chart now gives both TLS helpers a 128Mi ceiling, retaining
50m CPU and the existing requests. The rollout deadline and all hardening
controls remain unchanged. Structural and mutation fixtures constrain the
budget, and the enforcing-cluster run validates actual startup. Review namespace
memory quotas and explicitly adopt this ceiling if an older release's reused
values retain 32Mi; retain the qualified budget during policy-only recovery.

### Nonroot fixture image

The third run passed real chart boot, password/TOTP CLI doctor and PostgreSQL
readiness loss/recovery without a restart. Its synthetic targets then failed
to start: the worker's umask also made newly built fixture executables 0700,
while the image copied them as root and ran as UID 65532. A standalone
reproduction returned OCI permission denied (exit 126). The fixture image now
copies both binaries as UID/GID 65532 with mode 0555. Its standalone HTTP
control passes as UID 65532 with a read-only root filesystem. A preflight lists
the functional test from that same restricted image before cluster loading;
target readiness failures retain status and logs before teardown.

The fourth run reached the server from its default unapproved peer but received
HTTP 404: the harness omitted the explicit HTML Accept header required by the
SPA route. The probe and matching port-forward recovery request now send
`Accept: text/html`, consistent with the existing chart UI check. That failed
baseline is retained as a harness failure, not isolation evidence.

The next run passed baseline, restricted allowed traffic and denied network
probes. Its new lifecycle assertion incorrectly expected one owned row, while
the real GitLab adapter owns both the published value and its sentinel. The
test now reads back and later verifies deletion of both, requires two owned
ledger rows, and isolates each engine's prefix so an early failure cannot
contaminate the other engine's provider state.

### Disposable exercise

`scripts/ci/chart-kind.sh` installs checksum-pinned Calico v3.33.0 instead of
kindnet in a newly created kind cluster. The existing chart boot, authenticated
CLI doctor and real PostgreSQL readiness-loss/recovery tests remain intact.
The runner then invokes `scripts/ci/chart-network-policy.sh`.

The exercise retains cluster/CNI/image identity, source head/diff, all standard
and Calico policy inventories, the server policy's UID/spec across default,
restricted and recovered states, and every curl outcome. It establishes live
positive controls before denied probes, tests namespace/pod selector conjunction,
operator/workload access, PodIP and Service paths, DNS UDP/TCP, allowed synthetic
IdP/adapter/remote/collector transport, denied management-sentinel egress,
loopback collection, a real reboot, API-server port-forward and policy-only
Helm recovery.

`TestNetworkPolicySyntheticProviderCleanup` runs in a separate pod selected by
the same chart policy. It uses the real service, encrypted-value publication,
outbox worker, GitLab client and durable journal on SQLite and PostgreSQL.
A stateful synthetic GitLab endpoint permits provider-value readback, then
verifies removal of the value and sentinel through target cleanup, ledger release and subsequent provider
credential revocation. This is functional source/service integration under the
chart policy; it is not an HTTP-authentication or real GitLab acceptance claim.
IdP and remote network probes establish transport, not real OAuth or pairing.

The test uses a new synthetic trust root and credentials, the source-owned
signed chart fixture and isolated storage. It never consumes production
credentials, submits Apple material, activates stable trust or spends cloud
resources. No shipped tests were removed for excluded #158/#1057 scope.

### Legacy values consumer

A reproduction using Helm v4.2.3 and a values map without `networkPolicy`
failed with a nil dereference at the original template entry point. Helm's
`--reuse-values` can retain exactly that old coalesced map. The template now
uses empty-map defaults and retains the legacy Ingress-only policy. The chart
checker models the absent-defaults map and requires its policy to equal the
ordinary default render. For the first opt-in, the guide uses
`--reset-then-reuse-values` with reviewed overrides, merging new defaults with
prior user configuration; operators must inspect other refreshed defaults too.

## Upgrade and recovery

1. Retain the same chart/image and installation custody, the prior policy values
   and independent Kubernetes API access. Confirm port-forward and policy-only
   Helm permissions before restriction; Hikyo login must not be the only access.
2. Inventory actual proxy/workload/operator peers, all selecting policies, DNS,
   datastore, provider endpoints and pending asynchronous cleanup. Replace the
   example labels and namespaces with reviewed installation-specific inputs.
3. Rehearse the default-to-restricted transition, reboot and recovery in an
   enforcing disposable cluster. Apply ingress/egress opt-ins independently if
   appropriate. Preserve every integration destination until cleanup finishes.
4. Recover connectivity with the previous reviewed policy values or explicitly
   set both restriction flags false, using the same chart/image. That restores
   broad connectivity deliberately. A binary/schema rollback is a different
   operation and remains governed by signed upgrade/restore procedures.
5. Production application and acceptance remain HOLD until the #1055 operator
   plan is separately approved. Fixture outcomes do not approve that deployment.

Repeatable commands and retained-evidence fields are in
`docs/operations/network-policy-probes.md`; the installation guide is
`docs/site/src/content/docs/docs/kubernetes-operator.mdx`.

## CodeQL fixture repair and repeated qualification

CodeQL on `5cc5608d673e3486b3ff2aa81f7f5f2400d68656` reported three
`go/reflected-xss` paths ending at MCP/server response writers. The SARIF paths
all start in the new synthetic provider's streaming JSON encoder. The scoped
repair buffers a typed variable through `json.Marshal` before writing the HTTP
response and adds `X-Content-Type-Options: nosniff`. Product response handlers
are unchanged. No alert suppression, dismissal or check weakening is used.

`TestVariableResponsesPreserveValuesAsSafeJSON` exercises POST, PUT and GET with
HTML/script-like values. It requires JSON content type, nosniff, escaped wire
characters and unchanged decoded values. The before check fails for absent
nosniff; all cases pass after the repair. Hosted CodeQL must independently
validate the pushed head before merge.

The complete enforcing exercise was repeated on repair revision `6f4e5c690fdeb306ec98b8f882883df2b4d99941`
with an empty recorded source patch. All 28 final HTTP outcomes, seven denials,
DNS UDP/TCP, PostgreSQL, real password/TOTP CLI doctor, outage/recovery,
restricted reboot, loopback collection, port-forward and policy-only recovery
pass. Functional publication, value/sentinel readback, cleanup and credential
revocation pass on both SQLite and PostgreSQL.

- Cluster UID: `7775a71b-7b7e-4815-8dfd-fa5f923ef1d2`.
- Same default/restricted/recovered policy UID: `e52a0dd7-687f-4474-ad6b-6cf38ba801a6`.
- Fresh candidate image index: `sha256:ed25f26d7fb5a2fc1b3383a5e9243f9ea20cc2da7f4a800a58cb3119806f267b`.
- Fresh application binary: `sha256:42af7e5f902abe7e8a103e431a5c1580df49c4236e647a31d627cd4cbef57793`.

This candidate and its synthetic signed fixture were built from the repair
revision. The earlier candidate identity above remains historical evidence.
The Jev advisory model call is SKIPPED under the task's additional-model and
paid-usage boundary; it is not a review or merge gate waiver.

## Cleanup

The disposable cluster, registry and anonymous volume, candidate/helper images,
diagnostic volume/images and owned processes are removed. Generated fixture
private keys, temporary binaries, downloads and task-installed local dependency
and build outputs are removed after validation. Public fixture bytes, hashes,
policy snapshots and failure/success receipts are retained in ignored local
evidence. The pre-existing shared Docker network, shared caches, worktree and
branch binding are preserved. The offline HTML companion is local-only.

## Remaining boundaries

Maintainer security/adversarial review, current-head hosted feedback/invariants and the
protected queue's full immutable head/base proof remain distinct requirements.
Merge-on-green is authorized after required reviews and protected queue proof.
Release, publication and production application remain outside this assignment.
Real provider endpoints, actual deployment values and cluster-admin recovery
access require installation-specific acceptance under #1055.
