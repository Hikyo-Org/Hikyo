import { useId, useState, type ReactNode } from 'react';

import { Button } from '../../ui/Button.tsx';
import { Checkbox } from '../../ui/Checkbox.tsx';
import { cx } from '../../ui/cx.ts';
import { Dialog } from '../../ui/Dialog.tsx';
import {
  ALL,
  allowed,
  effective,
  envNames,
  folderNames,
  hasWhere,
  itemState,
  keyPick,
  label,
  narrowKeys,
  PERM_GROUPS,
  PERMS,
  personName,
  PRESETS,
  projectKeys,
  projectsOf,
  reachOf,
  reachText,
  setMode,
  SHAPE_WHY,
  toggleItem,
  whereText,
  type Axis,
  type Perm,
  type Rule,
  type World,
} from './model.ts';
import { EnvName, KeyItem, Pick } from './parts.tsx';

/** A fresh rule for a member: See ticked, no Where yet. Id 0 until saved. */
export const newRule = (member: string): Rule => ({ id: 0, member, perms: ['read'], projects: [], envs: ALL, keys: ALL });

/**
 * The rule editor: presets that only tick boxes, the permissions grouped
 * Values / Secrets / Administration with what each lets you do, and Where as
 * three axes of taps. A permission the rule's Where is too narrow to carry is
 * disabled with the reason inline; it stays ticked in the draft and comes
 * back when Where widens. The sentence at the foot reads the draft live.
 */
export function RuleEditorDialog({
  world,
  rule,
  onSave,
  onRemove,
  onCancel,
}: {
  world: World;
  rule: Rule;
  onSave: (rule: Rule) => void;
  onRemove?: (id: number) => void;
  onCancel: () => void;
}) {
  const [draft, setDraft] = useState(rule);
  const projects = projectsOf(world, draft);
  const eff = effective(draft);
  const where = hasWhere(draft);
  const valid = eff.length > 0 && where;
  const dropped = draft.perms.filter((id) => !allowed(id, draft));
  const name = personName(world, draft.member);
  const setAxis = (axis: 'envs' | 'keys') => (next: Axis) => setDraft({ ...draft, [axis]: next });
  const toggleProject = (project: string) => {
    const list = draft.projects === '*' ? [] : draft.projects;
    setDraft({ ...draft, projects: list.includes(project) ? list.filter((p) => p !== project) : [...list, project] });
  };
  const singleKeys = projects.flatMap((p) => projectKeys(world, p)).map((k) => keyPick(k.id));

  return (
    <Dialog
      size="wide"
      className="access-dialog"
      title={`${rule.id === 0 ? 'New rule' : 'Edit rule'} · ${name}`}
      onCancel={onCancel}
      actions={
        <>
          {rule.id !== 0 && onRemove !== undefined ? (
            <Button type="button" variant="danger" className="access-editor__remove" onClick={() => onRemove(rule.id)}>
              Remove rule
            </Button>
          ) : null}
          <Button type="button" onClick={onCancel}>
            Cancel
          </Button>
          <Button type="button" variant="primary" disabled={!valid} onClick={() => onSave(draft)}>
            Save
          </Button>
        </>
      }
    >
      <fieldset className="access-editor__section">
        <legend className="eyebrow">Permissions</legend>
        <div className="access-presets">
          <span>Presets tick boxes:</span>
          {PRESETS.map((preset) => (
            <Button key={preset.name} type="button" variant="quiet" onClick={() => setDraft({ ...draft, perms: [...preset.perms] })}>
              {preset.name}
            </Button>
          ))}
        </div>
        {PERM_GROUPS.map((group) => (
          <div key={group} className="access-perm-group">
            <h3 className="eyebrow">{group}</h3>
            {PERMS.filter((p) => p.group === group).map((p) => (
              <PermRow
                key={p.id}
                perm={p}
                available={allowed(p.id, draft)}
                checked={draft.perms.includes(p.id)}
                onChange={(on) => setDraft({ ...draft, perms: on ? [...draft.perms, p.id] : draft.perms.filter((x) => x !== p.id) })}
              />
            ))}
          </div>
        ))}
      </fieldset>

      <fieldset className="access-editor__section">
        <legend className="eyebrow">Where</legend>
        <AxisBox title="Projects" hint={draft.projects === '*' ? 'All projects, including ones created later.' : 'Tap one or more projects.'}>
          <div className="access-picks">
            <Pick state={draft.projects === '*' ? 'included' : 'off'} onToggle={() => setDraft({ ...draft, projects: draft.projects === '*' ? [] : '*' })}>
              all projects
            </Pick>
            {world.projects.map((p) => (
              <Pick
                key={p.id}
                state={draft.projects === '*' ? 'implied' : draft.projects.includes(p.id) ? 'included' : 'off'}
                disabled={draft.projects === '*'}
                onToggle={() => toggleProject(p.id)}
              >
                {p.id}
              </Pick>
            ))}
          </div>
        </AxisBox>
        {projects.length > 0 ? (
          <>
            <AxisEditor title="Environments" axis={draft.envs} onChange={setAxis('envs')} items={envNames(world, projects)} render={(e) => <EnvName world={world} name={e} />} />
            <AxisEditor title="Keys" axis={draft.keys} onChange={setAxis('keys')} items={folderNames(world, projects)} render={(f) => `${f}/`}>
              <details className="access-single-keys">
                <summary>Single keys…</summary>
                <div className="access-picks">
                  {singleKeys.map((item) => (
                    <Pick key={item} state={itemState(draft.keys, item)} onToggle={() => setAxis('keys')(toggleItem(draft.keys, item))}>
                      <KeyItem world={world} item={item} />
                    </Pick>
                  ))}
                </div>
              </details>
            </AxisEditor>
          </>
        ) : null}
      </fieldset>

      <div className="access-summary">
        <p className="access-summary__sentence">
          {valid ? (
            <>
              {name.split(' ')[0]} can <strong>{eff.map(label).join(', ')}</strong> in <strong>{whereText(world, draft)}</strong>.
            </>
          ) : eff.length === 0 ? (
            'Tick at least one permission.'
          ) : (
            'Pick where this rule applies.'
          )}
        </p>
        {valid ? <p>{reachText(reachOf(world, draft))}</p> : null}
        {dropped.length > 0 && where ? (
          <p>Left out because this rule is narrower than they need: {dropped.map(label).join(', ')}. They come back if you widen Where.</p>
        ) : null}
        {valid && !eff.includes('read') && (eff.includes('reveal') || eff.includes('reveal-history')) ? (
          <p>No See in this rule: Reveal only works where another of their rules gives See.</p>
        ) : null}
        {valid && narrowKeys(draft) && eff.includes('read') ? (
          <p>See always covers the whole environment: key names and config values are shared by all keys.</p>
        ) : null}
      </div>
    </Dialog>
  );
}

