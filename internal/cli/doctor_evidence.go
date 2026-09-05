package cli

import (
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
)

// doctorEvidence is a collection record, not an attestation. It deliberately
// excludes session artifacts and server configuration, which may contain secrets.
type doctorEvidence struct {
	SchemaVersion int          `json:"schema_version"`
	CollectedAt   time.Time    `json:"collected_at"`
	ServerOrigin  string       `json:"server_origin"`
	ServerVersion string       `json:"server_version"`
	APIRevision   int          `json:"api_revision"`
	ClientVersion string       `json:"client_version"`
	Measurements  doctorResult `json:"measurements"`
	NotAssessed   []string     `json:"not_assessed"`
}

func newDoctorEvidence(result doctorResult, meta apigen.Meta, clientVersion, origin string, at time.Time) doctorEvidence {
	return doctorEvidence{
		SchemaVersion: 1, CollectedAt: at.UTC(), ServerOrigin: origin,
		ServerVersion: meta.ServerVersion, APIRevision: meta.ApiRevision,
		ClientVersion: clientVersion, Measurements: result,
		NotAssessed: []string{
			"legal bases, notices, rights decisions and external-copy deletion",
			"staff and supplier controls, access reviews and organization-wide MFA enforcement",
			"host and backup storage encryption, physical security and independent audit custody",
			"production network exposure and externally terminated TLS policy",
			"control effectiveness over an audit period",
		},
	}
}
