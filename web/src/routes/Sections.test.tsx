// @vitest-environment happy-dom
import { MemoryRouter } from 'react-router';
import { expect, it, vi } from 'vitest';

import { renderForm } from '../testkit/renderForm.tsx';
import { SectionTabs } from './Sections.tsx';

const sections = [
  { id: 'demo-profile', label: 'Profile', content: <p>Profile body</p> },
  { id: 'demo-sessions', label: 'Sessions', content: <p>Sessions body</p> },
];

it.each(['#%', '#%E0%A4%A'])('opens the first tab for the malformed fragment %s without throwing', async (fragment) => {
  const { container, unmount } = await renderForm(
    <MemoryRouter initialEntries={[`/${fragment}`]}>
      <SectionTabs label="Demo sections" idPrefix="demo" sections={sections} />
    </MemoryRouter>,
  );
  try {
    expect(container.querySelector('[role="tab"][aria-selected="true"]')?.textContent).toBe('Profile');
  } finally {
    await unmount();
  }
});

it('opens and scrolls to the tab that now holds a section an older ?tab= link names', async () => {
  const scroll = vi.spyOn(Element.prototype, 'scrollIntoView').mockImplementation(() => {});
  const { container, unmount } = await renderForm(
    <MemoryRouter initialEntries={['/?tab=sessions']}>
      <SectionTabs
        label="Demo sections"
        idPrefix="demo"
        sections={[
          { id: 'demo-profile', label: 'Profile', content: <p>Profile body</p> },
          { id: 'demo-access', label: 'Access', anchors: ['demo-sessions'], content: <p id="demo-sessions">Sessions body</p> },
        ]}
      />
    </MemoryRouter>,
  );
  try {
    expect(container.querySelector('[role="tab"][aria-selected="true"]')?.textContent).toBe('Access');
    expect(scroll.mock.contexts).toEqual([container.querySelector('#demo-sessions')]);
  } finally {
    scroll.mockRestore();
    await unmount();
  }
});

it('does not scroll for a live, unknown or unanchored ?tab= value', async () => {
  const scroll = vi.spyOn(Element.prototype, 'scrollIntoView').mockImplementation(() => {});
  try {
    // `env` names a real element that no section lists as an anchor.
    for (const tab of ['sessions', 'profile', 'nonsense', 'env']) {
      const { unmount } = await renderForm(
        <MemoryRouter initialEntries={[`/?tab=${tab}`]}>
          <SectionTabs
            label="Demo sections"
            idPrefix="demo"
            sections={sections.map((section) => ({ ...section, content: <p id={section.id}><span id="demo-env">Body</span></p> }))}
          />
        </MemoryRouter>,
      );
      await unmount();
    }
    expect(scroll).not.toHaveBeenCalled();
  } finally {
    scroll.mockRestore();
  }
});
