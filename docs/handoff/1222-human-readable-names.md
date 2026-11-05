# Human-readable names on consent and list screens (#1222)

## Result

Consent screens and day-to-day views name what a person reads and approves instead of showing raw IDs. The trigger was the Authorize CLI screen listing `key_…` IDs with no way to tell what was being approved.

- Names on consent pages come from existing authorized reads, never from the CLI or URL. The server returns only org and project IDs.
- Approve waits until those reads settle. A refused read leaves the ID, and the key list stays complete.
- List views get optional `*_name` fields, set only for subjects already on a row the caller may read. Transit callers also need a grant in the key's tenant.
- CLI tables show `id (name)` with control and bidi characters removed. `-o json` keeps the API shape and makes no extra requests.

## Evidence

Current results, including hosted CI and merge state, are on the PR. Screenshots are hosted there and are not in Git.

## Remaining work

These are deliberately not done:

- Forms that ask for a typed ID. The pages cannot search the user directory, so changing this is a product decision.
- `file-target list` service account names. The client lookup needs manage-identities and writes an audit event per listing. A server-side `service_account_name` on `FileTarget` is the way to do it.
- SCIM credential labels, which need a schema change.
- `developer_credentials list` environment names, because the list spans projects.
- `created_by_name` on origins and remotes, which is not rendered anywhere.

## Known gaps

- `TestFreezeGateFixtures/remove-past-sunset-{request,response}-property-fails` fails on clean `dev` too.
- Service and CLI tests do not cover naming on connections, the definitions last apply, or the identities preview.
- The real-flow run covered CLI reauth only, on sqlite with a virtual passkey. Workspace approval and the other web screens have story evidence only.
- Existing CLI cells such as SSH CA and policy names print unfiltered. Only names that pass through `idWithName` are cleaned, because `Render` also prints revealed values and must not alter them.

## Next steps

1. Decide whether ID-entry forms should get a picker, and what directory read that would need.
2. Add `service_account_name` to `FileTarget` if operators ask for it.
