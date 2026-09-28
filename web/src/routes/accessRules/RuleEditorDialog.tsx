import { useId, useState, type ReactNode } from 'react';

import { Button } from '../../ui/Button.tsx';
import { ChoiceGroup } from '../../ui/ChoiceGroup.tsx';
import { Dialog } from '../../ui/Dialog.tsx';
import { Disclosure } from '../../ui/Disclosure.tsx';
import { Radio } from '../../ui/Radio.tsx';
import { ToggleChip } from '../../ui/ToggleChip.tsx';
import {
  ALL,
  availability,
  effective,
  envNames,
  folderNames,
  hasWhere,
  keyPick,
  kindOf,
  label,
  MACHINE_FORBIDDEN_WHY,
  narrowKeys,
  personName,
  PRESETS,
  projectKeys,
  projectsOf,
  setMode,
  tapped,
  toggleItem,
  type Axis,
  type Rule,
  type World,
} from './model.ts';
import { EnvName, KeyItem, RuleSummary } from './parts.tsx';
import { PermissionList } from './PermissionList.tsx';

/** A fresh rule for a member: See ticked, no Where yet. Id 0 until saved. */
export const newRule = (member: string): Rule => ({ id: 0, member, perms: ['read'], projects: [], envs: ALL, keys: ALL });

/**
 * The rule editor. Where comes first (projects, environments, keys, each
 * "All, except..." or "Only..."), then what: presets that only tick boxes,
 * and the permission list. Nothing above a tapped control changes when it is
 * tapped: Where only grows downwards, the permission rows keep their height
 * (see {@link PermissionList}), and "Saves as" sits at the foot. The
 * action row stays pinned, and a click on the scrim cancels, like every editor.
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
  const kind = kindOf(world, draft.member);
  const projects = projectsOf(world, draft);
  const eff = effective(draft, kind);
  const where = hasWhere(draft);
  const valid = eff.length > 0 && where;
  const whyNot = draft.perms.flatMap((id) => {
    const a = availability(id, draft, kind);
    return a.ok ? [] : [{ id, why: a.why }];
  });
  const machineDropped = whyNot.filter((x) => x.why === MACHINE_FORBIDDEN_WHY).map((x) => x.id);
  const shapeDropped = whyNot.filter((x) => x.why !== MACHINE_FORBIDDEN_WHY).map((x) => x.id);
  const name = personName(world, draft.member);
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
      onBackdropClick={onCancel}
      pinActions
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
        <legend className="eyebrow">Where</legend>
        <AxisBox title="Projects" hint="Tap one or more projects, or all projects, which includes ones created later.">
          <div className="access-picks">
            <ToggleChip mono pressed={draft.projects === '*'} onClick={() => setDraft({ ...draft, projects: draft.projects === '*' ? [] : '*' })}>
              all projects
            </ToggleChip>
            {world.projects.map((p) =>
              draft.projects === '*' ? (
                <ToggleChip key={p.id} mono mode="exclude" pressed={false} disabled>
                  {p.id}
                </ToggleChip>
              ) : (
                <ToggleChip key={p.id} mono pressed={draft.projects.includes(p.id)} onClick={() => toggleProject(p.id)}>
                  {p.id}
                </ToggleChip>
              ),
            )}
          </div>
        </AxisBox>
        {projects.length > 0 ? (
          <>
            <AxisEditor
              title="Environments"
              axis={draft.envs}
              onChange={(envs) => setDraft({ ...draft, envs })}
              items={envNames(world, projects)}
              render={(e) => <EnvName world={world} name={e} />}
            />
            <AxisEditor
              title="Keys"
              axis={draft.keys}
              onChange={(keys) => setDraft({ ...draft, keys })}
              items={folderNames(world, projects)}
              render={(f) => `${f}/`}
            >
              <Disclosure label={`Pick single keys (${singleKeys.length})`} className="access-picks">
                {singleKeys.map((item) => (
                  <AxisChip key={item} axis={draft.keys} item={item} onChange={(keys) => setDraft({ ...draft, keys })}>
                    <KeyItem world={world} item={item} />
                  </AxisChip>
                ))}
              </Disclosure>
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
          kind={kind}
          selected={draft.perms}
          onChange={(perms) => setDraft({ ...draft, perms })}
          blocked={(id) => {
            const a = availability(id, draft, kind);
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
        {shapeDropped.length > 0 && where ? <p className="access-hint">Left out until Where is wider: {shapeDropped.map(label).join(', ')}.</p> : null}
        {machineDropped.length > 0 ? <p className="access-hint">Left out, machines cannot hold: {machineDropped.map(label).join(', ')}.</p> : null}
        {valid && !eff.includes('read') && (eff.includes('reveal') || eff.includes('reveal-history')) ? (
          <p className="access-hint">No See here: Reveal only works where another of their rules gives See.</p>
        ) : null}
        {valid && narrowKeys(draft) && eff.includes('read') ? <p className="access-hint">See always covers the whole environment.</p> : null}
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

function AxisChip({ axis, item, onChange, children }: { axis: Axis; item: string; onChange: (axis: Axis) => void; children: ReactNode }) {
  return (
    <ToggleChip mono mode={axis.mode === 'all' ? 'exclude' : 'include'} pressed={tapped(axis, item)} onClick={() => onChange(toggleItem(axis, item))}>
      {children}
    </ToggleChip>
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
        {items.map((item) => (
          <AxisChip key={item} axis={axis} item={item} onChange={onChange}>
            {render(item)}
          </AxisChip>
        ))}
      </div>
      {children}
    </AxisBox>
  );
}
