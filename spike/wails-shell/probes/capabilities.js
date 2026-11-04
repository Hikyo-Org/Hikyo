(async () => {
  if (window.__probed) return; window.__probed = true;
  const report = (k, v) => fetch('/__spike/report', {method:'POST', body: JSON.stringify({k, v})}).catch(e => console.log(e));
  await report('env', {href: location.href, origin: location.origin, secure: window.isSecureContext,
    subtle: typeof crypto.subtle, credentials: typeof navigator.credentials, clipboard: typeof navigator.clipboard,
    cookie: document.cookie, ua: navigator.userAgent, localStorage: (()=>{try{localStorage.setItem('x','1');return localStorage.getItem('x')}catch(e){return String(e)}})(),
    broadcast: typeof BroadcastChannel, wails: typeof window._wails, wkhandler: typeof window.webkit?.messageHandlers?.external});
  const w = await fetch('/api/v1/auth/whoami'); await report('whoami', {status: w.status, artifact: (await w.json()).session?.artifact});
  const rt = await fetch('/wails/runtime.js'); await report('wails-runtime', rt.status);
  try { const x = await fetch('wails://conn-9/api/v1/auth/whoami'); await report('cross', x.status);} catch(e){ await report('cross-err', String(e)); }
  const post = await fetch('/api/v1/auth/totp/enrol/start', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({password:'wrong'})}); await report('post-with-body', {status: post.status, body:(await post.text()).slice(0,160)});
  setTimeout(() => report('dom', {title: document.title, path: location.pathname, text: document.body.innerText.slice(0,300), scripts: document.scripts.length}), 2500);
})();
