# Personal values: design handoff

## Result

The design for per-developer local values is written up as [personal-values.md](../adr/personal-values.md), with amendment banners
on 14 locked ADRs. This is a design change only. No code implements it, and the ADR is `proposed`.

## Owner decisions, 2026-11-03

- One shared environment plus per-developer values, not one environment per developer.
- Server-side encryption, so the browser and CLI each offer a complete experience.
- Administrators must not be able to see personal secrets.
- Administrator-assigned per-person values are out of v1.

## Owner decisions, 2026-11-06

The owner asked to rebase, fix and merge, which accepts the recommendations that were open here. They are recorded as O5 to O8 in
the ADR.

1. **The two declared limits are accepted as declared.** Anyone with `edit` and `publish` on the shared environment can capture a
   personal value by publishing a proxy setting or a changed endpoint. An administrator who sets up an account before handover can
   keep a copy of its password and authenticator. No server-side check closes either, so the ADR declares them.
2. **The account rules stay as written.** Network `credential-reset` refuses accounts with a linked external identity, whether or
   not they use personal values. Losing the only factor, including through a recovery code, deletes personal values.
3. **No browser `.env` download.** Managing values is complete on both surfaces, but running a local program needs the CLI, because
   a browser cannot start a process.
4. **Cross-provider review is waived.** The other provider is unavailable, and the owner's current policy makes cross-provider
   review optional. It did not run and is not CLEAN.

Everything else in the ADR is a proposal derived from these decisions. Merging the ADR accepts it.

## Evidence

- Two independent reviews ran on `claude-opus-5-5` with fresh context, one Standards and one Spec and security. They found a reset
  takeover chain, a schema oracle through delivery, a silent fallback after deletion, and a delivery route that refuses humans. The
  ADR was rewritten to address them.
- Two targeted re-reviews on the same model found further takeover chains. Most led to the qualifying-account rule. The last found the
  prepared-account chain, which no server-side check closes. Heavier rules that only narrowed it (retiring codes and links at first factor,
  sweeping authorities) were then removed rather than kept. **That final simplification has had no independent re-review.**
- Findings are from reading ADRs and source. Nothing was executed, because nothing is implemented.

## Not done

- **Cross-provider review:** not run, not CLEAN. [oss-mechanics.md](../adr/oss-mechanics.md) asks for it, and the owner waived it
  on 2026-11-06 (O8 in the ADR).
- **Tickets:** the tickets of the amended ADRs have not been reopened, and no issue exists for this feature.
- **Spec synthesis:** `docs/spec/` was not updated. Its README says ADRs win over spec wording.

## Separate bug, fixed elsewhere

`hikyo run --use-human-session` called `fetchDelivery`, which refuses human sessions. It was reproduced against a real server and is fixed in
[#1173](https://github.com/Hikyo-Org/Hikyo/pull/1173), which discloses through `exportValues`. Personal values stay out of that path, as the
ADR states.

## Next steps

1. Open an implementation issue and reopen the amended tickets, then cite the issue in the banners.
2. Implement against § Implementation acceptance in the ADR, starting with storage, the envelope kind and delivery overlay. The
   implementation PR flips the ADR status.
