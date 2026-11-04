(async () => {
  if (window.__t3) return; window.__t3 = 1;
  const realFetch = window.fetch.bind(window);
  const report = (k, v) => realFetch('/__spike/report', {method:'POST', body: JSON.stringify({k, v})}).catch(()=>{});
  const wait = ms => realFetch('/__spike/sleep?ms='+ms);
  const O='__ORG__',P='__PROJECT__',E='__ENV__';
  const go = async (route, ms) => { history.pushState({},'',route); window.dispatchEvent(new PopStateEvent('popstate',{state:{}})); await wait(ms); };
  const dialog = () => document.querySelector('dialog[open],[role=dialog],[role=alertdialog]');
  const secret = () => (document.body.innerText.match(/hunter2-[a-z]+|sk_test_[a-z]+/) || [null])[0];
  window.fetch = async (...a) => {
    const r = await realFetch(...a);
    if (String(a[0]).includes('/__shell/reauth')) { const t = await r.clone().text(); report('shell-reauth-response', {status: r.status, body: t.slice(0, 160), hasToken: /hik_/.test(t)}); }
    return r;
  };
  await wait(2500);
  await go(`/orgs/${O}/projects/${P}/environments/${E}/values`, 6000);
  window.__marker = 'kept-' + Math.random().toString(36).slice(2);
  const path = location.pathname;
  await report('before', {path, marker: window.__marker, secretShown: secret(), focus: document.hasFocus()});
  [...document.querySelectorAll('button')].find(b => (b.innerText || '').trim() === 'Reveal').click();
  await wait(1500);
  const dlg = dialog();
  await report('dialog', {found: !!dlg, text: dlg && dlg.innerText.slice(0, 300), buttons: dlg && [...dlg.querySelectorAll('button')].map(b => b.innerText.trim())});
  const pk = dlg && [...dlg.querySelectorAll('button')].find(b => /passkey/i.test(b.innerText));
  if (!pk) { await report('done', 'no passkey button'); return; }
  pk.click();
  await report('clicked-passkey', Date.now());
  let shown = null;
  const focusTrace = [];
  for (let i = 0; i < 25 && !shown; i++) { await wait(1000); focusTrace.push(document.hasFocus() ? 1 : 0); shown = secret(); }
  await report('focus-trace', focusTrace.join(''));
  await wait(1500);
  const w = await realFetch('/api/v1/auth/whoami');
  await report('result', {valueShown: shown, markerKept: window.__marker, samePath: location.pathname === path, focus: document.hasFocus(),
    dialogOpen: !!dialog(), alerts: [...document.querySelectorAll('[role=alert],[role=status]')].map(x => x.innerText.slice(0, 140)).filter(Boolean), whoami: w.status});
  // Give a person (or the test harness) time to bring the window back, then see whether the gate lets go.
  await wait(10000);
  await report('after-return', {valueShown: secret(), focus: document.hasFocus(), alerts: [...document.querySelectorAll('[role=alert],[role=status]')].map(x => x.innerText.slice(0, 80)).filter(x => !/Datastore|escrow/.test(x))});
  await report('done', 1);
})();
