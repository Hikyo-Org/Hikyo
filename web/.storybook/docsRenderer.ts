import type { ReactRenderer } from '@storybook/react-vite';

/**
 * Storybook 10.6 rerenders Docs on globals updates, and its exported renderer
 * gives the root error boundary a new key each time. Retain the mounted page
 * for the same context and parameters; its blocks already subscribe to args
 * and globals. New contexts/parameters and explicit unmount still delegate.
 */
export function retainDocsRender<Context, Parameters>(renderer: {
  render: (context: Context, parameters: Parameters, element: HTMLElement) => Promise<void>;
  unmount: (element: HTMLElement) => void;
}) {
  const renders = new WeakMap<HTMLElement, {
    context: Context;
    parameters: Parameters;
    completion: Promise<void>;
  }>();
  return {
    render: (context: Context, parameters: Parameters, element: HTMLElement) => {
      const existing = renders.get(element);
      if (existing?.context === context && existing.parameters === parameters) return existing.completion;
      const completion = renderer.render(context, parameters, element);
      const mounted = { context, parameters, completion };
      renders.set(element, mounted);
      void completion.catch(() => {
        if (renders.get(element) === mounted) renders.delete(element);
      });
      return completion;
    },
    unmount: (element: HTMLElement) => {
      renders.delete(element);
      renderer.unmount(element);
    },
  };
}

// Keep the installed Docs renderer, MDX provider and React root implementation.
// Load them only when Storybook requests a Docs page.
export async function createDocsRenderer() {
  const { DocsRenderer } = await import('@storybook/addon-docs');
  return retainDocsRender(new DocsRenderer<ReactRenderer>());
}
