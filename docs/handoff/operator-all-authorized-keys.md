# Operator delivery of all authorized keys

## Contract and implementation

The owner approved automatic delivery of new application settings without a
second per-key list in either the operator resource or the consuming workload.
The declared amendment to [the Kubernetes ADR](../adr/k8s-integration.md#amendment-all-authorized-keys)
reopens [#19](https://github.com/Hikyo-Org/Hikyo/issues/19).

`spec.allKeys: true` is an opt-in alternative to a nonempty `mapping`, restricted
to Opaque targets. Admission and runtime validate the selection; runtime refuses
invalid selection before credential acquisition. The server still controls the
authorized manifest, projection and reveal permissions. The complete manifest
replaces target data, including empty manifests and removal of withdrawn keys.

Exact loader-control names require explicit acknowledgement. In this mode an
acknowledgement can precede publication of the named key. Invalid, duplicate,
wildcard and non-baseline acknowledgements are rejected. Selection and sorted
acknowledgements bind the cursor. Protected-key refusal retains the prior target
and clears the cursor; presence-only secrets refuse partial delivery. Client
validation also rejects invalid or duplicate response key names.

Existing mapping digests and native Secret type behavior remain compatible.
Per-target content stamps and the existing workload opt-in annotation determine
rollouts. No server API, authorization grant, operator-wide credential or RBAC
permission is added.

## Activation order

1. Merge and release the reviewed operator and matching CRD.
2. Install both through the existing verified release process. Do not opt in
   against an older operator or schema.
3. Replace `mapping` with `allKeys: true` on the intended environment's
   `HikyoSecret`. Keep its identity, scope, target and workload consent unchanged.
4. Consume the Secret with `envFrom.secretRef`. Explicit container `env` values
   take precedence. The application owns required-setting validation.
5. Publish a setting and verify the managed Secret, rollout and application
   behavior. Additions and removals must not require resource edits.

For PokéTracker, this removes production delivery lists. It does not provision
the catalog updater credential, configure Forgejo workflow audiences, refresh
the dataset or publish the paired scanner artifact. Preview/staging's separate
CI exporter remains intentionally allowlisted because it writes to runner
environment state.

## Verification

- Regenerated CRD with `scripts/gen-crds.sh`.
- `go test ./internal/operator/... -count=1` passed, including admission,
  manifest validation, selection transitions, prospective consent, withdrawal,
  empty delivery and unchanged-content regression cases.
- `go vet ./internal/operator/...`, Go formatting and `git diff --check` passed.
- Delivery, boundary and repository lint regression packages passed with
  `go test ./internal/delivery ./internal/boundary ./internal/lint -count=1`.
- Documentation status validation, docs check and docs build passed with the
  repository-pinned Node and pnpm versions.
- Chart rendering and exact RBAC, hardening and rollout policy checks passed.
- The real operator scenario on isolated kind Kubernetes v1.36.1 passed: a
  newly published key appeared in a replacement `envFrom` pod; withdrawal
  removed it from another replacement; unchanged delivery retained the pod.
  Admission rejects invalid selectors, read-only full projection refuses
  partial delivery, config-only succeeds, and an unacknowledged protected key
  retains the accepted target without rolling the workload.
- Seven existing operator scenarios also passed: convergence, token-key
  rotation, designation refusal, managed-object conflict, lifecycle, write
  ordering and federation. Native-type and server-image fixture scenarios were
  not included in this focused local run.

The first new-scenario run exposed a fixture ordering error: the consumer was
created after the initial delivery, while the current-response path correctly
does not patch it. Creating the workload before manager startup fixed that
assumption. A second run timed out waiting for the added-key consumer; its exact
predicate mismatch was not captured before cleanup and is not claimed resolved.
Detailed value-free diagnostics were added without weakening assertions. The
subsequent run passed (63.19 seconds), followed by an unchanged-source repeat
(64.97 seconds). Original failure logs were retained as local evidence.

## Independent review

Two independent OpenAI `gpt-6.1-sol/high` reviews checked the spec/security and
standards axes of the `gpt-6-astra/high` implementation. Both found no code
defects. A standards question about the ADR review gate was resolved against
the [original lock record](https://github.com/Hikyo-Org/Hikyo/issues/19#issuecomment-5144310304):
it specifies Codex cross-model review, not cross-provider review. Issue #19 was
reopened and the declared amendment records the change. A second standards
review confirmed this resolution. Cross-provider review was not performed.

Reviewers inspected source and test code; execution evidence comes from the
separately run checks above. Hosted CI and human merge approval remain separate.

No production resources were changed during implementation. A successful local
test does not establish deployment, publication or runtime activation.
