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
  machineRefusal,
  machineWhereRefusal,
  narrowKeys,
  personName,
  PRESETS,
  projectById,
  projectName,
  setMode,
  tapped,
  toggleItems,
  toggleAllProjects,
  overProjects,
  toggleProject,
  everyProject,
  type Axis,
  type EnvItem,
  type KeyItem,
  type MachineClass,
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
 * Members page). With `allProjects`, `projects` is the organisation's complete
 * list and "All projects" is offered (#1446): such a rule covers every
 * environment and narrows keys by folder only, so those controls step back.
 * What the server refuses is not offered, and a permission the Where cannot
 * carry is disabled with its reason.
 *
 * On Machine access the holder is a service account of class `machine` (D11):
 * a permission its class may not hold is disabled with the reason, the Keys
 * axis is not offered, and a workload rule lists its environments ("Only...").
 */
export function RuleEditorDialog({
  world,
  rule,
  projects,
  allProjects = false,
  busy = false,
  showReach = true,
  failure = null,
  machine,
  onSave,
  onRemove,
  onCancel,
}: {
  world: World;
  rule: Rule;
  projects: readonly string[];
  /** Offer "All projects": `projects` is then the organisation's complete list, sent with the rule. */
  allProjects?: boolean;
  busy?: boolean;
  /** The holder's class when the holder is a service account. */
  machine?: MachineClass;
  showReach?: boolean;
  failure?: string | null;
  onSave: (rule: Rule) => void;
  onRemove?: (rule: Rule) => void;
  onCancel: () => void;
}) {
  const [seed, setDraft] = useState(rule);
  // An all-projects draft follows the current project list (see overProjects).
  const draft = overProjects(seed, projects);
  const all = draft.projects === '*';
  const chosen = all ? projects : draft.projects;
  const eff = effective(draft, machine);
  const saved = { ...draft, perms: eff };
  const where = hasWhere(draft);
  const whereRefusal = machineWhereRefusal(draft, machine);
  // An all-projects rule is sent with the organisation's project list, which
  // only the org-wide page has: elsewhere it can be narrowed, not saved as is.
  const allElsewhere = all && !allProjects;
  // With no project there is no list to send: the server refuses such a rule.
  const noProject = allProjects && projects.length === 0;
  const valid = eff.length > 0 && where && whereRefusal === undefined && !allElsewhere && !(all && projects.length === 0);
  const dropped = draft.perms.filter((id) => machineRefusal(id, machine) === undefined && !availability(id, draft).ok);
  const editing = rule.source.kind === 'rule';
  const unreadable = chosen.filter((p) => projectById(world, p)?.keys === null);
  const singleKeys = all ? [] : keyChoices(world, chosen, draft);
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
            <Button type="button" variant="danger" className="access-editor__remove" disabled={busy} onClick={() => onRemove(rule)}>
              Remove rule
            </Button>
          ) : null}
          <Button type="button" disabled={busy} onClick={cancel}>
            Cancel
          </Button>
          <Button type="button" variant="primary" disabled={!valid || busy} aria-busy={busy ? true : undefined} onClick={() => onSave(saved)}>
            {busy ? 'Saving…' : 'Save'}
          </Button>
        </>
      }
    >
      {failure === null ? null : <Alert>{failure}</Alert>}
      <fieldset className="access-editor__section">
        <legend className="eyebrow">Where</legend>
        <AxisBox
          title="Projects"
          hint={
            allElsewhere
              ? "This rule covers all projects. Change it on the organisation's Members page, or tap projects to name them instead."
              : noProject && machine === undefined
                ? 'All projects needs a project first: create one, then come back.'
              : allProjects && machine === undefined
                ? 'Tap one or more projects, or All projects to include projects created later.'
                : 'Tap one or more projects. A rule names its projects: access to all projects stays an organisation grant.'
          }
        >
          <div className="access-picks">
            {allProjects && machine === undefined ? (
              <ToggleChip pressed={all} disabled={noProject && !all} onClick={() => setDraft(toggleAllProjects(draft, projects))}>
                All projects
              </ToggleChip>
            ) : null}
            {projects.map((p) => (
              <ToggleChip key={p} mono pressed={chosen.includes(p) && !all} onClick={() => setDraft(toggleProject(draft, p))}>
                {projectName(world, p)}
              </ToggleChip>
            ))}
          </div>
        </AxisBox>
        {chosen.length > 0 ? (
          <>
            {all ? (
              <AxisBox title="Environments" hint="All projects covers every environment, including those of projects created later." />
            ) : (
              <AxisEditor<EnvItem>
                title="Environments"
                modes={machine === 'workload' ? ['only'] : undefined}
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
            )}
            {machine !== undefined ? (
              <AxisBox title="Keys" hint="A machine rule covers every key of its environments: key limits are for people." />
            ) : (
              <AxisEditor<KeyItem>
                title="Keys"
                axis={draft.keys}
                onChange={(keys) => setDraft({ ...draft, keys })}
                choices={folderChoices(world, chosen, draft).map((c) => ({
                  id: `folder:${c.folder}`,
                  // Under All projects a folder means the same path in every project.
                  items: all ? everyProject(projects, [c.folder]) : c.items,
                  label: c.folder === '' ? '(no folder)' : <bdi>{c.folder}/</bdi>,
                }))}
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
            )}
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
          machine={machine}
          blocked={(id) => {
            const a = availability(id, draft, machine);
            return a.ok ? undefined : a.why;
          }}
        />
      </fieldset>

      <section className="access-summary" aria-label="Saves as">
        <h3 className="eyebrow">Saves as</h3>
        {valid ? (
          <RuleSummary world={world} rule={saved} reach={showReach} />
        ) : (
          <p className="access-summary__empty">{eff.length === 0 ? 'Tick at least one permission.' : !where ? 'Pick where this rule applies.' : whereRefusal}</p>
        )}
        {dropped.length > 0 && where ? <p className="access-hint">Left out until Where is wider: {dropped.map(label).join(', ')}.</p> : null}
        {valid && !eff.includes('read') && (eff.includes('reveal') || eff.includes('reveal-history')) ? (
          <p className="access-hint">No See here: Reveal only works where another rule or grant gives See.</p>
        ) : null}
        {valid && narrowKeys(draft) && eff.includes('read') ? <p className="access-hint">See always covers the whole environment: key names and config values are shared by all keys. Key limits narrow the other permissions only.</p> : null}
        {valid && narrowKeys(draft) && eff.includes('read') && eff.includes('manage-members') ? <p className="access-hint">Assigning See requires Manage access across every key of the selected environments.</p> : null}
      </section>
    </Dialog>
  );
}

function AxisBox({ title, hint, mode, children }: { title: string; hint: string; mode?: ReactNode; children?: ReactNode }) {
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
  modes = ['all', 'only'],
  axis,
  choices,
  onChange,
  children,
}: {
  title: string;
  /** The ways this axis may be picked; one mode offers no choice. */
  modes?: readonly Axis<T>['mode'][];
  axis: Axis<T>;
  choices: readonly { id: string; items: readonly T[]; label: ReactNode }[];
  onChange: (axis: Axis<T>) => void;
  children?: ReactNode;
}) {
  const group = useId();
  return (
    <AxisBox
      title={title}
      hint={
        modes.includes('all')
          ? 'All, except… covers ones added later: tap to leave one out. Only… covers just what you tap.'
          : 'Only… covers just what you tap: ones added later are not covered.'
      }
      mode={
        modes.length > 1 ? (
          <ChoiceGroup legend="How to pick" layout="wrap">
            <Radio name={group} label="All, except…" checked={axis.mode === 'all'} onChange={() => onChange(setMode(axis, 'all'))} />
            <Radio name={group} label="Only…" checked={axis.mode === 'only'} onChange={() => onChange(setMode(axis, 'only'))} />
          </ChoiceGroup>
        ) : undefined
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
