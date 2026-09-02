# Hikyo domain glossary

Ubiquitous language for the Hikyo control plane. Terms only; no implementation
detail. Decisions live in `docs/adr/`.

## Registration

- **Registration policy** — the single switch that opens sign-up on an
  otherwise closed instance. Bound to one scope (an org, or the instance).
  Absent policy means closed. It is the only path by which an *unsolicited*
  unknown identity (no invitation token, no SCIM binding, not bootstrap)
  becomes an account; the former per-provider JIT provisioning is a
  registration policy with a `none` landing.
- **Sign-up** — the act of an unknown identity (external, or a fresh local
  credential) becoming an account under a registration policy. Distinct from
  *invitation claiming*, which binds to an invitation token, and from *login*,
  which requires a known identity.
- **Landing** — where a sign-up arrives and with what authority. Determined
  by the policy's scope: an org-scope policy lands in that org with a role
  template; an instance-scope policy lands either nowhere (`none`: an account
  with zero grants) or in a *fresh org* whose first admin is the signer.
- **Fresh org** — an org minted by a sign-up under an instance-scope policy,
  with the signer as its first administrator.
- **Admission predicate** — the part of a registration policy that says which
  identities may sign up: a set of admitted *external entries* (a configured
  provider, optionally fenced by one issuer-specific claim allowlist, and
  always requiring a verified email assertion) and at most one *local entry*
  (email + password, optionally fenced by an email-domain allowlist).
- **Authority principal (of a policy)** — the principal whose current grants
  are re-checked at every sign-up to mint the landing; the policy is a
  standing delegation in the same sense as a pin or an adapter.
