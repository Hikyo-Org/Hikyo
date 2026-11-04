
(async () => {
  if (sessionStorage.getItem('t2')) { await fetch('/__spike/report',{method:'POST',body:JSON.stringify({k:'landed-again',v:location.href})}); return; }
  sessionStorage.setItem('t2','1');
  const realFetch = window.fetch.bind(window);
  const report = (k, v) => realFetch('/__spike/report', {method:'POST', body: JSON.stringify({k, v})}).catch(()=>{});
  const wait = ms => realFetch('/__spike/sleep?ms='+ms);
  const csp=[]; document.addEventListener('securitypolicyviolation', e => csp.push(e.violatedDirective+' '+e.blockedURI));
  const O='__ORG__',P='__PROJECT__',E='__ENV__',S='__TOTP_SECRET__';
  const go = async (route, ms) => { history.pushState({},'',route); window.dispatchEvent(new PopStateEvent('popstate',{state:{}})); await wait(ms);
     const main=document.querySelector('main')||document.body;
     return {route:route.replace(O,':o').replace(P,':p').replace(E,':e'), h:[...main.querySelectorAll('h1,h2')].map(x=>x.innerText).slice(0,1), alerts:[...document.querySelectorAll('[role=alert]')].map(x=>x.innerText.slice(0,90)).filter(Boolean).filter(x=>!/Datastore volume|root escrow/.test(x))}; };
  const totp = async () => { const al='ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'; let bits=''; for (const c of S) bits+=al.indexOf(c).toString(2).padStart(5,'0');
     const kb=new Uint8Array(Math.floor(bits.length/8)); for(let i=0;i<kb.length;i++) kb[i]=parseInt(bits.slice(i*8,i*8+8),2);
     const key=await crypto.subtle.importKey('raw',kb,{name:'HMAC',hash:'SHA-1'},false,['sign']); const c=new DataView(new ArrayBuffer(8)); c.setUint32(4,Math.floor(Date.now()/30000));
     const d=new Uint8Array(await crypto.subtle.sign('HMAC',key,c.buffer)); const o=d[19]&15; return String(((d[o]&127)<<24|d[o+1]<<16|d[o+2]<<8|d[o+3])%1000000).padStart(6,'0'); };
  if (location.host==='conn-1') {
    await wait(9000);
    await report('iso-conn1-storage',{ls:localStorage.getItem('spike-conn0'),ss:sessionStorage.getItem('t2')});
    await wait(25000);
    location.href='wails://conn-0/projects';
    return;
  }
  await wait(2500);
  localStorage.setItem('spike-conn0','secret-from-conn-0');
  // SSE
  const ac=new AbortController(); const t0=Date.now();
  (async()=>{ try { const r=await realFetch('/api/v1/orgs/'+O+'/projects/'+P+'/events',{headers:{Accept:'text/event-stream; hikyo-topology=1'},signal:ac.signal}); await report('sse-open',{status:r.status,type:r.headers.get('content-type')});
     const rd=r.body.getReader(); const dec=new TextDecoder(); for(;;){ const {value,done}=await rd.read(); if(done){await report('sse-end',Date.now()-t0);break;} await report('sse-chunk',{ms:Date.now()-t0,at:new Date().toISOString(),text:dec.decode(value).slice(0,140)}); } } catch(e){ await report('sse-err',String(e)); } })();
  // webauthn
  try { await report('webauthn-uvpa', await PublicKeyCredential.isUserVerifyingPlatformAuthenticatorAvailable()); } catch(e){ await report('webauthn-uvpa-err',String(e)); }
  try { const c=await Promise.race([navigator.credentials.get({publicKey:{challenge:crypto.getRandomValues(new Uint8Array(32)),rpId:'localhost',userVerification:'preferred'}}),realFetch('/__spike/sleep?ms=4000').then(()=>'timeout')]); await report('webauthn-get',String(c)); } catch(e){ await report('webauthn-get-err',e.name+': '+e.message); }
  try { const c=await Promise.race([navigator.credentials.get({publicKey:{challenge:crypto.getRandomValues(new Uint8Array(32)),userVerification:'preferred'}}),realFetch('/__spike/sleep?ms=4000').then(()=>'timeout')]); await report('webauthn-get-norp',String(c)); } catch(e){ await report('webauthn-get-norp-err',e.name+': '+e.message); }
  // clipboard
  try { await navigator.clipboard.writeText('hikyo-spike-clip'); await report('clip-write','ok'); } catch(e){ await report('clip-write-err',e.name+': '+e.message); }
  try { const t=await navigator.clipboard.readText(); await report('clip-read',t.slice(0,20)==='hikyo-spike-clip'?'match':'other'); } catch(e){ await report('clip-read-err',e.name+': '+e.message); }
  // popups and isolation
  try { const w=window.open('about:blank','_blank','popup=yes'); await report('window-open',w===null?'null':'window'); if(w) w.close(); } catch(e){ await report('window-open-err',String(e)); }
  try { const x=await realFetch('wails://conn-1/api/v1/auth/whoami'); await report('iso-fetch',x.status); } catch(e){ await report('iso-fetch-err',String(e)); }
  await wait(300);
  await new Promise(res=>{ const i=new Image(); i.onload=()=>{report('iso-img','loaded');res()}; i.onerror=()=>{report('iso-img','blocked');res()}; i.src='wails://conn-1/favicon.svg'; realFetch('/__spike/sleep?ms=1500').then(res); });
  try { const x=await realFetch('https://example.com/'); await report('external-fetch',x.status); } catch(e){ await report('external-fetch-err',String(e)); }
  await report('csp',csp.slice());
  try { const d=await realFetch('/wails/runtime',{method:'POST',body:'{}'}); await report('wails-post',d.status); } catch(e){ await report('wails-post-err',String(e)); }
  await report('wails-global',{wails:Object.keys(window._wails||{}),coi:window.crossOriginIsolated});
  // rotation through the proxy
  const before=await (await realFetch('/api/v1/auth/whoami')).json();
  await wait(31000);
  const su=await realFetch('/api/v1/auth/totp/step-up',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({code:await totp()})}); const st=await su.text();
  await report('rotate-step-up',{status:su.status,hasToken:st.includes('session_token'),leak:/hik_1_/.test(st),keys:Object.keys(JSON.parse(st)),detail:su.ok?'':st.slice(0,200)});
  const after=await realFetch('/api/v1/auth/whoami'); await report('rotate-after',{status:after.status,sameSession:(await after.json()).session?.id===before.session?.id});
  // instance routes with a refilled budget
  for (const r of ['/instance','/instance/config','/instance/audit','/instance/members','/settings']) { await wait(18000); await report('route2',await go(r,4000)); }
  // reveal in the SPA
  await wait(5000);
  await report('values',await go('/orgs/'+O+'/projects/'+P+'/environments/'+E+'/values',4000));
  const btns=[...document.querySelectorAll('button')].map(b=>(b.innerText||b.getAttribute('aria-label')||'').slice(0,40)).filter(Boolean); await report('values-buttons',btns.slice(0,30));
  const rv=[...document.querySelectorAll('button')].find(b=>/reveal/i.test(b.innerText+' '+(b.getAttribute('aria-label')||'')));
  if (rv) { rv.click(); await wait(2500); await report('reveal-ui',{dialog:(document.querySelector('[role=dialog]')||document.querySelector('main')).innerText.slice(0,500),buttons:[...document.querySelectorAll('[role=dialog] button')].map(b=>b.innerText.slice(0,40))}); }
  const win=await realFetch('/api/v1/orgs/'+O+'/projects/'+P+'/environments/'+E+'/reveal-window'); await report('reveal-window',{status:win.status,body:(await win.text()).slice(0,240)});
  // download paths
  const a=document.createElement('a'); a.href='/api/v1/orgs/'+O+'/projects/'+P+'/definitions/export'; a.download='spike-definitions.json'; document.body.appendChild(a); a.click(); await wait(1500); await report('dl-anchor',{href:location.href.replace(O,':o').replace(P,':p').replace(E,':e')});
  const u=URL.createObjectURL(new Blob(['spike'],{type:'text/plain'})); const b=document.createElement('a'); b.href=u; b.download='spike-blob.txt'; document.body.appendChild(b); b.click(); await wait(1500); await report('dl-blob',{href:location.href.replace(O,':o').replace(P,':p').replace(E,':e')});
  // sign-in inside the window: what a dead session would fall back to
  const li=await realFetch('/api/v1/auth/local/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({username:'spike',password:'wrong-password-probe',artifact:'browser'})}); await report('login-in-window-wrongpw',li.status);
  await report('done',1); ac.abort();
})();
