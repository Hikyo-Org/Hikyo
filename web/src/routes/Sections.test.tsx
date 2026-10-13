// @vitest-environment happy-dom
import { MemoryRouter } from 'react-router';
import { expect, it } from 'vitest';

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
