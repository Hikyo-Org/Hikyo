# Personal values: design handoff

## Result

The design for per-developer local values is written up as [personal-values.md](../adr/personal-values.md), with amendment banners
on 14 locked ADRs. This is a design change only. No code implements it, and the ADR is `proposed`.

## Owner decisions, 2026-11-03

- **O1.** One shared environment plus per-developer values, not one environment per developer.
- **O2.** Server-side encryption, so the instance operator can technically read values.
- **O3.** One complete surface: the browser and CLI each manage personal values without the other.
- **O4.** Administrator-assigned per-person values are out of v1.

The requirement behind them: administrators must not be able to see personal secrets.

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
  sweeping authorities) were then removed rather than kept. That final simplification had no independent re-review until 2026-11-06.
- On 2026-11-06, after the rebase onto `dev` and the owner decisions above, one bounded independent re-review of the final text ran on
  `claude-opus-5-5` as a fresh-context subagent, briefed with the files only, covering Standards and Spec and security. It also checked
  the text against the tenant-isolation and desktop-app banners that landed on `dev` after 2026-11-03. It found 9 Spec and security
  findings and 6 Standards findings, all fixed in the same PR:
  - declared that a holder of instance `instance-config` can confirm new provider certificates and so authenticate as a developer
    who qualifies through federated sign-in;
  - the linked-identity reset refusal now uses the existing uniform refusal, as the tenant-isolation amendment requires;
  - workspace sessions, whose bearer [multi-instance.md](../adr/multi-instance.md) declares extractable, are refused by every
    `personal` operation, and developer credentials reach values only through the delivery overlay;
  - `--config-only`, the human-session path and suspended values no longer fall back silently to a shared value: `hikyo run`
    refuses the key;
  - the edit-and-publish limit now names administrators who grant themselves those rights, and the prepared-account limit and the
    entry warning now cover kept recovery codes;
  - `--member` naming follows the tenant-isolation uniform answer;
  - the 14 banners now carry the review status and the 2026-11-06 decisions, the threat-model banner uses "org or project
    administrator" and sits in date order, the ops-spec link has its anchor, and the 2026-11-03 decisions above carry their O labels.
  These fixes have not had a further independent review. Cross-provider review did not run.
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
