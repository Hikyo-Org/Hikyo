import type { Meta, StoryObj } from '@storybook/react-vite';

/**
 * The type scale as one page: every heading level, the eyebrow, captions,
 * body and the value face, using the class names routes emit today so the
 * scale is checked against real markup.
 */
function Specimen() {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16, maxWidth: 560 }}>
      <div>
        <h1 className="page__title" style={{ margin: 0 }}>
          Environment matrix (h1)
        </h1>
        <p className="page__lede">A lede under the page title, secondary ink, 68ch measure.</p>
      </div>
      <h2 style={{ margin: 0 }}>Section heading (h2)</h2>
      <section className="panel" style={{ padding: 0, border: 0, background: 'none' }}>
        <h2 style={{ margin: 0 }}>Panel title (.panel h2)</h2>
      </section>
      <h3 style={{ margin: 0 }}>Subsection (h3)</h3>
      <div className="sidebar__section" style={{ padding: 0 }}>
        <h2 style={{ margin: 0 }}>Organisation (sidebar eyebrow h2)</h2>
      </div>
      <p className="eyebrow" style={{ margin: 0 }}>
        Eyebrow (.eyebrow)
      </p>
      <div className="field">
        <label>Field label (.field label)</label>
        <p className="field__hint">A hint under the control, same size and ink as the label.</p>
      </div>
      <p style={{ margin: 0 }}>
        Body text, 14px. Every resolved value can explain where it came from without leaving the
        screen. <a href="#unstyled">An unclassed link</a> beside it.
      </p>
      <p style={{ margin: 0 }}>
        Value face: <code>DATABASE_URL</code> and <span className="mono">postgres://app@db:5432/app</span>
      </p>
      <p style={{ margin: 0 }}>
        <span className="badge">badge</span> <span className="count">3</span>
      </p>
    </div>
  );
}

const meta = {
  title: 'ui/Typography',
  component: Specimen,
  tags: ['ai-generated'],
} satisfies Meta<typeof Specimen>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Scale: Story = {};

