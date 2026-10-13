/**
 * When the raked sand (app.css, Atmosphere) may move. Its animation repaints
 * the whole layer every frame, so:
 *  - without GPU acceleration it never moves (`sand-static` on the root):
 *    the sand stays, only the motion goes;
 *  - while the page is hidden or the window unfocused it pauses
 *    (`sand-paused`), and resumes where it was.
 * prefers-reduced-motion is handled in CSS and still wins. Classes only: the
 * CSP forbids inline styles.
 */

/** A WebGL context that would not be software-rendered or blocklisted. The
    renderer string is never read (a fingerprinting surface). */
export function hasGpu(doc: Document): boolean {
  try {
    const gl = doc.createElement('canvas').getContext('webgl', { failIfMajorPerformanceCaveat: true });
    if (gl === null) return false;
    gl.getExtension('WEBGL_lose_context')?.loseContext();
    return true;
  } catch {
    return false;
  }
}

export function installSandMotion(doc: Document = document, win: Window = window): () => void {
  const root = doc.documentElement;
  if (!hasGpu(doc)) {
    root.classList.add('sand-static');
    return () => root.classList.remove('sand-static');
  }
  const update = () => root.classList.toggle('sand-paused', doc.hidden || !doc.hasFocus());
  const events = ['blur', 'focus', 'pagehide', 'pageshow'] as const;
  for (const e of events) win.addEventListener(e, update);
  doc.addEventListener('visibilitychange', update);
  update();
  return () => {
    for (const e of events) win.removeEventListener(e, update);
    doc.removeEventListener('visibilitychange', update);
    root.classList.remove('sand-paused');
  };
}
