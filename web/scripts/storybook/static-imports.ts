/** Relative static imports emitted by Vite/Rolldown, including minified from clauses. */
export function staticImports(source: string): string[] {
  return [...source.matchAll(/(?:\bimport\s*(?:[^;"']*?\bfrom\s*)?|\bexport\s*[^;"']*?\bfrom\s*)["'](\.[^"']+)["']/g)]
    .flatMap((match) => match[1] === undefined ? [] : [match[1]]);
}
