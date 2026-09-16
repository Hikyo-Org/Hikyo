/**
 * The design system's token registry: every custom property in
 * src/styles/tokens.css, grouped by family, with the rule that governs it.
 * The Tokens story renders this with live values; its play test fails when
 * a listed token no longer resolves, so the page and the stylesheet cannot
 * drift apart. DESIGN.md carries the same rules in prose.
 */
export type TokenFamily = {
  family: string;
  rule: string;
  tokens: readonly { name: string; role: string }[];
};

export const TOKEN_FAMILIES: readonly TokenFamily[] = [
  {
    family: 'Surface',
    rule: 'Tinted neutrals, OKLCH throughout. --bg-panel is chrome; it sits above the page in dark and below it in light.',
    tokens: [
      { name: '--bg', role: 'page surface' },
      { name: '--bg-raise', role: 'raised rows; sheets on a page with no chrome' },
      { name: '--bg-panel', role: 'persistent chrome and its overlays' },
      { name: '--bg-hover', role: 'hover and selected inside chrome' },
    ],
  },
  {
    family: 'Line',
    rule: 'Hairlines are 1px, never shadows. --line is the control boundary (>= 3:1 on --bg).',
    tokens: [
      { name: '--line', role: 'hairlines and control boundaries' },
      { name: '--chrome-line', role: 'structural rules inside chrome' },
      { name: '--panel-line', role: 'dense settings-panel boundaries' },
    ],
  },
  {
    family: 'Ink',
    rule: 'Body 13:1, secondary 7:1. State is never colour-only: every state colour rides beside a glyph or a word.',
    tokens: [
      { name: '--tx', role: 'primary text' },
      { name: '--tx-dim', role: 'secondary: captions, labels, ledes' },
      { name: '--tx-faint', role: 'tertiary: eyebrows, counts' },
      { name: '--accent', role: 'interactive teal; ok state hairline' },
      { name: '--on-accent', role: 'text on the accent' },
      { name: '--danger', role: 'violation, refusal' },
      { name: '--changed', role: 'pending, drafted: slate, never red' },
      { name: '--accent-soft', role: 'accent fill' },
      { name: '--danger-soft', role: 'danger fill' },
      { name: '--changed-soft', role: 'changed fill' },
    ],
  },
  {
    family: 'Shape',
    rule: 'Radius carries a role: containers 6, controls 4, badges 3. The pill is for identity circles and counts of 12px or less only.',
    tokens: [
      { name: '--radius-container', role: 'cards, panels, dialogs' },
      { name: '--radius-control', role: 'buttons, inputs, selects, checkboxes' },
      { name: '--radius-badge', role: 'badges, chips' },
      { name: '--radius-pill', role: 'identity circles, count pills' },
    ],
  },
  {
    family: 'Type',
    rule: 'Five sizes and the value face; weights 400, 500, 700. Headings win by weight, not size. Only the eyebrow is uppercase.',
    tokens: [
      { name: '--fs-xs', role: 'eyebrow, badge (500, uppercase eyebrow)' },
      { name: '--fs-sm', role: 'captions: labels, legends, ledes, hints' },
      { name: '--fs-md', role: 'body, h3, panel titles (700)' },
      { name: '--fs-lg', role: 'h2, dialog titles (700)' },
      { name: '--fs-xl', role: 'h1 (700)' },
      { name: '--fs-mono', role: 'keys and values' },
      { name: '--tracking-eyebrow', role: 'eyebrow letter-spacing' },
      { name: '--font-ui', role: 'Instrument Sans' },
      { name: '--font-mono', role: 'IBM Plex Mono' },
    ],
  },
  {
    family: 'Control',
    rule: 'One height for every control; no surface overrides it. Compact is the in-row tertiary tier. Both become the touch floor on a coarse pointer.',
    tokens: [
      { name: '--control', role: 'buttons, inputs, selects, tabs, menu rows, chips' },
      { name: '--control-compact', role: 'quiet and icon-quiet buttons' },
      { name: '--badge-height', role: 'badges' },
      { name: '--touch', role: 'the floor on a coarse pointer' },
      { name: '--hit-min', role: 'the floor on a fine pointer (WCAG 2.5.8)' },
      { name: '--chk-box', role: 'checkbox and radio hit box' },
      { name: '--chk-visual', role: 'checkbox and radio drawn box' },
      { name: '--chk-gap', role: 'box-to-label gap' },
      { name: '--row', role: 'table and matrix rows on a fine pointer' },
      { name: '--rail', role: 'the organisation rail' },
    ],
  },
  {
    family: 'Space',
    rule: '4-based. Gaps, paddings and margins are one of these, never a literal; the adherence check counts literals.',
    tokens: [
      { name: '--space-1', role: '4: legend to options, tight gaps' },
      { name: '--space-2', role: '8: action rows, option rows, chip gaps' },
      { name: '--space-3', role: '12: form and panel gaps, control padding' },
      { name: '--space-4', role: '16: wrapping-row column gaps, panel padding' },
      { name: '--space-5', role: '24: dialog padding, section gaps' },
      { name: '--space-6', role: '32: dialog viewport inset' },
    ],
  },
  {
    family: 'Measure',
    rule: 'Dialogs are narrow or wide; prose never exceeds the measure.',
    tokens: [
      { name: '--width-dialog', role: 'decision dialogs' },
      { name: '--width-dialog-wide', role: 'editor dialogs' },
      { name: '--measure', role: 'ledes, hints, errors' },
    ],
  },
  {
    family: 'Depth and motion',
    rule: 'Shadows only on modal overlays. Layers are named; no surface invents a z-index. Motion 150 to 220ms ease-out-quart, transform and opacity only, off under reduced motion.',
    tokens: [
      { name: '--overlay-shadow', role: 'dialogs and sheets' },
      { name: '--z-chrome', role: 'rail, sidebar, sticky headers' },
      { name: '--z-sticky', role: 'in-content sticky bars' },
      { name: '--z-drawer', role: 'drawers' },
      { name: '--z-overlay', role: 'overlays outside the platform top layer' },
      { name: '--ease', role: 'ease-out-quart' },
      { name: '--dur', role: '180ms; 0 under reduced motion' },
    ],
  },
];
