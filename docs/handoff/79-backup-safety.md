# Backup safety findings during 1.0 acceptance

Base: 9827af64962a36e2e31906b48906fd59c7a36c1c. Worktree /tmp/hikyo-drill-cleanup. Parent owns signed delivery and final-candidate replay.

`restore drill --cleanup` unconditionally removed SQLite target and sidecars even when decryption, manifest parsing or existing-target preflight failed. A real encrypted-fixture regression reproduced all three destructive paths before the fix. Cleanup now requires no drill error and `DrillReport.OK()`, preserving failed targets including RTO failures. Existing successful CLI drills on both engines passed after the fix; SQLite cleanup remains exercised.

Export previously fsynced the encrypted file and published a non-overwriting hard link without syncing its destination directory. It now syncs the complete resolved ancestry after publication and returns success only after all syncs pass. A directory-sync error returns a named durability-unconfirmed error and zero successful result while preserving the complete artifact. Callers cannot advance export health or continue configured pre-migration work from failure. Retries re-sync all ancestors; merely existing directories may belong to a previous uncertain publication. The operator must use storage supporting these sync operations.

Independent cleanup review CLEAN. Publication R1 caught retry/ancestor ambiguity; R2 CLEAN after full-ancestry sync and retry regression. Five publication cases independently passed. This is filesystem-level durability and cleanup safety, not proof of physical power-loss behavior or completion of the separate signed-upgrade foundations.

Parent validation: full app and service tests; real SQLite/PostgreSQL restore drill and backup/destroy/restore acceptance; docs verification. All passed: four real both-engine drill entrypoints completed in 7.091 seconds; full app/service and docs checks passed. Service publication race checks passed three runs and vet passed. HTML report records final local results; exact-head CI remains required.
