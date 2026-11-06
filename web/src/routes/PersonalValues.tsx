import { useCallback, useEffect, useMemo, useState } from 'react';
import { generatePath, Link, useParams } from 'react-router';

import { useAuth } from '../app/AuthProvider.tsx';
import { surfaceById } from '../app/navigation.ts';
import { plural } from '../app/duration.ts';
import { useTotpStatus } from '../api/account.ts';
import type { EnvRef } from '../api/keys.ts';
import {
  NOTICE_CAUSE_TEXT,
  PERSONAL_STATEMENT,
  personalSetRefusalText,
  useDeclaredKeys,
  useDeletePersonalValue,
  usePersonalValues,
  useRevealPersonalValue,
  useSetPersonalValue,
  type PersonalValueEntry,
} from '../api/personal.ts';
import { useSensitiveState } from '../api/sensitiveMutation.ts';
import { useWorkspaceContext, withRemote, useTransport } from '../api/transport.tsx';
import { disclosureRefusalText, fetchRevealWindow, useEnvironments, useRevealWindow } from '../api/values.ts';
import { Alert } from '../ui/Alert.tsx';
import { Badge } from '../ui/Badge.tsx';
import { Button } from '../ui/Button.tsx';
import { CeremonyNotice } from '../ui/CeremonyNotice.tsx';
import { Dialog } from '../ui/Dialog.tsx';
import { Select } from '../ui/Select.tsx';
import { SidePanel } from '../ui/SidePanel.tsx';
import { Textarea } from '../ui/Textarea.tsx';
import { Ceremony, type CeremonyPurpose } from './Ceremony.tsx';
import { useCeremonyTask, type CeremonyTask } from './useCeremonyTask.ts';
import { FOCUS_REMASK_NOTICE, useRemaskOnFocusLoss, waitForDisclosureFocus } from './useRemaskOnFocusLoss.ts';

/**
 * My personal values (personal-values ADR, O3): the owner's own values for one
 * shared environment, masked, with the checklist of required keys, removal
 * notices with their cause, suspension and schema warnings. Set and reveal take
 * the environment's reveal ceremony, approved only by the owner's passkey or
 * authenticator app. There is no `.env` download (O7): running a program needs
 * the CLI.
 */
export function PersonalValues() {
  const params = useParams();
  const org = params['org'] ?? '';
  const project = params['project'] ?? '';
  const environment = params['environment'] ?? '';
  // Keyed per environment so plaintext, a staged ceremony and an open form die on navigation.
  return <PersonalSurface key={`${org}/${project}/${environment}`} env={{ org, project, environment }} />;
}

const MASK = '••••••••';
const REMASK_MS = 10_000;

type Shown = { readonly key: string; readonly value: string; readonly until: number };

