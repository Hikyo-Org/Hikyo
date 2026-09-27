package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/pki"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// pkiPolicyJSON is the stored (and wire) spelling of a profile's policy. It is
// encoded from the normalized policy, so equal policies encode identically.
type pkiPolicyJSON struct {
	AllowedIssuers     []string `json:"allowed_issuers"`
	DNSPatterns        []string `json:"dns_patterns"`
	IPRanges           []string `json:"ip_ranges"`
	URIPatterns        []string `json:"uri_patterns"`
	AllowWildcardNames bool     `json:"allow_wildcard_names"`
	KeyAlgorithms      []string `json:"key_algorithms"`
	KeyUsages          []string `json:"key_usages"`
	ExtKeyUsages       []string `json:"ext_key_usages"`
	MaxTTLSeconds      int64    `json:"max_ttl_seconds"`
	DefaultTTLSeconds  int64    `json:"default_ttl_seconds"`
	RenewWindowSeconds int64    `json:"renew_window_seconds"`
	AllowCSR           bool     `json:"allow_csr"`
	AllowGeneratedKey  bool     `json:"allow_generated_key"`
	MachineIssuance    bool     `json:"machine_issuance"`
	Organization       string   `json:"organization"`
}

func strs[T ~string](in []T) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		out = append(out, string(v))
	}
	return out
}

func typed[T ~string](in []string) []T {
	out := make([]T, 0, len(in))
	for _, v := range in {
		out = append(out, T(v))
	}
	return out
}

func nonNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// encodePKIPolicy normalizes a policy and encodes it as JSON, using empty arrays
// for absent lists and whole seconds for durations. It does not validate policy.
func encodePKIPolicy(p pki.Policy) (string, error) {
	p = p.Normalize()
	body, err := json.Marshal(pkiPolicyJSON{
		AllowedIssuers: nonNil(p.AllowedIssuers), DNSPatterns: nonNil(p.DNSPatterns),
		IPRanges: nonNil(p.IPRanges), URIPatterns: nonNil(p.URIPatterns),
		AllowWildcardNames: p.AllowWildcardNames, KeyAlgorithms: strs(p.KeyAlgorithms),
		KeyUsages: strs(p.KeyUsages), ExtKeyUsages: strs(p.ExtKeyUsages),
		MaxTTLSeconds: int64(p.MaxTTL / time.Second), DefaultTTLSeconds: int64(p.DefaultTTL / time.Second),
		RenewWindowSeconds: int64(p.RenewWindow / time.Second), AllowCSR: p.AllowCSR,
		AllowGeneratedKey: p.AllowGeneratedKey, MachineIssuance: p.MachineIssuance, Organization: p.Organization,
	})
	return string(body), err
}

// decodePKIPolicy parses stored JSON, converts second counts to durations, and
// normalizes the policy without validating it. Malformed JSON returns an error.
func decodePKIPolicy(raw string) (pki.Policy, error) {
	var in pkiPolicyJSON
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return pki.Policy{}, fmt.Errorf("service: stored certificate profile policy is malformed: %w", err)
	}
	return pki.Policy{
		AllowedIssuers: in.AllowedIssuers, DNSPatterns: in.DNSPatterns, IPRanges: in.IPRanges,
		URIPatterns: in.URIPatterns, AllowWildcardNames: in.AllowWildcardNames,
		KeyAlgorithms: typed[pki.KeyAlgorithm](in.KeyAlgorithms), KeyUsages: typed[pki.KeyUsage](in.KeyUsages),
		ExtKeyUsages: typed[pki.ExtKeyUsage](in.ExtKeyUsages), MaxTTL: time.Duration(in.MaxTTLSeconds) * time.Second,
		DefaultTTL: time.Duration(in.DefaultTTLSeconds) * time.Second, RenewWindow: time.Duration(in.RenewWindowSeconds) * time.Second,
		AllowCSR: in.AllowCSR, AllowGeneratedKey: in.AllowGeneratedKey, MachineIssuance: in.MachineIssuance,
		Organization: in.Organization,
	}.Normalize(), nil
}

// PKIProfileBindingView is one explicit reach of a profile.
type PKIProfileBindingView struct {
	ID            string
	OrgID         string
	ProjectID     string
	EnvironmentID string
	CreatedAt     time.Time
}

