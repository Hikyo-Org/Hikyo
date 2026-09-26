package repscan

import (
	"encoding/json"
	"io"
	"net/url"
)

// SARIF 2.1.0, hand-rolled: the subset a code-scanning consumer needs and
// nothing that could carry match text. There is no snippet, no contextRegion,
// no column and no message argument derived from content; the message names
// the rule only.

const (
	sarifSchema  = "https://json.schemastore.org/sarif-2.1.0.json"
	sarifVersion = "2.1.0"
	// SARIFFingerprintKey is the partialFingerprints key the fingerprint rides.
	SARIFFingerprintKey = "hikyo/v1"
	sarifToolName       = "hikyo-scan"
	sarifInformationURI = "https://github.com/Hikyo-Org/Hikyo"
)

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool       sarifTool      `json:"tool"`
	Results    []sarifResult  `json:"results"`
	Properties map[string]any `json:"properties"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	Version        string      `json:"version"`
	InformationURI string      `json:"informationUri"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID               string            `json:"id"`
	ShortDescription sarifMessage      `json:"shortDescription"`
	Properties       map[string]string `json:"properties"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifResult struct {
	RuleID              string             `json:"ruleId"`
	RuleIndex           int                `json:"ruleIndex"`
	Level               string             `json:"level"`
	Message             sarifMessage       `json:"message"`
	Locations           []sarifLocation    `json:"locations"`
	PartialFingerprints map[string]string  `json:"partialFingerprints"`
	Suppressions        []sarifSuppression `json:"suppressions,omitempty"`
	Properties          map[string]string  `json:"properties,omitempty"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysical `json:"physicalLocation"`
}

type sarifPhysical struct {
	ArtifactLocation sarifArtifactLoc `json:"artifactLocation"`
	Region           *sarifRegion     `json:"region,omitempty"`
}

type sarifArtifactLoc struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine int `json:"startLine"`
}

type sarifSuppression struct {
	Kind          string `json:"kind"`
	Justification string `json:"justification,omitempty"`
}

// sarifURI percent-encodes a slash-separated relative path as a URI
// reference, so an untrusted file name cannot inject URI syntax.
func sarifURI(p string) string {
	return (&url.URL{Path: p}).EscapedPath()
}

// WriteSARIF renders the report as a SARIF 2.1.0 log. Suppressed findings
// are included with their suppression, so a consumer that honours SARIF
// suppressions shows the same verdict the exit code gives.
// Paths are relative to the scan base (the repository top level), which is
// how code-scanning consumers resolve an artifact location without a base id.
func WriteSARIF(w io.Writer, r *Report, toolVersion string) error {
	if toolVersion == "" {
		toolVersion = "dev"
	}
	driver := sarifDriver{Name: sarifToolName, Version: toolVersion, InformationURI: sarifInformationURI, Rules: []sarifRule{}}
	index := map[string]int{}
	for i, rule := range r.Ruleset.Rules {
		index[rule.ID] = i
		driver.Rules = append(driver.Rules, sarifRule{
			ID:               rule.ID,
			ShortDescription: sarifMessage{Text: "Credential-shaped string matching the " + rule.ID + " rule"},
			Properties:       map[string]string{"semanticDigest": rule.SemanticDigest, "snapshot": r.Ruleset.Snapshot},
		})
	}
	results := make([]sarifResult, 0, len(r.Findings))
	for _, f := range r.Findings {
		loc := sarifPhysical{ArtifactLocation: sarifArtifactLoc{URI: sarifURI(f.Path)}}
		if f.Line > 0 {
			loc.Region = &sarifRegion{StartLine: f.Line}
		}
		res := sarifResult{
			RuleID:              f.RuleID,
			RuleIndex:           index[f.RuleID],
			Level:               "error",
			Message:             sarifMessage{Text: "A credential-shaped string matched rule " + f.RuleID + ". The match is redacted."},
			Locations:           []sarifLocation{{PhysicalLocation: loc}},
			PartialFingerprints: map[string]string{SARIFFingerprintKey: f.Fingerprint},
		}
		if f.Commit != "" {
			res.Properties = map[string]string{"commit": f.Commit}
		}
		switch f.Suppressed {
		case SuppressedInline:
			res.Suppressions = []sarifSuppression{{Kind: "inSource", Justification: "hikyo-scan:ignore " + f.RuleID}}
		case SuppressedFingerprint:
			res.Suppressions = []sarifSuppression{{Kind: "external", Justification: r.reasons[f.Fingerprint]}}
		}
		results = append(results, res)
	}
	log := sarifLog{
		Schema:  sarifSchema,
		Version: sarifVersion,
		Runs: []sarifRun{{
			Tool:    sarifTool{Driver: driver},
			Results: results,
			Properties: map[string]any{
				"schema":        r.Schema,
				"mode":          r.Mode,
				"snapshot":      r.Ruleset.Snapshot,
				"excludedRules": r.Ruleset.ExcludedRules,
				"summary":       r.Summary,
			},
		}},
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(log)
}
