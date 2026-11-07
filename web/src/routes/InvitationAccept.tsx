import { useEffect, useRef, useState } from 'react';
import { Link, useLocation, useNavigate } from 'react-router';
import { z } from 'zod';

import {
  acceptanceFailureText,
  acceptEmailInvitation,
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
  // The preview keeps the token it was fetched for: Accept submits that token,
  // so the organisation on screen is always the one being accepted. It holds
  // the token, so a session retirement clears it like the token itself.
  const [previewed, setPreviewed] = useSensitiveState<{ token: string; offer: EmailInvitationPreview } | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const requested = useRef('');
  const ending = useRef<ReturnType<typeof auth.captureTransition> | null>(null);
  const signedIn = auth.state.status === 'authenticated';
  const login = surfaceById('login').path;

  const previewing = useSensitiveMutation({ mutationFn: previewEmailInvitation });
  const accepting = useSensitiveMutation({
    onMutate: auth.captureTransition,
    mutationFn: acceptEmailInvitation,
    onSuccess: (result, _token, guard) => {
      setToken('');
      const joined = { org_name: result.org_name, template: result.template, signed_out: result.signed_out };
      if (result.signed_out && guard !== undefined) ending.current = guard;
      void navigate(location.pathname, { replace: true, state: { joined } });
    },
  });

  useEffect(() => {
    if (location.hash === '') return;
    setToken(new URLSearchParams(location.hash.slice(1)).get('token') ?? '');
    // Remove the bearer token from the current history entry once captured.
    void navigate(`${location.pathname}${location.search}`, { replace: true });
  }, [location.hash, location.pathname, location.search, navigate, setToken]);

  useEffect(() => {
    // Forget every copy of a cleared token (a refusal, an acceptance, a link
    // without one or a session retirement), and revoke a preview still pending
    // for it, so opening the same link again is previewed afresh.
    if (token === '') {
      requested.current = '';
      setPreviewed(null);
      // Only while pending: the effect runs every render and a reset re-renders.
      if (previewing.isPending) previewing.reset();
    }
    // A pending Accept may already have committed: resetting it would drop its
    // outcome, so nothing is previewed while it is pending. A link that arrives
    // meanwhile is previewed after the Accept fails; after a success the page
    // shows that outcome and the new link must be opened again.
    if (!signedIn || token === '' || requested.current === token || accepting.isPending) return;
    requested.current = token;
    // A new token's preview clears an Accept failure that belonged to the previous one.
    accepting.reset();
    previewing.mutate(token, {
      onSuccess: (offer) => {
        setFailure(null);
        setPreviewed({ token, offer });
      },
      onError: (error) => {
        setToken('');
        setFailure(acceptanceFailureText(error));
      },
    });
  }, [signedIn, token, previewing, accepting, setToken, setPreviewed]);

  const preview = previewed?.token === token ? previewed.offer : null;
  // Every refusal retires the token, so a failure belongs to the empty token only.
  const shownFailure = token === '' ? failure : null;
  const outcome = zJoined.safeParse(location.state);
  // End the session only once the router holds the outcome: ending it remounts
  // the whole tree, router included, and drops a navigation not yet committed.
  useEffect(() => {
    if (!outcome.success || ending.current === null) return;
    const guard = ending.current;
    ending.current = null;
    auth.endSession(guard);
  }, [outcome.success, auth]);
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
  } else if (shownFailure !== null) {
    body = <>
      <Alert>{shownFailure}</Alert>
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
        <p>This page needs the link from your invitation email. Open that link again, signed in with the account it was sent to.</p>
        <Link className="btn" to={surfaceById('projects').path}>Go to projects</Link>
      </>
      : <p role="status">Checking the invitation…</p>;
  } else {
    body = <>
      <dl className="invitation__facts">
        <dt>Organisation</dt>
        <dd>{preview.org_name}</dd>
        <dt>Role template</dt>
        <dd>{preview.template}</dd>
        <dt>Expires</dt>
        <dd>{formatWhen(preview.expires_at)}</dd>
      </dl>
      <Alert tone="warn">
        If accepting adds access, you will be signed out on every device and your developer
        credentials will stop working until you create new ones. If this becomes your only
        organisation, its administrators can reset your sign-in.
      </Alert>
      {accepting.isError ? <Alert>{acceptanceFailureText(accepting.error)}</Alert> : null}
      <div className="actions">
        <Button
          type="button"
          variant="primary"
          disabled={accepting.isPending}
          aria-busy={accepting.isPending ? true : undefined}
          onClick={() => accepting.mutate(token, {
            onError: (error, refused) => {
              // The uniform 404 is final; anything else (throttle, transport) can be retried.
              if (!(error instanceof ApiError) || error.status !== 404) return;
              // Reopening the same link must preview it afresh, not show the refused
              // offer; a link opened while Accept was pending keeps its own token.
              setPreviewed(null);
              setToken((current) => (current === refused ? '' : current));
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
