export type RenderCondition = { selector: string; text?: string; absent?: boolean; value?: string };
export type RenderSample = { label: string; durationMs: number };

// These are lab action-to-DOM-plus-frame samples, not field INP. Timing starts
// at the trusted browser input, so Playwright locator/actionability waits are
// excluded. A second rAF provides a rendering opportunity after the condition.
export function installTiming() {
  const samples: RenderSample[] = [];
  const events: { name: string; durationMs: number }[] = [];
  let armed: { label: string; condition: RenderCondition; start?: number } | undefined;
  const observed = PerformanceObserver.supportedEntryTypes.includes('event');
  const options: PerformanceObserverInit & { durationThreshold: number } = { type: 'event', buffered: true, durationThreshold: 16 };
  if (observed) new PerformanceObserver((list) => {
    for (const entry of list.getEntries()) events.push({ name: entry.name, durationMs: entry.duration });
  }).observe(options);
  const matches = (condition: RenderCondition) => {
    const element = document.querySelector(condition.selector);
    if (condition.absent === true) return element === null;
    if (element === null) return false;
    if (condition.value !== undefined) return (element instanceof HTMLInputElement || element instanceof HTMLTextAreaElement) && element.value === condition.value;
    return condition.text === undefined || element.textContent?.includes(condition.text) === true;
  };
  const input = (event: Event) => {
    if (!event.isTrusted) return;
    const measurement = armed;
    if (measurement === undefined || measurement.start !== undefined) return;
    measurement.start = performance.now();
    const tick = () => {
      if (armed !== measurement) return;
      if (!matches(measurement.condition)) { requestAnimationFrame(tick); return; }
      requestAnimationFrame(() => {
        if (measurement.start === undefined) throw new Error('Measurement never started');
        samples.push({ label: measurement.label, durationMs: performance.now() - measurement.start });
        armed = undefined;
      });
    };
    requestAnimationFrame(tick);
  };
  for (const event of ['pointerdown', 'keydown', 'input']) document.addEventListener(event, input, true);
  return {
    arm(label: string, condition: RenderCondition) {
      if (armed !== undefined) throw new Error('Previous measurement is still pending');
      if (matches(condition)) throw new Error(`Measurement condition already true: ${label}`);
      armed = { label, condition };
    },
    read: () => ({ samples, events, eventTimingSupported: observed }),
  };
}
