package service

import "github.com/Hikyo-Org/hikyo/internal/authz"

// stubProof stands in for the proof of tests whose stub repositories never
// read it. Tests of real authorization use proofs minted in a transaction.
var stubProof authz.Proof
