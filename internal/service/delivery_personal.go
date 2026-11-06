package service

import (
	"context"
	"fmt"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/delivery"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Personal overlay dispositions on DeliveredKey.Personal.
const (
	PersonalDelivered = "value"
	PersonalWithheld  = "withheld"
	PersonalNotice    = "notice"
)

// Withheld reasons on DeliveredKey.PersonalReason.
const (
	PersonalReasonConsent    = "consent"
	PersonalReasonConfigOnly = "config_only"
	PersonalReasonSuspended  = "suspended"
)

// personalOverlay applies the developer credential's human's own values and
// notices on top of the committed snapshot's rows (personal-values ADR,
// Delivery). It is the one declared exception to delivery reading only
// snapshots. The owner is the proof's principal, which for a developer
// credential is its recorded human.
//
// Every value and notice also enters the manifest, keyed by a fresh row id or
// the notice's cause and time, so the change token (and with it the cursor)
// moves on any personal change, including deleting the last value.
//
// A key where the owner holds a value or notice is never served the shared
// value: either the personal value crosses, or the key arrives with no value
// and a reason, and `hikyo run` refuses it.
func personalOverlay(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, p authz.Proof,
	sealer *crypto.ProjectSealer, scope domain.Scope, caller authz.Identity, mode delivery.Mode,
	only map[string]bool, rows []DeliveredKey, manifest []delivery.Row) ([]DeliveredKey, []delivery.Row, error) {
	values, err := r.PersonalValues().ListOwn(ctx, p)
	if err != nil {
		return nil, nil, err
	}
	notices, err := r.PersonalValues().NoticesOwn(ctx, p)
	if err != nil {
		return nil, nil, err
	}
	if len(values) == 0 && len(notices) == 0 {
		return rows, manifest, nil
	}
	keys, err := r.Catalogue().List(ctx, p)
	if err != nil {
		return nil, nil, err
	}
	byID := map[string]store.CatalogueKey{}
	byName := map[string]store.CatalogueKey{}
	for _, k := range keys {
		byID[k.ID] = k
		byName[k.Name] = k
	}
	qualified, err := personalQualified(ctx, az, caller.AuthorityPrincipal)
	if err != nil {
		return nil, nil, err
	}
	reason := ""
	switch {
	case mode == delivery.ModeConfigOnly:
		reason = PersonalReasonConfigOnly
	case !caller.PersonalConsent:
		reason = PersonalReasonConsent
	case !qualified:
		reason = PersonalReasonSuspended
	}
	index := map[string]int{}
	for i, row := range rows {
		index[row.Name] = i
	}
	put := func(k DeliveredKey) {
		if i, ok := index[k.Name]; ok {
			rows[i] = k
			return
		}
		index[k.Name] = len(rows)
		rows = append(rows, k)
	}
	held := map[string]bool{}
	for _, v := range values {
		key, ok := byID[v.KeyID]
		if !ok {
			return nil, nil, fmt.Errorf("service: personal value %s names no declared key", v.ID)
		}
		// A file target's selection (#164) bounds the overlay as it bounds the
		// shared rows. A personal-only key has no shared row and still crosses.
		if only != nil && !only[key.ID] {
			continue
		}
		held[key.Name] = true
		// The reason is part of the occurrence, so losing or regaining the
		// predicate is never answered as current.
		manifest = append(manifest, delivery.Row{Key: "personal\x00" + key.Name, Classification: string(schema.Secret), Occurrence: v.ID + "/" + reason})
		out := DeliveredKey{KeyID: key.ID, Name: key.Name, Classification: string(schema.Secret), Presence: delivery.PresenceSet}
		if reason != "" {
			out.Personal, out.PersonalReason = PersonalWithheld, reason
			put(out)
			continue
		}
		plain, err := sealer.OpenPersonalValue(personalAAD(scope, key.ID, caller.AuthorityPrincipal), v.Ciphertext)
		if err != nil {
			return nil, nil, fmt.Errorf("service: personal value %s: %w", v.ID, err)
		}
		value := string(plain)
		out.Value, out.Personal = &value, PersonalDelivered
		put(out)
	}
	for _, n := range notices {
		// An undeclared name delivers no shared value to fall back to; the
		// notice stays stored for a key re-declared under that name.
		key, declared := byName[n.KeyName]
		if held[n.KeyName] || !declared || only != nil && !only[key.ID] {
			continue
		}
		manifest = append(manifest, delivery.Row{Key: "personal-notice\x00" + n.KeyName, Occurrence: n.Cause + "@" + n.CreatedAt.String()})
		put(DeliveredKey{KeyID: key.ID, Name: key.Name, Classification: string(schema.Secret), Presence: delivery.PresenceSet,
			Personal: PersonalNotice, PersonalReason: n.Cause})
	}
	rendered := 0
	for _, row := range rows {
		if row.Value != nil {
			rendered += len(*row.Value)
		}
	}
	if rendered > MaxRenderBytesPerTarget {
		return nil, nil, invalidDetail("resolved environment exceeds the per-target render limit")
	}
	return rows, manifest, nil
}

// auditPersonalDelivered records one personal value crossing, under the fetch
// envelope, before the response leaves the server.
func auditPersonalDelivered(ctx context.Context, r store.Repos, p authz.Proof, caller authz.Identity,
	scope domain.Scope, correlation string, k DeliveredKey) error {
	ev, err := newAuditEvent(ctx, audit.EventPersonalValueDelivered, caller.Principal,
		audit.Object{Type: "environment", ID: string(scope.Env)}, audit.OutcomeSuccess, correlation, audit.Payload{
			"key":                k.Name,
			"owner_principal_id": string(caller.AuthorityPrincipal),
			"credential_id":      caller.CredentialID,
			"scope":              renderScope(scope),
		})
	if err != nil {
		return err
	}
	ev.AuthorityID = string(caller.AuthorityPrincipal)
	ev.Actor.CredentialID = caller.CredentialID
	return r.Audit().InsertTenant(ctx, p, ev)
}
