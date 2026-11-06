import { useEffect, useRef, useState } from 'react';
import { Link, useLocation, useNavigate } from 'react-router';
import { z } from 'zod';

import {
  acceptanceFailureText,
  acceptEmailInvitation,
  INVALID_INVITATION_TEXT,
  previewEmailInvitation,
  type EmailInvitationPreview,
} from '../api/emailInvitations.ts';
import { ApiError } from '../api/client.ts';
import { useSensitiveMutation, useSensitiveState } from '../api/sensitiveMutation.ts';
import { useAuth } from '../app/AuthProvider.tsx';
import { surfaceById } from '../app/navigation.ts';
import { formatWhen } from '../lib/formatWhen.ts';
import { Alert } from '../ui/Alert.tsx';
import { Button } from '../ui/Button.tsx';

/** What the page keeps across the session ending: names only, never the token. */
const zJoined = z.object({
  joined: z.object({ org_name: z.string(), template: z.string(), signed_out: z.boolean() }),
});

/**
 * Accept an email invitation (#1207, registry surface `invitation-accept`).
 *
 * The single-use token arrives in the URL fragment, is read once, removed from
 * the address bar and held only in component memory. A signed-out person is
 * told to sign in with the invited account and open the link again: the token
 * is never carried through sign-in. Accepting ends every session of the
 * person, this one included, so the outcome is kept in router history state
 * (names only) and survives the remount the session change causes.
 */
export function InvitationAccept() {
  const location = useLocation();
  const navigate = useNavigate();
  const auth = useAuth();
  const [token, setToken] = useSensitiveState('');
  const [preview, setPreview] = useState<EmailInvitationPreview | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const requested = useRef(false);
  const signedIn = auth.state.status === 'authenticated';
  const login = surfaceById('login').path;

  const previewing = useSensitiveMutation({ mutationFn: previewEmailInvitation });
  const accepting = useSensitiveMutation({
    onMutate: auth.captureTransition,
    mutationFn: acceptEmailInvitation,
    onSuccess: (result, _token, guard) => {
      setToken('');
      const joined = { org_name: result.org_name, template: result.template, signed_out: result.signed_out };
      void navigate(location.pathname, { replace: true, state: { joined } });
      if (result.signed_out && guard !== undefined) auth.endSession(guard);
    },
  });

  useEffect(() => {
    if (location.hash === '') return;
    setToken(new URLSearchParams(location.hash.slice(1)).get('token') ?? '');
    // Remove the bearer token from the current history entry once captured.
    void navigate(`${location.pathname}${location.search}`, { replace: true });
  }, [location.hash, location.pathname, location.search, navigate, setToken]);

  useEffect(() => {
    if (!signedIn || token === '' || requested.current) return;
    requested.current = true;
    previewing.mutate(token, {
      onSuccess: setPreview,
      onError: (error) => {
        setToken('');
        setFailure(acceptanceFailureText(error));
      },
    });
  }, [signedIn, token, previewing, setToken]);

  const outcome = zJoined.safeParse(location.state);
  let body;
  if (outcome.success) {
    const { org_name: org, template, signed_out: signedOut } = outcome.data.joined;
    body = <>
      <Alert tone="done">You joined {org} as {template}.</Alert>
      {signedOut ? <>
        <p>Accepting ended all your sessions, so sign in again to continue.</p>
        <Link className="btn btn--primary" to={login}>Sign in</Link>
      </> : <Link className="btn btn--primary" to={surfaceById('projects').path}>Go to projects</Link>}
    </>;
  } else if (failure !== null) {
    body = <>
      <Alert>{failure}</Alert>
      <Link className="btn" to={signedIn ? surfaceById('projects').path : login}>{signedIn ? 'Go to projects' : 'Go to sign in'}</Link>
    </>;
  } else if (!signedIn) {
    body = <>
      <p>
        To accept this invitation, sign in with the account it was sent to, then open the link from
        your email again.
      </p>
      <Link className="btn btn--primary" to={login}>Go to sign in</Link>
    </>;
  } else if (preview === null) {
    // A fragment still being captured is not yet a missing token.
    body = token === '' && location.hash === '' && !previewing.isPending
      ? <>
        <Alert>{INVALID_INVITATION_TEXT}</Alert>
        <Link className="btn" to={surfaceById('projects').path}>Go to projects</Link>
      </>
      : <p role="status">Checking the invitation…</p>;
  } else {
    body = <>
      <dl>
        <dt>Organisation</dt>
        <dd>{preview.org_name}</dd>
        <dt>Role template</dt>
        <dd>{preview.template}</dd>
        <dt>Expires</dt>
        <dd>{formatWhen(preview.expires_at)}</dd>
      </dl>
      <Alert tone="warn">
        Accepting signs you out on every device and ends your developer credentials, so anything
        using them stops working until you create new ones. If this becomes your only organisation,
        its administrators can reset your sign-in.
      </Alert>
      {accepting.isError ? <Alert>{acceptanceFailureText(accepting.error)}</Alert> : null}
      <div className="actions">
        <Button
          type="button"
          variant="primary"
          disabled={accepting.isPending}
          aria-busy={accepting.isPending ? true : undefined}
          onClick={() => accepting.mutate(token, {
            onError: (error) => {
              // The uniform 404 is final; anything else (throttle, transport) can be retried.
              if (!(error instanceof ApiError) || error.status !== 404) return;
              setToken('');
              setFailure(acceptanceFailureText(error));
            },
          })}
        >
          {accepting.isPending ? 'Accepting…' : 'Accept'}
        </Button>
        <Button type="button" disabled={accepting.isPending} onClick={() => { setToken(''); void navigate(surfaceById('projects').path); }}>
          Not now
        </Button>
      </div>
    </>;
  }

  return <main className="login"><div className="login__card">
    <h1 className="login__title">Accept an invitation</h1>
    {body}
  </div></main>;
}
