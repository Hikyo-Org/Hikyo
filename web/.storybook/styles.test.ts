import { readFile } from 'node:fs/promises';
import { describe, expect, it } from 'vitest';

const read = (path: string) => readFile(new URL(path, import.meta.url), 'utf8');

describe('Storybook uses the application style owner', () => {
  it('imports the single application cascade, with no extra app stylesheet or font overrides', async () => {
    const preview = await read('./preview.tsx');
    const main = await read('../src/main.tsx');
    expect(preview).toContain("import '../src/styles/index.ts'");
    expect(main).toContain("import './styles/index.ts'");
    for (const source of [preview, main]) {
      const appImports = source.replace("import './docs.css'", '');
      expect(appImports).not.toMatch(/import\s+['"][^'"]+\.css['"]/);
      expect(source).not.toContain('@fontsource');
    }
    const owner = await read('../src/styles/index.ts');
    expect(owner.indexOf("'./tokens.css'")).toBeLessThan(owner.indexOf("'./app.css'"));
  });
});
