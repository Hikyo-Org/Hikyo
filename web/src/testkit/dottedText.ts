/**
 * A `getByText` matcher for text split by `ui/Sep`: matches the innermost
 * element whose whole text content matches `pattern` (write the separator as
 * `\s*·\s*`).
 */
export function dottedText(pattern: RegExp) {
  return (_content: string, element: Element | null): boolean =>
    element !== null &&
    pattern.test(element.textContent ?? '') &&
    [...element.children].every((child) => !pattern.test(child.textContent ?? ''));
}
