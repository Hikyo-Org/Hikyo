import type { ReactNode } from 'react';

import { JumpIndex, Panel } from '../Sections.tsx';
import { label, MACHINE_REVEAL_HINT, PERM_GROUPS, PERMS, PRESETS, SHAPE_NEEDS } from './model.ts';
import { Lock } from './parts.tsx';

function Terms({ terms }: { terms: readonly (readonly [term: ReactNode, id: string, definition: ReactNode])[] }) {
  return (
    <dl className="access-gloss">
      {terms.map(([term, id, definition]) => (
        <div key={id}>
          <dt>{term}</dt>
          <dd>{definition}</dd>
        </div>
      ))}
    </dl>
  );
}

/**
 * The Glossary: every word the access screens use, and nothing else. The
 * permission and preset entries are generated from the same table the rule
 * editor and Who can...? read, so the three cannot drift apart.
 */
export function AccessGlossary() {
  return (
    <div className="page page--chrome access-rules">
      <h1>Glossary</h1>
      <p className="page__lede">
        Every word the access screens use, and nothing else. The Members page, the rule editor and Who can…? all use exactly these names.
      </p>
      <JumpIndex
        sections={[
          { id: 'gloss-people', label: 'People and rules' },
          ...PERM_GROUPS.map((group) => ({ id: `gloss-${group.toLowerCase()}`, label: group })),
          { id: 'gloss-presets', label: 'Presets' },
          { id: 'gloss-where', label: 'Where' },
        ]}
      />
      <Panel id="gloss-people" title="People and rules">
        <Terms
          terms={[
            ['Member', 'member', 'A person or machine (service account) in this organisation.'],
            ['Machine', 'machine', `A service account: its credential carries the identity. A machine never holds Pin or a management permission. Reveal: ${MACHINE_REVEAL_HINT}`],
            ['Rule', 'rule', 'One line of access: a set of permissions, and where they apply. A member can have several rules; they add up.'],
            ['Permission', 'permission', 'One thing a member may do, like See or Reveal. Listed below. Each is ticked on its own.'],
            ['Preset', 'preset', 'A shortcut that ticks a common set of permissions: Viewer, Editor, Publisher, Admin. Only the ticked permissions are saved; the preset name is shown when a rule matches one exactly.'],
          ]}
        />
      </Panel>
      {PERM_GROUPS.map((group) => (
        <Panel key={group} id={`gloss-${group.toLowerCase()}`} title={`Permissions: ${group.toLowerCase()}`}>
          <Terms
            terms={PERMS.filter((p) => p.group === group).map((p) => [
              p.label,
              p.id,
              <>
                {p.desc}
                {p.shape === 'key' ? null : <span className="access-hint"> Only on rules that cover {SHAPE_NEEDS[p.shape]}.</span>}
              </>,
            ])}
          />
        </Panel>
      ))}
      <Panel id="gloss-presets" title="Presets">
        <Terms
          terms={PRESETS.map((preset) => [
            preset.name,
            preset.name,
            `${preset.perms.map(label).join(', ')}${preset.name === 'Admin' ? '. On a folder or some environments, the permissions that need a whole project drop out automatically.' : '.'}`,
          ])}
        />
      </Panel>
      <Panel id="gloss-where" title="Where">
        <Terms
          terms={[
            ['Project', 'project', 'A set of environments sharing one list of keys.'],
            ['Environment', 'environment', 'One place the values apply, like dev or prod. Environments with the same name in different projects are picked together.'],
            [<>Protected<Lock word="protected" /></>, 'protected', 'An environment that asks for extra confirmation before changes and reveals.'],
            ['Folder', 'folder', <>A group of keys, like <code>db/</code>. Folders organise keys; a rule can target one.</>],
            [
              'Key',
              'key',
              <>
                One named setting, like <code>DB_PASSWORD</code>. A <strong>secret</strong> key
                <Lock word="secret" /> has masked values; a <strong>config</strong> key does not.
              </>,
            ],
            ['All, except…', 'all-except', 'Everything at this step, including things added later, minus what you tap.'],
            ['Only…', 'only', 'Just what you tap. Things added later are not included.'],
            ['Except', 'except', <>Leaves something out of <strong>this rule only</strong>. If another of the member's rules covers it, they still have it. It is not a ban.</>],
            ['Folder pick', 'folder-pick', 'A rule that names a folder covers whatever keys are in it right now: new keys join, keys moved out leave, a rename changes nothing.'],
            ['Single-key pick', 'single-key-pick', 'A rule that names one key follows that key by its id: it keeps covering it after a rename or a move to another folder.'],
            ['Reach', 'reach', 'What a rule covers, counted: environments, keys, secret values it can reveal, protected environments.'],
          ]}
        />
      </Panel>
    </div>
  );
}
