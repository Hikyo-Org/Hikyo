// Document-start shim: the WebView hands shouldInterceptRequest no request body, so a body is sent to the app over the
// WebMessageListener `spikeBodies` first and the real request carries only the X-Spike-Body id.
(() => {
  const ch = window.spikeBodies;
  if (!ch) return;
  const waiting = new Map();
  ch.onmessage = (e) => { const done = waiting.get(e.data); if (done) { waiting.delete(e.data); done(e.data); } };
  const sameOrigin = (u) => new URL(u, location.href).origin === location.origin;
  async function stash(bytes) {
    const id = crypto.randomUUID();
    const msg = new Uint8Array(36 + bytes.length);
    msg.set(new TextEncoder().encode(id));
    msg.set(bytes, 36);
    await new Promise((res, rej) => {
      waiting.set(id, (ok) => (ok === id ? res() : rej(new Error('body refused'))));
      ch.postMessage(msg.buffer);
    });
    return id;
  }
  const origFetch = window.fetch;
  window.fetch = async function (input, init) {
    const req = new Request(input, init);
    if (req.method === 'GET' || req.method === 'HEAD' || !req.body || !sameOrigin(req.url)) return origFetch.call(this, req);
    const id = await stash(new Uint8Array(await req.arrayBuffer()));
    const headers = new Headers(req.headers);
    headers.set('X-Spike-Body', id);
    return origFetch.call(this, new Request(req.url, {
      method: req.method, headers, signal: req.signal, credentials: req.credentials, mode: req.mode,
      cache: req.cache, redirect: req.redirect, referrer: req.referrer, referrerPolicy: req.referrerPolicy,
    }));
  };
  const open = XMLHttpRequest.prototype.open;
  const send = XMLHttpRequest.prototype.send;
  XMLHttpRequest.prototype.open = function (method, url) { this._spike = { method: String(method).toUpperCase(), url: String(url) }; return open.apply(this, arguments); };
  XMLHttpRequest.prototype.send = function (body) {
    const m = this._spike;
    if (body == null || !m || m.method === 'GET' || m.method === 'HEAD' || !sameOrigin(m.url)) return send.call(this, body);
    const res = new Response(body);
    res.arrayBuffer().then((buf) => stash(new Uint8Array(buf))).then((id) => {
      const ct = res.headers.get('content-type');
      if (ct && !this._spikeCT) super_setRequestHeader.call(this, 'Content-Type', ct);
      super_setRequestHeader.call(this, 'X-Spike-Body', id);
      send.call(this, null);
    }, () => this.dispatchEvent(new ProgressEvent('error')));
  };
  const super_setRequestHeader = XMLHttpRequest.prototype.setRequestHeader;
  XMLHttpRequest.prototype.setRequestHeader = function (k, v) { if (String(k).toLowerCase() === 'content-type') this._spikeCT = true; return super_setRequestHeader.call(this, k, v); };
})();
