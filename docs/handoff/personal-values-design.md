# Personal values: design handoff

## Result

The design for per-developer local values is written up as [personal-values.md](../adr/personal-values.md), with amendment banners
on 14 locked ADRs. This is a design change only. No code implements it, and the ADR is `proposed`.

## Owner decisions, 2026-11-03

- One shared environment plus per-developer values, not one environment per developer.
- Server-side encryption, so the browser and CLI each offer a complete experience.
- Administrators must not be able to see personal secrets.
- Administrator-assigned per-person values are out of v1.

Everything else in the ADR is a proposal derived from those decisions. Merging the ADR accepts it.

## Decisions the owner should check before merging

1. **The guarantee has two limits the owner has not explicitly accepted.** Anyone with `edit` and `publish` on the shared environment can
   capture a personal value by publishing a proxy setting or a changed endpoint. An administrator who sets up an account before handover
   can keep a copy of its password and authenticator. No server-side check closes either, so the ADR declares them.
2. **Account rules change, and a lost factor costs values.** Network `credential-reset` refuses accounts with a linked external identity,
   whether or not they use personal values. Losing the only factor, including through a recovery code, deletes personal values.
3. **No browser `.env` download.** Managing values is complete on both surfaces, but running a local program needs the CLI, because a
   browser cannot start a process.

## Evidence

- Two independent reviews ran on `claude-opus-5-5` with fresh context, one Standards and one Spec and security. They found a reset
  takeover chain, a schema oracle through delivery, a silent fallback after deletion, and a delivery route that refuses humans. The
  ADR was rewritten to address them.
- Two targeted re-reviews on the same model found further takeover chains. Most led to the qualifying-account rule. The last found the
  prepared-account chain, which no server-side check closes. Heavier rules that only narrowed it (retiring codes and links at first factor,
  sweeping authorities) were then removed rather than kept. **That final simplification has had no independent re-review.**
- Findings are from reading ADRs and source. Nothing was executed, because nothing is implemented.

## Not done

- **Cross-provider review:** not run, not CLEAN. [oss-mechanics.md](../adr/oss-mechanics.md) asks for it, so it needs an explicit
  owner waiver recorded in the ADR.
- **Tickets:** the tickets of the amended ADRs have not been reopened, and no issue exists for this feature.
- **Spec synthesis:** `docs/spec/` was not updated. Its README says ADRs win over spec wording.

## Separate possible bug

`hikyo run --use-human-session` calls `fetchDelivery` (`internal/cli/compose_run.go`), while the contract refuses human sessions on that
route (`api/openapi.yaml`, `x-hikyo-artifacts`, and `internal/isolation/artifact_admission_wire_test.go`). This may already fail in
production. It was read, not executed, and is not addressed here.

## Next steps

1. Owner confirms the three decisions above and records the review waiver or requests cross-provider review.
2. Run one bounded independent re-review of the final text, then mark the PR ready. It stays a draft until then.
3. Open an implementation issue and reopen the amended tickets, then cite the issue in the banners.
4. Implement against § Implementation acceptance in the ADR, starting with storage, the envelope kind and delivery overlay.
