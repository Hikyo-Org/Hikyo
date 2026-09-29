import type { ReactNode } from 'react';

import { Disclosure } from '../../ui/Disclosure.tsx';
import { label, NOT_ON_RULES, PERM_GROUPS, PERMS, PRESETS, SHAPE_NEEDS } from './model.ts';
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
 * editor and Who can...? read, so the three cannot drift apart. Each part
 * folds away, so the Members page it closes stays short.
 */
export function AccessGlossary() {
  return (
    <div className="access-gloss-parts">
      <p className="access-hint">The grant list, the rules, the rule editor and Who can…? all use exactly these names.</p>
      <Disclosure label="People and rules">
        <Terms
          terms={[
            ['Member', 'member', 'A person or machine (service account) in this organisation.'],
            ['Machine', 'machine', 'A service account: its credential carries the identity. Machines keep their grants and are managed on Machine access; rules are for people.'],
            ['Grant', 'grant', 'One permission on a whole organisation, project or environment, every key included.'],
            ['Rule', 'rule', 'A set of permissions for one person, and where they apply. A person can have several rules; rules and grants add up.'],
            ['Permission', 'permission', 'One thing a member may do, like See or Reveal. Listed below. Each is ticked on its own.'],
            ['Preset', 'preset', 'A shortcut that ticks a common set of permissions: Viewer, Editor, Publisher, Admin. Only the ticked permissions are saved; the preset name is shown when a rule matches one exactly.'],
          ]}
        />
      </Disclosure>
      {PERM_GROUPS.map((group) => (
        <Disclosure key={group} label={`Permissions: ${group.toLowerCase()}`}>
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
          {group === 'Administration' ? <p className="access-hint">{NOT_ON_RULES}</p> : null}
        </Disclosure>
      ))}
      <Disclosure label="Presets">
        <Terms
          terms={PRESETS.map((preset) => [
            preset.name,
            preset.name,
            `${preset.perms.map(label).join(', ')}${preset.name === 'Admin' ? '. On a folder or some environments, the permissions that need a whole project are left out.' : '.'}`,
          ])}
        />
      </Disclosure>
      <Disclosure label="Where">
        <Terms
          terms={[
            ['Project', 'project', 'A set of environments sharing one list of keys. A rule names its projects.'],
            ['Environment', 'environment', 'One place the values apply, like dev or prod. Environments with the same name in the picked projects are picked together.'],
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
            ['Except', 'except', <>Leaves something out of <strong>this rule only</strong>. If another rule or grant of the member covers it, they still have it. It is not a ban.</>],
            ['Folder pick', 'folder-pick', 'A rule that names a folder covers whatever keys are in it right now: new keys join, keys moved out leave. Leaving a folder out also leaves out the folders inside it; picking only a folder picks just that folder.'],
            ['Single-key pick', 'single-key-pick', 'A rule that names one key follows that key by its id: it keeps covering it after a rename or a move to another folder.'],
            ['Reach', 'reach', 'What a rule covers, counted: environments, keys, secret values it can reveal, protected environments.'],
          ]}
        />
      </Disclosure>
    </div>
  );
}
