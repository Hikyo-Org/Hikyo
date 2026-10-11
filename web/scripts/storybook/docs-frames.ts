import { posix } from 'node:path';
import { babelParse, traverse, types } from 'storybook/internal/babel';
import { loadCsf } from 'storybook/internal/csf-tools';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

function unwrap(node: types.Node): types.Node {
  if (types.isTSAsExpression(node) || types.isTSSatisfiesExpression(node) || types.isTSNonNullExpression(node)) return unwrap(node.expression);
  return node;
}

function keyName(node: types.Node, computed = false): string | undefined {
  if (types.isStringLiteral(node)) return node.value;
  if (!computed && types.isIdentifier(node)) return node.name;
  return undefined;
}

/** Inspect isolation fields only; never execute story modules or fixture helpers. */
function parameterReader(path: string, ast: types.File) {
  const bindings = new Map<string, types.Node>();
  const scopedReferences = new Set<types.Node>();
  const shared = types.valueToNode(topLayerDocs);
  const sharedNodes = new Set<types.Node>();
  types.traverseFast(shared, (node) => { sharedNodes.add(node); });
  for (const statement of ast.program.body) {
    if (types.isImportDeclaration(statement)) {
      const owner = posix.normalize(posix.join(posix.dirname(path.replaceAll('\\', '/')), statement.source.value));
      if (owner !== '.storybook/topLayerDocs.ts') continue;
      for (const specifier of statement.specifiers) {
        if (types.isImportSpecifier(specifier) && keyName(specifier.imported) === 'topLayerDocs') bindings.set(specifier.local.name, shared);
      }
      continue;
    }
    const declaration = types.isExportNamedDeclaration(statement) ? statement.declaration : statement;
    if (types.isVariableDeclaration(declaration) && declaration.kind === 'const') {
      for (const item of declaration.declarations) if (types.isIdentifier(item.id) && item.init != null) bindings.set(item.id.name, item.init);
    } else if (types.isFunctionDeclaration(declaration) && declaration.id != null) {
      bindings.set(declaration.id.name, declaration);
    }
  }

  function resolve(node: types.Node, seen = new Set<types.Node>()): types.Node {
    node = unwrap(node);
    if (scopedReferences.has(node)) throw new Error('helper isolation depends on a local parameter; declare it directly');
    if (seen.has(node)) throw new Error('cyclic isolation parameters');
    const next = new Set(seen).add(node);
    if (types.isIdentifier(node) && bindings.has(node.name)) {
      const binding = bindings.get(node.name);
      if (binding !== undefined) return resolve(binding, next);
    }
    if (types.isMemberExpression(node)) {
      const key = keyName(node.property, node.computed);
      const member = key === undefined ? undefined : field(node.object, key, next);
      if (member !== undefined) return resolve(member, next);
    }
    if (types.isCallExpression(node) && types.isIdentifier(node.callee)) {
      const helper = bindings.get(node.callee.name);
      const fn = helper === undefined ? undefined : unwrap(helper);
      if (types.isArrowFunctionExpression(fn) || types.isFunctionExpression(fn) || types.isFunctionDeclaration(fn)) {
        if (fn.async || fn.generator) throw new Error('isolation helper must return parameters synchronously');
        const parameters = new Set(fn.params.flatMap((parameter) => Object.keys(types.getBindingIdentifiers(parameter))));
        types.traverseFast(fn.body, (child) => {
          if (types.isIdentifier(child) && parameters.has(child.name)) scopedReferences.add(child);
        });
      }
      if (types.isArrowFunctionExpression(fn) && !types.isBlockStatement(fn.body)) return resolve(fn.body, next);
      if ((types.isArrowFunctionExpression(fn) || types.isFunctionExpression(fn) || types.isFunctionDeclaration(fn)) && types.isBlockStatement(fn.body)) {
        // Only an unconditional single return is statically safe. Other helpers
        // must expose their isolation parameters directly rather than being run.
        const [statement] = fn.body.body;
        if (fn.body.body.length === 1 && types.isReturnStatement(statement) && statement.argument != null) return resolve(statement.argument, next);
      }
    }
    return node;
  }

  function field(input: types.Node | undefined, key: string, seen = new Set<types.Node>()): types.Node | undefined {
    if (input === undefined) return undefined;
    const node = resolve(input, seen);
    if (types.isNullLiteral(node) || types.isBooleanLiteral(node) || types.isFunctionDeclaration(node) || types.isArrowFunctionExpression(node)) return undefined;
    if (!types.isObjectExpression(node)) throw new Error(`cannot statically verify ${key} isolation parameters`);
    const next = new Set(seen).add(node);
    // JavaScript object spreads are shallow and later properties win.
    for (const property of [...node.properties].reverse()) {
      if (types.isSpreadElement(property)) {
        const value = field(property.argument, key, next);
        if (value !== undefined) return value;
      } else {
        const name = keyName(property.key, property.computed);
        if (name === undefined) throw new Error('computed isolation parameter key is not statically known');
        if (name === key) {
          if (!types.isObjectProperty(property)) throw new Error(`method ${key} cannot define isolation parameters`);
          return property.value;
        }
      }
    }
    return undefined;
  }

  function descend(layers: readonly types.Node[], key: string): types.Node[] {
    const children: types.Node[] = [];
    for (const layer of layers) {
      const value = field(layer, key);
      if (value === undefined) continue;
      const node = resolve(value);
      // Storybook merges parameter objects recursively; a scalar replaces
      // inherited objects. Descriptions and heights do not erase inline:false.
      if (!types.isObjectExpression(node)) children.length = 0;
      children.push(node);
    }
    return children;
  }
  return { field, descend, resolve, sharedNodes };
}

