# Instance settings groups (handoff)

## Result

`/instance` now has six group tabs instead of 14 flat tabs. Each group stacks its
existing panels, the pattern SAML and Certificates already used.

| Group (`?tab=`) | Panels |
|---|---|
| General (default, no param) | Hikyo configuration, Mailer |
| Organisations & members (`organisations`) | Organisations, Members |
| Sign-in (`sign-in`) | Identity providers, GitHub sign-in, SAML providers, SAML SP signing keys |
| Credentials (`credentials`) | Machine credential policy, Developer credential policy, Federation issuers |
| Keys & certificates (`keys`) | Keys & crypto, Certificate authorities, Certificate profiles |
| Operations (`operations`) | Instance audit, Retention health |

## Decisions

- Every `Panel` id is unchanged, so fragments and panel locators keep working.
- The "Policy" panel is now titled "Machine credential policy", because it sits
  directly above "Developer credential policy".
- `SectionTabs` now also resolves a retired `?tab=` value through `anchors`, so
  `?tab=oidc`, `?tab=pki` and similar links open the group that holds the panel and
  scroll to it.
- The `routes/InstanceAdmin.tsx` sensitivity pin was refreshed after a line-level
  audit: section entries moved and one panel title changed; no query, mutation or
  plaintext handling changed.

## Next steps

Rename or move a group by editing the `sections` array in
`web/src/routes/InstanceAdmin.tsx`, then update the tab names in
`web/e2e/flows/instance-admin.spec.ts`, `web/src/routes/InstanceAdmin.stories.tsx`
and the docs pages that name the tab.
