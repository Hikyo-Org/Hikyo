# 001 — Reveal flow steps in order

- **Status**: DONE
- **Commit**: 132ddae4 (prepared against the existing uncommitted landing-page work shown below)
- **Severity**: LOW
- **Category**: Missed opportunities / cohesion
- **Estimated scope**: 2 source files, approximately 60 lines

## Problem

The landing page's workflow section describes a strict three-step sequence, but all
three steps appear at once. The content order is clear in text, yet the presentation
does not reinforce that order when a visitor reaches the section for the first time.

The page is calm, technical, and documentation-first. This animation therefore must
explain sequence without adding decorative motion, delaying interaction, moving
borders, or replaying during normal scrolling.

The worktree already contains landing-page edits in `index.astro` and
`landing.css`. They belong to the user. Preserve them; do not reset either file to
commit `132ddae4` before executing this plan.

```astro
<!-- docs/site/src/pages/index.astro:204 — current -->
<section class="block flow" aria-labelledby="flow-heading">
  <div class="wrap flow-grid">
    <div>
      <p class="kicker">// one path from intent to runtime</p>
      <h2 id="flow-heading">Declare once. Decide per environment.</h2>
      <p class="section-lede">
        Hikyo separates the shape of a key from each environment's value, then keeps delivery inside the
        same authorization model.
      </p>
    </div>
    <ol class="steps">
      <li>
        <span>01</span>
        <div><h3>Declare the contract</h3><p>Choose config or secret, add validation, and state where the key is required.</p></div>
        <code>key create</code>
      </li>
      <li>
        <span>02</span>
        <div><h3>Set and review state</h3><p>Work in an explicit matrix where absence, invalid input, and pending changes stay visible.</p></div>
        <code>values set</code>
      </li>
      <li>
        <span>03</span>
        <div><h3>Deliver with intent</h3><p>Fetch at runtime, render for Compose, reconcile Kubernetes, or use a declared adapter.</p></div>
        <code>hikyo run</code>
      </li>
    </ol>
  </div>
</section>
```

```css
/* docs/site/src/styles/landing.css:717 — current */
.steps {
  margin: 0;
  padding: 0;
  border-block-start: 1px solid var(--line);
  list-style: none;
}

.steps li {
  display: grid;
  grid-template-columns: 2.5rem minmax(0, 1fr) auto;
  align-items: start;
  gap: 1rem;
  padding-block: 1.25rem;
  border-block-end: 1px solid var(--line);
}
```

## Target

Reveal the contents of the three list items once, in reading order, when at least
25% of the list enters the viewport. Keep each `<li>` stationary because it owns
the row's bottom border:

- Step-content entrance: `opacity: 0` and `transform: translateY(0.375rem)` to
  `opacity: 1` and `transform: translateY(0)`.
- Duration: `280ms` for both properties.
- Easing: existing `--ease-out`, which resolves to
  `cubic-bezier(0.22, 1, 0.36, 1)` in this repository.
- Stagger: step 1 `0ms`, step 2 `45ms`, step 3 `90ms`.
- Last step must finish by `370ms` after reveal starts.
- Normal motion animates only `transform` and `opacity`.
- Reduced motion removes translation and staggering but keeps a `120ms ease`
  opacity transition.
- The animation runs once. Scrolling away and back must not replay it.
- Content stays visible when JavaScript is disabled, `IntersectionObserver` is
  unavailable, or the section is already visible when the script initializes.
- Initialize after the window `load` event plus one animation frame so fragment
  scrolling and scroll restoration settle before visibility is measured.
- If the URL fragment targets an element inside `.flow`, leave the list visible
  and do not opt it into motion.
- No DOM, accessibility-tree, or layout dimensions change during motion.

Add one progressive-enhancement marker to the existing list:

```astro
<!-- docs/site/src/pages/index.astro — target -->
<ol class="steps" data-flow-steps>
```

Add the following normal-motion rules next to the existing `.steps` styles:

```css
/* docs/site/src/styles/landing.css — target */
@media (prefers-reduced-motion: no-preference) {
  .steps[data-motion-ready='true'] > li > * {
    opacity: 0;
    transform: translateY(0.375rem);
    transition:
      opacity 280ms var(--ease-out),
      transform 280ms var(--ease-out);
  }

  .steps[data-motion-ready='true'][data-revealed='true'] > li > * {
    opacity: 1;
    transform: translateY(0);
  }

  .steps[data-motion-ready='true'][data-revealed='true'] > li:nth-child(2) > * {
    transition-delay: 45ms;
  }

  .steps[data-motion-ready='true'][data-revealed='true'] > li:nth-child(3) > * {
    transition-delay: 90ms;
  }
}
```

Inside the existing final `@media (prefers-reduced-motion: reduce)` block, after
the wildcard rule, add this scoped exception. Its higher specificity and later
position intentionally override the global `0.01ms !important` duration for this
comprehension-aiding opacity transition only:

```css
/* docs/site/src/styles/landing.css — target, inside the existing reduce block */
.steps[data-motion-ready='true'] > li > * {
  opacity: 0;
  transform: none;
  transition: opacity 120ms ease !important;
}

.steps[data-motion-ready='true'][data-revealed='true'] > li > * {
  opacity: 1;
}
```

Append this setup to the existing landing-page script after the theme-toggle click
handler. Do not create another script block:

