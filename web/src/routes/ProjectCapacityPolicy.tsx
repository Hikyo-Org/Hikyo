import { useContext, useId, useState } from 'react';

import { projectCapacityFailureText, useProjectCapacity, useSetProjectCapacity, type ProjectCapacity } from '../api/projectCapacity.ts';
import { ApiError } from '../api/client.ts';
import { useResetOnChange } from '../app/useResetOnChange.ts';
import { Alert } from '../ui/Alert.tsx';
import { Button } from '../ui/Button.tsx';
import { ChoiceGroup } from '../ui/ChoiceGroup.tsx';
import { Input } from '../ui/Input.tsx';
import { Radio } from '../ui/Radio.tsx';
import { ReadOnlyContext } from '../ui/ReadOnlyContext.ts';
import { SidePanel } from '../ui/SidePanel.tsx';
import { useFeedback } from './useFeedback.ts';
import './ProjectCapacityPolicy.css';

const count = (value: number) => value.toLocaleString('en-US');

/** Project policy metadata. Mount only after can_manage_policy is established. */
export function ProjectCapacityPolicy({ org, project }: { org: string; project: string }) {
  const policy = useProjectCapacity(org, project);
  const update = useSetProjectCapacity(org, project);
  const feedback = useFeedback(projectCapacityFailureText);
  const [editing, setEditing] = useState(false);
  const [conflicted, setConflicted] = useState(false);
  const current = policy.data;
  useResetOnChange(`${org}\u0000${project}\u0000${current === null}`, () => { setEditing(false); setConflicted(false); feedback.clear(); });

  return <section id="project-capacity" className="settings-row settings-row--stacked" aria-label="Key capacity">
    <div className="settings-row__copy">
      <span className="settings-row__title">Key capacity</span>
      {current == null ? null : <>
        <span className="settings-row__detail">{count(current.key_count)} keys used / {count(current.effective_limit)} key limit</span>
        <span className="settings-row__detail">{current.inherited ? 'Inherited from the instance default' : `Project override: ${count(current.key_limit_override ?? current.effective_limit)} keys`}. Instance default: {count(current.instance_default)}; maximum: {count(current.instance_maximum)}.</span>
      </>}
    </div>
    {policy.isPending ? <p role="status">Loading key capacity…</p> : null}
    {policy.isError ? <Alert>Key capacity could not be read. Refresh to try again.</Alert> : null}
    {feedback.done === null ? null : <Alert tone="done">{feedback.done}</Alert>}
    {current == null ? null : <>
      {current.capped ? <Alert>The stored project override exceeds the instance maximum. The effective limit is capped at {count(current.effective_limit)} keys.</Alert> : null}
      {current.key_count > current.effective_limit ? <Alert>This project is over its effective key limit. Existing keys are preserved. Remove keys or raise the limit before adding more.</Alert> : null}
      <p className="settings-note">{count(current.environment_count)} environments. The separate 100,000-cell budget still applies. Lowering a limit never deletes existing keys.</p>
    </>}
    <div className="actions">
      {current == null ? null : <Button type="button" disabled={policy.isError} onClick={() => { feedback.clear(); setConflicted(false); setEditing(true); }}>Edit key capacity</Button>}
      <Button type="button" readOnlySafe disabled={policy.isFetching || update.isPending} onClick={() => { void policy.refetch(); }}>Refresh key capacity</Button>
    </div>
    {current == null ? null : <>
      {editing ? <CapacityEditor current={current} busy={update.isPending} failure={feedback.failure} conflicted={conflicted}
        onClose={() => { if (!update.isPending) setEditing(false); }}
        onRefresh={async () => {
          const refreshed = await policy.refetch();
          if (!refreshed.isSuccess || refreshed.data === null) return null;
          setConflicted(false);
          feedback.clear();
          return refreshed.data;
        }}
        onSave={(override, base) => {
          feedback.clear();
          update.mutate({ key_limit_override: override, expected_revision: base.revision,
            expected_instance_default: base.instance_default, expected_instance_maximum: base.instance_maximum }, {
            onSuccess: () => { setEditing(false); feedback.ok('Key capacity saved. Existing keys are preserved.'); },
            onError: (error) => { setConflicted(error instanceof ApiError && error.status === 409); feedback.report(error); },
          });
        }} /> : null}
    </>}
  </section>;
}

