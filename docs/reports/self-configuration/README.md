# Hikyo self-configuration decision report

Design approved and implementation built on 2026-09-06. Local validation passes and both independent reviews are clean; see the report status and validation record for delivery evidence.

Open [index.html](./index.html) directly in a browser. It embeds its fonts, styles, scripts and decision data, so the report remains readable and interactive when copied elsewhere without a server. External source links require a network connection. There are no analytics, remote assets, API calls or live configuration actions.

The report is served on the LAN at <http://192.168.0.30:8769/>. To serve it from this checkout on that machine:

```sh
python3 -m http.server 8769 --bind 192.168.0.30 --directory docs/reports/self-configuration
```

Open <http://192.168.0.30:8769/> from a LAN device. This is a LAN preview, not a deployment to hikyo.app. The HTML remains available as a file after the preview process stops. A separate loopback listener also serves the earlier <http://127.0.0.1:8769/> link.

## Contents

- 27 questions with options, recommendation, decision, rationale, evidence and required proof. Three decisions were explicitly approved; 24 were selected under the user's delegated approval.
- Nine managed keys and an exhaustive disposition of the current known environment keys. The report distinguishes first-delivery scope, bootstrap inputs and existing dedicated operations. A topology diagram shows one root management view, separate owner-local projects per independent instance and sharing only within HA.
- Five interactive activation scenarios, with current-phase status and per-node revisions, explicitly scoped to one logical instance rather than its independent remotes.
- Five implementation milestones, their validation requirements and the owning ADR amendments.
- Search, topic/authority filters, deep links, disclosure controls, dual themes, print/PDF preparation and downloadable HTML/JSON.

## Editing and regeneration

`report-data.json` is the content source; `build.mjs` escapes it into static HTML. `report.css` and `report.js` provide the report presentation and local interactions. The generator uses only Node built-ins and a static JSON module import, with no dependency installation or network access.

```sh
node docs/reports/self-configuration/build.mjs
node --check docs/reports/self-configuration/build.mjs
node --check docs/reports/self-configuration/report.js
```

Use the repository's pinned Node version. Rebuild `index.html` after editing any source and recheck affected report interactions. The source JavaScript's `SCENARIOS` marker is replaced at build time; only the generated HTML is the complete browser artifact.

Fonts are vendored from the repository-pinned Fontsource packages: `@fontsource/instrument-sans@5.3.0` (Latin regular and bold) and `@fontsource/ibm-plex-mono@5.3.0` (Latin regular). Their SIL Open Font Licenses live beside the font files and are embedded in the generated report.

## Related records

See the [design summary](../../spec/self-configuration-proposal.md), [implementation handoff](../../handoff/self-configuration-design.md) and [validation record](./validation.md). Repository evidence is pinned to `90b4ca6a5d22438e751cf9af83aa4fd077a6a61c`; issue state is dated, not a promise about future work.
