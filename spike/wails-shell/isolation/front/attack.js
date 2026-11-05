// Attack page, served by the attacker connection's own front with no CSP, as a hostile instance would.
// It works unchanged on wails://conn-N and https://conn-N.wails.localhost. Attacker conn-0 and victim conn-1 unless window.RIG says otherwise.
(async () => {
  const q = new URLSearchParams(location.search);
  for (const [k, v] of Object.entries(window.RIG || {})) if (!q.has(k)) q.set(k, v);
  const out = document.getElementById('o');
  const swapped = location.host.replace(/conn-(\d+)/, (_, n) => 'conn-' + (n === '0' ? '1' : '0'));
  const victimHost = q.get('vh') || swapped;
  const victim = location.protocol + '//' + victimHost;
  const direct = q.get('direct') || 'http://127.0.0.1:18200'; // attacker front reached without the proxy
  const run = q.get('run') || ('r' + Date.now());
  const phase = q.get('phase') || 'sub';
  const report = (v, r) => { out.append(`${v}: ${r}\n`); return fetch(`/report?run=${run}&v=${encodeURIComponent(v)}&r=${encodeURIComponent(r)}`).catch(() => {}); };
  const sleep = (ms) => new Promise((res) => setTimeout(res, ms));
  const V = (name, kind, extra = '') => `${victim}/v/${name}?kind=${kind}&run=${run}${extra}`;
  const el = (tag, attrs, parent = document.body) => { const e = document.createElement(tag); Object.assign(e, attrs); parent.append(e); return e; };
  const watch = (name, e) => { e.onload = () => report(name, 'onload'); e.onerror = () => report(name, 'onerror'); return e; };
  const hiddenFrame = (name) => { const f = el('iframe', { name, style: 'display:none' }); return f; };
  // A GET form replaces the query, so the action's own parameters travel as fields.
  const submit = (action, method, target) => { const f = el('form', { action, method, target }); el('input', { name: 'x', value: '1', type: 'hidden' }, f); for (const [k, v] of new URL(action).searchParams) el('input', { name: k, value: v, type: 'hidden' }, f); f.submit(); };

  // Gives native setup time on the first load only: later loads carry their own query.
  if (q.get('wait') && !location.search) await sleep(Number(q.get('wait')));
  report('page', `host=${location.host} secure=${isSecureContext} sw=${!!navigator.serviceWorker} shared=${typeof SharedWorker}`);

  const sub = {
    'img': () => watch('img', el('img', { src: V('img', 'img') })),
    'img-noreferrer': () => { const i = el('img', { referrerPolicy: 'no-referrer' }); watch('img-noreferrer', i).src = V('img-noreferrer', 'img'); },
    'img-cors-attr': () => { const i = el('img', { crossOrigin: 'anonymous' }); watch('img-cors-attr', i).src = V('img-cors-attr', 'img'); },
    'script': () => watch('script', el('script', { src: V('script', 'js') })),
    'css-url': () => { el('style', { textContent: `#u{background:url(${V('css-url', 'img')})}` }, document.head); el('div', { id: 'u', textContent: '.' }); },
    'css-import': () => el('style', { textContent: `@import url(${V('css-import', 'css')});` }, document.head),
    'font': () => { const s = el('style', { textContent: `@font-face{font-family:fx;src:url(${V('font', 'img')})}#f{font-family:fx}` }, document.head); el('div', { id: 'f', textContent: 'x' }); },
    'link-stylesheet': () => watch('link-stylesheet', el('link', { rel: 'stylesheet', href: V('link-stylesheet', 'css') }, document.head)),
    'link-prefetch': () => watch('link-prefetch', el('link', { rel: 'prefetch', href: V('link-prefetch', 'img') }, document.head)),
    'link-preload': () => watch('link-preload', el('link', { rel: 'preload', as: 'image', href: V('link-preload', 'img') }, document.head)),
    'link-modulepreload': () => watch('link-modulepreload', el('link', { rel: 'modulepreload', href: V('link-modulepreload', 'js') }, document.head)),
    'fetch-no-cors': () => fetch(V('fetch-no-cors', 'img'), { mode: 'no-cors' }).then((r) => report('fetch-no-cors', 'resolved-' + r.type), (e) => report('fetch-no-cors', 'rejected ' + e)),
    'fetch-no-cors-noreferrer': () => fetch(V('fetch-no-cors-noreferrer', 'img'), { mode: 'no-cors', referrerPolicy: 'no-referrer' }).then((r) => report('fetch-no-cors-noreferrer', 'resolved-' + r.type), (e) => report('fetch-no-cors-noreferrer', 'rejected ' + e)),
    'fetch-keepalive': () => fetch(V('fetch-keepalive', 'img'), { mode: 'no-cors', keepalive: true }).then((r) => report('fetch-keepalive', 'resolved-' + r.type), (e) => report('fetch-keepalive', 'rejected ' + e)),
    'fetch-cors': () => fetch(V('fetch-cors', 'img')).then((r) => report('fetch-cors', 'resolved-' + r.status), (e) => report('fetch-cors', 'rejected ' + e)),
    'fetch-post-no-cors': () => fetch(V('fetch-post-no-cors', 'img'), { method: 'POST', mode: 'no-cors', body: 'x=1' }).then((r) => report('fetch-post-no-cors', 'resolved-' + r.type), (e) => report('fetch-post-no-cors', 'rejected ' + e)),
    'xhr': () => { const x = new XMLHttpRequest(); x.open('GET', V('xhr', 'img')); x.onload = () => report('xhr', 'onload ' + x.status); x.onerror = () => report('xhr', 'onerror'); x.send(); },
    'beacon': () => report('beacon', 'queued=' + navigator.sendBeacon(V('beacon', 'img'), 'x=1')),
    'eventsource': () => { const s = new EventSource(V('eventsource', 'img')); s.onopen = () => { report('eventsource', 'open'); s.close(); }; s.onerror = () => { report('eventsource', 'error'); s.close(); }; },
    'import-dynamic': () => import(V('import-dynamic', 'js')).then(() => report('import-dynamic', 'loaded'), (e) => report('import-dynamic', 'rejected ' + e)),
    'worker-fetch': () => new Worker(`/worker.js?victim=${encodeURIComponent(victim)}&v=worker-fetch&run=${run}`),
    'sharedworker-fetch': () => { if (typeof SharedWorker === 'undefined') return report('sharedworker-fetch', 'no SharedWorker'); return new SharedWorker(`/worker.js?victim=${encodeURIComponent(victim)}&v=sharedworker-fetch&run=${run}`); },
    'worker-cross-origin': () => { try { new Worker(V('worker-cross-origin', 'js')); report('worker-cross-origin', 'constructed'); } catch (e) { report('worker-cross-origin', 'threw ' + e.name); } },
    'sw-own': () => { if (!navigator.serviceWorker) return report('sw-own', 'no navigator.serviceWorker'); return navigator.serviceWorker.register(`/sw.js?victim=${encodeURIComponent(victim)}&run=${run}`).then((r) => report('sw-own', 'registered scope=' + r.scope), (e) => report('sw-own', 'rejected ' + e)); },
    'sw-cross-origin': () => { if (!navigator.serviceWorker) return report('sw-cross-origin', 'no navigator.serviceWorker'); return navigator.serviceWorker.register(V('sw-cross-origin', 'js')).then(() => report('sw-cross-origin', 'registered'), (e) => report('sw-cross-origin', 'rejected ' + e.name)); },
    'websocket': () => { try { const s = new WebSocket(`${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${victimHost}/v/websocket?run=${run}`); s.onopen = () => report('websocket', 'open'); s.onerror = () => report('websocket', 'error'); } catch (e) { report('websocket', 'threw ' + e.name); } },
    'video': () => { const v = el('video', { preload: 'auto' }); v.onerror = () => report('video', 'onerror'); v.src = V('video', 'img'); },
    'audio': () => { const v = el('audio', { preload: 'auto' }); v.onerror = () => report('audio', 'onerror'); v.src = V('audio', 'img'); },
    'object': () => watch('object', el('object', { data: V('object', 'doc') })),
    'embed': () => el('embed', { src: V('embed', 'doc') }),
    'svg-image': () => { document.body.insertAdjacentHTML('beforeend', `<svg width=1 height=1><image href="${V('svg-image', 'img')}"/></svg>`); },
    'input-image': () => el('input', { type: 'image', src: V('input-image', 'img') }),
    'guess-unknown-host': () => watch('guess-unknown-host', el('img', { src: `${location.protocol}//${victimHost.replace(/conn-\d+/, 'conn-7')}/v/guess-unknown-host?kind=img&run=${run}` })),
    'iframe-attach': () => el('iframe', { src: V('iframe-attach', 'attach'), style: 'display:none' }),
    'iframe': () => watch('iframe', el('iframe', { src: V('iframe', 'doc') })),
    'iframe-sandbox': () => { const f = document.createElement('iframe'); f.setAttribute('sandbox', 'allow-scripts'); f.src = V('iframe-sandbox', 'doc'); document.body.append(watch('iframe-sandbox', f)); },
    'iframe-sandbox-post': () => { const f = document.createElement('iframe'); f.setAttribute('sandbox', 'allow-scripts allow-forms'); f.srcdoc = `<form id=g method=post action="${V('iframe-sandbox-post', 'doc')}"><input name=x value=1></form><script>g.submit()<\/script>`; document.body.append(f); },
    'iframe-hard': () => watch('iframe-hard', el('iframe', { src: V('iframe-hard', 'doc', '&hard=1') })),
    'form-get-iframe': () => { hiddenFrame('fg'); submit(V('form-get-iframe', 'doc'), 'get', 'fg'); },
    'form-post-iframe': () => { hiddenFrame('fp'); submit(V('form-post-iframe', 'doc'), 'post', 'fp'); },
    'a-ping': () => { hiddenFrame('pf'); const a = el('a', { href: '/report?v=a-ping-nav&r=clicked&run=' + run, target: 'pf', textContent: 'p' }); a.setAttribute('ping', V('a-ping', 'img')); a.click(); },
    'a-download': () => { const a = el('a', { href: V('a-download', 'img'), download: 'x.gif', textContent: 'd' }); a.click(); },
    'window-open': () => { const w = window.open(V('window-open', 'doc'), 'w1'); report('window-open', 'returned ' + (w === null ? 'null' : 'window')); },
    'a-blank': () => { const a = el('a', { href: V('a-blank', 'doc'), target: '_blank', textContent: 'b' }); a.click(); },
    'redir-own-proxy': () => watch('redir-own-proxy', el('img', { src: '/redir?to=' + encodeURIComponent(V('redir-own-proxy', 'img')) })),
    'redir-direct': () => watch('redir-direct', el('img', { src: direct + '/redir?to=' + encodeURIComponent(V('redir-direct', 'img')) })),
    'redir-direct-iframe': () => watch('redir-direct-iframe', el('iframe', { src: direct + '/redir?to=' + encodeURIComponent(V('redir-direct-iframe', 'doc')) })),
    'redir-tls-img': () => watch('redir-tls-img', el('img', { src: 'https://127.0.0.1:18203/redir?to=' + encodeURIComponent(V('redir-tls-img', 'img')) })),
    'redir-tls-fetch': () => fetch('https://127.0.0.1:18203/redir?to=' + encodeURIComponent(V('redir-tls-fetch', 'img')), { mode: 'no-cors' }).then((r) => report('redir-tls-fetch', 'resolved-' + r.type), (e) => report('redir-tls-fetch', 'rejected ' + e)),
    'direct-tls-img': () => watch('direct-tls-img', el('img', { src: `https://127.0.0.1:18203/v/direct-tls-img?kind=img&run=${run}` })),
    'direct-img': () => watch('direct-img', el('img', { src: direct + '/v/direct-img?kind=img&run=' + run })),
    'direct-fetch': () => fetch(direct + `/v/direct-fetch?kind=img&run=${run}`, { mode: 'no-cors' }).then((r) => report('direct-fetch', 'resolved-' + r.type), (e) => report('direct-fetch', 'rejected ' + e)),
    'oracle-img-200': () => watch('oracle-img-200', el('img', { src: V('oracle-img-200', 'img') })),
    'oracle-img-404': () => watch('oracle-img-404', el('img', { src: V('oracle-img-404', 'img', '&status=404') })),
    'oracle-script-200': () => watch('oracle-script-200', el('script', { src: V('oracle-script-200', 'js') })),
    'oracle-script-404': () => watch('oracle-script-404', el('script', { src: V('oracle-script-404', 'js', '&status=404') })),
    'oracle-hard-img-200': () => watch('oracle-hard-img-200', el('img', { src: V('oracle-hard-img-200', 'img', '&hard=1') })),
    'oracle-hard-img-404': () => watch('oracle-hard-img-404', el('img', { src: V('oracle-hard-img-404', 'img', '&hard=1&status=404') })),
    'oracle-hard-script-200': () => watch('oracle-hard-script-200', el('script', { src: V('oracle-hard-script-200', 'js', '&hard=1') })),
    'oracle-hard-script-404': () => watch('oracle-hard-script-404', el('script', { src: V('oracle-hard-script-404', 'js', '&hard=1&status=404') })),
    'spoof-window-id-own': () => fetch(`/v/spoof-window-id-own?kind=img&run=${run}`, { headers: { 'x-wails-window-id': '9999' } }).then((r) => report('spoof-window-id-own', 'status ' + r.status), (e) => report('spoof-window-id-own', 'rejected ' + e)),
    'spoof-window-id-cross': () => fetch(V('spoof-window-id-cross', 'img'), { headers: { 'x-wails-window-id': '2' } }).then((r) => report('spoof-window-id-cross', 'status ' + r.status), (e) => report('spoof-window-id-cross', 'rejected ' + e)),
    'csp-trigger': () => el('img', { src: '/v/csp-trigger?kind=img&run=' + run }),
  };

  const nav = {
    'location-assign': (u) => location.assign(u),
    'location-replace': (u) => location.replace(u),
    'location-href': (u) => { location.href = u; },
    'a-click': (u) => el('a', { href: u, textContent: 'x' }).click(),
    'a-top': (u) => el('a', { href: u, target: '_top', textContent: 'x' }).click(),
    'window-open-self': (u) => window.open(u, '_self'),
    'form-get-top': (u) => submit(u, 'get', '_self'),
    'form-post-top': (u) => submit(u, 'post', '_self'),
    'meta-refresh': (u) => { const m = document.createElement('meta'); m.httpEquiv = 'refresh'; m.content = '0;url=' + u; document.head.append(m); },
    'iframe-top': (u) => { const f = el('iframe', {}); f.contentDocument.write(`<script>top.location=${JSON.stringify(u)}<\/script>`); f.contentDocument.close(); },
    'nav-attach': (u) => { location.href = u.replace('kind=doc', 'kind=attach'); },
    'redir-own-nav': (u) => { location.href = '/redir?to=' + encodeURIComponent(u); },
    'redir-direct-nav': (u) => { location.href = direct + '/redir?to=' + encodeURIComponent(u); },
  };
  const navNames = Object.keys(nav);

  // A running page keeps firing while native code switches the WebView away from it: this counts what a switch lets through.
  if (phase === 'race') {
    let i = 0;
    const fire = (tag) => { new Image().src = V(`race-${tag}`, 'img', `&i=${i++}`); };
    setInterval(() => fire('tick'), 1);
    addEventListener('pagehide', () => { fire('pagehide'); fetch(V('race-keepalive', 'img'), { mode: 'no-cors', keepalive: true }).catch(() => {}); });
    addEventListener('visibilitychange', () => fire('visibility'));
    addEventListener('pageshow', (e) => report('race-pageshow', 'persisted=' + e.persisted + ' host=' + location.host));
    return;
  }

  if (phase === 'sub') {
    const only = q.get('only');
    for (const [name, fn] of Object.entries(sub)) {
      if (only && !only.split(',').includes(name)) continue;
      if ((q.get('skip') || '').split(',').includes(name)) continue;
      try { await Promise.race([fn(), sleep(150)]); } catch (e) { report(name, 'threw ' + e); }
    }
    await sleep(4000);
    await report('done-sub', 'all fired');
    if (q.get('thennav') === '1') location.href = `/?phase=nav&n=0&run=${run}&vh=${encodeURIComponent(victimHost)}&direct=${encodeURIComponent(direct)}`;
  } else if (phase === 'nav') {
    const n = Number(q.get('n') || 0);
    if (n >= navNames.length) { await report('done-nav', 'all fired'); return; }
    const name = navNames[n];
    const next = `${location.origin}/?phase=nav&n=${n + 1}&run=${run}&vh=${encodeURIComponent(victimHost)}&direct=${encodeURIComponent(direct)}`;
    const target = V(name, 'doc', '&back=' + encodeURIComponent(next));
    await report(name, 'firing');
    try { nav[name](target); } catch (e) { report(name, 'threw ' + e); }
    await sleep(3000);
    await report(name, 'stayed on ' + location.host);
    location.href = next;
  }
})();
