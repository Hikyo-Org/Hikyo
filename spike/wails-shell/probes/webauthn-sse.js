
(async () => {
  if (window.__t3) return; window.__t3=1;
  const realFetch = window.fetch.bind(window);
  const report = (k, v) => realFetch('/__spike/report', {method:'POST', body: JSON.stringify({k, v})}).catch(()=>{});
  const wait = ms => realFetch('/__spike/sleep?ms='+ms);
  const O='__ORG__',P='__PROJECT__',E='__ENV__',S='__TOTP_SECRET__';
  const go = async (route, ms) => { history.pushState({},'',route); window.dispatchEvent(new PopStateEvent('popstate',{state:{}})); await wait(ms);
     const main=document.querySelector('main')||document.body;
     return {route:route.replace(O,':o').replace(P,':p').replace(E,':e'), h:[...main.querySelectorAll('h1,h2')].map(x=>x.innerText).slice(0,1), alerts:[...document.querySelectorAll('[role=alert]')].map(x=>x.innerText.slice(0,90)).filter(Boolean).filter(x=>!/Datastore volume|root escrow/.test(x))}; };
  const totp = async () => { const al='ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'; let bits=''; for (const c of S) bits+=al.indexOf(c).toString(2).padStart(5,'0');
     const kb=new Uint8Array(Math.floor(bits.length/8)); for(let i=0;i<kb.length;i++) kb[i]=parseInt(bits.slice(i*8,i*8+8),2);
     const key=await crypto.subtle.importKey('raw',kb,{name:'HMAC',hash:'SHA-1'},false,['sign']); const c=new DataView(new ArrayBuffer(8)); c.setUint32(4,Math.floor(Date.now()/30000));
     const d=new Uint8Array(await crypto.subtle.sign('HMAC',key,c.buffer)); const o=d[19]&15; return String(((d[o]&127)<<24|d[o+1]<<16|d[o+2]<<8|d[o+3])%1000000).padStart(6,'0'); };
  await wait(2500);
  await report('focus',{hasFocus:document.hasFocus(),vis:document.visibilityState});
  try { await report('webauthn-uvpa', await PublicKeyCredential.isUserVerifyingPlatformAuthenticatorAvailable()); } catch(e){ await report('webauthn-uvpa-err',String(e)); }
  for (const [name,opts] of [['rp-localhost',{rpId:'localhost'}],['rp-conn',{rpId:'conn-0'}],['rp-default',{}]]) {
    try { const c=await Promise.race([navigator.credentials.get({publicKey:Object.assign({challenge:crypto.getRandomValues(new Uint8Array(32)),userVerification:'preferred'},opts)}),realFetch('/__spike/sleep?ms=3000').then(()=>'still-pending')]); await report('webauthn-'+name,String(c)); } catch(e){ await report('webauthn-'+name+'-err',e.name+': '+e.message); }
  }
  (async()=>{ const r=await realFetch('/api/v1/orgs/'+O+'/projects/'+P+'/events',{headers:{Accept:'text/event-stream; hikyo-topology=1'}});
     const rd=r.body.getReader(); const dec=new TextDecoder(); for(;;){ const {value,done}=await rd.read(); if(done)break; await report('sse',{at:new Date().toISOString(),hasFocus:document.hasFocus(),text:dec.decode(value).replace(/data:.*/g,'').replace(/\n+/g,' ').slice(0,90)}); } })();
  await wait(30000);
  await report('route',await go('/instance',4000));
  await wait(25000);
  await report('route',await go('/orgs/'+O+'/projects/'+P+'/environments/'+E+'/values',4000));
  const rv=[...document.querySelectorAll('button')].find(b=>(b.innerText||'').trim()==='Reveal'); rv.click(); await wait(1500);
  const dlg=document.querySelector('dialog[open],[role=dialog],[role=alertdialog]');
  await report('reveal-dialog',{found:!!dlg,text:(dlg||{}).innerText?.slice(0,420),inputs:dlg?[...dlg.querySelectorAll('input')].map(i=>i.type+':'+(i.autocomplete||'')+':'+(i.getAttribute('aria-label')||i.name||'')):[],buttons:dlg?[...dlg.querySelectorAll('button')].map(b=>b.innerText.slice(0,40)):[]});
  const chooser=[...(dlg||document).querySelectorAll('button')].find(b=>/authenticator|code|totp/i.test(b.innerText)); if(chooser){ chooser.click(); await wait(1000); await report('reveal-chose',chooser.innerText.slice(0,40)); }
  const dlg2=document.querySelector('dialog[open],[role=dialog],[role=alertdialog]')||document;
  const input=[...dlg2.querySelectorAll('input')].find(i=>i.type!=='hidden'&&i.type!=='checkbox');
  if (input) { const set=Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set; set.call(input,await totp()); input.dispatchEvent(new Event('input',{bubbles:true})); await wait(500);
     const submit=[...dlg2.querySelectorAll('button')].filter(b=>!/cancel|close|back/i.test(b.innerText)).pop(); await report('reveal-submit',submit&&submit.innerText.slice(0,40)); submit&&submit.click(); await wait(4000); }
  const body=document.body.innerText;
  await report('reveal-result',{valueVisible:body.includes('spike-secret-value'),dialogOpen:!!document.querySelector('dialog[open],[role=dialog],[role=alertdialog]'),alerts:[...document.querySelectorAll('[role=alert]')].map(x=>x.innerText.slice(0,120)).filter(Boolean).filter(x=>!/Datastore volume|root escrow/.test(x)),recorded:(body.match(/Recorded this session[\s\S]{0,200}/)||[''])[0].replace(/\n+/g,' ')});
  const w=await realFetch('/api/v1/auth/whoami'); await report('whoami-after',w.status);
  await report('done',1);
})();