```ts
// docs/site/src/pages/index.astro — target
const flowSteps = document.querySelector<HTMLOListElement>('[data-flow-steps]');

if (flowSteps) {
  const prepareFlowReveal = () => {
    const rect = flowSteps.getBoundingClientRect();
    const alreadyVisible = rect.top < window.innerHeight && rect.bottom > 0;
    const fragmentId = window.location.hash.slice(1);
    const fragmentTarget = fragmentId ? document.getElementById(fragmentId) : null;
    const flowSection = flowSteps.closest('.flow');
    const fragmentTargetsFlow = fragmentTarget !== null && flowSection?.contains(fragmentTarget) === true;

    if (alreadyVisible || fragmentTargetsFlow || !('IntersectionObserver' in window)) return;

    flowSteps.dataset.motionReady = 'true';

    const observer = new IntersectionObserver(
      ([entry]) => {
        if (!entry?.isIntersecting) return;

        flowSteps.dataset.revealed = 'true';
        observer.disconnect();
      },
      { threshold: 0.25 },
    );

    observer.observe(flowSteps);
  };

  const scheduleFlowReveal = () => requestAnimationFrame(prepareFlowReveal);

  if (document.readyState === 'complete') {
    scheduleFlowReveal();
  } else {
    window.addEventListener('load', scheduleFlowReveal, { once: true });
  }
}
```

## Repo conventions to follow

- Motion stays in `docs/site/src/styles/landing.css`; do not add a motion library.
- Use the existing `--ease-out: cubic-bezier(0.22, 1, 0.36, 1)` token from
  `docs/site/src/styles/landing.css:25`; do not introduce a competing easing.
- Match the existing hero's `prefers-reduced-motion: no-preference` containment
  at `docs/site/src/styles/landing.css:346`.
- Use CSS transitions for the observer-triggered state change so it can retarget
  safely. Do not use keyframes for this dynamic reveal.
- Preserve the repository's progressive-enhancement rule: visible by default;
  JavaScript may opt an offscreen element into motion.

## Steps

1. In `docs/site/src/pages/index.astro`, add `data-flow-steps` to the existing
   `<ol class="steps">`. Change no content, order, semantics, or IDs.
2. In the existing script in `docs/site/src/pages/index.astro`, add the exact
   post-load observer setup from **Target** after the theme-toggle handler.
3. In `docs/site/src/styles/landing.css`, add the normal-motion transition rules
   from **Target** beside the existing `.steps` block.
4. In the existing reduced-motion media query at the end of
   `docs/site/src/styles/landing.css`, add the scoped opacity-only override from
   **Target** after the wildcard rule.
5. Run the mechanical and rendered checks below. If any timing or selector must
   differ from this plan because the stamped code has drifted, stop and report the
   mismatch instead of improvising.

## Boundaries

- Do NOT touch `/docs/` navigation, documentation components, or documentation
  content.
- Do NOT change the hero entrance, CTA feedback, theme-icon motion, comparison
  table, feature artifacts, FAQ, or footer.
- Do NOT add dependencies, a motion library, a second script block, or a global
  scroll listener.
- Do NOT animate layout, borders, colors, shadows, filters, width, height, margin,
  padding, `top`, or `left`; animate only `transform` and `opacity`.
- Do NOT transform `<li>` itself. Animate its three direct children so the border
  on `<li>` remains stationary.
- Do NOT hide content unless JavaScript has set `data-motion-ready="true"`.
- Do NOT replay the reveal after the observer disconnects.
- If these source excerpts no longer match when execution begins, STOP and report
  drift. Do not reset or discard the existing uncommitted landing-page work.

## Verification

- **Mechanical**:
  1. Run `rtk pnpm --dir docs/site verify`.
  2. Expect Astro check to report `0 errors`, `0 warnings`, and `0 hints`.
  3. Expect static build, OSS policy, PWA, and offline-browser gates to pass.
  4. Run `rtk git diff --check`; expect no output and exit code `0`.
  5. Confirm `git diff --name-only` contains only
     `docs/site/src/pages/index.astro`, `docs/site/src/styles/landing.css`, and
     plan bookkeeping already present in the worktree.
- **Feel check**:
  1. Run `rtk pnpm --dir docs/site dev` and open `http://localhost:4321/` at
     `1280×800`.
  2. Scroll down once. When 25% of the ordered list enters, confirm `01` starts,
     `02` follows `45ms` later, and `03` follows `90ms` after the first step.
  3. Confirm all three rows finish within `370ms`, remain readable throughout,
     and their contents never move more than `0.375rem` vertically.
  4. Scroll away and back. Confirm no replay.
  5. In DevTools Animations, set playback to 10%. Confirm list borders and
     surrounding layout remain stationary; `<li>` itself keeps `transform: none`,
     and only its direct children's opacity and Y transform change.
  6. Emulate `prefers-reduced-motion: reduce`. Reload and confirm no translation
     or stagger; rows use one `120ms` opacity fade.
  7. Disable JavaScript and reload. Confirm all three steps remain visible.
  8. Open `http://localhost:4321/#flow-heading`. Confirm an initially visible
     workflow list remains visible, never receives `data-motion-ready`, and does
     not flash hidden.
  9. Repeat at `390×844`; confirm no horizontal overflow or clipped step content.
- **Done when**: first-time scrolling communicates `01 → 02 → 03` with the exact
  timing above, the animation never replays, reduced-motion users receive only the
  brief opacity cue, no-JavaScript content stays visible, and all mechanical checks
  pass.
