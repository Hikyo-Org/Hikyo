# Human-readable names on consent and list screens (#1222)

## Result

Consent screens and day-to-day views name what a person reads and approves instead of showing raw IDs. The trigger was the Authorize CLI screen listing `key_…` IDs with no way to tell what was being approved.

- Names on consent pages come from existing authorized reads, never from the CLI or URL. The server returns only org and project IDs.
- Approve waits until those reads settle. A refused read leaves the ID, and the key list stays complete.
- Consent screens print the last six ID characters beside each org, project, environment and key name, because anyone who may rename the thing chooses what the name says. Requester and voter rows on approvals do the same, since display names are self-edited and not unique. Change approvals head a request with system text and keep the requester's free-text purpose in a labelled row.
- `NamedId`, the approval heading and purpose, and the signed-in name on workspace approval render in `bdi`.
- New names with embedding, override or isolate characters are refused on orgs, projects, environments, remotes, service accounts, profiles, invitations, privacy corrections, bootstrap and SCIM `userName`. The LRM, RLM and ALM marks stay allowed for right-to-left names. Stored names are not re-checked, so a SCIM deactivation or a definitions plan is never blocked by one.
- List views get optional `*_name` fields, set only for subjects already on a row the caller may read. A name containing `@` is treated as contact detail on the rows this work added, so those rows keep the ID.
- Transit callers are named only on a key's detail, and only when they hold a grant in the key's own org or project. An instance-level grant does not count.
- CLI tables show `id (name)` with control, format and line separator characters removed. `-o json` keeps the API shape, gains the optional name fields, and makes no extra requests. Machine credentials print bare environment IDs and make no lookup.
- The step-up handoff discloses org and project IDs only when the environment listing would show the caller that environment, and does the same check for a missing environment.

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

- Web consent pages and human CLI tables still write a `grant.denied` record when the caller cannot read the names. Removing that needs server-side names on those payloads.
- Names stored before this work may still hold direction controls, and no migration cleans them. `bdi` contains them on the web and `idWithName` strips them in the CLI. A profile edit that resends a stored display name is refused until the name is cleaned in the same request.
- SCIM accounts whose display name is an email show as IDs on the new name fields until an admin sets a display name.
- Non-failed update jobs keep their ID on hover. The failed state prints it, because that is when someone needs it for the logs.
- Independent human security review has not run. Adversarial security review was automated and is compensating only (`CONTRIBUTING.md`).

## Next steps

1. Decide whether ID-entry forms should get a picker, and what directory read that would need.
2. Add `service_account_name` to `FileTarget` if operators ask for it.
