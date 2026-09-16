// Sweeps every shell route of the prototype-mode app (pnpm prototype --port 5174)
// and prints the distinct computed-style signatures of buttons, field controls,
// labels, headings, badges, links and text per pointer mode. This is the
// evidence behind docs/handoff/storybook-ui-consistency.md; rerun it for the
// next audit slice. Usage: node scripts/audit-styles.mjs > /tmp/app-audit.txt

import { chromium, devices } from 'playwright';
import { writeFileSync } from 'node:fs';
const APP = 'http://localhost:5174';
const ORG = 'org_11111111-1111-4111-8111-111111111111', PRJ = 'prj_11111111-1111-4111-8111-111111111111';
const ENV = 'env_11111111-1111-4111-8111-111111111111', KEY = 'key_11111111-1111-4111-8111-111111111111';
const routes = ['/', '/projects', '/remotes', `/orgs/${ORG}/members`, `/orgs/${ORG}/settings`, `/orgs/${ORG}/scim`, `/orgs/${ORG}/audit`,
  '/instance', '/instance/config', '/instance/members', '/settings',
  `/orgs/${ORG}/projects/${PRJ}/matrix`, `/orgs/${ORG}/projects/${PRJ}/matrix/history`, `/orgs/${ORG}/projects/${PRJ}/matrix/keys/${KEY}`,
  `/orgs/${ORG}/projects/${PRJ}/environments/${ENV}/values`, `/orgs/${ORG}/projects/${PRJ}/machine-access`,
  `/orgs/${ORG}/projects/${PRJ}/change-approvals`, `/orgs/${ORG}/projects/${PRJ}/adapters`, `/orgs/${ORG}/projects/${PRJ}/audit`,
  `/orgs/${ORG}/projects/${PRJ}/settings`];
const modes = [['fine', { viewport: { width: 1280, height: 900 } }], ['coarse', { ...devices['Pixel 5'] }]];
const browser = await chromium.launch();
const all = {};
for (const [mode, opts] of modes) {
  const ctx = await browser.newContext({ ...opts, colorScheme: 'dark' });
  for (const route of routes) {
    const page = await ctx.newPage();
    try {
      await page.goto(APP + route, { waitUntil: 'load', timeout: 20000 });
      await page.waitForTimeout(2000);
      const data = await page.evaluate(() => {
        const rows = [];
        const px = (v) => Math.round(parseFloat(v));
        const vis = (el) => { const r = el.getBoundingClientRect(); return r.width > 0 && r.height > 0; };
        const cls = (el) => (typeof el.className === 'string' ? el.className.trim().split(/\s+/).filter(Boolean).slice(0, 3).join('.') : '') || el.tagName.toLowerCase();
        const sel = {
          button: 'button, a.btn, [role=button]',
          input: 'input:not([type=checkbox]):not([type=radio]):not([type=hidden]):not([type=file]), select, textarea',
          label: '.field label, .field__label, legend, label',
          heading: 'h1, h2, h3, h4',
          badge: '.badge, [class*="chip"], [class*="tag"], [class*="pill"], [class*="count"]',
          text: 'p, dd, dt, td, th, li, summary',
          link: 'a:not(.btn)',
        };
        for (const [cat, s] of Object.entries(sel)) {
          for (const el of document.querySelectorAll(s)) {
            if (!vis(el)) continue;
            const cs = getComputedStyle(el); const r = el.getBoundingClientRect();
            rows.push({ cat, cls: cls(el), tag: el.tagName.toLowerCase(), h: Math.round(r.height), fs: px(cs.fontSize), fw: cs.fontWeight, pad: `${px(cs.paddingTop)}/${px(cs.paddingRight)}`, rad: px(cs.borderRadius), color: cs.color, bg: cs.backgroundColor, bc: cs.borderTopColor, bw: px(cs.borderTopWidth), ff: cs.fontFamily.split(',')[0], lh: px(cs.lineHeight), td: cs.textDecorationLine, tt: cs.textTransform, ls: cs.letterSpacing });
          }
        }
        return rows;
      });
      all[`${mode} ${route}`] = data;
    } catch (e) { all[`${mode} ${route}`] = { error: String(e).split('\n')[0] }; }
    await page.close();
  }
  await ctx.close();
}
await browser.close();
if (process.env.AUDIT_JSON) writeFileSync(process.env.AUDIT_JSON, JSON.stringify(all));
// aggregate
const agg = {};
for (const [k, rows] of Object.entries(all)) {
  if (!Array.isArray(rows)) { console.log('ERR', k, rows.error); continue; }
  const mode = k.split(' ')[0];
  for (const r of rows) {
    const key = `${mode}|${r.cat}`;
    agg[key] ??= {};
    const sig = r.cat === 'heading' ? `${r.tag} fs${r.fs} fw${r.fw} lh${r.lh}` :
      r.cat === 'button' ? `h${r.h} fs${r.fs} fw${r.fw} pad${r.pad} rad${r.rad} bw${r.bw}` :
      r.cat === 'input' ? `${r.tag} h${r.h} fs${r.fs} pad${r.pad} rad${r.rad} bg${r.bg}` :
      r.cat === 'label' ? `${r.tag} fs${r.fs} fw${r.fw} col${r.color} tt${r.tt}` :
      r.cat === 'badge' ? `fs${r.fs} rad${r.rad} pad${r.pad} bw${r.bw} tt${r.tt}` :
      r.cat === 'link' ? `fs${r.fs} col${r.color} td${r.td}` :
      `${r.tag} fs${r.fs} col${r.color} ff${r.ff}`;
    agg[key][sig] ??= { n: 0, cls: new Set(), routes: new Set() };
    agg[key][sig].n++; agg[key][sig].cls.add(r.cls); agg[key][sig].routes.add(k.split(' ')[1].split('/').slice(-1)[0] || 'overview');
  }
}
for (const [k, sigs] of Object.entries(agg)) {
  console.log(`\n### ${k}`);
  for (const [sig, v] of Object.entries(sigs).sort((a, b) => b[1].n - a[1].n)) console.log(`  ${String(v.n).padStart(4)}  ${sig}  :: ${[...v.cls].slice(0, 6).join(', ')}  @ ${[...v.routes].slice(0, 5).join(',')}`);
}
