package domain

import "time"

// Scheduled credential rotation bounds (#148, ops-catalogue "Scheduled
// credential rotation"). They are validated when a rotation is created; a hit
// is a named `invalid` refusal. The interval is the cadence of new credentials;
// the revocation delay is how long the superseded credential stays valid after
// the new one is published, so consumers can pick it up.
const (
	MinRotationInterval = time.Hour
	MaxRotationInterval = 90 * 24 * time.Hour
	MinRevocationDelay  = 5 * time.Minute
	MaxRevocationDelay  = 7 * 24 * time.Hour
)
