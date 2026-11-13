-- name: GatedBoth :one
SELECT id FROM principals AS p WHERE p.privacy_state = 'active' AND p.containment_state = 'none';

-- name: PrivacyOnly :one
SELECT id FROM principals AS p WHERE p.privacy_state = 'active';

-- name: WrongQualifier :many
SELECT g.capability FROM grants AS g
JOIN principals AS p ON p.id = g.principal_id
JOIN principals AS jp ON jp.id = g.principal_id
WHERE p.privacy_state = 'active' AND p.containment_state = 'none' AND jp.privacy_state = 'active';

-- name: BareGated :one
SELECT session_generation FROM principals WHERE id = ? AND privacy_state = 'active' AND containment_state = 'none';

-- name: BareUngated :one
SELECT session_generation FROM principals WHERE id = ? AND privacy_state = 'active';

-- name: Exempted :one
SELECT id FROM principals WHERE privacy_state = 'active';

-- name: ExemptedButGated :one
SELECT id FROM principals WHERE privacy_state = 'active' AND containment_state = 'none';

-- name: NoPrivacyAtAll :one
SELECT id FROM principals WHERE id = ?;

-- name: CommentedLine :one
SELECT id FROM principals AS p WHERE p.privacy_state = 'active' -- AND p.containment_state = 'none'
;

-- name: CommentedBlock :one
SELECT id FROM principals AS p WHERE p.privacy_state = 'active' /* AND p.containment_state = 'none' */;

-- name: CommentedNested :one
SELECT id FROM principals AS p WHERE p.privacy_state = 'active' /* a /* b */ AND p.containment_state = 'none' */;

-- name: CommentedPrivacyOnly :one
SELECT id FROM principals AS p WHERE p.containment_state = 'none' -- AND p.privacy_state = 'active'
;

-- name: ReversedPrivacy :one
SELECT id FROM principals AS p WHERE 'active' = p.privacy_state;

-- name: InListPrivacy :one
SELECT id FROM principals AS p WHERE p.privacy_state IN ('active');

-- name: NotEqualRestricted :one
SELECT id FROM principals AS p WHERE p.privacy_state <> 'restricted';

-- name: BangEqualRestricted :one
SELECT id FROM principals AS p WHERE p.privacy_state != 'restricted';

-- name: LoudPrivacy :one
SELECT id FROM principals AS p WHERE   P.PRIVACY_STATE   =   'active';

-- name: LiteralInComment :one
SELECT id FROM principals AS p WHERE p.privacy_state = 'active' AND p.containment_state = 'none' -- privacy_state = 'active' again
;

-- name: ReversedGated :one
SELECT id FROM principals AS p WHERE 'active' = p.privacy_state AND 'none' = p.containment_state;

-- name: InListGated :one
SELECT id FROM principals AS p WHERE p.privacy_state IN ('active') AND p.containment_state IN ('none');

-- name: NotEqualGated :one
SELECT id FROM principals AS p WHERE p.privacy_state <> 'restricted' AND p.containment_state <> 'contained';

-- name: LoudGated :one
SELECT id FROM principals AS p WHERE   P.PRIVACY_STATE   =   'active' AND   P.Containment_State='none';

-- name: QuotedAliasPredicate :one
SELECT id AS "p.containment_state = 'none'" FROM principals AS p WHERE p.privacy_state = 'active';

-- name: StringAliasPredicate :one
SELECT id AS 'p.containment_state = ''none''' FROM principals AS p WHERE p.privacy_state = 'active';

-- A quoted reference to the column is the column: this is gated.
-- name: QuotedIdentifierPredicate :one
SELECT id FROM principals AS p WHERE p.privacy_state = 'active' AND p."containment_state" = 'none';

-- name: QuotedDashesUngated :one
SELECT id AS "-- label" FROM principals AS p WHERE p.privacy_state = 'active';

-- name: QuotedDashesGated :one
SELECT id AS "-- label" FROM principals AS p WHERE p.privacy_state = 'active' AND p.containment_state = 'none';

