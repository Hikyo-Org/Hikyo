# Service fixture cost and merge queue decision


The merge queue remains required. It checks the exact candidate against its real
base; the separate main push run supplies exact-commit release evidence and
trusted cache publication. The OSS mechanics ADR now records #813's amendment,
including the remaining retargeted-fork limitation. No workflow or ruleset changed.

Local race profiles found repeated SQLite migration execution dominated service
fixture setup. Token and adapter fixtures now copy a schema-applied database
prepared through the actual signed development migration gate. That template has
no runtime admission or encryption hierarchy. Every copy finishes the real boot
gate with a freshly generated root and hierarchy before opening the runtime store.
Only public release evidence is cached; private signing custody is deleted after
template preparation. The cache is confined to the test process.

Copies retain the prepared installation identity, so reuse is explicitly limited
to acknowledgement-token and adapter fixtures. Independent-owner/self-configuration
fixtures, fresh application boot tests, PostgreSQL setup and migration tests keep
their original preparation. Existing foreign-keyring token refusal remains tested.
New tests reject schema/release-root/encryption-root tampering, runtime access
without admission, and admission reuse against another copy; another test proves
writes cannot leak between copies.

The same three token tests took 47.67 seconds before and 18.98 seconds after locally,
including the first template build. Later copies took 0.83 and 1.54 seconds. These
are local measurements under race instrumentation, not a promised CI duration.
The profile artifacts and audit entry point are `.xreview/merge-queue-ci-audit.md`.
Claude review is skipped at the owner's request because the subscription ended;
this delta does not inherit a CLEAN verdict from earlier revisions. PR #843 merged during validation; this follow-up is based on its landed tree.
No automatic merge is authorized for the follow-up.

Validation: the full service suite passed with the dedicated PostgreSQL DSN
(255.041 seconds), all affected token/adapter race tests passed (76.807 seconds),
and the remaining GitLab/VaultKV and adapter sibling race tests passed. The
independent-owner race test passed (54.18 seconds), as did the unchanged fresh-app
boot race test (31.602 seconds). Vet, import formatting, the full lint suite
(133.949 seconds), API freeze, docs verification and git diff checks passed.
