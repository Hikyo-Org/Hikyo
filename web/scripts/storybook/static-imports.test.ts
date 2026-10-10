import { describe, expect, it } from 'vitest';
import { staticImports } from './static-imports.ts';

describe('initial app payload static dependency accounting', () => {
  it('follows readable and minified imports and reexports, excluding lazy imports', () => {
    expect(staticImports(`import{a}from"./shared.js";import './side.js';export{b}from'./export.js';export * from './all.js';const lazy=()=>import('./lazy.js');`))
      .toEqual(['./shared.js', './side.js', './export.js', './all.js']);
  });
});
