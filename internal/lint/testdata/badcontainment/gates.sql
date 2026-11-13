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
