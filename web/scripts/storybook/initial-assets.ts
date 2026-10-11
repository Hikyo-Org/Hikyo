import { Window } from 'happy-dom';

/** Parse build entry points without executing scripts or loading resources. */
export async function initialAssets(html: string): Promise<string[]> {
  const window = new Window({ settings: {
    disableJavaScriptEvaluation: true,
    disableJavaScriptFileLoading: true,
    disableCSSFileLoading: true,
    disableIframePageLoading: true,
  } });
  try {
    const document = new window.DOMParser().parseFromString(html, 'text/html');
    const paths: string[] = [];
    for (const element of document.querySelectorAll('script, link')) {
      if (element.localName === 'script' && element.getAttribute('type')?.toLowerCase() === 'module') {
        const source = element.getAttribute('src');
        if (source) paths.push(source);
      } else if (element.localName === 'link') {
        const relations = element.getAttribute('rel')?.toLowerCase().split(/\s+/) ?? [];
        const href = element.getAttribute('href');
        if (href && (relations.includes('modulepreload') || relations.includes('stylesheet'))) paths.push(href);
      }
    }
    return paths;
  } finally {
    await window.happyDOM.close();
  }
}
