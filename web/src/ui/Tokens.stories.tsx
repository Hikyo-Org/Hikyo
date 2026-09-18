import type { Meta, StoryObj } from '@storybook/react-vite';
import { useState } from 'react';
import { expect } from 'storybook/test';

import { TOKEN_FAMILIES } from './tokens.ts';

/**
 * The design system on one page: every token in src/styles/tokens.css with
 * its live value in the current theme and the rule that governs its family.
 * Atoms, routes and the design file (design/hikyo.pen, seeded from the same
 * stylesheet) all reference these; `design:check` fails the build when a
 * stylesheet states a size that is not one of them.
 */
function TokensPage() {
  // Read the live token values once at mount: they come straight off
  // :root computed style, so lazy state init does it during render without a
  // reset effect (and without the one blank frame an effect would paint).
  const [values] = useState<Record<string, string>>(() => {
    const style = getComputedStyle(document.documentElement);
    const next: Record<string, string> = {};
    for (const family of TOKEN_FAMILIES) {
      for (const token of family.tokens) next[token.name] = style.getPropertyValue(token.name).trim();
    }
    return next;
  });
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-5)', maxWidth: 760 }}>
      {TOKEN_FAMILIES.map((family) => (
        <section key={family.family} className="field">
          <h2 style={{ margin: 0 }}>{family.family}</h2>
          <p className="page__lede">{family.rule}</p>
          <table className="tokens" style={{ borderCollapse: 'collapse', width: '100%' }}>
            <thead>
              <tr>
                <th scope="col" style={{ textAlign: 'left' }}>
                  Token
                </th>
                <th scope="col" style={{ textAlign: 'left' }}>
                  Value
                </th>
                <th scope="col" style={{ textAlign: 'left' }}>
                  Role
                </th>
              </tr>
            </thead>
            <tbody>
              {family.tokens.map((token) => {
                const value = values[token.name] ?? '';
                const swatch = family.family === 'Surface' || family.family === 'Line' || family.family === 'Ink';
                return (
                  <tr key={token.name} data-token={token.name}>
                    <td>
                      <code>{token.name}</code>
                    </td>
                    <td>
                      {swatch ? (
                        <span
                          aria-hidden="true"
                          style={{
                            display: 'inline-block',
                            width: 'var(--space-4)',
                            height: 'var(--space-4)',
                            marginRight: 'var(--space-2)',
                            verticalAlign: 'middle',
                            border: '1px solid var(--line)',
                            borderRadius: 'var(--radius-badge)',
                            background: `var(${token.name})`,
                          }}
                        />
                      ) : null}
                      <span className="mono">{value}</span>
                    </td>
                    <td className="page__lede">{token.role}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </section>
      ))}
    </div>
  );
}

const meta = {
  title: 'ui/Tokens',
  component: TokensPage,
  tags: ['ai-generated'],
} satisfies Meta<typeof TokensPage>;

export default meta;
type Story = StoryObj<typeof meta>;

export const DesignSystem: Story = {};

// Every listed token resolves: a token removed from tokens.css, or renamed
// without this registry following, fails here before it fails a screen.
export const EveryTokenResolves: Story = {
  play: async ({ canvasElement }) => {
    const style = getComputedStyle(document.documentElement);
    const missing = TOKEN_FAMILIES.flatMap((family) => family.tokens)
      .map((token) => token.name)
      .filter((name) => style.getPropertyValue(name).trim() === '');
    await expect(missing).toEqual([]);
    await expect(canvasElement.querySelectorAll('[data-token]').length).toBeGreaterThan(40);
  },
};
