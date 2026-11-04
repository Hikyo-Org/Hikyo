
(async () => {
  if (window.__t3) return; window.__t3=1;
  if (location.host==='conn-1') { const rf=window.fetch.bind(window); await rf('/__spike/sleep?ms=8000'); rf('/__spike/report',{method:'POST',body:JSON.stringify({k:'conn1-navigating',v:location.href})}); location.href='wails://conn-0/projects'; return; }
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
  await wait(62000);
  await report('route',await go('/orgs/'+O+'/projects/'+P+'/environments/'+E+'/values',4000));
  const rv=[...document.querySelectorAll('button')].find(b=>(b.innerText||'').trim()==='Reveal'); rv.click(); await wait(1500);
  const dlg=document.querySelector('dialog[open],[role=dialog],[role=alertdialog]');
  await report('reveal-dialog',{found:!!dlg,text:(dlg||{}).innerText?.slice(0,420),inputs:dlg?[...dlg.querySelectorAll('input')].map(i=>i.type+':'+(i.autocomplete||'')+':'+(i.getAttribute('aria-label')||i.name||'')):[],buttons:dlg?[...dlg.querySelectorAll('button')].map(b=>b.innerText.slice(0,40)):[]});
  const chooser=[...(dlg||document).querySelectorAll('button')].find(b=>/authenticator|code|totp/i.test(b.innerText)); if(chooser){ chooser.click(); await wait(1000); await report('reveal-chose',chooser.innerText.slice(0,40)); }
  const dlg2=document.querySelector('dialog[open],[role=dialog],[role=alertdialog]')||document;
  const input=[...dlg2.querySelectorAll('input')].find(i=>i.type!=='hidden'&&i.type!=='checkbox');
  if (input) { const set=Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set; set.call(input,await totp()); input.dispatchEvent(new Event('input',{bubbles:true})); await wait(500);
     const submit=[...dlg2.querySelectorAll('button')].filter(b=>!/cancel|close|back/i.test(b.innerText)).pop(); await report('reveal-submit',submit&&submit.innerText.slice(0,40)); submit&&submit.click(); await wait(4000); }
  await wait(3000);
  const body=document.body.innerText;
  await report('focus-at-result',{hasFocus:document.hasFocus(),vis:document.visibilityState});
  await report('reveal-result',{valueVisible:body.includes('spike-secret-value'),dialogOpen:!!document.querySelector('dialog[open],[role=dialog],[role=alertdialog]'),alerts:[...document.querySelectorAll('[role=alert]')].map(x=>x.innerText.slice(0,120)).filter(Boolean).filter(x=>!/Datastore volume|root escrow/.test(x)),recorded:(body.match(/Recorded this session[\s\S]{0,200}/)||[''])[0].replace(/\n+/g,' ')});
  const w=await realFetch('/api/v1/auth/whoami'); await report('whoami-after',w.status);
  await wait(1000);
  await report('after-instance',{visible:document.body.innerText.includes('Instance settings')});
  await report('done',1);
})();
