# #655 restored SQLite publication durability

Base `353e724d9b09e418fc3fd8ce665fe6307813dec3`, worktree `/tmp/hikyo-drill-cleanup`. Uncommitted implementation and reports; no push. Parent owns signed delivery.

The actual restore path had the same file-fsync/hard-link-without-directory-sync defect as archive export. It now syncs complete resolved directory ancestry before reporting success. A failure returns `ErrRestoreDurabilityUnconfirmed` plus the retained target path and underlying error, with zero Manifest. Existing app failure exits stop automatic roll-forward and failed-drill cleanup. Recovery epoch mutations remain in the retained target. Owned staging cleanup and no-overwrite publication stay intact.

Extracted archive export's already reviewed ancestry/sync functions unchanged into `internal/filedurability`; updated both callers and service tests. No changes to securefile.WriteAtomic, F1-F5 runtime gates, platform apply or unrelated application code.

Real embedded migrations plus VACUUM backup fixture proves credential epoch advance and unreconciled-principal state survive injected destination/parent/ancestor sync failures. A retry refuses the existing target. Temporary overlay removing only the directory-sync loop fails all four new cases. Focused store/service tests: 23 pass. Full `go test -p 2 ./internal/store ./internal/app ./internal/service ./internal/lint -count=1`: 596 pass. Both-engine backup/restore, CLI drill and hostile archive epoch suites: six top-level cases, no skips, 16.633s. `go vet ./internal/filedurability ./internal/store ./internal/service ./internal/app`: pass. Independent Standards/Spec review CLEAN; reviewer replayed ten focused cases.

Report: `docs/reports/1.0/restore-publication.html`, with code hashes and redacted drill/old-behavior logs. PostgreSQL used the uniquely created base `hikyo_restore_durability_655` and harness-owned siblings on the local hikyo-pg test container. Credential-safe replay runner `/tmp/hikyo-restore-durability/run-drills.py` reads container config without emitting custody values. Test databases are retained temporarily for the parent replay and may be removed after verification; unrelated databases were not reset.

Read-only F5 preparation separately exists at `/tmp/hikyo-f5-boot-design.md` and `.html`; no foundation implementation is claimed by this repair.