function PermRow({ perm, available, checked, onChange }: { perm: Perm; available: boolean; checked: boolean; onChange: (on: boolean) => void }) {
  const descId = useId();
  return (
    <div className={cx('access-perm', !available && 'access-perm--off')}>
      <Checkbox label={perm.label} checked={checked && available} disabled={!available} aria-describedby={descId} onChange={(e) => onChange(e.target.checked)} />
      <p id={descId} className="access-perm__desc">
        {perm.desc}
        {!available && perm.shape !== 'key' ? <span className="access-perm__why"> Not available here: {SHAPE_WHY[perm.shape]}.</span> : null}
      </p>
    </div>
  );
}

function AxisBox({ title, hint, mode, children }: { title: string; hint: string; mode?: ReactNode; children: ReactNode }) {
  const headingId = useId();
  return (
    <div className="access-axis" role="group" aria-labelledby={headingId}>
      <div className="access-axis__head">
        <h3 id={headingId}>{title}</h3>
        {mode}
      </div>
      <p className="access-hint">{hint}</p>
      {children}
    </div>
  );
}

function AxisEditor({
  title,
  axis,
  items,
  render,
  onChange,
  children,
}: {
  title: string;
  axis: Axis;
  items: readonly string[];
  render: (item: string) => ReactNode;
  onChange: (axis: Axis) => void;
  children?: ReactNode;
}) {
  return (
    <AxisBox
      title={title}
      hint={axis.mode === 'all' ? 'All of them, including ones added later. Tap to leave one out.' : 'Only the ones you tap.'}
      mode={
        <div className="access-seg" role="group" aria-label={`${title}: how to pick`}>
          <Button type="button" variant="quiet" aria-pressed={axis.mode === 'all'} onClick={() => onChange(setMode(axis, 'all'))}>
            All, except…
          </Button>
          <Button type="button" variant="quiet" aria-pressed={axis.mode === 'only'} onClick={() => onChange(setMode(axis, 'only'))}>
            Only…
          </Button>
        </div>
      }
    >
      <div className="access-picks">
        {items.map((item) => (
          <Pick key={item} state={itemState(axis, item)} onToggle={() => onChange(toggleItem(axis, item))}>
            {render(item)}
          </Pick>
        ))}
      </div>
      {children}
    </AxisBox>
  );
}
