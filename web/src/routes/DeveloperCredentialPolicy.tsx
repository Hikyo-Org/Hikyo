import { useContext, useId, useState } from 'react';

import { developerCredentialFailureText, useDeveloperCredentialPolicy, useSetDeveloperCredentialPolicy } from '../api/developerCredentials.ts';
import { duration } from '../app/duration.ts';
import { Alert } from '../ui/Alert.tsx';
import { Button } from '../ui/Button.tsx';
import { ReadOnlyContext } from '../ui/ReadOnlyContext.ts';
import { SidePanel } from '../ui/SidePanel.tsx';
import { ConsequencesDialog, Panel } from './Sections.tsx';
import { useFeedback } from './useFeedback.ts';

export function DeveloperCredentialPolicy() {
  const policy = useDeveloperCredentialPolicy();
  const update = useSetDeveloperCredentialPolicy();
  const readOnly = useContext(ReadOnlyContext);
  const inputId = useId();
  const hintId = useId();
  const [draft, setDraft] = useState<string | null>(null);
  const [proposal, setProposal] = useState<number | null>(null);
  const feedback = useFeedback(developerCredentialFailureText);
  const [validation, setValidation] = useState<string | null>(null);
  const current = policy.isSuccess ? policy.data : undefined;

  return <Panel id="instance-developer-credentials" title="Developer credential policy">
    <p className="settings-note">Developer credentials are online-only, nonrenewable delegations for one unprotected environment. At most four live credentials per human are allowed across this instance. Instance-config permission and second-factor assurance are required here.</p>
    {policy.isPending ? <p role="status">Loading developer credential policy…</p> : null}
    {policy.isError ? <Alert>{developerCredentialFailureText(policy.error)}</Alert> : null}
    {feedback.done === null ? null : <Alert tone="done">{feedback.done}</Alert>}
    {draft === null && feedback.failure !== null ? <Alert>{feedback.failure}</Alert> : null}
    <div className="actions"><Button type="button" readOnlySafe disabled={policy.isFetching || update.isPending} onClick={() => { void policy.refetch(); }}>Refresh developer credential policy</Button></div>
    {current === undefined ? null : <>
      <div className="settings-row">
        <div className="settings-row__copy"><span className="settings-row__title">Maximum developer credential lifetime</span><span className="settings-row__detail">{duration(Number(current.max_lifetime_seconds))} ({String(current.max_lifetime_seconds)} seconds); hard limit eight hours</span></div>
        <span className="settings-row__spacer" />
        <Button type="button" onClick={() => { feedback.clear(); setValidation(null); setDraft(String(current.max_lifetime_seconds)); }}>Edit developer credential policy</Button>
      </div>
      {draft === null ? null : <SidePanel title="Edit developer credential policy" onClose={() => { if (!update.isPending) { setDraft(null); setProposal(null); } }}>
        <form onSubmit={(event) => {
          event.preventDefault();
          if (readOnly || update.isPending) return;
          const seconds = Number(draft);
          if (!/^\d+$/.test(draft) || !Number.isInteger(seconds) || seconds < 1 || seconds > 28800) {
            setValidation('Enter a whole number from 1 to 28,800 seconds.');
            return;
          }
          feedback.clear();
          setProposal(seconds);
        }}>
          <div className="field">
            <label htmlFor={inputId}>Maximum developer credential lifetime (seconds)</label>
            <input id={inputId} name="max_lifetime_seconds" type="number" inputMode="numeric" min="1" max="28800" step="1" required value={draft} disabled={readOnly || update.isPending} aria-describedby={hintId} aria-invalid={validation !== null} onChange={(event) => { setDraft(event.target.value); setProposal(null); setValidation(null); }} />
            <p className="field__hint" id={hintId}>1 to 28,800 seconds. Lowering the ceiling permanently clamps existing expiry to the earlier of its current expiry and mint time plus this ceiling. Raising it never extends or revives a credential.</p>
          </div>
          {validation === null ? null : <Alert>{validation}</Alert>}
          {proposal === null && feedback.failure !== null ? <Alert>{feedback.failure}</Alert> : null}
          <div className="actions"><Button type="submit" variant="primary" disabled={update.isPending}>Review developer credential policy</Button><Button type="button" disabled={update.isPending} onClick={() => { setDraft(null); setProposal(null); }}>Cancel</Button></div>
        </form>
      </SidePanel>}
      {proposal === null ? null : <ConsequencesDialog title="Change developer credential ceiling?" confirmLabel="Apply developer credential policy" busyLabel="Applying developer credential policy…" busy={update.isPending} failure={feedback.failure} onCancel={() => { setProposal(null); feedback.clear(); }} onConfirm={() => {
        if (readOnly) return;
        update.mutate(proposal, {
          onSuccess: (result) => { setProposal(null); setDraft(null); feedback.ok(`Developer credential ceiling saved: ${String(result.max_lifetime_seconds)} seconds. Existing expiry reductions are permanent.`); },
          onError: feedback.report,
        });
      }}>
        <p>Set the instance ceiling to <strong>{proposal} seconds</strong>. The last loaded ceiling is {String(current.max_lifetime_seconds)} seconds.</p>
        <p>If this lowers the current server policy, outstanding credentials are shortened from their original mint time and may expire immediately. Raising the ceiling later will not restore their old expiry. New credentials remain capped at eight hours.</p>
      </ConsequencesDialog>}
    </>}
  </Panel>;
}
