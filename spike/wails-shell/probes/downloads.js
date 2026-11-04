(async () => {
  if (window.__t3 || location.host !== 'conn-0') return; window.__t3 = 1;
  const realFetch = window.fetch.bind(window);
  const report = (k, v) => realFetch('/__spike/report', {method:'POST', body: JSON.stringify({k, v})}).catch(()=>{});
  const wait = ms => realFetch('/__spike/sleep?ms='+ms);
  const O='__ORG__',P='__PROJECT__',OTHER='wails://conn-1/';
  const go = async (route, ms) => { history.pushState({},'',route); window.dispatchEvent(new PopStateEvent('popstate',{state:{}})); await wait(ms); };
  const click = (el) => { document.body.append(el); el.click(); };
  await wait(2500);

  // 1. <a download> to a wails:// API URL
  const a1 = Object.assign(document.createElement('a'), {href:'/api/v1/auth/whoami', download:'anchor-api.json'});
  click(a1); await wait(3000); await report('anchor-api', {href: location.href});

  // 2. blob <a download>, 1 MB, object URL kept
  const text = 'x'.repeat(1000000);
  const u2 = URL.createObjectURL(new Blob([text], {type:'text/plain'}));
  click(Object.assign(document.createElement('a'), {href:u2, download:'blob-kept.txt'})); await wait(3000); await report('blob-kept', {url:u2});

  // 2b. blob revoked in the next task, as Audit.tsx does
  const u3 = URL.createObjectURL(new Blob([text], {type:'text/plain'}));
  click(Object.assign(document.createElement('a'), {href:u3, download:'blob-revoked.txt'})); setTimeout(() => URL.revokeObjectURL(u3), 0); await wait(3000); await report('blob-revoked', {url:u3});

  // 3. the SPA's real definitions export buttons
  await go(`/orgs/${O}/projects/${P}/settings`, 6000);
  for (const label of ['Download definitions bundle', 'Download portable bundle']) {
    const el = [...document.querySelectorAll('a.btn')].find(x => x.innerText.trim() === label);
    await report('export-button', {label, found: !!el, href: el && el.getAttribute('href'), download: el && el.getAttribute('download')});
    if (el) { el.click(); await wait(3500); }
  }

  // 4. the audit Export button
  await go(`/orgs/${O}/projects/${P}/audit`, 6000);
  const exp = [...document.querySelectorAll('button')].find(b => /export/i.test(b.innerText));
  await report('audit-button', {found: !!exp, text: exp && exp.innerText.trim()});
  if (exp) { exp.click(); await wait(6000); await report('audit-after', {alerts: [...document.querySelectorAll('[role=alert],[role=status]')].map(x=>x.innerText.slice(0,120))}); }

  // 5. window.open, target=_blank, location.href to https
  await report('window.open', String(window.open('https://example.com/?probe=window-open')));
  await wait(1000);
  click(Object.assign(document.createElement('a'), {href:'https://example.com/?probe=blank', target:'_blank'})); await wait(1500);
  click(Object.assign(document.createElement('a'), {href:'https://example.com/?probe=same-frame'})); await wait(1500);
  await report('after-anchors', {href: location.href});
  location.href = 'https://example.com/?probe=location-href'; await wait(2000);
  await report('after-location-https', {href: location.href});

  // 6. the other connection, and hostile schemes
  location.href = OTHER; await wait(2000);
  await report('after-location-other', {href: location.href});
  location.href = 'javascript:void(0)'; location.href = 'file:///etc/hosts'; await wait(1000);
  click(Object.assign(document.createElement('a'), {href:'mailto:x@example.com'})); await wait(500);
  await report('after-hostile', {href: location.href});
  const w = await realFetch('/api/v1/auth/whoami'); await report('whoami-after', w.status);
  await report('done', 1);
})();