-- name: OrInsideGroup :one
SELECT id FROM principals AS p WHERE p.privacy_state = 'active' AND (p.containment_state = 'none' OR p.id = ?);

-- name: OrAtTopLevel :one
SELECT id FROM principals AS p WHERE p.privacy_state = 'active' AND p.containment_state = 'none' OR p.id = ?;

-- name: OrBothInGroup :one
SELECT id FROM principals AS p WHERE (p.privacy_state = 'active' AND p.containment_state = 'none') OR p.id = ?;

-- name: OrElsewhereGated :one
SELECT id FROM principals AS p WHERE (p.id = ? OR p.id = ?) AND p.privacy_state = 'active' AND p.containment_state = 'none';

-- name: OrInJoinGated :many
SELECT g.capability FROM grants AS g
JOIN principals AS p ON p.id = g.principal_id AND p.privacy_state = 'active' AND p.containment_state = 'none'
WHERE g.capability = ? OR g.capability = ?;

-- name: NegatedGroup :one
SELECT id FROM principals AS p WHERE p.privacy_state = 'active' AND NOT (p.containment_state = 'none');

-- name: NegatedCondition :one
SELECT id FROM principals AS p WHERE p.privacy_state = 'active' AND NOT p.containment_state = 'none';

-- name: CaseOnly :one
SELECT CASE WHEN p.containment_state = 'none' THEN 1 ELSE 0 END FROM principals AS p WHERE p.privacy_state = 'active';

-- name: SelectListOnly :one
SELECT p.containment_state = 'none' AS held FROM principals AS p WHERE p.privacy_state = 'active';

-- name: ContainmentInOtherSubquery :one
SELECT id FROM principals AS p WHERE p.privacy_state = 'active'
  AND EXISTS (SELECT 1 FROM grants AS g WHERE g.principal_id = p.id AND p.containment_state = 'none');

-- name: BareContainmentInSubquery :one
SELECT id FROM principals WHERE privacy_state = 'active'
  AND id IN (SELECT id FROM principals WHERE containment_state = 'none');

-- name: BothInSubquery :one
SELECT id FROM grants WHERE principal_id IN (SELECT p.id FROM principals AS p WHERE p.privacy_state = 'active' AND p.containment_state = 'none');

-- name: UnionBothGated :many
SELECT p.id FROM principals AS p WHERE p.privacy_state = 'active' AND p.containment_state = 'none'
UNION ALL
SELECT p.id FROM principals AS p WHERE p.privacy_state = 'active' AND p.containment_state = 'none';

-- name: UnionHalfGated :many
SELECT p.id FROM principals AS p WHERE p.privacy_state = 'active' AND p.containment_state = 'none'
UNION ALL
SELECT p.id FROM principals AS p WHERE p.privacy_state = 'active';

-- name: QuotedQualifiedPrivacy :one
SELECT id FROM principals AS p WHERE p."privacy_state" = 'active';

-- name: QuotedBarePrivacy :one
SELECT id FROM principals WHERE "privacy_state" = 'active';

-- name: BacktickQualifiedPrivacy :one
SELECT id FROM principals AS p WHERE p.`privacy_state` = 'active';

-- name: BacktickBarePrivacy :one
SELECT id FROM principals WHERE `privacy_state` = 'active';

-- name: QuotedEverywherePrivacy :one
SELECT id FROM principals AS "p" WHERE "p"."privacy_state" = 'active';

-- name: QuotedEverywhereGated :one
SELECT id FROM principals AS "p" WHERE "p"."privacy_state" = 'active' AND "p"."containment_state" = 'none';

-- name: BacktickGated :one
SELECT id FROM principals AS p WHERE p.`privacy_state` = 'active' AND p.`containment_state` = 'none';

-- name: QuotedBareGated :one
SELECT id FROM principals WHERE "privacy_state" = 'active' AND "containment_state" = 'none';
