# #725: native Kubernetes Secret types

The operator supports `spec.target.type`, defaulting to `Opaque`, with the
closed allowlist requested by [#725](https://github.com/Hikyo-Org/Hikyo/issues/725).
The CRD rejects other types and reports supported values. Required destination
mappings are checked before plaintext fetch. Required delivered keys and
Kubernetes content constraints are checked before writes; status/events never
include the rejected payload. Basic auth intentionally requires both username
and password, as specified by the ticket.

Existing Secret type changes report `Conflict=True/TargetTypeImmutable` and
never delete or recreate a target. Type participates in cursor binding,
eligibility and uncached post-write verification. Ownership remains the
controller UID; labels never authorize adoption or deletion.

Typed Secrets cannot be emptied during authorization withdrawal. An
authenticated 404, or an authorized manifest missing a mandatory mapped key,
conditionally deletes the owned typed target with UID/resourceVersion
preconditions and verifies absence uncached. Opaque withdrawal remains empty
delivery. A failed deletion clears the cursor and reports failure. Successful
withdrawal clears object identity, stamps opted-in workloads with empty
content, and keeps the target absent until a complete authorized delivery.
`Orphan` does not override withdrawal. A malformed mapping or invalid payload
retains the prior target, matching existing content-refusal semantics. An
accidentally published unset still withdraws: intent cannot be inferred, and
continuing to serve an absent credential would violate the authorized manifest.
Malformed present data rejects a replacement instead. TLS bytes are not parsed
by the operator, so certificate recovery behavior belongs to the consumer.

The chart's `operator.nativeSecretTypes` defaults false. Explicit opt-in enables
native targets and Secret `delete` within the existing namespace authority;
Opaque-only installs omit delete. `HIKYO_OPERATOR_NATIVE_SECRET_TYPES` enforces
the same boundary before credential acquisition or fetch. Disabling support
retains existing typed targets for explicit migration and clears cursors.
Secret list/watch remain absent. The chart structural and mutation checks
track this exact verb set. The ADR amendment and Kubernetes guide document
withdrawal, migration, native consumers, and the unchanged in-memory residual.

Verification entry points:

- `go test ./internal/operator ./internal/operator/api/v1alpha1`
- `scripts/gen-crds.sh` followed by generated-file drift verification
- `scripts/ci/check-chart.sh` and `scripts/ci/check-chart_test.sh`

Tests cover default/native delivery, content refusal without leaks, missing
mapping refusal before fetch, immutable type conflict, mandatory-key removal,
404 withdrawal, deletion preconditions, replacement races, and type verification.
Admission tests exercise the generated CRD's default and JSON Schema allowlist.
`TestK8sOperator/native_secret_types` (build tag `k8se2e`) exercises the real
operator, server and Kubernetes API with generated test credentials. The CI
runner provisions a disposable authenticated registry: a pod without credentials
must fail to pull, and a pod referencing the delivered dockerconfigjson Secret
must run. The same test creates an Ingress referencing the delivered TLS Secret,
performs a hostname-verified HTTPS handshake using its persisted bytes, refuses
an existing target's type change without recreation, then revokes the workload's
read grant and checks that both native Secrets are deleted. It does not install
or claim compatibility with a particular third-party Ingress controller.

Live acceptance limitation (2026-09-12): two disposable local kind clusters
failed during control-plane bootstrap, before the registry fixture or native
consumer tests ran. The second failure was kubeadm's attempt to create the
admin ClusterRoleBinding receiving `connect: connection refused` from the new
API server. Its diagnostic log is `/tmp/hikyo-native-5016-retry.log` on the
implementation workstation. Docker recorded three OOM events on the existing
`hikyo-rollout-818e-control-plane` during these attempts, supporting shared-host
memory contention; these events do not prove the new node's failure cause.
The Docker VM had approximately 2.35 GiB total memory. Both disposable clusters
were removed; no third attempt was made because capacity was unsuitable.
The reusable native acceptance test is wired into CI, but no successful local
live native-consumer result is claimed.

CI acceptance (2026-09-12): [validation / k8s-e2e run 34707669054](https://github.com/Hikyo-Org/Hikyo/actions/runs/34707669054/job/103590591973)
passed on the initial PR #731 head, including the native consumer fixture. This
supersedes the acceptance evidence gap from local bootstrap failures above;
the capability-gate review fix still requires a fresh CI run.