/** Check each app export's effective metadata and story parameters. */
export function validateAppDocsFrames(modules: readonly { path: string; text: string }[]): number {
  const failures: string[] = [];
  let framed = 0;
  for (const { path, text } of modules) {
    try {
      const ast = babelParse(text);
      const reader = parameterReader(path, ast);
      const declaration = ast.program.body.find((node) => types.isExportDefaultDeclaration(node));
      if (!types.isExportDefaultDeclaration(declaration)) throw new Error('missing CSF metadata');
      const metaParameters = reader.field(declaration.declaration, 'parameters');
      const csf = loadCsf(text, { fileName: path, makeTitle: (title) => title }).parse();
      const exports = new Set(csf.indexInputs.map((input) => input.exportName));
      const annotations = new Set<types.Node>();
      for (const statement of ast.program.body) {
        if (!types.isExpressionStatement(statement)) {
          if (types.isDeclaration(statement) || types.isImportDeclaration(statement)
            || types.isExportDeclaration(statement) || types.isEmptyStatement(statement)) continue;
          throw new Error('top-level control flow cannot define static Docs isolation');
        }
        const expression = statement.expression;
        // CSF2 annotations are read by the installed parser. Other top-level
        // mutations/calls could alter a parameter alias after static resolution.
        if (types.isAssignmentExpression(expression, { operator: '=' }) && types.isMemberExpression(expression.left)
          && types.isIdentifier(expression.left.object) && exports.has(expression.left.object.name)
          && ['parameters', 'args', 'argTypes', 'render', 'play', 'tags', 'name', 'decorators'].includes(keyName(expression.left.property, expression.left.computed) ?? '')) {
          annotations.add(expression);
          continue;
        }
        throw new Error('top-level parameter mutations or calls cannot define static Docs isolation');
      }
      traverse(ast, {
        noScope: true,
        Function(location) { location.skip(); },
        AssignmentExpression(location) {
          if (!annotations.has(location.node)) throw new Error('evaluated parameter mutations cannot define static Docs isolation');
        },
        UpdateExpression() { throw new Error('evaluated parameter updates cannot define static Docs isolation'); },
      });
      let hasApp = false;
      for (const input of csf.indexInputs) {
        const name = input.exportName;
        if (name === undefined) continue;
        // The installed parser includes CSF2 annotation assignments here.
        const annotation = csf._storyAnnotations[name]?.parameters;
        const storyParameters = annotation ?? reader.field(csf.getStoryExport(name), 'parameters');
        const layers = [metaParameters, storyParameters].filter((node) => node !== undefined);
        const app = reader.descend(layers, 'app').at(-1);
        if (app === undefined) continue;
        if (!types.isObjectExpression(app)) throw new Error(`${name}: app fixture parameters must be an object`);
        hasApp = true;
        const inline = reader.descend(reader.descend(reader.descend(layers, 'docs'), 'story'), 'inline').at(-1);
        if (!types.isBooleanLiteral(inline, { value: false }) || !reader.sharedNodes.has(inline)) {
          failures.push(`${path}#${name}: app fixtures require the shared topLayerDocs frame parameters without an inline override`);
        }
      }
      if (hasApp) framed++;
    } catch (error) {
      failures.push(`${path}: ${error instanceof Error ? error.message : String(error)}`);
    }
  }
  if (failures.length > 0) throw new Error(`Storybook Docs isolation failed:\n${failures.join('\n')}`);
  return framed;
}
