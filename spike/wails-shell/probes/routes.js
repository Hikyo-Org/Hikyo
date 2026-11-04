(async () => {
  if (window.__probed) return; window.__probed = true;
  const realFetch = window.fetch.bind(window);
  const report = (k, v) => realFetch('/__spike/report', {method:'POST', body: JSON.stringify({k, v})}).catch(()=>{});
  const failures = [];
  window.fetch = async (...a) => { const r = await realFetch(...a); if (!r.ok) failures.push(r.status+' '+(a[0].url||a[0])); return r; };
  const csp = []; document.addEventListener('securitypolicyviolation', e => csp.push(e.violatedDirective+' '+e.blockedURI));
  const errs = []; const ce = console.error; console.error = (...a) => { errs.push(a.map(String).join(' ').slice(0,160)); ce(...a); };
  window.addEventListener('error', e => errs.push('error: '+e.message)); window.addEventListener('unhandledrejection', e => errs.push('rejection: '+String(e.reason).slice(0,160)));
  const o='__ORG__',p='__PROJECT__',e='__ENV__';
  const P=`/orgs/${o}/projects/${p}`;
  const routes=['/','/projects','/remotes',`/orgs/${o}/members`,`/orgs/${o}/settings`,`/orgs/${o}/scim`,`/orgs/${o}/audit`,'/instance','/instance/config','/instance/audit','/instance/members','/settings',
    `${P}/matrix`,`${P}/matrix/history`,`${P}/matrix/keys/SPIKE_SECRET`,`${P}/environments/${e}/values`,`${P}/machine-access`,`${P}/transit-keys`,`${P}/change-approvals`,`${P}/temporary-access`,`${P}/adapters`,`${P}/audit`,`${P}/settings`];
  await realFetch('/__spike/sleep?ms=1500');
  for (const route of routes) {
    failures.length=0; csp.length=0; errs.length=0;
    history.pushState({},'',route); window.dispatchEvent(new PopStateEvent('popstate',{state:{}}));
    await realFetch('/__spike/sleep?ms=5000');
    const main=document.querySelector('main')||document.body;
    const h=[...main.querySelectorAll('h1,h2')].map(x=>x.innerText).slice(0,2);
    const alerts=[...document.querySelectorAll('[role=alert]')].map(x=>x.innerText.slice(0,120)).filter(Boolean).filter(x=>!/Datastore volume|root escrow/.test(x));
    await report('route',{route:route.replace(o,':org').replace(p,':project').replace(e,':env'),at:location.pathname===route?'ok':location.pathname,h,alerts,failures:[...failures],csp:[...csp],errs:[...errs]});
  }
  await report('done',1);
})();