function CapacityEditor({ current, busy, failure, conflicted, onClose, onRefresh, onSave }: {
  current: ProjectCapacity;
  busy: boolean;
  failure: string | null;
  conflicted: boolean;
  onClose: () => void;
  onRefresh: () => Promise<ProjectCapacity | null>;
  onSave: (override: number | null, base: ProjectCapacity) => void;
}) {
  const readOnly = useContext(ReadOnlyContext);
  const name = useId();
  // Keep the edit snapshot and the user's draft stable across background refetches.
  const [base, setBase] = useState(current);
  const [inherited, setInherited] = useState(current.inherited);
  const [value, setValue] = useState(String(current.key_limit_override ?? current.instance_default));
  const [validation, setValidation] = useState<string>();
  const [refreshing, setRefreshing] = useState(false);
  const stale = conflicted || current.revision !== base.revision || current.instance_default !== base.instance_default || current.instance_maximum !== base.instance_maximum;
  const disabled = busy || refreshing || readOnly;
  const proposed = inherited ? Math.min(base.instance_default, base.instance_maximum) : Number(value);
  const validProposal = Number.isSafeInteger(proposed) && proposed > 0 && proposed <= base.instance_maximum;

  return <SidePanel title="Edit key capacity" onClose={onClose}>
    <form className="project-capacity__form" onSubmit={(event) => {
      event.preventDefault();
      if (disabled || stale) return;
      if (!inherited && (!/^\d+$/.test(value) || !Number.isSafeInteger(proposed) || proposed < 1 || proposed > base.instance_maximum)) {
        setValidation(`Enter a whole number from 1 to ${count(base.instance_maximum)}.`);
        return;
      }
      onSave(inherited ? null : proposed, base);
    }}>
      <ChoiceGroup legend="Key limit source" hint={`Instance default: ${count(base.instance_default)} keys. Instance maximum: ${count(base.instance_maximum)} keys.`}>
        <Radio name={name} label="Inherit instance default" checked={inherited} disabled={disabled} onChange={() => { setInherited(true); setValidation(undefined); }} />
        <Radio name={name} label="Custom project limit" checked={!inherited} disabled={disabled} onChange={() => setInherited(false)} />
      </ChoiceGroup>
      {inherited ? null : <Input label="Project key limit" type="number" inputMode="numeric" min={1} max={base.instance_maximum} step={1} required
        value={value} disabled={disabled} error={validation} hint={`1 to ${count(base.instance_maximum)} keys. The instance maximum always applies.`}
        onChange={(event) => { setValue(event.target.value); setValidation(undefined); }} />}
      <p className="settings-note">Current usage: {count(current.key_count)} keys. {validProposal ? `Proposed effective limit: ${count(proposed)} keys.` : ''} Existing keys are preserved when lowering the limit, including below current usage. New keys must fit within the effective limit and the 100,000-cell budget.</p>
      {stale ? <Alert>The current policy changed while you were editing. Load the current policy to keep your draft and review the latest limits.</Alert> : null}
      {failure === null ? null : <Alert>{failure}</Alert>}
      <div className="actions actions--stack">
        <Button type="submit" variant="primary" disabled={disabled || stale}>{busy ? 'Saving key capacity…' : 'Save key capacity'}</Button>
        <Button type="button" readOnlySafe disabled={busy || refreshing} onClick={async () => {
          setRefreshing(true);
          try { const next = await onRefresh(); if (next !== null) { setBase(next); setValidation(undefined); } } finally { setRefreshing(false); }
        }}>{refreshing ? 'Loading current policy…' : 'Load current policy'}</Button>
        <Button type="button" readOnlySafe disabled={busy} onClick={onClose}>Cancel</Button>
      </div>
    </form>
  </SidePanel>;
}
