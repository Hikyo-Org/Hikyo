import { useId, useState, type ReactNode } from 'react';

import { Alert } from '../../ui/Alert.tsx';
import { Button } from '../../ui/Button.tsx';
import { ChoiceGroup } from '../../ui/ChoiceGroup.tsx';
import { Dialog } from '../../ui/Dialog.tsx';
import { Disclosure } from '../../ui/Disclosure.tsx';
import { Radio } from '../../ui/Radio.tsx';
import { ToggleChip } from '../../ui/ToggleChip.tsx';
import {
  availability,
  effective,
  envChoices,
  folderChoices,
  hasWhere,
  keyChoices,
  label,
  narrowKeys,
  personName,
  PRESETS,
  projectById,
  projectName,
  selfRemoveRefusal,
  selfSaveRefusal,
  replacementSaveRefusal,
  setMode,
  tapped,
  toggleItems,
  toggleProject,
  type Axis,
  type EnvItem,
  type KeyItem,
  type Rule,
  type World,
} from './model.ts';
import { KeyLabel, Lock, RuleSummary } from './parts.tsx';
import { PermissionList } from './PermissionList.tsx';

/**
 * The rule editor. Where comes first (projects, environments, keys, each
 * "All, except..." or "Only..."), then what: presets that only tick boxes,
 * and the permission list. Nothing above a tapped control changes when it is
 * tapped: Where only grows downwards, the permission rows keep their height
 * (see {@link PermissionList}), and "Saves as" sits at the foot. The
 * action row stays pinned, and a click on the scrim cancels, like every editor.
 *
 * `projects` are the projects this surface may name (one on a project's
 * Members page). What the server refuses is not offered: no "all projects",
 * and a permission the Where cannot carry is disabled with its reason.
 */
export function RuleEditorDialog({
  world,
  rule,
  projects,
  busy = false,
  failure = null,
  actingPrincipal = '',
  onSave,
  onRemove,
  onCancel,
}: {
  world: World;
  rule: Rule;
  projects: readonly string[];
  busy?: boolean;
  failure?: string | null;
  actingPrincipal?: string;
  onSave: (rule: Rule) => void;
  onRemove?: (rule: Rule) => void;
  onCancel: () => void;
}) {
  const [draft, setDraft] = useState(rule);
  const chosen = draft.projects === '*' ? projects : draft.projects;
  const eff = effective(draft);
  const where = hasWhere(draft);
  const valid = eff.length > 0 && where;
  const dropped = draft.perms.filter((id) => !availability(id, draft).ok);
  const editing = rule.source.kind === 'rule';
  const unreadable = chosen.filter((p) => projectById(world, p)?.keys === null);
  const singleKeys = keyChoices(world, chosen, draft);
  const saveRefusal = replacementSaveRefusal(editing ? rule : null, draft)
    ?? selfSaveRefusal(editing ? rule : null, draft, actingPrincipal);
  const removeRefusal = selfRemoveRefusal(rule, actingPrincipal);
  const cancel = () => {
    if (!busy) onCancel();
  };

  return (
    <Dialog
      size="wide"
      className="access-dialog"
      title={`${editing ? 'Edit rule' : 'New rule'} · ${personName(world, draft.member)}`}
      onCancel={(event) => {
        event.preventDefault();
        cancel();
      }}
      onBackdropClick={cancel}
      pinActions
      actions={
        <>
          {editing && onRemove !== undefined ? (
            <Button type="button" variant="danger" className="access-editor__remove" disabled={busy || removeRefusal !== null} onClick={() => onRemove(rule)}>
              Remove rule
            </Button>
          ) : null}
          <Button type="button" disabled={busy} onClick={cancel}>
            Cancel
          </Button>
          <Button type="button" variant="primary" disabled={!valid || busy || saveRefusal !== null} aria-busy={busy ? true : undefined} onClick={() => onSave(draft)}>
            {busy ? 'Saving…' : 'Save'}
          </Button>
        </>
      }
    >
      {failure === null ? null : <Alert>{failure}</Alert>}
      {saveRefusal === null && removeRefusal === null ? null : <Alert>{saveRefusal ?? removeRefusal}</Alert>}
      <fieldset className="access-editor__section">
        <legend className="eyebrow">Where</legend>
        <AxisBox title="Projects" hint="Tap one or more projects. A rule names its projects: access to all projects stays an organisation grant.">
          <div className="access-picks">
            {projects.map((p) => (
              <ToggleChip key={p} mono pressed={chosen.includes(p) && draft.projects !== '*'} onClick={() => setDraft(toggleProject(draft, p))}>
                {projectName(world, p)}
              </ToggleChip>
            ))}
          </div>
        </AxisBox>
        {chosen.length > 0 ? (
          <>
            <AxisEditor<EnvItem>
              title="Environments"
              axis={draft.envs}
              onChange={(envs) => setDraft({ ...draft, envs })}
              choices={envChoices(world, chosen).map((c) => ({
                id: c.name,
                items: c.items,
                label: (
                  <>
                    {c.name}
                    {c.protected ? <Lock word="protected" /> : null}
                  </>
                ),
              }))}
            />
            <AxisEditor<KeyItem>
              title="Keys"
              axis={draft.keys}
              onChange={(keys) => setDraft({ ...draft, keys })}
              choices={folderChoices(world, chosen, draft).map((c) => ({ id: `folder:${c.folder}`, items: c.items, label: c.folder === '' ? '(no folder)' : `${c.folder}/` }))}
            >
              {unreadable.length > 0 ? (
                <p className="access-hint">
                  Key names in {unreadable.map((p) => projectName(world, p)).join(', ')} need See, which you do not hold there: its folders and keys cannot be picked
                  here.
                </p>
              ) : null}
              {singleKeys.length > 0 ? (
                <Disclosure label={`Pick single keys (${singleKeys.length})`} className="access-picks">
                  {singleKeys.map((item) => (
                    <AxisChip<KeyItem> key={`${item.project}|${'key' in item ? item.key : ''}`} axis={draft.keys} items={[item]} onChange={(keys) => setDraft({ ...draft, keys })}>
                      <KeyLabel world={world} item={item} />
                    </AxisChip>
                  ))}
                </Disclosure>
              ) : null}
            </AxisEditor>
          </>
        ) : null}
      </fieldset>

      <fieldset className="access-editor__section">
        <legend className="eyebrow">Permissions</legend>
        <div className="access-presets" role="group" aria-label="Presets">
          <span className="access-hint">Presets tick boxes:</span>
          {PRESETS.map((preset) => (
            <Button key={preset.name} type="button" variant="quiet" onClick={() => setDraft({ ...draft, perms: [...preset.perms] })}>
              {preset.name}
            </Button>
          ))}
        </div>
        <PermissionList
          selected={draft.perms}
          onChange={(perms) => setDraft({ ...draft, perms })}
          blocked={(id) => {
            const a = availability(id, draft);
            return a.ok ? undefined : a.why;
          }}
        />
      </fieldset>

      <section className="access-summary" aria-label="Saves as">
        <h3 className="eyebrow">Saves as</h3>
        {valid ? (
          <RuleSummary world={world} rule={draft} reach />
        ) : (
          <p className="access-summary__empty">{eff.length === 0 ? 'Tick at least one permission.' : 'Pick where this rule applies.'}</p>
        )}
        {dropped.length > 0 && where ? <p className="access-hint">Left out until Where is wider: {dropped.map(label).join(', ')}.</p> : null}
        {valid && !eff.includes('read') && (eff.includes('reveal') || eff.includes('reveal-history')) ? (
          <p className="access-hint">No See here: Reveal only works where another rule or grant gives See.</p>
        ) : null}
        {valid && narrowKeys(draft) ? <p className="access-hint">See and Pin always cover a whole environment: give them on a rule without key limits.</p> : null}
      </section>
    </Dialog>
  );
}

