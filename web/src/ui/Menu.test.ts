import { describe, expect, it } from 'vitest';

import { menuFocusTarget } from './Menu.tsx';

describe('menuFocusTarget', () => {
  it('moves with wrap, jumps with Home/End, and enters from either end', () => {
    expect(menuFocusTarget('ArrowDown', 0, 3)).toBe(1);
    expect(menuFocusTarget('ArrowDown', 2, 3)).toBe(0);
    expect(menuFocusTarget('ArrowUp', 0, 3)).toBe(2);
    expect(menuFocusTarget('ArrowUp', 2, 3)).toBe(1);
    expect(menuFocusTarget('Home', 2, 3)).toBe(0);
    expect(menuFocusTarget('End', 0, 3)).toBe(2);
    expect(menuFocusTarget('ArrowDown', -1, 3)).toBe(0);
    expect(menuFocusTarget('ArrowUp', -1, 3)).toBe(2);
  });

  it('owns no other key and nothing in an empty menu', () => {
    expect(menuFocusTarget('Tab', 0, 3)).toBeNull();
    expect(menuFocusTarget('Enter', 1, 3)).toBeNull();
    expect(menuFocusTarget('ArrowDown', -1, 0)).toBeNull();
  });
});