// PKIProfileView is a profile with its policy and bindings.
type PKIProfileView struct {
	ID         string
	Name       string
	Policy     pki.Policy
	Bindings   []PKIProfileBindingView
	RowVersion int64
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func pkiProfileView(profile store.PKIProfile, bindings []store.PKIProfileBinding) (PKIProfileView, error) {
	policy, err := decodePKIPolicy(profile.Policy)
	if err != nil {
		return PKIProfileView{}, err
	}
	view := PKIProfileView{
		ID: profile.ID, Name: profile.Name, Policy: policy, RowVersion: profile.RowVersion,
		CreatedAt: profile.CreatedAt, UpdatedAt: profile.UpdatedAt, Bindings: []PKIProfileBindingView{},
	}
	for _, b := range bindings {
		view.Bindings = append(view.Bindings, PKIProfileBindingView{
			ID: b.ID, OrgID: b.OrgID, ProjectID: b.ProjectID, EnvironmentID: b.EnvironmentID, CreatedAt: b.CreatedAt,
		})
	}
	return view, nil
}

func recordPKIProfileEvent(ctx context.Context, r store.Repos, proof authz.Proof, principal domain.PrincipalID, profile store.PKIProfile, action string, binding *store.PKIProfileBinding) error {
	payload := audit.Payload{"action": action, "name": profile.Name}
	if binding != nil {
		payload["binding_org"] = binding.OrgID
		payload["binding_project"] = binding.ProjectID
		if binding.EnvironmentID != "" {
			payload["binding_environment"] = binding.EnvironmentID
		}
	}
	event, err := domainEvent(ctx, audit.EventPKIProfile, principal, audit.Object{Type: "pki-profile", ID: profile.ID}, payload)
	if err != nil {
		return err
	}
	return r.Audit().InsertInstance(ctx, proof, event)
}

// checkProfileIssuers refuses a profile naming an issuer that does not exist.
func checkProfileIssuers(ctx context.Context, r store.Repos, proof authz.Proof, policy pki.Policy) error {
	issuers, err := r.PKI().ListIssuers(ctx, proof)
	if err != nil {
		return err
	}
	for _, name := range policy.AllowedIssuers {
		if !slices.ContainsFunc(issuers, func(i store.PKIIssuer) bool { return i.Name == name }) {
			return fmt.Errorf("%w: allowed issuer %q does not exist", domain.ErrInvalid, name)
		}
	}
	return nil
}

// ListProfiles returns profiles ordered by name, including policy and bindings,
// and audits the inventory read. Authorization, storage, policy decoding, and
// audit errors are returned; results are usable only when err is nil.
func (s *PKI) ListProfiles(ctx context.Context, actor Actor) ([]PKIProfileView, error) {
	var out []PKIProfileView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		out = nil
		caller, proof, err := authorize(ctx, az, actor, authz.OpPKIProfileInspect, domain.Scope{}, s.now())
		if err != nil {
			return err
		}
		profiles, err := r.PKI().ListProfiles(ctx, proof)
		if err != nil {
			return err
		}
		for _, profile := range profiles {
			bindings, err := r.PKI().ListBindings(ctx, proof, profile.ID)
			if err != nil {
				return err
			}
			view, err := pkiProfileView(profile, bindings)
			if err != nil {
				return err
			}
			out = append(out, view)
		}
		return recordPKIInventoryRead(ctx, r, proof, caller.Principal, "profile", "list", len(out))
	})
	return out, err
}

// ShowProfile returns a named profile with its policy and bindings and audits
// the read. Missing profiles return not found; authorization, storage, policy
// decoding, and audit errors are propagated.
func (s *PKI) ShowProfile(ctx context.Context, actor Actor, name string) (PKIProfileView, error) {
	var out PKIProfileView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, err := authorize(ctx, az, actor, authz.OpPKIProfileInspect, domain.Scope{}, s.now())
		if err != nil {
			return err
		}
		profile, err := r.PKI().GetProfile(ctx, proof, name)
		if err != nil {
			return err
		}
		bindings, err := r.PKI().ListBindings(ctx, proof, profile.ID)
		if err != nil {
			return err
		}
		if out, err = pkiProfileView(profile, bindings); err != nil {
			return err
		}
		return recordPKIInventoryRead(ctx, r, proof, caller.Principal, "profile", "show", 1)
	})
	return out, err
}

