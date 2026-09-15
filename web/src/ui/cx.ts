/** Join class names, dropping falsy branches. The one shared helper the ui/
 *  primitives use to merge their base class with modifiers and a caller's
 *  `className`. No dependency: `clsx` would be a package for three lines. */
export function cx(...parts: Array<string | false | null | undefined>): string {
  return parts.filter(Boolean).join(' ');
}
