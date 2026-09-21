/**
 * ThemeIcon is the polymorphing sun↔moon (author: Marc Went). The morph, the
 * ray draw-in and the moon shimmer are CSS in app.css, the CSP forbids inline
 * `<style>`, so nothing renders one here. The sun path is baked as an attribute
 * so a browser without the CSS `d` property still shows a static sun rather
 * than nothing; the CSS overrides it where supported.
 *
 * Lives in `ui/` so the real toggle glyph is shared between the app chrome
 * (`routes/Shell.tsx`) and the `Button/Icon` Storybook pilot (#757); a text
 * moon glyph exported as an empty headless frame because no bundled font
 * covered it.
 */
export function ThemeIcon({ dark }: { dark: boolean }) {
  return (
    <svg
      className={dark ? 'theme-icon theme-icon--dark' : 'theme-icon'}
      viewBox="0 0 100 100"
      width="24"
      height="24"
      fill="none"
      aria-hidden="true"
      focusable="false"
    >
      <path
        className="theme-icon__shine"
        d="M70 49.5C70 60.8218 60.8218 70 49.5 70C38.1782 70 29 60.8218 29 49.5C29 38.1782 38.1782 29 49.5 29C39 45 49.5 59.5 70 49.5Z"
      />
      <g className="theme-icon__rays">
        <path d="M50 2V11" pathLength="1" />
        <path d="M85 15L78 22" pathLength="1" />
        <path d="M98 50H89" pathLength="1" />
        <path d="M85 85L78 78" pathLength="1" />
        <path d="M50 98V89" pathLength="1" />
        <path d="M23 78L16 84" pathLength="1" />
        <path d="M11 50H2" pathLength="1" />
        <path d="M23 23L16 16" pathLength="1" />
      </g>
      <path
        className="theme-icon__shape"
        d="M70 49.5C70 60.8218 60.8218 70 49.5 70C38.1782 70 29 60.8218 29 49.5C29 38.1782 38.1782 29 49.5 29C60 29 69.5 38 70 49.5Z"
      />
    </svg>
  );
}
