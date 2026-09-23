import { useId, useState } from 'react';

import { accountFailureText, useMyProfile, useUpdateMyProfile } from '../api/account.ts';
import { ApiError } from '../api/client.ts';
import { useSensitiveState } from '../api/sensitiveMutation.ts';
import { useAuth } from '../app/AuthProvider.tsx';
import { Alert } from '../ui/Alert.tsx';
import { Button } from '../ui/Button.tsx';
import { Panel } from './Sections.tsx';

export function AccountProfile() {
  const profile = useMyProfile();
  const auth = useAuth();
  return (
    <Panel id="account-profile" title="Profile" tight>
      {profile.isPending ? <p role="status">Loading your profile…</p> : null}
      {profile.isError ? <Alert>Your profile could not be loaded. <Button type="button" onClick={() => { void profile.refetch(); }}>Try again</Button></Alert> : null}
      {profile.isSuccess ? <ProfileForm key={auth.identity?.principal.id} profile={profile.data} /> : null}
    </Panel>
  );
}

type Profile = NonNullable<ReturnType<typeof useMyProfile>['data']>;

function ProfileForm({ profile }: { profile: Profile }) {
  const update = useUpdateMyProfile();
  const id = useId();
  const [saved, setSaved] = useState(profile);
  const [username, setUsername] = useState(profile.username);
  const [displayName, setDisplayName] = useState(profile.display_name);
  const [proof, setProof] = useSensitiveState('');
  const [done, setDone] = useState(false);
  const dirty = username !== saved.username || displayName !== saved.display_name;
  const needsProof = username !== saved.username;

  return (
    <form onSubmit={(event) => {
      event.preventDefault();
      if (!dirty || update.isPending || (needsProof && proof === '')) return;
      setDone(false);
      update.mutate({ username, display_name: displayName, ...(needsProof ? { proof } : {}) }, {
        onSuccess: (result) => {
          setSaved(result);
          setUsername(result.username);
          setDisplayName(result.display_name);
          setDone(true);
        },
      });
      setProof('');
    }}>
      {done ? <Alert tone="done">Profile saved.</Alert> : null}
      {update.error !== null ? <Alert>{update.error instanceof ApiError && update.error.status === 409
        ? 'That username is already in use. Choose another username.'
        : accountFailureText(update.error)}</Alert> : null}
      <fieldset disabled={update.isPending} className="settings-grid account-profile__fields" aria-label="Profile details">
        <div className="field">
          <label htmlFor={`${id}-name`}>Display name</label>
          <input id={`${id}-name`} name="display_name" autoComplete="name" maxLength={256}
            value={displayName} readOnly={profile.managed} onChange={(event) => { setDisplayName(event.target.value); setDone(false); }} />
        </div>
        {!profile.managed && profile.username_editable ? <div className="field">
          <label htmlFor={`${id}-username`}>Username</label>
          <input id={`${id}-username`} name="username" autoComplete="username" required maxLength={256}
            value={username} readOnly={profile.managed} onChange={(event) => { setUsername(event.target.value); setDone(false); }} />
          <p className="settings-note">Use this username when signing in with a password.</p>
        </div> : null}
        {saved.email !== null ? <div className="field">
          <label htmlFor={`${id}-email`}>Sign-in email</label>
          <input id={`${id}-email`} name="email" type="email" readOnly value={saved.email} aria-describedby={`${id}-email-hint`} />
          <p id={`${id}-email-hint`} className="settings-note">Verified when you signed up. It cannot be changed here.</p>
        </div> : null}
        {needsProof ? <div className="field">
          <label htmlFor={`${id}-proof`}>Code or password</label>
          <input id={`${id}-proof`} name="proof" type="password" autoComplete="current-password" required
            value={proof} onChange={(event) => setProof(event.target.value)} aria-describedby={`${id}-proof-hint`} />
          <p id={`${id}-proof-hint`} className="settings-note">Confirm your username change with your authenticator code if enrolled, otherwise your current password.</p>
        </div> : null}
      </fieldset>
      {profile.managed ? <p className="settings-note">Your identity provider manages your username and display name. Change them there.</p> : null}
      {!profile.managed && !profile.username_editable ? <p className="settings-note">You sign in through your identity provider. Your display name can be changed here.</p> : null}
      <Button type="submit" variant="primary" disabled={!dirty || update.isPending || (needsProof && proof === '')}>
        {update.isPending ? 'Saving…' : 'Save profile'}
      </Button>
    </form>
  );
}
