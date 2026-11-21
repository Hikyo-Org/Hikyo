'use strict';
// The connection list. Everything it shows from an instance (names, origins)
// goes in through textContent, never as markup.
const $ = (id) => document.getElementById(id);
const list = $('connections');
const form = $('add');
let busy = false;
// The instance whose refusal the error box is showing: the one the browser button opens.
let refusedOrigin = '';

async function call(path, body) {
  const response = await fetch(path, {
    method: body === undefined ? 'GET' : 'POST',
    headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (response.status === 204) return {};
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw Object.assign(new Error(data.message || 'Something went wrong.'), { code: data.error });
  return data;
}

function show(error) {
  const box = $('error');
  box.hidden = !error;
  $('error-text').textContent = error ? error.message : '';
  $('open-browser').hidden = !(error && error.code === 'capability_missing');
}

function setBusy(on, message) {
  busy = on;
  $('add-button').disabled = on;
  $('cancel').hidden = !on;
  $('status').textContent = message || '';
  for (const b of list.querySelectorAll('button')) b.disabled = on;
}

async function run(message, fn) {
  show(null);
  setBusy(true, message);
  try {
    await fn();
  } catch (error) {
    if (error.code !== 'cancelled') show(error);
  } finally {
    setBusy(false);
    await refresh();
  }
}

function button(text, className, onClick) {
  const b = document.createElement('button');
  b.type = 'button';
  b.textContent = text;
  if (className) b.className = className;
  b.addEventListener('click', onClick);
  return b;
}

function card(c) {
  const li = document.createElement('li');
  li.className = 'card';
  const title = document.createElement('h3');
  title.textContent = c.name;
  const meta = document.createElement('div');
  meta.className = 'meta';
  const trust = c.trust === 'spki' ? `pinned key ${c.pin}` : 'system certificate authorities';
  meta.textContent = `${c.origin} · ${trust}` + (c.principal ? ` · ${c.principal}` : '');
  li.append(title, meta);
  if (c.stopped) {
    const warn = document.createElement('div');
    warn.className = 'warn';
    warn.textContent = c.stopped;
    li.append(warn);
  }
  const actions = document.createElement('div');
  actions.className = 'actions';
  if (c.stopped) {
    actions.append(button('Sign in', '', () => run('Waiting for you to sign in in your browser…', () => call('/__app/sign-in', { host: c.host }))));
  }
  actions.append(button('Open', c.stopped ? 'secondary' : '', () => run('', async () => {
    refusedOrigin = c.origin;
    await call('/__app/open', { host: c.host });
  })));
  const remove = button('Remove', 'danger', () => {
    if (remove.dataset.armed !== '1') {
      remove.dataset.armed = '1';
      remove.textContent = 'Confirm remove';
      return;
    }
    run('Removing…', async () => {
      const result = await call('/__app/remove', { host: c.host });
      if (result.message) show(new Error(result.message));
    });
  });
  actions.append(remove);
  li.append(actions);
  return li;
}

async function refresh() {
  const state = await call('/__app/state');
  list.replaceChildren(...state.connections.map(card));
  $('empty').hidden = state.connections.length > 0;
  for (const b of list.querySelectorAll('button')) b.disabled = busy;
}

form.addEventListener('submit', (event) => {
  event.preventDefault();
  const data = new FormData(form);
  refusedOrigin = String(data.get('origin') || '');
  run('Check the fingerprint in the confirmation window, then finish signing in in your browser…', async () => {
    await call('/__app/add', {
      origin: refusedOrigin,
      name: String(data.get('name') || ''),
      trust_mode: String(data.get('trust')),
      method: data.get('device') ? 'device' : 'browser',
    });
    form.reset();
  });
});

$('cancel').addEventListener('click', () => call('/__app/cancel', {}).catch(() => {}));
$('open-browser').addEventListener('click', () => call('/__app/open-browser', { origin: refusedOrigin }).catch(show));
refresh().catch(show);
