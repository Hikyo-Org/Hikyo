import { useState } from 'react';
import { identityRefusalText, type ProjectRef } from '../../api/identities.ts';
import { useMachineReveal, useSetMachineReveal } from '../../api/machineReveal.ts';
import { Alert } from '../../ui/Alert.tsx';
import { Button } from '../../ui/Button.tsx';
import { Checkbox } from '../../ui/Checkbox.tsx';
import { Dialog } from '../../ui/Dialog.tsx';

/**
 * PolicyStrip is the per-project machine-reveal opt-in (source-of-truth ADR:
 * "an explicit, documented, per-project operator opt-in, never a default").
 *
 * It is a toggle with a ceremony both ways. Enabling states, at opt-in time,
 * what the machine-identities ADR requires it to state: a machine principal
 * holding reveal is a standing decryption capability, while workload
 * reveal-history is admitted only for an active non-current pin. Withdrawing
 * states the other half: both disclosure atoms go inert on the next fetch,
 * and every machine cursor moves. The write is
 * project-settings ∧ reveal at project depth, so it is MFA-mandatory; a
 * session short of that is told so by the server and the strip repeats it.
 */
export function PolicyStrip({ project }: { project: ProjectRef }) {
  const query = useMachineReveal(project.org, project.project);
  const write = useSetMachineReveal(project.org, project.project);
  const [confirming, setConfirming] = useState<boolean | null>(null);
  const [failure, setFailure] = useState<string | null>(null);

  if (query.isPending) {
    return (
      <p className="notice machine__policy" role="status">{/* markup-check: machine__policy skin */}
        <span className="alert__glyph" aria-hidden="true">
          ◆
        </span>
        <span>Reading the project&apos;s machine-reveal opt-in…</span>
      </p>
    );
  }
  if (query.isError) {
    return (
      <p className="alert machine__policy" role="alert">{/* markup-check: machine__policy skin */}
        <span className="alert__glyph" aria-hidden="true">
          !
        </span>
        <span>
          The project&apos;s machine-reveal opt-in could not be read: {identityRefusalText(query.error)}
        </span>
      </p>
    );
  }
  const enabled = query.data.enabled;
  const commit = (next: boolean) => {
    setFailure(null);
    write.mutate(next, {
      onSuccess: () => setConfirming(null),
      onError: (error) => setFailure(identityRefusalText(error)),
    });
  };
  return (
    <>
      <p className="notice machine__policy" role="status">{/* markup-check: machine__policy skin */}
        <span className="alert__glyph" aria-hidden="true">
          ◆
        </span>
        <span>
          <strong>
            Machine secret delivery (per-project opt-in): {enabled ? 'on' : 'off'}.
          </strong>{' '}
          {enabled
            ? 'Workload and automation principals may hold reveal; a workload may also hold reveal-history while pinned to a non-current revision. Either can deliver secret plaintext.'
            : 'Every workload delivery is configuration and secret presence only; the grant API refuses both machine disclosure capabilities until this is on.'}
        </span>
        <Button
          id="machine-policy-toggle"
          type="button"
          className="machine__policy-toggle"
          onClick={() => setConfirming(!enabled)}
          disabled={write.isPending}
        >
          {enabled ? 'Withdraw the opt-in…' : 'Enable the opt-in…'}
        </Button>
      </p>
      {confirming !== null ? (
        <MachineRevealDialog
          enable={confirming}
          busy={write.isPending}
          failure={failure}
          onConfirm={() => commit(confirming)}
          onClose={() => {
            setConfirming(null);
            setFailure(null);
          }}
        />
      ) : null}
    </>
  );
}

export function MachineRevealDialog({
  enable,
  busy,
  failure,
  onConfirm,
  onClose,
}: {
  enable: boolean;
  busy: boolean;
  failure: string | null;
  onConfirm: () => void;
  onClose: () => void;
}) {
  const [acknowledged, setAcknowledged] = useState(false);
  return (
    <Dialog
      title={enable ? 'Enable machine secret delivery' : 'Withdraw machine secret delivery'}
      onCancel={(event) => {
        // The element used to let the platform close it and reported that
        // through `onClose`; the atom owns the element, so the same refusal
        // while busy and the same dismissal otherwise are spelled here.
        event.preventDefault();
        if (!busy) {
          onClose();
        }
      }}
      actions={
        <>
          <Button type="button" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button
            type="button"
            variant="primary"
            onClick={onConfirm}
            disabled={busy || (enable && !acknowledged)}
          >
            {enable ? 'Enable the opt-in' : 'Withdraw the opt-in'}
          </Button>
        </>
      }
    >
      {enable ? (
        <>
          <p>
            A machine principal holding <strong>reveal</strong> is a standing decryption capability:
            no second factor, no ceremony, every current fetch delivers plaintext while the grant
            stands. A CI runner holding it is that capability in the most-attacked box in the system.
          </p>
          <p>
            Enabling admits <strong>reveal</strong> grants onto workload and automation principals.
            It also admits <strong>reveal-history</strong> onto a workload only while that workload
            has an active non-current pin. Each grant still runs its own widening ceremony. Nothing
            is granted by this act alone.
          </p>
          <Checkbox
            className="ceremony__ack"
            label="I understand this admits standing decryption capabilities onto machine principals in this project."
            checked={acknowledged}
            onChange={(event) => setAcknowledged(event.target.checked)}
            disabled={busy}
          />
        </>
      ) : (
        <p>
          Withdrawing makes every machine <strong>reveal</strong> and{' '}
          <strong>reveal-history</strong> grant in this project inert on the next fetch and moves
          every machine cursor. Workloads keep receiving configuration and secret presence only.
          Grant rows stay where they are so the withdrawal is reversible by the same act.
        </p>
      )}
      {failure !== null ? (
        <Alert>{failure}</Alert>
      ) : null}
    </Dialog>
  );
}
