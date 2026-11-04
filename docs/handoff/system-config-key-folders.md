# System configuration key folders (handoff)

## Result

The Hikyo system project's Environment matrix now shows its 32 keys in folders
instead of one "No folder" group.

| Folder | Keys |
|---|---|
| Audit | `HIKYO_AUDIT_*` |
| Authentication | `HIKYO_ARGON2_*`, `HIKYO_REAUTH_WINDOW_SECONDS`, `HIKYO_SECOND_FACTOR` |
| Backup | `HIKYO_BACKUP_*` |
| Capacity | `HIKYO_DEFAULT_KEYS_PER_PROJECT`, `HIKYO_MAX_KEYS_PER_PROJECT` |
| Deployment | `HIKYO_BOOTSTRAP_SOURCES`, `HIKYO_NODE_OVERRIDES` |
| Mail | `HIKYO_MAIL_*` |
| MCP | `HIKYO_MCP_*` |
| Network | `HIKYO_DIRECTORY_PROXY`, `HIKYO_EXTERNAL_ORIGIN`, `HIKYO_PAIRING_SPKI_PIN` |
| Updates | `HIKYO_UPDATE_CHANNEL` |

## Decisions

- `runtimeconfig.Folder` owns the mapping, and `Keys.Get` and `Keys.List` apply it
  for the system project only, through `authz.IsSelfConfig`.
- Folders are not stored. `service/self_config_runtime.go`,
  `store/upgrade/configuration_migrations.go` and `app/candidate_configuration.go`
  refuse a system catalogue whose stored folder is not empty. Staying flat means no
  migration, immediate effect on adopted instances once upgraded, and a safe downgrade.
- Definitions export reads the stored row, so it keeps an empty `folder_path` while
  `keys list` reports the folder. Export and check still agree.
- Member access rules cannot name this project, so these folders do not affect
  authorization.

## Next steps

A new catalogue key needs a `keyFolders` entry: `TestEveryCatalogueKeyHasAFolder`
fails until it has one. If stored folders become a real need, change all three fences
above and add an upgrade reconcile.
