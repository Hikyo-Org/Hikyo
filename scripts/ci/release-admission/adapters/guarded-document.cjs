'use strict';
const { checkSharedWindow } = require('./shared-window.cjs');
// Owned R1 lexical dependency, separately pinned from the unchanged G functions.
// Only the DOM surface used by those public functions is exposed. No global patch.
function createGuardedDocument(globalObject, allowedURLs, sharedWindow) {
  const nativeDocument = globalObject.document;
  const fail = () => { throw new Error('aws-operator-held'); };
  const one = new Set(['#nav-usernameMenu', '#app', '#c', '[data-testid="iam-v2-vite"]']);
  const many = 'header, nav, #awsc-nav-account-menu-button, [data-testid="awsc-nav-account-menu-button"]';
  function guard() {
    checkSharedWindow(globalObject, sharedWindow);
    // The URL check MUST precede every real document/node/collection access.
    if (!allowedURLs.includes(globalObject.location.href)) fail();
    if (globalObject.document !== nativeDocument) fail();
    const banner = Reflect.apply(nativeDocument.querySelector, nativeDocument, ['#nav-usernameMenu']);
    if (!banner || Reflect.apply(banner.getClientRects, banner, []).length === 0) fail();
    const text = banner.textContent;
    if (typeof text !== 'string' || text.length > 512 || !/(^|[^0-9])099256073061([^0-9]|$)/.test(text)) fail();
  }
  function node(value) {
    if (value === null) return null;
    const attribute = name => {
      guard();
      if (name !== 'aria-label') fail();
      return Reflect.apply(value.getAttribute, value, [name]);
    };
    // Returned nodes cannot expose real DOM objects through arbitrary members.
    return Object.freeze(Object.defineProperties(Object.create(null), {
      textContent: { enumerable: true, get() { guard(); return value.textContent; } },
      src: { enumerable: true, get() { guard(); return value.src; } },
      getAttribute: { enumerable: true, get() { guard(); return attribute; } }
    }));
  }
  function collection(value) {
    function* iterate() {
      guard();
      const iterator = Reflect.apply(value[Symbol.iterator], value, []);
      while (true) {
        guard();
        const part = Reflect.apply(iterator.next, iterator, []);
        if (part.done) return;
        yield node(part.value);
      }
    }
    return Object.freeze(Object.defineProperties(Object.create(null), {
      length: { enumerable: true, get() { guard(); return value.length; } },
      [Symbol.iterator]: { get() { guard(); return iterate; } }
    }));
  }
  const queryOne = selector => {
    guard();
    if (!one.has(selector)) fail();
    return node(Reflect.apply(nativeDocument.querySelector, nativeDocument, [selector]));
  };
  const queryMany = selector => {
    guard();
    if (selector !== many) fail();
    return collection(Reflect.apply(nativeDocument.querySelectorAll, nativeDocument, [selector]));
  };
  return Object.freeze(Object.defineProperties(Object.create(null), {
    querySelector: { enumerable: true, get() { guard(); return queryOne; } },
    querySelectorAll: { enumerable: true, get() { guard(); return queryMany; } },
    scripts: { enumerable: true, get() { guard(); return collection(nativeDocument.scripts); } }
  }));
}
module.exports = { createGuardedDocument };
