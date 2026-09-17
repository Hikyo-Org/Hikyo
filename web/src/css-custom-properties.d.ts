// React's CSSProperties has no index signature for custom properties, so an
// inline `--token` would need an `as CSSProperties` assertion. The repo
// standard is no assertions: widen the type once here for every `--*` key so a
// style object with a custom property is checked by assignment instead.
import 'react';

declare module 'react' {
  interface CSSProperties {
    [customProperty: `--${string}`]: string | number | undefined;
  }
}
