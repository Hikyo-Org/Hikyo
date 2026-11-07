-- Personal values on the authorization surface (#1235, personal-values ADR,
-- Who can do what). ASCII only: multibyte characters shift sqlite statement
-- offsets.

-- The owner fallback authorize() makes for personal-value list and delete:
-- an owner may list masked and delete their own values whatever their grants,
-- while the account lives. It carries the gates ListGrantsForPrincipal
-- carries plus kind = 'human', and answers only whether the principal holds a
-- value or a notice in the resolved environment.
-- hikyo:reason authorize() admits the owner's own personal-value list and delete in an environment where the principal holds a value or notice; returns a count, never ciphertext or another principal's row.
-- hikyo:authn-resolution
-- name: PersonalOwnerHoldsInEnvironment :one
SELECT COUNT(*) FROM principals AS p
WHERE p.id = sqlc.arg(principal_id)
  AND p.kind = 'human'
  AND p.privacy_state = 'active'
  AND p.reconciled_epoch >= (SELECT restore_epoch FROM auth_instance_state WHERE auth_instance_state.id = 1)
  AND (EXISTS (
      SELECT 1 FROM personal_values AS v
      WHERE v.org_id = sqlc.arg(org_id) AND v.project_id = sqlc.arg(project_id)
        AND v.environment_id = sqlc.arg(env_id) AND v.owner_principal_id = p.id)
    OR EXISTS (
      SELECT 1 FROM personal_value_notices AS n
      WHERE n.org_id = sqlc.arg(org_id) AND n.project_id = sqlc.arg(project_id)
        AND n.environment_id = sqlc.arg(env_id) AND n.owner_principal_id = p.id));

-- The candidate superset for the matrix denominator: humans with an account
-- who hold `reveal` from a grant, a temporary access grant or a member access
-- rule that could reach the environment. It decides nothing: authorize's own
-- read-and-reveal evaluation runs over each candidate's grants and rules.
-- hikyo:reason the personal-value counts denominator enumerates human principals that might hold reveal in one environment; authz evaluates each with the chokepoint's grant and rule logic and returns only a count.
-- hikyo:authn-resolution
-- name: PersonalEligibleCandidates :many
SELECT DISTINCT a.principal_id FROM accounts AS a
JOIN principals AS p ON p.id = a.principal_id
WHERE p.kind = 'human'
  AND (EXISTS (
      SELECT 1 FROM grants AS g
      WHERE g.principal_id = a.principal_id AND g.capability = 'reveal'
        AND (g.org_id IS NULL OR g.org_id = sqlc.arg(org_id)))
    OR EXISTS (
      SELECT 1 FROM access_grants AS j
      WHERE j.principal_id = a.principal_id AND j.capability = 'reveal'
        AND j.org_id = sqlc.arg(org_id))
    OR EXISTS (
      SELECT 1 FROM rules AS r
      WHERE r.principal_id = a.principal_id AND r.capability = 'reveal'
        AND r.org_id = sqlc.arg(org_id)))
ORDER BY a.principal_id;