// CreateProfile validates and stores a normalized policy with no bindings,
// requiring every named issuer to exist, and audits the creation. Invalid input
// wraps domain.ErrInvalid; an existing name returns ErrPKIProfileExists.
// Authorization, storage, and audit errors are propagated.
func (s *PKI) CreateProfile(ctx context.Context, actor Actor, name string, policy pki.Policy) (PKIProfileView, error) {
	if err := pki.ValidateName(name); err != nil {
		return PKIProfileView{}, fmt.Errorf("%w: profile name: %v", domain.ErrInvalid, err)
	}
	policy = policy.Normalize()
	if err := policy.Validate(); err != nil {
		return PKIProfileView{}, pkiInvalid(err)
	}
	encoded, err := encodePKIPolicy(policy)
	if err != nil {
		return PKIProfileView{}, err
	}
	var out PKIProfileView
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, err := authorize(ctx, az, actor, authz.OpPKIProfileCreate, domain.Scope{}, s.now())
		if err != nil {
			return err
		}
		if _, err := r.PKI().GetProfile(ctx, proof, name); err == nil {
			return ErrPKIProfileExists
		} else if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		if err := checkProfileIssuers(ctx, r, proof, policy); err != nil {
			return err
		}
		id, err := newID("pkip")
		if err != nil {
			return err
		}
		now := store.CanonTime(s.now())
		row := store.PKIProfile{ID: id, Name: name, Policy: encoded, CreatedBy: string(caller.Principal), CreatedAt: now}
		if err := r.PKI().CreateProfile(ctx, proof, row); err != nil {
			return err
		}
		created, err := r.PKI().GetProfile(ctx, proof, name)
		if err != nil {
			return err
		}
		if out, err = pkiProfileView(created, nil); err != nil {
			return err
		}
		return recordPKIProfileEvent(ctx, r, proof, caller.Principal, created, "create", nil)
	})
	return out, err
}

// UpdateProfile replaces a profile's policy, but only with a provable
// narrowing (ADR D5). expectedRowVersion, when non-zero, pins the version the
// operator reviewed, so a concurrent edit cannot be silently overwritten.
func (s *PKI) UpdateProfile(ctx context.Context, actor Actor, name string, policy pki.Policy, expectedRowVersion int64) (PKIProfileView, error) {
	policy = policy.Normalize()
	if err := policy.Validate(); err != nil {
		return PKIProfileView{}, pkiInvalid(err)
	}
	encoded, err := encodePKIPolicy(policy)
	if err != nil {
		return PKIProfileView{}, err
	}
	var out PKIProfileView
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, err := authorize(ctx, az, actor, authz.OpPKIProfileUpdate, domain.Scope{}, s.now())
		if err != nil {
			return err
		}
		current, err := r.PKI().GetProfile(ctx, proof, name)
		if err != nil {
			return err
		}
		if expectedRowVersion != 0 && expectedRowVersion != current.RowVersion {
			return ErrPKIProfileRace
		}
		older, err := decodePKIPolicy(current.Policy)
		if err != nil {
			return err
		}
		if err := pki.Narrows(older, policy); err != nil {
			return fmt.Errorf("%w: %v", domain.ErrConflict, err)
		}
		if err := checkProfileIssuers(ctx, r, proof, policy); err != nil {
			return err
		}
		updated, err := r.PKI().UpdateProfile(ctx, proof, current.ID, encoded, current.RowVersion, store.CanonTime(s.now()))
		if err != nil {
			return err
		}
		if !updated {
			return ErrPKIProfileRace
		}
		fresh, err := r.PKI().GetProfile(ctx, proof, name)
		if err != nil {
			return err
		}
		if out, err = pkiProfileView(fresh, nil); err != nil {
			return err
		}
		return recordPKIProfileEvent(ctx, r, proof, caller.Principal, fresh, "update", nil)
	})
	return out, err
}

