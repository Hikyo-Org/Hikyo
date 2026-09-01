import { useMemo, useRef, useState } from 'react';

import { GIT_DEFINITIONS_NOTICE } from '../api/definitions.ts';
import { revisionNumber } from '../api/history.ts';
import {
  matrixMutationError,
  useCreateKey,
  useImportValues,
  useListValueOccurrences,
  type MatrixRef,
  type ValueOccurrenceList,
} from '../api/matrix.ts';
import type { KeyClassification } from '@hikyo/client';
import {
  indexOccurrences,
  needsTrim,
  parseDotenv,
  planEnvironment,
  suggestType,
  type ParseResult,
  type PrimitiveType,
} from './import-state.ts';
import { useModalDialog } from './useModalDialog.ts';

type WizardEnvironment = { readonly id: string; readonly name: string };

/** The primitives the wizard declares a new key as — `enum` is out (see
 * import-state `PrimitiveType`), so it is the same union `CreateKeyType` minus
 * `enum`, and every one is a legal `CreateKeyType` for the declare call. */
const TYPE_CHOICES: readonly PrimitiveType[] = ['string', 'integer', 'boolean', 'url', 'json'];

type Step = 'source' | 'classify' | 'review' | 'result';

/** A new key's operator-chosen declaration, gathered in the classify step. */
type Declaration = { readonly classification: KeyClassification; readonly type: PrimitiveType };

/** One environment's phase-2 outcome, for the result step. */
type EnvironmentOutcome = {
  readonly environmentId: string;
  readonly name: string;
  readonly imported: readonly string[];
  readonly skipped: readonly string[];
  readonly findingRules: readonly string[];
  readonly error: string | null;
};

/**
 * Browser dotenv import wizard (#495).
 *
 * A local .env file is read into memory, parsed with the strict grammar
 * (`import-state`), and reviewed BEFORE any plaintext leaves the browser: no
 * value rides a request until the operator starts the reviewed phase-2 write.
 * New keys are classified explicitly (secret by default) and typed only on an
 * accepted suggestion, then declared through the shared create path; values are
 * written per environment through `value.import`, which republishes and refuses
 * a moved state by name. Nothing here is retained in durable browser storage.
 */
