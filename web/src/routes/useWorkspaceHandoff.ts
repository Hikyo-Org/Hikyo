import { useCallback, useEffect, useRef, useState } from 'react';

import {
  openPrepared,
  prepareWorkspace,
  type PreparedWorkspace,
  type StepUpParams,
} from '../api/workspace.ts';

type HandoffPhase =
  | { readonly kind: 'contacting' }
  | { readonly kind: 'ready'; readonly prepared: PreparedWorkspace }
  | { readonly kind: 'authorising'; readonly prepared: PreparedWorkspace }
  | { readonly kind: 'failed'; readonly message: string };

type HandoffFailureStage = 'prepare' | 'authorise';

export type WorkspaceHandoffPreparation =
  | { readonly kind: 'establishment' }
  | { readonly kind: 'step-up'; readonly params: StepUpParams }
  | { readonly kind: 'refused'; readonly message: string };

type WorkspaceHandoffOptions = {
  readonly preparation: WorkspaceHandoffPreparation;
  readonly onFailMessage: (error: unknown, stage: HandoffFailureStage) => string;
  readonly onAuthorised?: () => void;
};

export type WorkspaceHandoff = {
  readonly phase: HandoffPhase;
  readonly retry: () => void;
  readonly authorise: () => void;
};

type HandoffActionLabels = {
  readonly ready: string;
  readonly authorising: string;
};

type HandoffAction = {
  readonly label: string;
  readonly disabled: boolean;
  readonly onClick: (() => void) | undefined;
};

/** Derives the one launcher action from the hook's discriminated phase. */
export function workspaceHandoffAction(
  handoff: WorkspaceHandoff,
  labels: HandoffActionLabels,
  onRetry: () => void = handoff.retry,
): HandoffAction {
  switch (handoff.phase.kind) {
    case 'contacting':
      return { label: 'Contacting…', disabled: true, onClick: undefined };
    case 'ready':
      return { label: labels.ready, disabled: false, onClick: handoff.authorise };
    case 'authorising':
      return { label: labels.authorising, disabled: true, onClick: undefined };
    case 'failed':
      return { label: 'Try again', disabled: false, onClick: onRetry };
  }
}

/**
 * Owns one cross-origin workspace handoff from eager preparation through the
 * popup wait. `authorise` deliberately calls `openPrepared` synchronously: no
 * promise or effect may sit between the click and `window.open`.
 *
 * Every attempt owns one AbortController. Retry, supersession by a changed
 * target, and unmount all abort the attempt they replace, which stops its
 * requests rather than merely ignoring their answers. A disposed attempt's
 * settlement is dropped without touching state; a deadline inside a live
 * attempt surfaces as `failed` with a retry.
 */
export function useWorkspaceHandoff(
  origin: string,
  options: WorkspaceHandoffOptions,
): WorkspaceHandoff {
  const [phase, setPhase] = useState<HandoffPhase>(() =>
    options.preparation.kind === 'refused'
      ? { kind: 'failed', message: options.preparation.message }
      : { kind: 'contacting' },
  );
  const phaseRef = useRef(phase);
  const attemptRef = useRef<AbortController | undefined>(undefined);
  const onFailMessageRef = useRef(options.onFailMessage);
  const onAuthorisedRef = useRef(options.onAuthorised);
  useEffect(() => {
    onFailMessageRef.current = options.onFailMessage;
    onAuthorisedRef.current = options.onAuthorised;
  }, [options.onFailMessage, options.onAuthorised]);

  const preparationKind = options.preparation.kind;
  const session =
    options.preparation.kind === 'step-up' ? options.preparation.params.session : undefined;
  const operation =
    options.preparation.kind === 'step-up' ? options.preparation.params.operation : undefined;
  const environment =
    options.preparation.kind === 'step-up' ? options.preparation.params.environment : undefined;
  const keySetKey =
    options.preparation.kind === 'step-up' ? options.preparation.params.keySet.join(',') : undefined;
  const unavailableMessage =
    options.preparation.kind === 'refused' ? options.preparation.message : undefined;
  const visiblePhase: HandoffPhase =
    preparationKind === 'refused'
      ? { kind: 'failed', message: unavailableMessage ?? 'Workspace handoff is unavailable.' }
      : phase;

  // The async attempt only. The mount effect runs this directly, so it must not
  // set phase synchronously: the initial phase is already seeded 'contacting'
  // (or 'failed' when refused), and only the button retry below transitions a
  // visible phase back to 'contacting'. Supersession rides this callback's deps.
  const kickoff = useCallback(() => {
    const { signal } = beginAttempt(attemptRef);
    if (preparationKind === 'refused') return;

    const stepUp =
      session === undefined || operation === undefined || environment === undefined
        ? undefined
        : {
            session,
            operation,
            environment,
            keySet: keySetKey === '' || keySetKey === undefined ? [] : keySetKey.split(','),
          };
    void prepareWorkspace(origin, { signal, stepUp })
      .then((prepared) => {
        if (signal.aborted) return;
        const ready: HandoffPhase = { kind: 'ready', prepared };
        phaseRef.current = ready;
        setPhase(ready);
      })
      .catch((error: unknown) => {
        if (signal.aborted) return;
        const failed: HandoffPhase = {
          kind: 'failed',
          message: onFailMessageRef.current(error, 'prepare'),
        };
        phaseRef.current = failed;
        setPhase(failed);
      });
  }, [environment, keySetKey, operation, origin, preparationKind, session]);

  const retry = useCallback(() => {
    if (preparationKind === 'refused') {
      const failed: HandoffPhase = {
        kind: 'failed',
        message: unavailableMessage ?? 'Workspace handoff is unavailable.',
      };
      phaseRef.current = failed;
      setPhase(failed);
      beginAttempt(attemptRef);
      return;
    }
    const contacting: HandoffPhase = { kind: 'contacting' };
    phaseRef.current = contacting;
    setPhase(contacting);
    kickoff();
  }, [kickoff, preparationKind, unavailableMessage]);

  useEffect(() => () => attemptRef.current?.abort(), []);

  useEffect(() => {
    kickoff();
  }, [kickoff]);

  const authorise = useCallback(() => {
    const ready = phaseRef.current;
    if (ready.kind !== 'ready') return;

    const { signal } = beginAttempt(attemptRef);
    const authorising: HandoffPhase = { kind: 'authorising', prepared: ready.prepared };
    phaseRef.current = authorising;
    setPhase(authorising);

    // Load-bearing popup invariant: this call remains in the click's stack.
    void openPrepared(ready.prepared, { signal })
      .then(() => {
        if (signal.aborted) return;
        onAuthorisedRef.current?.();
      })
      .catch((error: unknown) => {
        if (signal.aborted) return;
        const failed: HandoffPhase = {
          kind: 'failed',
          message: onFailMessageRef.current(error, 'authorise'),
        };
        phaseRef.current = failed;
        setPhase(failed);
      });
  }, []);

  return { phase: visiblePhase, retry, authorise };
}

/** Aborts the attempt in flight, if any, and installs the next one. */
function beginAttempt(attemptRef: { current: AbortController | undefined }): AbortController {
  attemptRef.current?.abort();
  const attempt = new AbortController();
  attemptRef.current = attempt;
  return attempt;
}