function AxisBox({ title, hint, mode, children }: { title: string; hint: string; mode?: ReactNode; children: ReactNode }) {
  const headingId = useId();
  return (
    <div className="access-axis" role="group" aria-labelledby={headingId}>
      <h3 id={headingId}>{title}</h3>
      {mode}
      <p className="access-hint">{hint}</p>
      {children}
    </div>
  );
}

function AxisChip<T extends EnvItem | KeyItem>({ axis, items, onChange, children }: { axis: Axis<T>; items: readonly T[]; onChange: (axis: Axis<T>) => void; children: ReactNode }) {
  return (
    <ToggleChip mono mode={axis.mode === 'all' ? 'exclude' : 'include'} pressed={tapped(axis, items)} onClick={() => onChange(toggleItems(axis, items))}>
      {children}
    </ToggleChip>
  );
}

function AxisEditor<T extends EnvItem | KeyItem>({
  title,
  axis,
  choices,
  onChange,
  children,
}: {
  title: string;
  axis: Axis<T>;
  choices: readonly { id: string; items: readonly T[]; label: ReactNode }[];
  onChange: (axis: Axis<T>) => void;
  children?: ReactNode;
}) {
  const group = useId();
  return (
    <AxisBox
      title={title}
      hint="All, except… covers ones added later: tap to leave one out. Only… covers just what you tap."
      mode={
        <ChoiceGroup legend="How to pick" layout="wrap">
          <Radio name={group} label="All, except…" checked={axis.mode === 'all'} onChange={() => onChange(setMode(axis, 'all'))} />
          <Radio name={group} label="Only…" checked={axis.mode === 'only'} onChange={() => onChange(setMode(axis, 'only'))} />
        </ChoiceGroup>
      }
    >
      <div className="access-picks">
        {choices.map((choice) => (
          <AxisChip<T> key={choice.id} axis={axis} items={choice.items} onChange={onChange}>
            {choice.label}
          </AxisChip>
        ))}
      </div>
      {children}
    </AxisBox>
  );
}