function PersonalSurface({ env }: { env: EnvRef }) {
  const transport = useTransport();
  const workspace = useWorkspaceContext();
  const remote = workspace?.remote ?? '';
  const auth = useAuth();
  const list = usePersonalValues(env);
  const catalogue = useDeclaredKeys(env);
  const environments = useEnvironments(env);
  const guard = useRevealWindow(env);
  const factors = useTotpStatus();
  const reveal = useRevealPersonalValue(env);
  const save = useSetPersonalValue(env);
  const remove = useDeletePersonalValue(env);
  const ceremony = useCeremonyTask([env.org, env.project, env.environment]);

  const [shown, setShown] = useSensitiveState<Shown | null>(null);
  const [now, setNow] = useState(() => Date.now());
  const [refusal, setRefusal] = useState<string | null>(null);
  // After a refusal that followed a skipped prompt, one explicit retry may force
  // the ceremony. A reveal retry holds only the key name; a set never holds the
  // value outside the panel's sensitive state, so its retry is the next save.
  const [retry, setRetry] = useState<(() => void) | null>(null);
  const [forceSet, setForceSet] = useState(false);
  const [done, setDone] = useState<string | null>(null);
  const [editing, setEditing] = useState<{ readonly key: string } | null>(null);
  const [deleting, setDeleting] = useState<PersonalValueEntry | null>(null);

  const focus = useRemaskOnFocusLoss(useCallback(() => setShown(null), [setShown]));
  useEffect(() => {
    if (shown === null) return;
    const id = setInterval(() => setNow(Date.now()), 250);
    return () => clearInterval(id);
  }, [shown]);
  useEffect(() => {
    if (shown !== null && shown.until <= now) setShown(null);
  }, [now, setShown, shown]);

  const environmentName = environments.data?.items.find((item) => item.id === env.environment)?.name ?? env.environment;
  const protectedEnvironment = guard.data?.protected === true;
  const hasFactor = factors.data === undefined || factors.data.has_passkey || factors.data.confirmed;
  const federatedSession = auth.identity?.session.assurance.method.startsWith('oidc:') === true ||
    auth.identity?.session.assurance.method.startsWith('saml:') === true;
  const entries = useMemo(() => sortEntries(list.data?.items ?? []), [list.data]);
  const keyIdOf = useCallback(
    (name: string) =>
      entries.find((entry) => entry.name === name)?.key_id ??
      catalogue.data?.items.find((key) => key.name === name)?.id ?? '',
    [catalogue.data, entries],
  );
  const matrixPath = withRemote(generatePath(surfaceById('matrix').path, env), remote);
  const valuesPath = withRemote(generatePath(surfaceById('values').path, env), remote);

  /**
   * The window gates the prompt, never the check (values.ts): a live window
   * skips the modal; anything else, or `force` after a refusal, asks first.
   */
  const withCeremony = useCallback(async (
    key: string,
    purpose: CeremonyPurpose,
    act: (task: CeremonyTask) => Promise<void>,
    force = false,
  ) => {
    const task = ceremony.begin([purpose, key, String(force)]);
    setRefusal(null);
    setRetry(null);
    let state;
    try {
      state = await fetchRevealWindow(env, transport.client, task.signal);
    } catch {
      if (ceremony.commit(task, () => setRefusal('The reveal window could not be read, so nothing was done.'))) ceremony.finish(task);
      return;
    }
    if (!ceremony.isCurrent(task)) return;
    if (!state.can_reveal) {
      if (ceremony.commit(task, () => setRefusal('Reveal is not granted to you here, so personal values cannot be set or shown.'))) {
        ceremony.finish(task);
      }
      return;
    }
    const run = async () => {
      try {
        await act(task);
      } finally {
        ceremony.finish(task);
      }
    };
    if (!force && state.live && !state.single_decision) {
      await run();
      return;
    }
    ceremony.stage(task, {
      purpose,
      environmentId: env.environment,
      environmentName,
      keys: [{ id: keyIdOf(key), name: key, classification: 'secret' }],
      window: state,
      possessionOnly: true,
    }, () => void run());
  }, [ceremony, env, environmentName, keyIdOf, transport.client]);

  const doReveal = (key: string, force = false) => {
    void withCeremony(key, 'reveal', async (task) => {
      if (!await waitForDisclosureFocus(task.signal) || !ceremony.isCurrent(task)) return;
      const generation = focus.current;
      try {
        const fresh = await reveal.mutateAsync(key);
        ceremony.commit(task, () => {
          if (generation !== focus.current || document.hidden || !document.hasFocus()) {
            setRefusal(FOCUS_REMASK_NOTICE);
            return;
          }
          const at = Date.now();
          setNow(at);
          setShown({ key, value: fresh.value ?? '', until: at + REMASK_MS });
        });
      } catch (error) {
        ceremony.commit(task, () => {
          setShown(null);
          setRefusal(disclosureRefusalText(error));
          if (!force) setRetry(() => () => doReveal(key, true));
        });
      }
    }, force);
  };

  const doSet = (key: string, value: string, force = false) => {
    void withCeremony(key, 'personal', async (task) => {
      try {
        await save.mutateAsync({ key, value });
        ceremony.commit(task, () => {
          setEditing(null);
          setForceSet(false);
          setShown(null);
          setDone(`Your personal value for ${key} is saved.`);
        });
      } catch (error) {
        ceremony.commit(task, () => {
          setRefusal(personalSetRefusalText(error, { hasFactor, federatedSession, protectedEnvironment }));
          if (!force && hasFactor && !protectedEnvironment) setForceSet(true);
        });
      }
    }, force);
  };

  const doDelete = (entry: PersonalValueEntry) => {
    setRefusal(null);
    setDone(null);
    remove.mutate(entry.name, {
      onSuccess: () => {
        setDeleting(null);
        setShown(null);
        setDone(entry.set ? `Your personal value for ${entry.name} is deleted.` : `The notice for ${entry.name} is dismissed.`);
      },
      onError: () => {
        setDeleting(null);
        setRefusal(`${entry.set ? 'The value' : 'The notice'} for ${entry.name} could not be removed. Reload and try again.`);
      },
    });
  };

  return (
    <section className="page page--chrome personal-values" aria-labelledby="personal-title">
      <h1 id="personal-title">My personal values</h1>
      <p className="page__lede">
        Your own values in <strong>{environmentName}</strong>. When you run a program with the CLI, each one replaces the
        shared value for you only. Everyone else keeps the shared value.
      </p>
      <CeremonyNotice glyph="lock">{PERSONAL_STATEMENT}</CeremonyNotice>
      {protectedEnvironment ? (
        <Alert tone="info">This environment is protected. Personal values are for unprotected environments only.</Alert>
      ) : null}
      {!hasFactor ? (
        <Alert tone="warn">
          Setting or revealing a personal value needs an authenticator app or passkey on your account. Add one under{' '}
          <Link to="/settings?tab=factors">Settings › Security</Link>.
        </Alert>
      ) : null}
      {refusal === null ? null : (
        <Alert action={retry === null ? undefined : (
          <Button type="button" onClick={() => retry()}>Approve with a passkey or authenticator</Button>
        )}>{refusal}</Alert>
      )}
      {done === null ? null : <Alert tone="done">{done}</Alert>}

      <div className="actions">
        <Button
          type="button"
          variant="primary"
          disabled={protectedEnvironment || !hasFactor || catalogue.data === undefined}
          onClick={() => { setDone(null); setEditing({ key: '' }); }}
        >
          Set a value
        </Button>
        <Link className="btn btn--quiet" to={valuesPath}>Shared values</Link>
        <Link className="btn btn--quiet" to={matrixPath}>Environment matrix</Link>
      </div>

      {list.isPending ? <p role="status">Loading your personal values…</p> : null}
      {list.isError ? <Alert>Your personal values could not be loaded. Reload to try again.</Alert> : null}
      {list.isSuccess && entries.length === 0 ? (
        <p role="status">You hold no personal values here, and none are required.</p>
      ) : null}
      {entries.length === 0 ? null : (
        <div className="table-scroll">
          <table className="table--cards">
            <caption className="visually-hidden">Your personal values, masked until you reveal one.</caption>
            <thead>
              <tr>
                <th scope="col">Key</th>
                <th scope="col">Your value</th>
                <th scope="col">State</th>
                <th scope="col">Actions</th>
              </tr>
            </thead>
            <tbody>
              {entries.map((entry) => {
                const live = shown?.key === entry.name ? shown : null;
                return (
                  <tr key={entry.name}>
                    <th scope="row" className="mono">{entry.name}</th>
                    <td>
                      {live !== null ? (
                        <span className="mono personal-values__plain">
                          {live.value}
                          <span className="personal-values__countdown" aria-hidden="true">
                            {`re-masks in ${plural(Math.max(0, Math.ceil((live.until - now) / 1000)), 'second')}`}
                          </span>
                        </span>
                      ) : entry.set ? (
                        <span className="mono" aria-label={`${entry.name} is masked`}>{MASK}</span>
                      ) : (
                        <span className="personal-values__absent">not set</span>
                      )}
                    </td>
                    <td><EntryState entry={entry} /></td>
                    <td>
                      <div className="actions">
                        {entry.set && entry.suspended !== true ? (
                          <Button type="button" aria-label={`Reveal ${entry.name}`} onClick={() => doReveal(entry.name)}>
                            Reveal
                          </Button>
                        ) : null}
                        <Button
                          type="button"
                          aria-label={`${entry.set ? 'Replace' : 'Set'} ${entry.name}`}
                          disabled={protectedEnvironment || !hasFactor}
                          onClick={() => { setDone(null); setEditing({ key: entry.name }); }}
                        >
                          {entry.set ? 'Replace' : 'Set'}
                        </Button>
                        {entry.set ? (
                          <Button type="button" variant="danger" aria-label={`Delete ${entry.name}`} onClick={() => setDeleting(entry)}>
                            Delete
                          </Button>
                        ) : null}
                        {!entry.set && entry.notice !== undefined ? (
                          <Button type="button" aria-label={`Dismiss the notice for ${entry.name}`} disabled={remove.isPending} onClick={() => doDelete(entry)}>
                            Dismiss notice
                          </Button>
                        ) : null}
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      <p className="personal-values__cli">
        To run a program with these values, mint a developer credential with <span className="mono">hikyo dev session --personal</span>{' '}
        and use <span className="mono">hikyo run</span>. The browser offers no download.
      </p>

      {editing === null ? null : (
        <SetValuePanel
          initialKey={editing.key}
          keys={(catalogue.data?.items ?? []).map((key) => key.name)}
          environmentName={environmentName}
          saving={save.isPending}
          refusal={refusal}
          forced={forceSet}
          onSave={(key, value) => doSet(key, value, forceSet)}
          onClose={() => { setEditing(null); setForceSet(false); }}
        />
      )}
      {deleting === null ? null : (
        <Dialog
          title={`Delete your value for ${deleting.name}?`}
          lede={`Programs you run in ${environmentName} use the shared value for ${deleting.name} again, if there is one.`}
          onCancel={() => setDeleting(null)}
          actions={(
            <>
              <Button type="button" onClick={() => setDeleting(null)}>Cancel</Button>
              <Button type="button" variant="danger" disabled={remove.isPending} onClick={() => doDelete(deleting)}>
                Delete my value
              </Button>
            </>
          )}
        >
          <p>The value is destroyed. Hikyo keeps no history of personal values.</p>
        </Dialog>
      )}
      {ceremony.request !== null ? (
        <Ceremony
          key={ceremony.requestKey}
          request={ceremony.request}
          onAuthorised={ceremony.onAuthorised}
          onCancel={ceremony.onCancel}
        />
      ) : null}
    </section>
  );
}

/** Required keys first, then by name, so the checklist leads. */
function sortEntries(items: readonly PersonalValueEntry[]): PersonalValueEntry[] {
  const rank = (entry: PersonalValueEntry) => (entry.required === true && !entry.set ? 0 : entry.notice !== undefined ? 1 : 2);
  return [...items].sort((a, b) => rank(a) - rank(b) || a.name.localeCompare(b.name));
}

/** Every state a value can be in, as words beside its tag. */
function EntryState({ entry }: { entry: PersonalValueEntry }) {
  const parts = [];
  if (entry.required === true) {
    parts.push(
      <Badge key="required" tone={entry.set ? 'ok' : 'danger'} icon={entry.set ? 'check' : 'alert'}>
        {entry.set ? 'required · set' : 'required · missing'}
      </Badge>,
    );
  }
  if (entry.notice !== undefined) {
    parts.push(
      <span key="notice" className="personal-values__state">
        <Badge tone="danger" icon="minus">removed</Badge>{' '}
        {NOTICE_CAUSE_TEXT[entry.notice]}. <span className="mono">hikyo run</span> refuses this key until you set a value or dismiss the notice.
      </span>,
    );
  }
  if (entry.suspended === true) {
    parts.push(
      <span key="suspended" className="personal-values__state">
        <Badge tone="changed" icon="pause">suspended</Badge>{' '}
        Not delivered while you lack access here
        {entry.suspended_at === undefined ? '' : `, since ${new Date(entry.suspended_at).toLocaleDateString()}`}. It is deleted 14 days after
        access was lost unless access returns.
      </span>,
    );
  }
  for (const warning of entry.warnings ?? []) {
    parts.push(
      <span key={`warning-${warning}`} className="personal-values__state">
        <Badge tone="changed" icon="warn">warning</Badge> {warning}. Your value is still delivered.
      </span>,
    );
  }
  if (entry.set && entry.updated_at !== undefined && parts.length === 0) {
    parts.push(<span key="updated">Changed {new Date(entry.updated_at).toLocaleString()}</span>);
  }
  return <div className="personal-values__states">{parts.length === 0 ? <span>set</span> : parts}</div>;
}

/** The form a Set or Replace button opens, in the side panel. */
function SetValuePanel({
  initialKey,
  keys,
  environmentName,
  saving,
  refusal,
  forced,
  onSave,
  onClose,
}: {
  initialKey: string;
  keys: readonly string[];
  environmentName: string;
  saving: boolean;
  /** Why the last save failed, repeated in the panel so it is read where the value is typed again. */
  refusal: string | null;
  /** The next save asks for the passkey or authenticator app even inside a live window. */
  forced: boolean;
  onSave: (key: string, value: string) => void;
  onClose: () => void;
}) {
  const [key, setKey] = useState(initialKey);
  const [value, setValue] = useSensitiveState('');
  return (
    <SidePanel title={initialKey === '' ? 'Set a personal value' : `Personal value for ${initialKey}`} onClose={onClose}>
      <CeremonyNotice glyph="lock">{PERSONAL_STATEMENT}</CeremonyNotice>
      {refusal === null ? null : <Alert>{refusal}</Alert>}
      <form
        className="form"
        onSubmit={(event) => {
          event.preventDefault();
          if (key === '' || value === '') return;
          onSave(key, value);
          setValue('');
        }}
      >
        {initialKey === '' ? (
          <Select label="Key" hint="A declared key of this project." value={key} onChange={(event) => setKey(event.target.value)} required>
            <option value="">Choose a key</option>
            {keys.map((name) => <option key={name} value={name}>{name}</option>)}
          </Select>
        ) : null}
        <Textarea
          label={`Your value for ${key === '' ? 'the key' : key} in ${environmentName}`}
          hint="Always kept secret and never validated against the key's rules. Saving asks for your passkey or authenticator app."
          mono
          autoComplete="off"
          spellCheck={false}
          rows={3}
          value={value}
          onChange={(event) => setValue(event.target.value)}
        />
        <div className="actions">
          <Button type="submit" variant="primary" disabled={saving || key === '' || value === ''}>
            {saving ? 'Saving…' : forced ? 'Approve with a passkey or authenticator and save' : 'Save my value'}
          </Button>
          <Button type="button" onClick={onClose}>Cancel</Button>
        </div>
      </form>
    </SidePanel>
  );
}
