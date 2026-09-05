# F4 preparation adapters in combined F3 worktree

Current source of truth for these files is `/tmp/hikyo-runtime-fence`, not the older `/tmp/hikyo-f4` copies:

- `internal/app/backup_upgrade.go`
- `internal/service/backup.go`
- `internal/service/backup_upgrade.go`
- `internal/service/backup_preparation_test.go`

`runUpgradeExport` holds `upgrade.WithLock` through actual source inspection, authenticated route selection, `Session.PrepareExport`, `store.OpenPreparation`, export and publication. It never opens an ordinary runtime datastore. Preparation remains a concrete opaque store type, and the owner cannot outlive the callback.

`service.ExportPreparedUpgrade(ctx, *store.PreparationDB, options, dir, plan, proposal)` uses a private shared archive writer for framing/publication. The existing runtime Backup service remains responsible for ordinary export and audit writes; no runtime store or audit operation is exposed through preparation. File naming uses the actual manifest engine, so preparation needs no generic DB field.

Validation:

- Actual both-engine preparation export normal1.928s/race7.641s: real signed route, exact encrypted receipt, full age decryption and opaque manifest proof; retained inspection/export refuse after owner ends, leaving no published debris.
- Actual both-engine ordinary passphrase backup with genuine development gate admission4.067s: existing v1 format, no upgrade receipt.
- Service package compilation and diff check passed.

App package compilation is currently blocked by the old `app.go:225` boot seam assigning the new store.Open function to its historical two-argument function type. Parent F5 app integration owns that seam. The CLI adaptation itself must receive its actual end-to-end check once the shared gate is present; the local service passes are not claimed as CLI proof. Existing F4 `backup_upgrade_drill.go` was copied into the F3 tree for compilation only and still awaits the narrow recovery API.

Independent adapter review is queued with approval_acceptance after its parent-assigned service fixture conversion. Parent owns final review, exact candidate CI, signing and delivery.
