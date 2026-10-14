package main

import (
	"encoding/json"
	"errors"
)

func requireRevisionMetadata(r response) error {
	var revision struct {
		Revision       int64  `json:"revision"`
		SchemaRevision int64  `json:"schema_revision"`
		ChangeToken    string `json:"change_token"`
		Keys           []struct {
			Name           string `json:"name"`
			Classification string `json:"classification"`
		} `json:"keys"`
	}
	if r.status != 200 || json.Unmarshal(r.body, &revision) != nil || revision.Revision < 1 || revision.SchemaRevision < 1 || revision.ChangeToken == "" {
		return errors.New("authorized revision omitted seeded snapshot identity")
	}
	want := map[string]string{"ALLOWED_CONFIG": "config", "SECOND_CONFIG": "config", "PROTECTED_SECRET": "secret"}
	if len(revision.Keys) != len(want) {
		return errors.New("authorized revision omitted seeded key metadata")
	}
	for _, key := range revision.Keys {
		if want[key.Name] != key.Classification {
			return errors.New("authorized revision returned unexpected key metadata")
		}
		delete(want, key.Name)
	}
	if len(want) != 0 {
		return errors.New("authorized revision repeated or omitted key metadata")
	}
	return nil
}

func requireCurrentDelivery(t *target, path, token string, canaries []string) error {
	r, err := t.request("GET", path, token, nil)
	if err != nil {
		return err
	}
	var d struct {
		Current bool              `json:"current"`
		Cursor  string            `json:"cursor"`
		Keys    []json.RawMessage `json:"keys"`
	}
	if r.status != 200 || json.Unmarshal(r.body, &d) != nil || !d.Current || d.Cursor == "" || len(d.Keys) != 0 {
		return errors.New("authorized conditional cursor did not produce an empty current response")
	}
	return noResponseCanary(r, canaries)
}

func requireDenialAudit(r response, principal, org, project string) error {
	var page struct {
		Items []struct {
			Type    string `json:"type"`
			Actor   string `json:"actor_id"`
			Org     string `json:"org_id"`
			Project string `json:"project_id"`
			Origin  string `json:"origin"`
			Outcome string `json:"outcome"`
			Payload struct {
				Operation string `json:"operation"`
			} `json:"payload"`
		} `json:"items"`
	}
	if r.status != 200 || json.Unmarshal(r.body, &page) != nil {
		return errors.New("audit page unavailable or malformed")
	}
	for _, event := range page.Items {
		if event.Type == "grant.denied" && event.Actor == principal && event.Org == org && event.Project == project && event.Origin == "mcp" && event.Outcome == "denied" && event.Payload.Operation == "key.list" {
			return nil
		}
	}
	return errors.New("matching durable MCP key-list denial audit record absent")
}