export function ImportWizard({
  matrixRef,
  environments,
  gitManaged,
  onClose,
}: {
  matrixRef: MatrixRef;
  environments: readonly WizardEnvironment[];
  gitManaged: boolean;
  onClose: () => void;
}) {
  const dialog = useModalDialog();
  // Two independent counters guard file reads, and they must not be conflated.
  // `readSeq` bumps when a selection STARTS: an earlier, slower `file.text()`
  // that resolves after a later selection is dropped, so the committed contents
  // always come from the latest selection. `parseVersion` bumps only when a
  // parse is COMMITTED: `beginReview` pins it and, if a newer file commits
  // during phase 1a, refuses to advance — otherwise a review begun against a
  // valid file could stride past the all-or-nothing gate into a newer, invalid
  // one. A single selection-start counter cannot do both: a Review clicked
  // between a selection and its commit would pin that pending selection's number.
  const readSeq = useRef(0);
  const parseVersion = useRef(0);
  const occurrences = useListValueOccurrences(matrixRef);
  const createKey = useCreateKey(matrixRef);
  const importValues = useImportValues(matrixRef);

  const [step, setStep] = useState<Step>('source');
  const [fileName, setFileName] = useState<string | null>(null);
  const [parse, setParse] = useState<ParseResult | null>(null);
  const [selected, setSelected] = useState<ReadonlySet<string>>(
    () => new Set(environments.map((environment) => environment.id)),
  );
  // Phase-1a presence per selected environment: what the project declares and,
  // per environment, what is already `set`. Declaration is project-scoped, so
  // `declared` agrees across environments; `set` is what varies.
  const [presence, setPresence] = useState<ReadonlyMap<string, ValueOccurrenceList>>(new Map());
  const [declarations, setDeclarations] = useState<ReadonlyMap<string, Declaration>>(new Map());
  const [overwrite, setOverwrite] = useState<ReadonlyMap<string, ReadonlySet<string>>>(new Map());
  const [trimAcks, setTrimAcks] = useState<ReadonlySet<string>>(new Set());
  const [outcomes, setOutcomes] = useState<readonly EnvironmentOutcome[]>([]);
  const [declaredKeys, setDeclaredKeys] = useState<readonly string[]>([]);
  const [error, setError] = useState<string | null>(null);

  const busy = occurrences.isPending || createKey.isPending || importValues.isPending;
  const entries = parse?.entries ?? [];

  // A key is new when the project does not declare it; read from any environment
  // (declaration is project-scoped). Empty until phase 1a has run.
  const firstPresence = useMemo(() => {
    for (const environmentId of selected) {
      const list = presence.get(environmentId);
      if (list !== undefined) {
        return indexOccurrences(list.items);
      }
    }
    return null;
  }, [presence, selected]);

  const newKeys = useMemo(() => {
    if (firstPresence === null) {
      return [];
    }
    return entries
      .map((entry) => entry.key)
      .filter((name) => {
        const occurrence = firstPresence.get(name);
        return occurrence === undefined || !occurrence.declared;
      });
  }, [entries, firstPresence]);

  // Git-managed projects declare keys only through `definitions apply`; new keys
  // cannot be declared here and are dropped from the import (their values would
  // be rejected by name). Already-declared keys still import their values.
  const blockedNewKeys = gitManaged ? newKeys : [];
  const excluded = useMemo(() => new Set(blockedNewKeys), [blockedNewKeys]);
  const importableEntries = useMemo(
    () => entries.filter((entry) => !excluded.has(entry.key)),
    [entries, excluded],
  );

  const trimOffenders = useMemo(
    () => importableEntries.filter((entry) => needsTrim(entry.value)),
    [importableEntries],
  );
  const trimSettled = trimOffenders.every((entry) => trimAcks.has(entry.key));

  const selectedEnvironments = environments.filter((environment) => selected.has(environment.id));

  const beginReview = async () => {
    setError(null);
    // Pin the committed parse this review began against. If a newer file
    // commits while phase 1a is in flight, this stale run must not advance to
    // classify — otherwise it would carry the old file's entries past the
    // all-or-nothing gate the new (possibly invalid) file has not cleared.
    const version = parseVersion.current;
    try {
      const results = await Promise.all(
        selectedEnvironments.map(async (environment) => {
          const list = await occurrences.mutateAsync({
            environment: environment.id,
            // Phase 1a only discovers what the project declares; its tokens are
            // discarded, so a default intent is fine. The binding read is 1b.
            candidates: entries.map((entry) => ({
              name: entry.key,
              classification: 'secret' as const,
              type: 'string' as const,
            })),
          });
          return [environment.id, list] as const;
        }),
      );
      if (version !== parseVersion.current) {
        return;
      }
      setPresence(new Map(results));
      // Seed each new key's declaration with the conservative floor: secret, and
      // type `string`. The suggestion is shown but never preselected.
      setDeclarations((current) => {
        const next = new Map(current);
        for (const [, list] of results) {
          for (const item of list.items) {
            if (!item.declared && !next.has(item.name)) {
              next.set(item.name, { classification: 'secret', type: 'string' });
            }
          }
        }
        return next;
      });
      setStep('classify');
    } catch (caught) {
      setError(matrixMutationError(asError(caught), 'import'));
    }
  };

  const runImport = async () => {
    setError(null);
    // Declare new keys first (skipped entirely on a git-managed project). A
    // declaration failure drops that key from the import rather than failing the
    // whole batch by name at phase 2.
    const declared: string[] = [];
    const declareFailures: string[] = [];
    if (!gitManaged) {
      for (const name of newKeys) {
        const declaration = declarations.get(name) ?? { classification: 'secret', type: 'string' };
        try {
          await createKey.mutateAsync({
            name,
            classification: declaration.classification,
            rule: { type: declaration.type },
            required: { mode: 'none', environmentIds: [] },
            forbidden: { mode: 'none', environmentIds: [] },
          });
          declared.push(name);
        } catch (caught) {
          declareFailures.push(name);
        }
      }
    }
    setDeclaredKeys(declared);
    const failedDeclarations = new Set([...excluded, ...declareFailures]);

    const results: EnvironmentOutcome[] = [];
    for (const environment of selectedEnvironments) {
      const chosen = overwrite.get(environment.id) ?? new Set<string>();
      const presenceList = presence.get(environment.id);
      const index = presenceList === undefined
        ? indexOccurrences([])
        : indexOccurrences(presenceList.items);
      const plan = planEnvironment(importableEntries, index, chosen);
      // Only keys that will actually be written are sent: a `set` key without an
      // overwrite is skipped BY OMISSION, exactly as the CLI's values file omits
      // it — its plaintext never leaves the browser. Report those from the plan,
      // since the server never sees them.
      const written = new Set(plan.imported.filter((name) => !failedDeclarations.has(name)));
      const toSend = importableEntries.filter((entry) => written.has(entry.key));
      const skippedNames = [
        ...plan.skipped,
        ...plan.imported.filter((name) => failedDeclarations.has(name)),
      ];
      const base: Omit<EnvironmentOutcome, 'imported' | 'findingRules' | 'error'> = {
        environmentId: environment.id,
        name: environment.name,
        skipped: skippedNames,
      };
      if (toSend.length === 0) {
        results.push({ ...base, imported: [], findingRules: [], error: null });
        continue;
      }
      try {
        // Phase 1b: re-read now that declarations have landed, so every token
        // names the exact state phase 2 will re-check inside its transaction.
        const list = await occurrences.mutateAsync({
          environment: environment.id,
          candidates: toSend.map((entry) => ({
            name: entry.key,
            classification: 'config' as const,
            type: 'string' as const,
          })),
        });
        const tokens = indexOccurrences(list.items);
        const result = await importValues.mutateAsync({
          environment: environment.id,
          entries: toSend.map((entry) => ({ key: entry.key, value: entry.value })),
          // Overwrite consent lists only keys this run carries — naming an
          // uncarried key is refused, so it is built from `toSend`.
          overwrite: toSend.map((entry) => entry.key).filter((name) => chosen.has(name)),
          precondition: {
            definitions_revision: revisionNumber(list.definitions_revision),
            environment_ids: [environment.id],
            occurrences: toSend.flatMap((entry) => {
              const occurrence = tokens.get(entry.key);
              return occurrence === undefined
                ? []
                : [{ key: entry.key, environment_id: environment.id, token: occurrence.token }];
            }),
          },
        });
        results.push({
          ...base,
          imported: result.imported,
          // Findings are redacted (rule id + surface + locator, never the value);
          // the wizard shows only the rule ids, warn-not-block.
          findingRules: (result.findings ?? []).map((finding) => finding.rule_id),
          error: null,
        });
      } catch (caught) {
        results.push({
          ...base,
          imported: [],
          findingRules: [],
          error: matrixMutationError(asError(caught), 'import'),
        });
      }
    }
    setOutcomes([
      ...results,
      ...declareFailures.map((name) => declareFailureOutcome(name)),
    ]);
    setStep('result');
  };

  const toggle = (set: ReadonlySet<string>, id: string): Set<string> => {
    const next = new Set(set);
    if (next.has(id)) {
      next.delete(id);
    } else {
      next.add(id);
    }
    return next;
  };

  return (
    <dialog
      ref={dialog}
      className="matrix-editor import-wizard"
      onClose={onClose}
      onClick={(event) => {
        if (event.target === event.currentTarget) {
          onClose();
        }
      }}
    >
      <form method="dialog" onSubmit={(event) => event.preventDefault()}>
        <div className="matrix-editor__head">
          <div>
            <p className="matrix-editor__eyebrow">Import</p>
            <h2>Import a .env file</h2>
            <p>Reviewed on this device; values are sent only when you start the import.</p>
          </div>
          <button
            type="button"
            className="btn matrix-editor__close"
            aria-label="Close import"
            onClick={onClose}
          >
            ✕
          </button>
        </div>

        <p className="notice" role="note">
          <span aria-hidden="true">⚠</span>
          <span>
            The file is read in this browser and reviewed here. Its values are sent only when you
            start the import, and are never stored by this page.
          </span>
        </p>

        {error === null ? null : (
          <p className="alert" role="alert">
            <span className="alert__glyph" aria-hidden="true">!</span>
            <span>{error}</span>
          </p>
        )}

        {step === 'source'
          ? renderSource()
          : step === 'classify'
            ? renderClassify()
            : step === 'review'
              ? renderReview()
              : renderResult()}
      </form>
    </dialog>
  );

  function renderSource() {
    const parseErrors = parse?.errors ?? [];
    // The server parses the whole file strictly and refuses it entirely on any
    // bad line; phase 2 only ever sees parsed entries, so the browser must
    // enforce that same all-or-nothing gate here rather than partially import a
    // file the Go parser would reject.
    const canContinue =
      entries.length > 0 && parseErrors.length === 0 && selected.size > 0 && !busy;
    return (
      <>
        <fieldset>
          <legend>File</legend>
          <input
            type="file"
            aria-label="Dotenv file"
            accept=".env,text/plain"
            onChange={async (event) => {
              const file = event.target.files?.[0] ?? null;
              if (file === null) {
                return;
              }
              readSeq.current += 1;
              const read = readSeq.current;
              const text = await file.text();
              // A newer selection started while this file was being read: its
              // result wins, so drop this stale one entirely.
              if (read !== readSeq.current) {
                return;
              }
              // Committing a parse advances its version, which invalidates any
              // in-flight review pinned to the previous one.
              parseVersion.current += 1;
              setFileName(file.name);
              setParse(parseDotenv(text));
              // A new file is a fresh review: drop every choice made against the
              // previous one so a stale overwrite or trim ack can never apply to
              // a value the operator has not seen.
              setPresence(new Map());
              setDeclarations(new Map());
              setOverwrite(new Map());
              setTrimAcks(new Set());
              setStep('source');
            }}
          />
          {parse === null ? null : (
            <p className="import-wizard__summary" role="status">
              {`${fileName ?? 'file'}: ${String(entries.length)} value${entries.length === 1 ? '' : 's'} read` +
                (parseErrors.length === 0
                  ? ''
                  : `, ${String(parseErrors.length)} invalid line${parseErrors.length === 1 ? '' : 's'} skipped`)}
            </p>
          )}
          {parseErrors.length === 0 ? null : (
            <>
              <p className="alert" role="alert">
                <span className="alert__glyph" aria-hidden="true">!</span>
                <span>
                  Fix these lines at the source and choose the file again — the import is
                  all-or-nothing, so nothing is sent while any line is invalid.
                </span>
              </p>
              <ul className="import-wizard__invalid" aria-label="Invalid lines">
                {parseErrors.map((invalid) => (
                  <li key={invalid.line}>{`Line ${String(invalid.line)}: ${invalid.reason}`}</li>
                ))}
              </ul>
            </>
          )}
        </fieldset>

        <fieldset>
          <legend>Target environments</legend>
          {environments.map((environment) => (
            <label key={environment.id} className="import-wizard__env">
              <input
                type="checkbox"
                checked={selected.has(environment.id)}
                onChange={() => setSelected((current) => toggle(current, environment.id))}
              />
              {environment.name}
            </label>
          ))}
        </fieldset>

        <footer className="matrix-editor__actions">
          <button type="button" className="btn" onClick={onClose}>
            Cancel
          </button>
          <button
            type="button"
            className="btn btn--primary"
            disabled={!canContinue}
            onClick={beginReview}
          >
            {busy ? 'Reading…' : 'Review'}
          </button>
        </footer>
      </>
    );
  }

  function renderClassify() {
    return (
      <>
        {gitManaged && newKeys.length > 0 ? (
          <p className="notice" role="status">
            <span aria-hidden="true">ℹ</span>
            <span>
              {GIT_DEFINITIONS_NOTICE} {`${String(newKeys.length)} new key${newKeys.length === 1 ? '' : 's'} ` +
                `(${newKeys.join(', ')}) cannot be declared here and will be skipped; already-declared keys still import.`}
            </span>
          </p>
        ) : null}

        {newKeys.length === 0 ? (
          <p className="import-wizard__summary" role="status">
            Every key is already declared — no classification needed.
          </p>
        ) : (
          <fieldset disabled={gitManaged}>
            <legend>Classify new keys</legend>
            {newKeys.map((name) => {
              const values = entries
                .filter((entry) => entry.key === name)
                .map((entry) => entry.value);
              const suggestion = suggestType(values);
              const declaration = declarations.get(name) ?? {
                classification: 'secret' as const,
                type: 'string' as const,
              };
              return (
                <div key={name} className="import-wizard__key">
                  <span className="import-wizard__key-name">{name}</span>
                  <label className="import-wizard__secret">
                    <input
                      type="checkbox"
                      checked={declaration.classification === 'secret'}
                      onChange={(event) =>
                        setDeclarations((current) =>
                          new Map(current).set(name, {
                            ...declaration,
                            classification: event.target.checked ? 'secret' : 'config',
                          }),
                        )
                      }
                    />
                    secret
                  </label>
                  <label>
                    Type
                    <select
                      value={declaration.type}
                      onChange={(event) =>
                        setDeclarations((current) =>
                          new Map(current).set(name, {
                            ...declaration,
                            type: parseType(event.target.value),
                          }),
                        )
                      }
                    >
                      {TYPE_CHOICES.map((choice) => (
                        <option key={choice} value={choice}>
                          {choice === suggestion && choice !== 'string'
                            ? `${choice} (suggested)`
                            : choice}
                        </option>
                      ))}
                    </select>
                  </label>
                  {suggestion === 'string' ? null : (
                    <span className="import-wizard__suggestion">{`Suggested: ${suggestion}`}</span>
                  )}
                </div>
              );
            })}
          </fieldset>
        )}

        <footer className="matrix-editor__actions">
          <button type="button" className="btn" onClick={() => setStep('source')}>
            Back
          </button>
          <button type="button" className="btn btn--primary" onClick={() => setStep('review')}>
            Review changes
          </button>
        </footer>
      </>
    );
  }

  function renderReview() {
    const anySendable = importableEntries.length > 0;
    return (
      <>
        {trimOffenders.length === 0 ? null : (
          <fieldset>
            <legend>Values with surrounding whitespace</legend>
            <p className="import-wizard__summary">
              These values have leading or trailing whitespace. Acknowledge each to import it as
              written, or fix it at the source.
            </p>
            {trimOffenders.map((entry) => (
              <label key={entry.key} className="import-wizard__env">
                <input
                  type="checkbox"
                  checked={trimAcks.has(entry.key)}
                  onChange={() => setTrimAcks((current) => toggle(current, entry.key))}
                />
                {entry.key}
              </label>
            ))}
          </fieldset>
        )}

        {selectedEnvironments.map((environment) => {
          const list = presence.get(environment.id);
          const index = list === undefined ? indexOccurrences([]) : indexOccurrences(list.items);
          const plan = planEnvironment(
            importableEntries,
            index,
            overwrite.get(environment.id) ?? new Set<string>(),
          );
          return (
            <fieldset key={environment.id}>
              <legend>{environment.name}</legend>
              <p className="import-wizard__summary">
                {`${String(plan.imported.length)} to import` +
                  (plan.newKeys.length === 0
                    ? ''
                    : `, ${String(plan.newKeys.length)} new key${plan.newKeys.length === 1 ? '' : 's'} declared`) +
                  (plan.skipped.length === 0
                    ? ''
                    : `, ${String(plan.skipped.length)} skipped (already set)`)}
              </p>
              {plan.collisions.length === 0 ? null : (
                <div className="import-wizard__collisions">
                  <span>Overwrite already-set values:</span>
                  {plan.collisions.map((name) => (
                    <label key={name} className="import-wizard__env">
                      <input
                        type="checkbox"
                        checked={(overwrite.get(environment.id) ?? new Set()).has(name)}
                        onChange={() =>
                          setOverwrite((current) => {
                            const next = new Map(current);
                            next.set(environment.id, toggle(current.get(environment.id) ?? new Set(), name));
                            return next;
                          })
                        }
                      />
                      {name}
                    </label>
                  ))}
                </div>
              )}
            </fieldset>
          );
        })}

        <footer className="matrix-editor__actions">
          <button type="button" className="btn" onClick={() => setStep('classify')}>
            Back
          </button>
          <button
            type="button"
            className="btn btn--primary"
            disabled={!anySendable || !trimSettled || busy}
            onClick={runImport}
          >
            {busy ? 'Importing…' : 'Import'}
          </button>
        </footer>
      </>
    );
  }

  function renderResult() {
    return (
      <>
        {declaredKeys.length === 0 ? null : (
          <p className="import-wizard__summary" role="status">
            {`Declared ${String(declaredKeys.length)} new key${declaredKeys.length === 1 ? '' : 's'}: ${declaredKeys.join(', ')}.`}
          </p>
        )}
        <ul className="import-wizard__results" aria-label="Import results">
          {outcomes.map((outcome) => (
            <li key={outcome.environmentId}>
              <strong>{outcome.name}</strong>
              {outcome.error === null ? (
                <span>
                  {` imported ${String(outcome.imported.length)}` +
                    (outcome.skipped.length === 0
                      ? ''
                      : `, skipped ${String(outcome.skipped.length)} (${outcome.skipped.join(', ')})`) +
                    (outcome.findingRules.length === 0
                      ? ''
                      : ` — secret-scanning warnings: ${outcome.findingRules.join(', ')}`)}
                </span>
              ) : (
                <span className="import-wizard__error"> {outcome.error}</span>
              )}
            </li>
          ))}
        </ul>
        <footer className="matrix-editor__actions">
          <button type="button" className="btn btn--primary" onClick={onClose}>
            Done
          </button>
        </footer>
      </>
    );
  }
}

function parseType(value: string): PrimitiveType {
  const match = TYPE_CHOICES.find((choice) => choice === value);
  // The <select> can only emit a declared option; a value outside the set is a
  // DOM tamper, and the floor is the conservative fallback.
  return match ?? 'string';
}

function asError(caught: unknown): Error {
  return caught instanceof Error ? caught : new Error('import failed');
}

function declareFailureOutcome(name: string): EnvironmentOutcome {
  return {
    environmentId: `declare:${name}`,
    name: `Declaration of ${name}`,
    imported: [],
    skipped: [],
    findingRules: [],
    error: 'The key could not be declared, so its values were not imported.',
  };
}
