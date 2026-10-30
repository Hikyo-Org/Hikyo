package main

import (
	"errors"
	"net/netip"
	"sort"
	"strconv"
	"time"
)

// Only these fields can leave the detector. Payload, user-agent, raw source IP,
// names, URLs, arbitrary causes and transport errors are never copied out.
type event struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	Version    int       `json:"schema_version"`
	RecordedAt time.Time `json:"recorded_at"`
	Actor      string    `json:"actor_id,omitempty"`
	Credential string    `json:"actor_credential_id,omitempty"`
	Object     string    `json:"object_id,omitempty"`
	Org        string    `json:"org_id,omitempty"`
	Project    string    `json:"project_id,omitempty"`
	Env        string    `json:"env_id,omitempty"`
	Outcome    string    `json:"outcome"`
	Origin     string    `json:"origin"`
	SourceIP   string    `json:"source_ip,omitempty"`
}

type reference struct {
	ID         string `json:"event_id"`
	Actor      string `json:"actor_id,omitempty"`
	Credential string `json:"credential_id,omitempty"`
	Object     string `json:"object_id,omitempty"`
	Org        string `json:"org_id,omitempty"`
	Project    string `json:"project_id,omitempty"`
	Env        string `json:"env_id,omitempty"`
}

type observation struct {
	Reference reference `json:"reference"`
	Source    string    `json:"source"`
	At        time.Time `json:"at"`
	Rule      string    `json:"rule"`
	Group     string    `json:"group"`
}

type alert struct {
	ID        string      `json:"id"`
	Rule      string      `json:"rule"`
	Source    string      `json:"source"`
	Instance  string      `json:"instance"`
	Org       string      `json:"org_id,omitempty"`
	Project   string      `json:"project_id,omitempty"`
	Group     string      `json:"group"`
	Count     int         `json:"count"`
	Events    []reference `json:"events"`
	CreatedAt time.Time   `json:"created_at"`
}

type delivery struct {
	Alert    alert     `json:"alert"`
	Status   string    `json:"status"`
	Attempts int       `json:"attempts"`
	Next     time.Time `json:"next_attempt"`
	Receipt  string    `json:"receipt_id,omitempty"`
}

type state struct {
	Version      int                  `json:"version"`
	Config       string               `json:"config"`
	Seen         map[string]time.Time `json:"seen"`
	Observations []observation        `json:"observations"`
	Outbox       map[string]*delivery `json:"outbox"`
}

func newState(c config) state {
	return state{Version: 1, Config: configID(c), Seen: map[string]time.Time{}, Outbox: map[string]*delivery{}}
}

