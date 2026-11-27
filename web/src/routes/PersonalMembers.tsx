import { useState } from 'react';
import { generatePath, Link } from 'react-router';

import { surfaceById } from '../app/navigation.ts';
import { ApiError } from '../api/client.ts';
import type { MatrixRef } from '../api/keys.ts';
import {
  NOTICE_CAUSE_TEXT,
  useDeletePersonalMember,
  usePersonalMembers,
  type PersonalValueMember,
} from '../api/personal.ts';
import { commonRefusalText, statusText } from '../api/statusText.ts';
import { useWorkspaceContext, withRemote } from '../api/transport.tsx';
import { Alert } from '../ui/Alert.tsx';
import { Badge } from '../ui/Badge.tsx';
import { Button } from '../ui/Button.tsx';
import { Dialog } from '../ui/Dialog.tsx';
import { TabPanel, Tabs } from '../ui/Tabs.tsx';

type EnvironmentOption = { readonly id: string; readonly name: string };

/**
 * Who holds a personal value or a notice for one key, per environment, with
 * when each changed (personal-values ADR). Metadata only: a `manage-members`
 * holder can delete someone's value, which leaves them a notice, and can never
 * read or overwrite it. Anyone else is refused by the server, and the section
 * then shows only the link to their own value.
 */
export function PersonalMembers({
  refData,
  keyName,
  environments,
}: {
  refData: MatrixRef;
  keyName: string;
  environments: readonly EnvironmentOption[];
}) {
  const [selected, setSelected] = useState(environments[0]?.id ?? '');
  const remote = useWorkspaceContext()?.remote ?? '';
  if (environments.length === 0) return null;
  const env = { ...refData, environment: selected };
  const ownPath = withRemote(generatePath(surfaceById('personal-values').path, env), remote);
  return (
    <section className="key-detail__section" aria-labelledby="key-detail-personal">
      <h3 id="key-detail-personal">Personal values</h3>
      <Tabs
        label="Environment"
        idPrefix="key-detail-personal"
        tabs={environments.map((environment) => ({ id: environment.id, label: environment.name }))}
        selected={selected}
        onSelect={setSelected}
      />
      <TabPanel idPrefix="key-detail-personal" selected={selected}>
        <MembersWithValue key={selected} refData={refData} environment={selected} keyName={keyName} />
        <p>
          <Link className="btn btn--quiet" to={ownPath}>Your own personal values in this environment</Link>
        </p>
      </TabPanel>
    </section>
  );
}

function MembersWithValue({ refData, environment, keyName }: { refData: MatrixRef; environment: string; keyName: string }) {
  const env = { ...refData, environment };
  const members = usePersonalMembers(env, keyName);
  const remove = useDeletePersonalMember(env, keyName);
  const [confirming, setConfirming] = useState<PersonalValueMember | null>(null);
  const [outcome, setOutcome] = useState<{ readonly tone: 'done' | 'danger'; readonly text: string } | null>(null);

  if (members.isPending) return <p role="status">Loading the members with a personal value…</p>;
  if (members.isError) {
    // Not a manager here: the server answers without saying which members have one.
    if (members.error instanceof ApiError && (members.error.status === 403 || members.error.status === 404)) return null;
    return <Alert>The members with a personal value could not be loaded. Reload to try again.</Alert>;
  }
  const nameOf = (member: PersonalValueMember) => member.username ?? member.principal_id;
  return (
    <>
      {outcome === null ? null : <Alert tone={outcome.tone}>{outcome.text}</Alert>}
      {members.data.length === 0 ? (
        <p>No member has a personal value or a notice for this key here.</p>
      ) : (
        <ul className="personal-members__list" aria-label={`Personal values for ${keyName}`}>
          {members.data.map((member) => (
            <li key={member.principal_id} className="personal-members__row">
              <span>
                <span className="mono">{nameOf(member)}</span>{' '}
                {member.set ? <Badge>has a value</Badge> : null}
                {member.notice === undefined ? null : (
                  <Badge tone="changed" icon="minus">notice: {NOTICE_CAUSE_TEXT[member.notice]}</Badge>
                )}
                {member.updated_at === undefined ? null : (
                  <span className="personal-members__when"> changed {new Date(member.updated_at).toLocaleString()}</span>
                )}
              </span>
              {member.set ? (
                <Button
                  type="button"
                  variant="danger"
                  aria-label={`Delete the personal value of ${nameOf(member)}`}
                  onClick={() => { setOutcome(null); setConfirming(member); }}
                >
                  Delete
                </Button>
              ) : null}
            </li>
          ))}
        </ul>
      )}
      {confirming === null ? null : (
        <Dialog
          title={`Delete the personal value of ${nameOf(confirming)}?`}
          lede="You cannot read or replace it. Deleting it leaves them a notice, and their next run refuses this key until they set a value or dismiss the notice."
          onCancel={() => setConfirming(null)}
          actions={(
            <>
              <Button type="button" onClick={() => setConfirming(null)}>Cancel</Button>
              <Button
                type="button"
                variant="danger"
                disabled={remove.isPending}
                onClick={() => {
                  const member = confirming;
                  remove.mutate(member.principal_id, {
                    onSuccess: () => setOutcome({ tone: 'done', text: `The personal value of ${nameOf(member)} is deleted. They have a notice.` }),
                    onError: (error) => setOutcome({ tone: 'danger', text: statusText(error, {
                      401: commonRefusalText.sessionEnded,
                      404: 'That value is already gone.',
                      429: commonRefusalText.requests,
                    }, 'The value could not be deleted. Reload and try again.') }),
                    onSettled: () => setConfirming(null),
                  });
                }}
              >
                Delete their value
              </Button>
            </>
          )}
        />
      )}
    </>
  );
}
