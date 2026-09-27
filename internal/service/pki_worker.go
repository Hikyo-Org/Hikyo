package service

import (
	"context"
	"crypto/x509"
	"errors"

	"github.com/Hikyo-Org/hikyo/internal/pki"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// pkiSweepBatch bounds one sweep's rows per phase, so a backlog drains over
// several ticks instead of one long transaction.
const pkiSweepBatch = 200

// RunPKISweep is one tick of the private-PKI worker (ADR D6-D8), run on every
// node: stale `issuing` rows become `unknown`, expired leaves become
// `expired`, and every issuer version whose CRL is due gets a freshly signed
// one. Every write is a CAS, so concurrent nodes never double-apply. It
// reports whether it did any work, so the caller can loop without sleeping
// while a backlog drains. Sweep errors stop the tick; CRL publication errors
// do not prevent attempts for other candidates, and the first is returned
// alongside whether any work succeeded.
func (s *PKI) RunPKISweep(ctx context.Context) (bool, error) {
	if s.Runtime == nil {
		return false, errors.New("service: PKI worker has no runtime")
	}
	now := store.CanonTime(s.now())
	unknown, err := s.Runtime.SweepStaleIssuing(ctx, now, pkiSweepBatch)
	if err != nil {
		return false, err
	}
	expired, err := s.Runtime.ExpireDue(ctx, now, pkiSweepBatch)
	if err != nil {
		return unknown > 0, err
	}
	due, err := s.Runtime.DueCRLs(ctx, now)
	if err != nil {
		return unknown+expired > 0, err
	}
	published := 0
	var firstErr error
	for _, candidate := range due {
		ok, err := s.publishDueCRL(ctx, candidate)
		if err != nil {
			// One broken issuer (for example a key sealed under a retired DEK)
			// must not starve the others; report the first failure.
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if ok {
			published++
		}
	}
	return unknown+expired+published > 0, firstErr
}

// publishDueCRL signs and stores a candidate's CRL with its current revocations.
// It returns false without error if publication loses the compare-and-swap;
// read, key, certificate, signing, and publication errors reach the caller.
func (s *PKI) publishDueCRL(ctx context.Context, candidate store.PKICRLCandidate) (bool, error) {
	now := store.CanonTime(s.now())
	entries, err := s.Runtime.RevokedEntries(ctx, candidate.IssuerID, now)
	if err != nil {
		return false, err
	}
	cert, err := x509.ParseCertificate(candidate.CertificateDER)
	if err != nil {
		return false, err
	}
	signer, err := s.openIssuerKey(candidate.IssuerID, candidate.EncryptedPrivateKey)
	if err != nil {
		return false, err
	}
	number := pki.NextCRLNumber(candidate.CRLNumber, now)
	der, err := signCRL(pki.Parent{Certificate: cert, Signer: signer}, entries, number, now)
	if err != nil {
		return false, err
	}
	return s.Runtime.PublishCRL(ctx, candidate, der, number, len(entries), now, now.Add(pki.CRLValidity))
}