// DeleteProfile removes a profile and its bindings. Certificates it issued
// keep their records (they name the profile by snapshot) but can no longer be
// renewed through it: removal is a narrowing.
func (s *PKI) DeleteProfile(ctx context.Context, actor Actor, name string) error {
	return tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, err := authorize(ctx, az, actor, authz.OpPKIProfileDelete, domain.Scope{}, s.now())
		if err != nil {
			return err
		}
		profile, err := r.PKI().GetProfile(ctx, proof, name)
		if err != nil {
			return err
		}
		deleted, err := r.PKI().DeleteProfile(ctx, proof, profile.ID)
		if err != nil {
			return err
		}
		if !deleted {
			return ErrPKIProfileRace
		}
		return recordPKIProfileEvent(ctx, r, proof, caller.Principal, profile, "delete", nil)
	})
}

// BindProfile reaches a profile into one project, or one environment of it.
func (s *PKI) BindProfile(ctx context.Context, actor Actor, name, orgID, projectID, environmentID string) (PKIProfileView, error) {
	if orgID == "" || projectID == "" {
		return PKIProfileView{}, fmt.Errorf("%w: a binding names an organization and a project", domain.ErrInvalid)
	}
	var out PKIProfileView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, err := authorize(ctx, az, actor, authz.OpPKIProfileBind, domain.Scope{}, s.now())
		if err != nil {
			return err
		}
		profile, err := r.PKI().GetProfile(ctx, proof, name)
		if err != nil {
			return err
		}
		bindings, err := r.PKI().ListBindings(ctx, proof, profile.ID)
		if err != nil {
			return err
		}
		for _, b := range bindings {
			if b.OrgID == orgID && b.ProjectID == projectID && b.EnvironmentID == environmentID {
				return ErrPKIBindingExists
			}
		}
		id, err := newID("pkib")
		if err != nil {
			return err
		}
		binding := store.PKIProfileBinding{
			ID: id, ProfileID: profile.ID, OrgID: orgID, ProjectID: projectID, EnvironmentID: environmentID,
			CreatedBy: string(caller.Principal), CreatedAt: store.CanonTime(s.now()),
		}
		if err := r.PKI().CreateBinding(ctx, proof, binding); err != nil {
			if errors.Is(err, domain.ErrConflict) {
				// The duplicate case was answered above, so a remaining
				// constraint failure is a foreign key: the target is absent.
				return ErrPKIBindingTarget
			}
			return err
		}
		if bindings, err = r.PKI().ListBindings(ctx, proof, profile.ID); err != nil {
			return err
		}
		if out, err = pkiProfileView(profile, bindings); err != nil {
			return err
		}
		return recordPKIProfileEvent(ctx, r, proof, caller.Principal, profile, "bind", &binding)
	})
	return out, err
}

// UnbindProfile removes one binding from the named profile and audits the
// change, returning the remaining bindings. A missing profile or binding
// returns not found; a lost deletion returns ErrPKIProfileRace. Authorization,
// storage, policy decoding, and audit errors are propagated.
func (s *PKI) UnbindProfile(ctx context.Context, actor Actor, name, bindingID string) (PKIProfileView, error) {
	var out PKIProfileView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, err := authorize(ctx, az, actor, authz.OpPKIProfileUnbind, domain.Scope{}, s.now())
		if err != nil {
			return err
		}
		profile, err := r.PKI().GetProfile(ctx, proof, name)
		if err != nil {
			return err
		}
		bindings, err := r.PKI().ListBindings(ctx, proof, profile.ID)
		if err != nil {
			return err
		}
		index := slices.IndexFunc(bindings, func(b store.PKIProfileBinding) bool { return b.ID == bindingID })
		if index < 0 {
			return fmt.Errorf("%w: binding not found", domain.ErrNotFound)
		}
		removed := bindings[index]
		deleted, err := r.PKI().DeleteBinding(ctx, proof, profile.ID, bindingID)
		if err != nil {
			return err
		}
		if !deleted {
			return ErrPKIProfileRace
		}
		if out, err = pkiProfileView(profile, slices.Delete(bindings, index, index+1)); err != nil {
			return err
		}
		return recordPKIProfileEvent(ctx, r, proof, caller.Principal, profile, "unbind", &removed)
	})
	return out, err
}
