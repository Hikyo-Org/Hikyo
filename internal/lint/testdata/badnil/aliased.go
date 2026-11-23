package badnil

import "github.com/Hikyo-Org/hikyo/internal/authz"

type proofAlias = authz.Proof

var unsetAlias proofAlias // want "nil in an authz\\.Proof position"

var parenNil = authz.Proof((nil)) // want "nil in an authz\\.Proof position"
