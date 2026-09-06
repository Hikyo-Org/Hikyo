import { useId, useState } from 'react';

import { accountFailureText, useMyProfile, useUpdateMyProfile } from '../api/account.ts';
import { ApiError } from '../api/client.ts';
import { useSensitiveState } from '../api/sensitiveMutation.ts';
import { useAuth } from '../app/AuthProvider.tsx';
import { Alert, Done, Panel } from './Sections.tsx';

export function AccountProfile() {
  const profile = useMyProfile();
  const auth = useAuth();
  return (
    <Panel id="account-profile" title="Profile" tight>
      {profile.isPending ? <p role="status">Loading your profile…</p> : null}
      {profile.isError ? <Alert>Your profile could not be loaded. <button type="button" className="btn" onClick={() => { void profile.refetch(); }}>Try again</button></Alert> : null}
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
  const [email, setEmail] = useState(profile.email);
  const [proof, setProof] = useSensitiveState('');
  const [done, setDone] = useState(false);
  const dirty = username !== saved.username || displayName !== saved.display_name || email !== saved.email;
  const needsProof = username !== saved.username;

  return (
    <form onSubmit={(event) => {
      event.preventDefault();
      if (!dirty || update.isPending || (needsProof && proof === '')) return;
      setDone(false);
      update.mutate({ username, display_name: displayName, email, ...(needsProof ? { proof } : {}) }, {
        onSuccess: (result) => {
          setSaved(result);
          setUsername(result.username);
          setDisplayName(result.display_name);
          setEmail(result.email);
          setDone(true);
        },
      });
      setProof('');
    }}>
      {done ? <Done>Profile saved.</Done> : null}
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
        <div className="field">
          <label htmlFor={`${id}-email`}>Email address</label>
          <input id={`${id}-email`} name="email" type="email" autoComplete="email" maxLength={254}
            value={email} onChange={(event) => { setEmail(event.target.value); setDone(false); }} />
          <p className="settings-note">Optional contact address. This does not change how you sign in or link accounts.</p>
        </div>
        {needsProof ? <div className="field">
          <label htmlFor={`${id}-proof`}>Code or password</label>
          <input id={`${id}-proof`} name="proof" type="password" autoComplete="current-password" required
            value={proof} onChange={(event) => setProof(event.target.value)} aria-describedby={`${id}-proof-hint`} />
          <p id={`${id}-proof-hint`} className="settings-note">Confirm your username change with your authenticator code if enrolled, otherwise your current password.</p>
        </div> : null}
      </fieldset>
      {profile.managed ? <p className="settings-note">Your identity provider manages your username and display name. Change them there.</p> : null}
      {!profile.managed && !profile.username_editable ? <p className="settings-note">You sign in through your identity provider. Your display name and contact email can be changed here.</p> : null}
      <button type="submit" className="btn btn--primary" disabled={!dirty || update.isPending || (needsProof && proof === '')}>
        {update.isPending ? 'Saving…' : 'Save profile'}
      </button>
    </form>
  );
}