func (st state) valid(c config) bool {
	if st.Version != 1 || st.Config != configID(c) || st.Seen == nil || st.Outbox == nil || len(st.Seen) > maxEvents || len(st.Observations) > maxEvents || len(st.Outbox) > maxAlerts {
		return false
	}
	for id, d := range st.Outbox {
		if d == nil || d.Alert.ID != id || len(id) != 64 || d.Attempts < 0 || d.Attempts > 3 || len(d.Alert.Events) > 20 || d.Alert.CreatedAt.IsZero() {
			return false
		}
		switch d.Status {
		case "pending", "uncertain", "exhausted-uncertain", "terminal":
		case "received":
			if !labelPattern.MatchString(d.Receipt) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func configID(c config) string {
	// Binding includes thresholds (the code version) and exact collection scope.
	return identity("alert-route-v1", mustJSON(c))
}

func (e event) valid(s source, now time.Time) bool {
	if !uuidPattern.MatchString(e.ID) || e.Version != 1 || e.RecordedAt.IsZero() || e.RecordedAt.After(now.Add(30*time.Second)) {
		return false
	}
	for _, id := range []string{e.Actor, e.Credential, e.Object, e.Org, e.Project, e.Env} {
		if id != "" && !uuidPattern.MatchString(id) {
			return false
		}
	}
	if s.Trail == "instance" {
		return e.Org == "" && e.Project == "" && e.Env == ""
	}
	return e.Org == s.Org && (s.Project == "" || e.Project == s.Project)
}

func classify(e event, c config) (string, string) {
	if e.Type == "auth.login" && e.Outcome == "failure" {
		ip, err := netip.ParseAddr(e.SourceIP)
		if err == nil {
			return "failed-login-burst", identity(ip.Unmap().String())
		}
		return "unattributed-login", "missing-source"
	}
	if e.Outcome != "success" {
		return "", ""
	}
	switch e.Type {
	case "disclosure.value_revealed", "disclosure.values_exported", "identity.disclosure":
		if e.Origin == "offline-reconciled" {
			return "", ""
		}
		for _, id := range c.WatchedCredentials {
			if e.Credential == id {
				return "watched-disclosure", id
			}
		}
		if e.Actor == "" || e.Credential == "" {
			return "disclosure-attribution-gap", "missing-identity"
		}
		return "disclosure-burst", identity(e.Actor, e.Credential, e.Org, e.Project, e.Env)
	case "grant.created", "grant.modified", "grant.revoked", "grant.template_applied",
		"auth.provider_changed", "auth.saml_provider_configure", "auth.saml_provider_remove",
		"identity.credential_minted", "identity.credential_revoked", "identity.developer_credential_minted",
		"identity.developer_credential_revoked", "identity.grant_widened", "auth.password_changed",
		"adapter.credential_replace", "adapter.credential_revoke", "dynamic.provider_configured",
		"dynamic.provider_credential_replace", "dynamic.provider_credential_revoke":
		return "sensitive-change", e.ID
	}
	return "", ""
}

func (st *state) add(c config, s source, now time.Time, rule, group string, refs []reference, count int) error {
	id := identity(configID(c), s.ID, rule, group)
	if _, ok := st.Outbox[id]; ok {
		return nil
	}
	if len(st.Outbox) >= maxAlerts {
		return errors.New("outbox full; independent escalation required")
	}
	if len(refs) > 20 {
		refs = refs[:20]
	}
	st.Outbox[id] = &delivery{Alert: alert{ID: id, Rule: rule, Source: s.ID, Instance: s.Instance, Org: s.Org, Project: s.Project, Group: group, Count: count, Events: refs, CreatedAt: now}, Status: "pending", Next: now}
	return nil
}

func (st *state) evaluate(c config, now time.Time) ([]string, error) {
	for id, at := range st.Seen {
		if now.Sub(at) > retention {
			delete(st.Seen, id)
		}
	}
	for id, d := range st.Outbox {
		if d.Status == "received" && now.Sub(d.Alert.CreatedAt) > retention {
			delete(st.Outbox, id)
		}
	}
	kept := st.Observations[:0]
	for _, o := range st.Observations {
		if now.Sub(o.At) <= 2*window+maxLag {
			kept = append(kept, o)
		}
	}
	st.Observations = kept
	unhealthy := []string{}
	for _, s := range c.Sources {
		events, err := loadSource(s, now)
		if err != nil {
			unhealthy = append(unhealthy, s.ID)
			if err := st.add(c, s, now, "collector-unavailable", strconv.FormatInt(now.Unix()/300, 10), nil, 1); err != nil {
				return nil, err
			}
			continue
		}
		for _, e := range events {
			// Historical re-exports are normal. They cannot trigger current windows.
			if now.Sub(e.RecordedAt) > 2*window+maxLag {
				continue
			}
			key := identity(s.ID, e.ID)
			if _, ok := st.Seen[key]; ok {
				continue
			}
			if len(st.Seen) >= maxEvents {
				return nil, errors.New("event dedup capacity reached")
			}
			st.Seen[key] = now
			rule, group := classify(e, c)
			if rule == "" {
				continue
			}
			ref := reference{ID: e.ID, Actor: e.Actor, Credential: e.Credential, Object: e.Object, Org: e.Org, Project: e.Project, Env: e.Env}
			st.Observations = append(st.Observations, observation{Reference: ref, Source: s.ID, At: e.RecordedAt, Rule: rule, Group: group})
		}
		// Fixed UTC 5-minute windows within a bounded 25-minute lookback.
		// Out-of-order records are grouped by recorded_at, not receipt time.
		groups := map[string][]observation{}
		for _, o := range st.Observations {
			if o.Source != s.ID {
				continue
			}
			key := identity(o.Rule, o.Group, strconv.FormatInt(o.At.Unix()/300, 10))
			groups[key] = append(groups[key], o)
		}
		keys := make([]string, 0, len(groups))
		for key := range groups {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			g := groups[key]
			threshold := 1
			if g[0].Rule == "failed-login-burst" {
				threshold = 5
			}
			if g[0].Rule == "disclosure-burst" {
				threshold = 20
			}
			if len(g) < threshold {
				continue
			}
			sort.Slice(g, func(i, j int) bool { return g[i].Reference.ID < g[j].Reference.ID })
			refs := make([]reference, 0, len(g))
			for _, o := range g {
				refs = append(refs, o.Reference)
			}
			if err := st.add(c, s, now, g[0].Rule, key, refs, len(g)); err != nil {
				return nil, err
			}
		}
	}
	return unhealthy, nil
}
