import { readFile, writeFile } from 'node:fs/promises';
import data from './report-data.json' with { type: 'json' };
import inventory from './variable-inventory.json' with { type: 'json' };

const read = (name) => readFile(new URL(name, import.meta.url));
const escape = (value) => String(value).replace(/[&<>"']/g, (character) => ({
  '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
})[character]);
const source = (path) => 'https://github.com/Hikyo-Org/Hikyo/blob/' + data.baseline + '/' + path;
const sourceLink = (path, label = path) => `<a href="${escape(source(path))}">${escape(label)}</a>`;
const link = (url, label) => `<a href="${escape(url)}">${escape(label)}</a>`;
const groups = [...new Set(data.decisions.map((decision) => decision.group))];
const explicitCount = data.decisions.filter((decision) => decision.approval === 'explicit').length;
let css = (await read('report.css')).toString();
for (const [placeholder, file] of [
  ['@@REGULAR@@', 'fonts/instrument-sans-400.woff2'],
  ['@@BOLD@@', 'fonts/instrument-sans-700.woff2'],
  ['@@MONO@@', 'fonts/ibm-plex-mono-400.woff2'],
]) css = css.replace(placeholder, (await read(file)).toString('base64'));
const script = (await read('report.js')).toString().replace('[/* SCENARIOS */]', JSON.stringify(data.scenarios).replaceAll('<', '\\u003c'));
const instrumentLicense = (await read('fonts/instrument-sans-LICENSE.txt')).toString();
const plexLicense = (await read('fonts/ibm-plex-mono-LICENSE.txt')).toString();

function decisionHTML(decision) {
  const explicit = decision.approval === 'explicit';
  return `<details class="decision" id="${decision.id}" data-group="${escape(decision.group)}" data-approval="${decision.approval}"${explicit ? ' open' : ''}>
    <summary><span class="decision-id">${decision.id}</span><span><span class="question">${escape(decision.question)}</span><span class="selected-answer">${escape(decision.recommendation)}</span></span><span class="disclosure" aria-hidden="true">+</span></summary>
    <div class="decision-body">
      <span class="approval-tag ${decision.approval}">${explicit ? 'Explicitly approved by you' : 'Decided under your delegated approval'}</span>
      <h4>Options considered</h4>
      <div class="options">${decision.options.map(([title, description], index) => `<div class="option${index === 0 ? ' selected' : ''}"><span class="option-letter">${String.fromCharCode(65 + index)}</span><div><strong>${escape(title)}${index === 0 ? '<span class="chosen">Recommended · selected</span>' : ''}</strong><span class="description">${escape(description)}</span></div></div>`).join('')}</div>
      <h4>Why this recommendation</h4><p>${escape(decision.rationale)}</p>
      <h4>Decision</h4><p>${escape(decision.decision)}</p>
      <h4>Required proof</h4><p>${escape(decision.acceptance)}</p>
      <h4>${decision.evidenceScope === 'worktree' ? 'Current implementation evidence' : 'Historical repository evidence'}</h4><div class="evidence-links">${decision.evidence.map((path) => decision.evidenceScope === 'worktree' ? `<code>${escape(path)}</code>` : sourceLink(path)).join('')}${link('#' + decision.id, 'Link to ' + decision.id)}</div>
    </div>
  </details>`;
}

const html = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <meta name="color-scheme" content="dark light">
  <meta name="description" content="Hikyo self-configuration design: ${data.decisions.length} resolved decisions, options, recommendations, runtime boundaries and delivery requirements.">
  <meta name="referrer" content="no-referrer">
  <title>Hikyo manages Hikyo | Decision report</title>
  <link rel="icon" href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 32 32'%3E%3Cpath fill='%235ab5ad' d='M4 3h6v10h12V3h6v26h-6V19H10v10H4z'/%3E%3C/svg%3E">
  <style>${css}</style>
