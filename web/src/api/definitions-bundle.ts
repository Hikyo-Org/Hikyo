import {
  applyDefinitionsPlanOp,
  checkDefinitionsOp,
  createDefinitionsPlanOp,
  getDefinitionsPlanOp,
  getDefinitionsSettingsOp,
} from '@hikyo/operations';
import {
  zDefinitionsBundle,
  zDefinitionsPlan,
  zKeyRule,
  zKeyDeclaration,
  zDefinitionsBundleKey,
  zDefinitionsBundleEntity,
  zDefinitionsBundlePresence,
} from '@hikyo/zod';
import type { DefinitionsBundle, DefinitionsDiff } from '@hikyo/client';
import { ApiError, parsed } from './client.ts';
import { z } from 'zod';
import { parseDocument } from 'yaml';
import type { MatrixRef } from './keys.ts';
import type { TransportOptions } from './transport.tsx';
export type { DefinitionsBundle, DefinitionsDiff };
export type DefinitionsPlan = z.infer<typeof zDefinitionsPlan>;

// Reject misspelled nested constraints instead of stripping them and sending
// a different, weaker definition to the instance.
const fileInteger = z
  .number()
  .refine(Number.isInteger)
  .transform((value) => {
    if (!Number.isSafeInteger(value))
      throw new Error(
        'This bundle contains an integer outside the browser’s exact JSON range. Use the CLI to preserve its value.',
      );
    return BigInt(value);
  });
const fileRule = zKeyRule.strict().extend({
  min: fileInteger.optional(),
  max: fileInteger.optional(),
});
const fileKey = zDefinitionsBundleKey.strict().extend({
  declaration: zKeyDeclaration.strict().extend({
    rule: fileRule.optional(),
    any_of: z.array(fileRule).min(2).max(8).optional(),
  }),
  required_in: zDefinitionsBundlePresence.strict(),
  forbidden_in: zDefinitionsBundlePresence.strict(),
});
const fileBundle = zDefinitionsBundle.strict().extend({
  base_revision: fileInteger.refine((value) => value >= 0n).optional(),
  environments: z.array(zDefinitionsBundleEntity.strict()).max(10000),
  key_groups: z.array(zDefinitionsBundleEntity.strict()).max(10000),
  keys: z.array(fileKey).max(10000),
});

/** Canonical export is JSON; input stays in the mounted dialog, never a URL/cache. */
export async function readDefinitionsFile(file: File): Promise<DefinitionsBundle> {
  if (file.size > 1_048_576) throw new Error('Choose a definitions bundle no larger than 1 MiB.');
  try {
    const text = await file.text();
    const bundle = fileBundle.parse(JSON.parse(text));
    // JSON.parse accepts duplicate members by keeping the last. Reject them
    // rather than publish a silent reinterpretation of the chosen file.
    if (parseDocument(text, { uniqueKeys: true }).errors.length > 0) {
      throw new Error('Definitions bundles cannot contain duplicate object fields.');
    }
    return {
      ...bundle,
      base_revision: wireInteger(bundle.base_revision),
      keys: bundle.keys.map((key) => ({
        ...key,
        declaration: {
          ...key.declaration,
          rule: key.declaration.rule === undefined ? undefined : wireRule(key.declaration.rule),
          any_of: key.declaration.any_of?.map(wireRule),
        },
      })),
    };
  } catch (error) {
    if (error instanceof SyntaxError || error instanceof z.ZodError) {
      throw new Error('Choose a valid JSON definitions bundle exported by Hikyo.');
    }
    throw error;
  }
}
function wireRule(rule: z.infer<typeof zKeyRule>) {
  return { ...rule, min: wireInteger(rule.min), max: wireInteger(rule.max) };
}
/** Generated response schemas use bigint; JSON requests require exact safe integers. */
function wireInteger(value: bigint | undefined): number | undefined {
  if (value === undefined) return undefined;
  const number = Number(value);
  if (!Number.isSafeInteger(number))
    throw new Error(
      'This bundle contains an integer outside the browser’s exact JSON range. Use the CLI to preserve its value.',
    );
  return number;
}
export function definitionsExportPath(ref: MatrixRef): string {
  return `/api/v1/orgs/${encodeURIComponent(ref.org)}/projects/${encodeURIComponent(ref.project)}/definitions/export`;
}
export function checkBundle(
  ref: MatrixRef,
  body: DefinitionsBundle,
  transport: TransportOptions,
  signal: AbortSignal,
) {
  return parsed(checkDefinitionsOp, { path: ref, body, ...transport, signal });
}
export async function planBundle(
  ref: MatrixRef,
  body: DefinitionsBundle,
  transport: TransportOptions,
  signal: AbortSignal,
  acknowledgements: readonly string[] = [],
) {
  const created = await parsed(createDefinitionsPlanOp, {
    path: ref,
    body,
    ...transport,
    signal,
    query: acknowledgements.length === 0 ? undefined : { acknowledge: [...acknowledgements] },
  });
  // Render the stored immutable plan, with its server-owned digest and expiry.
  return parsed(getDefinitionsPlanOp, {
    path: { ...ref, plan: created.plan.id },
    ...transport,
    signal,
  });
}
export async function applyBundle(
  ref: MatrixRef,
  plan: DefinitionsPlan,
  allowDelete: boolean,
  transport: TransportOptions,
  signal: AbortSignal,
  acknowledgements: readonly string[] = [],
) {
  // Recheck policy after impact review. The server intentionally allows CLI
  // apply in Git mode; the browser must preserve its read-only governance.
  const settings = await parsed(getDefinitionsSettingsOp, {
    path: ref,
    ...transport,
    signal,
  });
  if (settings.definitions_source !== 'db')
    throw new ApiError(
      409,
      'Git-managed project',
      'This project is now Git-managed. Apply from the repository with definitions plan / definitions apply.',
    );
  return parsed(applyDefinitionsPlanOp, {
    path: { ...ref, plan: plan.id },
    ...transport,
    signal,
    body: {
      digest: plan.digest,
      allow_delete: allowDelete,
      acknowledgements: [...acknowledgements],
    },
  });
}
export function bundleRefusalText(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.detail) return error.detail;
    if (error.status === 403 || error.status === 404)
      return 'This operation was refused. You need definitions-edit on this project and publish on every affected environment to apply. The project or plan may also be unavailable to you.';
    if (error.status === 409)
      return 'This bundle or plan is stale, expired, or conflicts with current state. Download the current bundle, reconcile your changes, and check again.';
    if (error.status === 429)
      return 'The instance refused this operation because its request or open-plan limit was reached. Try again later.';
    return `The instance refused the bundle operation (HTTP ${error.status}). Check the bundle and try again.`;
  }
  return 'The bundle operation could not complete. Check your connection and try again.';
}
