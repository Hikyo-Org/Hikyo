import { useState } from 'react';

import type { ServiceAccount } from '../../api/identities.ts';
import { ceremonyRefusalText } from '../../api/values.ts';
import { RuleReauthFailure, ruleFailureText, RuleSaveFailure, useRuleMutations, wideningEnvironment } from '../../api/rules.ts';
import { Alert } from '../../ui/Alert.tsx';
import { Button } from '../../ui/Button.tsx';
import { Glyph } from '../../ui/Glyph.tsx';
import { ALL, envName, newRule, type Rule, type World } from '../accessRules/model.ts';
import { RuleSummary } from '../accessRules/parts.tsx';
import { RuleEditorDialog } from '../accessRules/RuleEditorDialog.tsx';

/**
 * A service account's access rules (member-access-rules ADR D11): its rule
 * cards, and the Members rule editor for its class. A machine rule names this
 * project only and never limits keys. A workload rule lists its
 * environments; an automation rule may also cover environments added later.
 *
 * A save that makes plaintext newly reachable takes one passkey
 * reauthentication per such environment (see `saveRule`).
 */
export function MachineRules({
  org,
  project,
  account,
  world,
  unavailable,
  ready,
}: {
  org: string;
  project: string;
  account: ServiceAccount;
  /** This project, its service accounts and their rules. */
  world: World;
  /** Why the rules are not shown, or null when they are. */
  unavailable: string | null;
  /** The rules and the environments are read, so an edit starts from the current state. */
  ready: boolean;
}) {
  const mutations = useRuleMutations(org);
  const [editing, setEditing] = useState<{ readonly before: Rule | null; readonly draft: Rule } | null>(null);
  const [editorFailure, setEditorFailure] = useState<string | null>(null);
  const [outcome, setOutcome] = useState<{ readonly ok: boolean; readonly text: string } | null>(null);
  const rules = world.rules.filter((rule) => rule.member === account.principal_id && rule.source.kind === 'rule');
  const busy = mutations.save.isPending || mutations.remove.isPending;

  const open = (before: Rule | null, draft: Rule) => {
    setEditorFailure(null);
    setOutcome(null);
    setEditing({ before, draft });
  };
  const failureText = (error: unknown) => {
    if (error instanceof RuleReauthFailure) {
      return `This rule newly reaches plaintext in ${envName(world, error.environment)}, which needs a passkey reauthentication over it. ${ceremonyRefusalText(error.cause)} Nothing was saved.`;
    }
    const environment = wideningEnvironment(error);
    return environment === undefined
      ? ruleFailureText(error)
      : `The passkey reauthentication over ${envName(world, environment)} completed, but the server still refused this rule. Nothing was saved. Reload and try again.`;
  };
  // An atomic change can commit before its response is lost: never retry the
  // old IDs or draft, close the editor over the refreshed listing.
  const settleFailure = (error: unknown) => {
    if (error instanceof RuleSaveFailure) {
      setEditing(null);
      setOutcome({ ok: false, text: ruleFailureText(error) });
      return;
    }
    setEditorFailure(failureText(error));
  };

  return (
    <section aria-labelledby={`machine-rules-${account.id}`}>
      <h3 id={`machine-rules-${account.id}`} className="machine__subhead">
        Access rules
      </h3>
      {outcome === null ? null : <Alert tone={outcome.ok ? 'done' : 'danger'}>{outcome.text}</Alert>}
      {unavailable !== null ? (
        <p className="machine__none" role="status">
          {unavailable}
        </p>
      ) : rules.length === 0 ? (
        <p className="machine__none">No access rules. This account reaches only what its environment grants give.</p>
      ) : (
        <ul className="access-rule-list">
          {rules.map((rule, i) => (
            <li key={rule.id} className="access-rule">
              <RuleSummary world={world} rule={rule} />
              <Button type="button" variant="quiet" disabled={!ready || busy} onClick={() => open(rule, rule)}>
                Edit<span className="visually-hidden"> rule {i + 1} of {account.name}</span>
              </Button>
            </li>
          ))}
        </ul>
      )}
      {unavailable === null ? (
        <Button
          type="button"
          variant="quiet"
          disabled={!ready || busy}
          onClick={() =>
            open(null, { ...newRule(account.principal_id), projects: [project], envs: account.kind === 'workload' ? { mode: 'only', items: [] } : ALL })
          }
        >
          <Glyph name="plus" /> Add rule<span className="visually-hidden"> for {account.name}</span>
        </Button>
      ) : null}

      {editing === null ? null : (
        <RuleEditorDialog
          world={world}
          rule={editing.draft}
          projects={[project]}
          machine={account.kind}
          showReach={false}
          busy={busy}
          failure={editorFailure}
          onCancel={() => setEditing(null)}
          onRemove={(rule) => {
            setEditorFailure(null);
            mutations.remove.mutate(rule, {
              onSuccess: () => {
                setEditing(null);
                setOutcome({ ok: true, text: `Removed the rule from ${account.name}.` });
              },
              onError: settleFailure,
            });
          }}
          onSave={(draft) => {
            setEditorFailure(null);
            const before = editing.before;
            mutations.save.mutate(
              { before, draft },
              {
                onSuccess: () => {
                  setEditing(null);
                  setOutcome({ ok: true, text: `${before === null ? 'Added a rule for' : 'Saved the rule of'} ${account.name}. It re-scopes every credential of the account at once.` });
                },
                onError: settleFailure,
              },
            );
          }}
        />
      )}
    </section>
  );
}