</head>
<body>
<a class="skip" href="#overview">Skip to report</a>
<div class="shell">
  <aside class="rail" aria-label="Report navigation">
    <a class="brand" href="#overview"><svg aria-hidden="true" viewBox="0 0 32 32"><path fill="currentColor" d="M4 3h6v10h12V3h6v26h-6V19H10v10H4z"/></svg>hikyo</a>
    <div class="nav-block"><div class="eyebrow">Self-configuration</div><nav aria-label="Sections">
      <a href="#overview"><span>01</span>Overview</a><a href="#decisions"><span>02</span>Decisions</a><a href="#catalogue"><span>03</span>Settings catalogue</a><a href="#activation"><span>04</span>Apply & recovery</a><a href="#delivery"><span>05</span>Delivery & evidence</a>
    </nav></div>
    <div class="rail-bottom"><button id="theme" type="button">Use light theme</button><p>Design record<br>${escape(data.date)}<br>Local, standalone report</p></div>
  </aside>
  <main class="document" id="overview">
    <div class="masthead"><span class="eyebrow">Engineering / Decision report</span><span class="stamp"><b>${escape(data.deliveryStage)}</b> · ${escape(data.date)}</span></div>
    <header class="hero">
      <h1>Hikyo manages<br><span>Hikyo.</span></h1>
      <p class="lede">One project for each Hikyo instance. Shared configuration for its HA replicas. Independent configuration for every remote.</p>
      <div class="hero-meta"><span><strong>${data.decisions.length}</strong> resolved decisions</span><span><strong>${explicitCount}</strong> explicit approvals</span><span><strong>${data.decisions.length - explicitCount}</strong> delegated decisions</span></div>
      <div class="status-note"><div><strong>${escape(data.status)}</strong><p>${escape(data.revisionNote)}</p></div><span class="mark">${escape(data.deliveryStage ?? 'BUILD / VERIFYING')}</span></div>
      <div class="topology" id="topology" aria-labelledby="topology-title">
        <h2 id="topology-title">${escape(data.topology.heading)}</h2>
        <p class="caption">${escape(data.topology.summary)}</p>
        <div class="topology-root"><strong>${escape(data.topology.root)}</strong><span>${escape(data.topology.rootKind)}</span></div>
        <div class="topology-projects">${data.topology.projects.map((project) => `<article class="topology-project"><div><span class="eyebrow">${escape(project.owner)}</span><h3>${escape(project.name)}</h3><span class="caption">Project · ${escape(project.environment)}</span></div><div><strong>${escape(project.members)}</strong><p>${escape(project.note)}</p></div></article>`).join('')}</div>
        <p class="caption">${escape(data.topology.ownership)}</p><p class="caption"><strong>${escape(data.topology.boundary)}</strong> ${link('#D26', 'Instance boundary: D26')} · ${link('#D27', 'Root ownership: D27')}</p>
      </div>
      <div class="approval"><div><h3>You explicitly approved</h3><p>${data.authority.explicit.map(escape).join(' ')}</p></div><div><h3>You delegated the remaining choices</h3><p>“${escape(data.authority.delegation)}” Each remaining record is marked as a delegated decision, rather than an answer you personally gave. The root-management-view interpretation is a delegated recommendation; your remote/HA distinction is explicit.</p></div></div>
      ${data.bootstrapRolloutDecision ? `<div class="status-note" id="bootstrap-clarification"><div><strong>Bootstrap rollouts explicitly approved</strong><h3>${escape(data.bootstrapRolloutDecision.question)}</h3><p>${escape(data.bootstrapRolloutDecision.reason)}</p><ul>${data.bootstrapRolloutDecision.options.map(([title, detail]) => `<li><strong>${escape(title)}</strong>: ${escape(detail)}</li>`).join('')}</ul><p><strong>Recommendation:</strong> ${escape(data.bootstrapRolloutDecision.recommendation)}</p><p>${escape(data.bootstrapRolloutDecision.status)}</p></div></div>` : ''}
    </header>

    <section class="section" id="decisions" aria-labelledby="decisions-title">
      <div class="section-top"><span class="section-number">02</span><h2 id="decisions-title">The decision record</h2></div>
      <p class="section-lede">Every question has a selected option, a reason and an observable acceptance condition. Expand a question for the full decision.</p>
      <div class="controls no-print"><label>Search decisions<input id="search" type="search" placeholder="Try SMTP, rollback, access…" autocomplete="off"></label><label>Topic<select id="group-filter"><option value="">All topics</option>${groups.map((group) => `<option value="${escape(group)}">${escape(group)}</option>`).join('')}</select></label><label>Decision authority<select id="approval-filter"><option value="">All decisions</option><option value="explicit">Explicit approval</option><option value="delegated">Delegated approval</option></select></label></div>
      <div class="ledger-tools no-print"><p id="result-count" role="status" aria-live="polite">${data.decisions.length} of ${data.decisions.length} decisions</p><button id="expand" type="button">Expand visible</button></div>
      <p class="empty" id="empty" hidden>No matching decisions. Clear the search or change a filter.</p>
      ${groups.map((group) => `<div class="decision-group"><h3 class="group-heading">${escape(group)}</h3>${data.decisions.filter((decision) => decision.group === group).map(decisionHTML).join('')}</div>`).join('')}
    </section>

    <section class="section" id="catalogue" aria-labelledby="catalogue-title">
      <div class="section-top"><span class="section-number">03</span><h2 id="catalogue-title">Configuration coverage</h2></div>
      <p class="section-lede">The current catalogue has 27 top-level keys: nine historical mail/channel keys, 16 shared owner settings, secret node overrides and external bootstrap aliases. Ordinary owner and node consumers reload live. The complete-server-variable objective still has the explicit deployment gaps listed below.</p>
      <p><strong>The applied revision is authoritative after adoption.</strong> Environment variables and configured mail files seed supported keys once. They never silently override a saved revision on restart.</p>
      <div class="table-wrap" tabindex="0" role="region" aria-label="Managed setting catalogue; scroll horizontally on narrow screens"><table class="catalogue"><caption>Historical nine-key slice, still included in the current 27-key catalogue. Earlier CI/browser evidence applies to this slice.</caption><thead><tr><th scope="col">Managed key</th><th scope="col">Classification</th><th scope="col">Absence, defaults and validation</th></tr></thead><tbody>${data.catalogue.map(([key, classification, initial, validation, owner, origin]) => `<tr><td><code class="key">${escape(key)}</code><span class="key-note">${escape(owner)} · ${escape(origin)}</span></td><td class="${classification === 'Secret' ? 'secret' : ''}">${classification === 'Secret' ? '● Secret' : 'Configuration'}</td><td>${escape(initial)}<span class="validation">${escape(validation)}</span></td></tr>`).join('')}</tbody></table></div>
      <p class="caption">Mail is off by default. The setup helper enables or clears the mail group together, through ordinary drafts. Partial credentials fail validation. There is no new plaintext secret store.</p>
      <h3 class="subheading">Eighteen added top-level keys</h3>
      <div class="table-wrap" tabindex="0" role="region" aria-label="Expanded managed catalogue; scroll horizontally on narrow screens"><table><caption>Current expansion. The node document contains 15 permitted per-node fields; it is one secret top-level project value.</caption><thead><tr><th scope="col">Key / classification</th><th scope="col">Activation</th><th scope="col">Meaning and boundary</th></tr></thead><tbody>${data.expandedCatalogue.map(([key, classification, activation, description]) => `<tr><td><code class="key">${escape(key)}</code><span class="key-note">${escape(classification)}</span></td><td>${escape(activation)}</td><td>${escape(description)}</td></tr>`).join('')}</tbody></table></div>
      <h3 class="subheading">Every recognized input</h3>
      <p class="section-lede">${inventory.length} recognized environment inputs, generated from the code inventory. Lifecycle describes the activation mechanism required by D11, not a claim that it is implemented. Paths identify sources; they are not permission to read arbitrary remote files.</p>
      <div class="table-wrap" tabindex="0" role="region" aria-label="Complete variable inventory; scroll horizontally on narrow screens"><table><caption>Recognized environment inputs, including server, client, command-only and retired inputs. Inventory does not imply all of them have an implemented Apply consumer.</caption><thead><tr><th scope="col">Input</th><th scope="col">Consumer / scope</th><th scope="col">Required lifecycle</th><th scope="col">Source boundary</th></tr></thead><tbody>${inventory.map((entry) => `<tr><td><code class="key">${escape(entry.key)}</code>${entry.secret ? '<span class="key-note">Credential-bearing value</span>' : ''}${entry.referencedContentSecret ? '<span class="key-note">Referenced file contents are secret</span>' : ''}</td><td>${escape(entry.audience)} / ${escape(entry.scope)}</td><td>${escape(entry.activation)}</td><td>${escape(entry.import)}${entry.fileContentKey ? `<span class="key-note">Imports into ${escape(entry.fileContentKey)}</span>` : ''}</td></tr>`).join('')}</tbody></table></div>
      <h3 class="subheading">Expanded scope and activation requirements</h3><p class="section-lede">The earlier external-only restriction is superseded by D11. These families are part of the expanded work where they configure the server. Bootstrap transitions require an independently available source and a deployment integration; read-only inventory alone does not satisfy remote Apply.</p>
      ${data.external.map(([title, keys, explanation]) => `<details class="external"><summary>${escape(title)}</summary><p>${escape(explanation)}</p><div class="keys">${keys.map((key) => `<code>${escape(key)}</code>`).join('')}</div></details>`).join('')}
      <p class="caption" style="margin-top:20px">Read ${link('#D11', 'D11–D15')} for source precedence, controlled file import, SMTP testing and schema ownership. The database connection and root-key source must be available before Hikyo can decrypt any managed configuration.</p>
    </section>

    <section class="section" id="activation" aria-labelledby="activation-title">
      <div class="section-top"><span class="section-number">04</span><h2 id="activation-title">Follow an apply</h2></div>
      <p class="section-lede">Local swaps are atomic. HA transitions are coordinated within one logical instance. Independent remotes never join that apply: their projects, values and revisions remain unchanged.</p>
      <div class="simulator">
        <div class="eyebrow">Interactive design model · no live actions</div>
        <div class="scenario-tabs no-print" role="group" aria-label="Choose an activation scenario">${data.scenarios.map((scenario, index) => `<button type="button" data-scenario="${index}" aria-pressed="${index === 0}">${escape(scenario.label)}</button>`).join('')}</div>
        <div class="sim-title-row"><h3 id="scenario-title">Valid email change</h3><span id="scenario-outcome" class="outcome">Draft</span></div>
        <p id="scenario-intro">Illustrative model only. It does not call Hikyo or send email.</p>
        <div class="sim-state" role="status" aria-live="polite" aria-atomic="true">
          <div class="sim-stats"><div class="sim-stat"><span>Durable target</span><strong id="desired-revision">r12</strong></div><div class="sim-stat"><span>Observed nodes</span><strong id="node-revisions">r12 / r12</strong></div></div>
          <div class="steps" id="step-progress" aria-hidden="true"></div><div class="step-title" id="step-title">Edit</div><p class="step-detail" id="step-detail">SMTP edits start as drafts. Running mail settings remain unchanged.</p>
        </div>
        <div class="step-controls no-print"><span class="caption" id="step-count">Step 1 of 5</span><div class="buttons"><button type="button" id="previous-step" disabled>Previous</button><button type="button" class="primary" id="next-step">Next step →</button></div></div>
        <noscript><p>JavaScript is off. The full protocol and every decision remain readable in the report.</p></noscript>
      </div>
      <div class="protocol"><div><h3>Before durable commit</h3><p>Resolve an exact revision, validate against the current runtime catalogue and prepare all admitted nodes. The review TTL is five minutes; each request waits at most 30 seconds. Final commitment requires worker attestations younger than 30 seconds and exact fresh MFA. Any failure here keeps the existing target and runtime active. No SMTP probe is sent.</p></div><div><h3>After durable commit</h3><p>The new generation is the durable target. Nodes swap and acknowledge it. A missing acknowledgement means pending or partial, not success. Restart resumes this target; stale nodes must reconcile or refuse affected work.</p></div></div>
      <div class="table-wrap"><table><caption>Failure semantics selected in D16–D24</caption><thead><tr><th scope="col">Condition</th><th scope="col">Selected behavior</th></tr></thead><tbody>
        <tr><td>Invalid candidate</td><td>Reject before commit. Keep active revision. Name invalid fields without values.</td></tr>
        <tr><td>SMTP unavailable after apply</td><td>Keep the applied config. Show the failed send. No automatic rollback or duplicate retry.</td></tr>
        <tr><td>HA node cannot acknowledge</td><td>Show partial/mixed state; fence stale consumers and keep reconciling.</td></tr>
        <tr><td>Applied snapshot becomes old</td><td>Explicit runtime retention references protect active, desired and recovery payloads from GC.</td></tr>
        <tr><td>Partial bootstrap rollout</td><td>Fresh exact Restore MFA, durable business fence and controller-confirmed Restored. Then a separate fresh-MFA repair Apply. Neither restoration nor a request receipt means runtime Applied.</td></tr>
        <tr><td>Root source changed</td><td>Raw keys remain in external custody. Exact MFA gates wrapper persistence. Never automatically retire the old root wrapper.</td></tr>
        <tr><td>Backup restored</td><td>Keep outbound use fenced until existing recovery reconciliation and explicit credential resumption.</td></tr>
      </tbody></table></div>
      <p class="caption">An already admitted send may finish on its captured revision within the existing 15-second deadline. “Applied” covers new work; it cannot undo an email already in flight.</p>
    </section>

    <section class="section" id="delivery" aria-labelledby="delivery-title">
      <div class="section-top"><span class="section-number">05</span><h2 id="delivery-title">From decisions to delivery</h2></div>
      <p class="section-lede">The expansion is saved in signed checkpoints, with later reviewed slices integrating locally. Final exact-head CI is pending. The completed milestone history and historical screenshots belong to the earlier nine-key implementation. New desktop evidence below exercises the expanded catalogue and prepare-before-MFA UI.</p>
      <div class="table-wrap"><table><caption>Current expansion evidence and historical delivery boundary</caption><thead><tr><th scope="col">Check</th><th scope="col">Current result</th></tr></thead><tbody>${(data.deliveryEvidence ?? []).map(([check, result]) => `<tr><td>${escape(check)}</td><td>${escape(result)}</td></tr>`).join('')}</tbody></table></div>
      <p class="caption">The <a href="./validation.md">validation record</a> and <a href="https://github.com/Hikyo-Org/Hikyo/pull/686">PR #686</a> include commands, review findings and delivery limits. No production deployment is implied.</p>
      <p class="caption">Current expanded-catalogue desktop proof: <a href="./validation/managed-configuration-expanded-desktop-applied.png">local passkey Apply</a> · <a href="./validation/managed-configuration-expanded-desktop-nodes.png">node acknowledgement</a> · <a href="./validation/managed-configuration-expanded-desktop-independent-owner.png">independent-owner TOTP Apply</a>. These disposable-instance journeys edit the update channel; they do not prove Kubernetes rollout or every new setting. Current expanded-catalogue mobile proof: <a href="./validation/managed-configuration-expanded-mobile-applied.png">local passkey Apply</a> · <a href="./validation/managed-configuration-expanded-mobile-nodes.png">node acknowledgement</a> · <a href="./validation/managed-configuration-expanded-mobile-independent-owner.png">independent-owner TOTP Apply</a>.</p>
      <p class="caption">Historical nine-key browser proof: <a href="./validation/managed-configuration-desktop.png">desktop apply</a> · <a href="./validation/managed-configuration-nodes-mobile.png">mobile node convergence</a> · <a href="./validation/managed-configuration-independent-owner-mobile.png">independent owner</a>. Screenshots use disposable test instances.</p>
      <h3 class="subheading">Remaining acceptance gaps</h3><ul class="acceptance-gaps">${data.acceptanceGaps.map((gap) => `<li>${escape(gap)}</li>`).join('')}</ul>
      <h3 class="subheading">Historical nine-key milestones</h3>
      <div class="milestones">${data.milestones.map((milestone, index) => `<article class="milestone"><span class="number">0${index + 1}</span><div><h3>${escape(milestone.title)}</h3><small>${escape(milestone.status)} · Depends on: ${escape(milestone.depends)}</small><p>${escape(milestone.scope)}</p><p class="proof"><strong>Done when:</strong> ${escape(milestone.proof)}</p></div></article>`).join('')}</div>
      <div class="amendments"><h3>Contract changes are explicit</h3><p>The owning ADRs now declare the approved amendments for system-resource authorization, narrow runtime authority, retention references, managed mail configuration, activation audit, HA fencing and restored credentials. The implementation includes the shared TLS mail transport and its tests; unrelated account sign-up remains a separate feature.</p></div>
      <h3 class="subheading">Design baseline inspected</h3><p class="caption">The descriptions below explain the starting point, before this implementation. Code links are pinned to ${sourceLink('internal/config/config.go', data.baseline.slice(0, 8))}. Issue state was checked on ${escape(data.date)}.</p>
      <div class="sources">
        <div class="source"><div>${sourceLink('internal/service/bootstrap.go', 'First administrator bootstrap')}</div><p>Creates the first account, operator grants and establishment authority. Does not provision a configuration organization or project.</p></div>
        <div class="source"><div>${sourceLink('internal/config/config.go', 'Startup config')} · ${sourceLink('internal/app/app.go', 'Application wiring')}</div><p>Flags and environment produce a startup config. Runtime services capture configured values. There is no managed-revision activation path.</p></div>
        <div class="source"><div>${sourceLink('internal/app/tls.go', 'TLS reload')} · ${sourceLink('internal/app/ha.go', 'HA coordination')}</div><p>Certificate replacement already retains the last valid pair. HA already has node registration, datastore-clock liveness and readiness plumbing.</p></div>
        <div class="source"><div>${sourceLink('internal/service/pins.go', 'Revision pins')} · ${sourceLink('internal/authz/authorize.go', 'Authorization')}</div><p>Ordinary pins expire. System proof sites are closed and transaction-bound. Both need specific, visible contract changes for this feature.</p></div>
        <div class="source"><div>${link('https://github.com/Hikyo-Org/Hikyo/issues/608', 'Mailer implementation #608')} · ${sourceLink('docs/spec/social-signin.md', 'Mailer specification')}</div><p>This is the historical investigation baseline. The earlier nine-key implementation subsequently added the shared mail transport and live configuration path.</p></div>
        <div class="source"><div>${sourceLink('docs/adr/multi-instance.md', 'Remote-instance ownership')} · ${sourceLink('docs/site/src/content/docs/docs/high-availability.mdx', 'HA topology')}</div><p>Remote values remain on their owning instance and flow directly to the authorized browser. HA replicas share one logical instance, its PostgreSQL datastore and root-key authority. Root navigation introduces no central secret store.</p></div>
      </div>
    </section>

    <footer class="footer"><button class="no-print" type="button" id="print">Print / save PDF</button><a class="no-print" id="download-html" href="./index.html" download="hikyo-self-configuration-report.html">Download standalone HTML</a><a class="no-print" id="download-data" href="./report-data.json" download="hikyo-self-configuration-decisions.json">Decision data</a><a href="#overview">Back to overview ↑</a>
      <p>${escape(data.status)}. This report records the approved design and implementation evidence; it performs no configuration actions.</p>
      <p>Fonts embedded for offline reading: Instrument Sans and IBM Plex Mono, distributed under the SIL Open Font License.</p>
      <details class="license"><summary>Font licenses</summary><pre>${escape(instrumentLicense)}\n\n${escape(plexLicense)}</pre></details>
    </footer>
  </main>
</div>
<script type="application/json" id="report-data">${JSON.stringify(data).replaceAll('<', '\\u003c')}</script>
<script>${script}</script>
</body>
</html>
`;
await writeFile(new URL('index.html', import.meta.url), html);
console.log(`Built index.html: ${data.decisions.length} decisions, ${data.catalogue.length + data.expandedCatalogue.length} current top-level keys, ${Buffer.byteLength(html)} bytes.`);
