package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"time"
)

var mandatoryCases = []string{"native-bootstrap", "authorized-controls", "cross-project-denial", "cross-tenant-denial", "denied-mutation-unchanged", "grant-removal", "grant-restoration", "credential-revocation", "cursor-binding", "projection-boundary", "revision-boundary", "restart-continuity", "audit-boundary", "generated-sequence"}

type attackCase struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	Expected   string `json:"expected"`
	Observed   string `json:"observed"`
	Assertions int    `json:"assertions"`
	Error      string `json:"error,omitempty"`
}
type attackBackend struct {
	Engine        string       `json:"engine"`
	Cases         []attackCase `json:"cases"`
	Sequence      []string     `json:"sequence"`
	FailurePrefix []string     `json:"failure_prefix,omitempty"`
	Cleanup       string       `json:"cleanup"`
}
type attackResult struct {
	SchemaVersion   int               `json:"schema_version"`
	Profile         string            `json:"profile"`
	HarnessVersion  int               `json:"harness_version"`
	ArtifactSHA256  string            `json:"artifact_sha256"`
	BinaryVersion   string            `json:"binary_version"`
	Seed            int64             `json:"seed"`
	Steps           int               `json:"steps"`
	StartedAt       time.Time         `json:"started_at"`
	CompletedAt     time.Time         `json:"completed_at"`
	Status          string            `json:"status"`
	ExpectedCases   []string          `json:"expected_cases"`
	Backends        []attackBackend   `json:"backends"`
	Limitations     []string          `json:"limitations"`
	Configuration   map[string]string `json:"configuration"`
	AssertionMetric string            `json:"assertion_metric"`
	Error           string            `json:"error,omitempty"`
}

func validateAttacks(r attackResult, digest string) error {
	if r.SchemaVersion != 1 || r.Profile != "hikyo-artifact-adversarial-v1" || r.HarnessVersion != 1 || r.Status != "passed" || r.Error != "" || r.ArtifactSHA256 != digest || !digestPattern.MatchString(digest) || r.BinaryVersion == "" {
		return errors.New("incomplete or mismatched artifact campaign")
	}
	if r.AssertionMetric != "successful oracle groups, not individual predicates, HTTP requests, distinct attacks or coverage percentage" || !maps.Equal(r.Configuration, requiredArtifactConfiguration()) {
		return errors.New("artifact configuration or assertion metric outside the reviewed profile")
	}
	if r.Steps < 8 || r.Steps > 24 || r.StartedAt.IsZero() || !r.CompletedAt.After(r.StartedAt) || r.CompletedAt.After(time.Now().Add(time.Minute)) {
		return errors.New("invalid campaign bounds or timestamps")
	}
	if !sameSet(r.ExpectedCases, mandatoryCases) || len(r.Backends) != 2 {
		return errors.New("campaign omitted mandatory case/backend inventory")
	}
	engines := map[string]bool{}
	for _, backend := range r.Backends {
		if engines[backend.Engine] || (backend.Engine != "sqlite" && backend.Engine != "postgres") || backend.Cleanup != "complete" || len(backend.FailurePrefix) != 0 || len(backend.Sequence) != r.Steps {
			return errors.New("campaign backend incomplete or cleanup failed")
		}
		engines[backend.Engine] = true
		ids := []string{}
		for _, c := range backend.Cases {
			if c.Status != "passed" || c.Assertions < 1 || c.Expected == "" || c.Observed == "" || c.Error != "" {
				return fmt.Errorf("campaign case %s did not execute and pass", c.ID)
			}
			if c.ID == "generated-sequence" && c.Assertions != r.Steps*3 {
				return errors.New("generated sequence assertion count mismatch")
			}
			ids = append(ids, c.ID)
		}
		if !sameSet(ids, mandatoryCases) {
			return errors.New("campaign backend omitted or duplicated mandatory cases")
		}
	}
	return nil
}

func requiredArtifactConfiguration() map[string]string {
	return map[string]string{
		"trust_domain":          "production",
		"transport":             "private HTTPS with pinned synthetic certificate",
		"second_factor":         "required; native enrolled WebAuthn software authenticator",
		"disclosure_window":     "production default zero; each widening has native signed mint ceremony",
		"credential_issuance":   "native local administrator bootstrap and public API issuance",
		"operator_custody":      "fresh independent ECDSA public key and temporary upgrade state",
		"authentication_pacing": "at most eight authentication POSTs and fifty MCP calls per rolling minute; server policy unchanged",
	}
}

func sameSet(actual, expected []string) bool {
	a, b := slices.Clone(actual), slices.Clone(expected)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

type scanEvidence struct {
	Tool               string        `json:"tool"`
	Version            string        `json:"version"`
	Mode               string        `json:"mode"`
	ScanTime           time.Time     `json:"scan_completed_at"`
	Database           string        `json:"database"`
	DatabaseModifiedAt time.Time     `json:"database_modified_at"`
	GoVersion          string        `json:"go_version"`
	Findings           int           `json:"symbol_findings"`
	ModuleFindings     int           `json:"module_advisory_records"`
	PackageFindings    int           `json:"package_advisory_records"`
	Advisories         []scanFinding `json:"nonblocking_advisories"`
	Disposition        string        `json:"finding_policy"`
	RawSHA256          string        `json:"raw_scan_sha256"`
	Scope              string        `json:"scope"`
}

type scanFrame struct {
	Module   string          `json:"module"`
	Version  string          `json:"version,omitempty"`
	Package  string          `json:"package,omitempty"`
	Function string          `json:"function,omitempty"`
	Receiver string          `json:"receiver,omitempty"`
	Position json.RawMessage `json:"position,omitempty"`
}
type scanFinding struct {
	OSV          string      `json:"osv"`
	FixedVersion string      `json:"fixed_version,omitempty"`
	Trace        []scanFrame `json:"trace"`
}

func validateScan(raw []byte) (scanEvidence, error) {
	var result scanEvidence
	d := json.NewDecoder(bytes.NewReader(raw))
	configs := 0
	messages := 0
	sboms := 0
	for {
		var msg struct {
			Config *struct {
				ProtocolVersion string    `json:"protocol_version"`
				ScannerName     string    `json:"scanner_name"`
				ScannerVersion  string    `json:"scanner_version"`
				DB              string    `json:"db"`
				DBLastModified  time.Time `json:"db_last_modified"`
				GoVersion       string    `json:"go_version"`
				ScanMode        string    `json:"scan_mode"`
				ScanLevel       string    `json:"scan_level"`
			} `json:"config"`
			Finding json.RawMessage `json:"finding"`
			SBOM    *struct {
				GoVersion string   `json:"go_version"`
				Roots     []string `json:"roots"`
			} `json:"SBOM"`
		}
		if err := d.Decode(&msg); err == io.EOF {
			break
		} else if err != nil {
			return result, err
		}
		messages++
		if len(msg.Finding) != 0 {
			var finding scanFinding
			findingDecoder := json.NewDecoder(bytes.NewReader(msg.Finding))
			findingDecoder.DisallowUnknownFields()
			if err := findingDecoder.Decode(&finding); err != nil || !regexp.MustCompile(`^GO-[0-9]{4}-[0-9]+$`).MatchString(finding.OSV) || len(finding.Trace) != 1 || finding.Trace[0].Module == "" {
				return result, errors.New("malformed or unclassifiable binary vulnerability finding")
			}
			if finding.Trace[0].Function == "" && finding.Trace[0].Receiver != "" || len(finding.Trace[0].Position) > 0 {
				return result, errors.New("unclassifiable binary vulnerability trace")
			}
			if finding.Trace[0].Function != "" {
				return result, errors.New("symbol-reachable binary vulnerability finding present")
			}
			if finding.Trace[0].Package != "" {
				result.PackageFindings++
			} else {
				result.ModuleFindings++
			}
			result.Advisories = append(result.Advisories, finding)
		}
		if msg.SBOM != nil {
			sboms++
			if configs != 1 || msg.SBOM.GoVersion == "" || len(msg.SBOM.Roots) == 0 {
				return result, errors.New("binary scan has no executable SBOM")
			}
			result.GoVersion = msg.SBOM.GoVersion
		}
		if msg.Config != nil {
			configs++
			c := msg.Config
			if messages != 1 || c.ProtocolVersion != "v1.0.0" || c.ScannerName != "govulncheck" || c.ScannerVersion != "v1.7.0" || c.ScanMode != "binary" || c.ScanLevel != "symbol" || c.DB == "" || c.DBLastModified.IsZero() {
				return result, errors.New("binary scanner configuration incomplete or unpinned")
			}
			result = scanEvidence{Tool: c.ScannerName, Version: c.ScannerVersion, Mode: c.ScanMode, ScanTime: time.Now().UTC(), Database: c.DB, DatabaseModifiedAt: c.DBLastModified, GoVersion: c.GoVersion, RawSHA256: hashBytes(raw), Advisories: []scanFinding{},
				Disposition: "Symbol-reachable findings block PASS, matching the existing pinned govulncheck symbol policy. Module/package advisory records remain nonblocking and untriaged here; their presence is not a claim of no vulnerabilities.",
				Scope:       "Exact downloaded linux/amd64 binary, symbol-level Go vulnerability analysis; no assertion about future database updates, repository alerts, frontend dependencies or all attack classes."}
		}
	}
	if configs != 1 || sboms != 1 || messages < 2 {
		return result, errors.New("missing binary scanner execution stream")
	}
	return result, nil
}

// The dedicated browser reporter emits the exact mandatory inventory and
// negative controls; a generic Playwright job success cannot substitute it.
func validateBrowser(raw []byte, binaryDigest, source string) error {
	var report struct {
		Schema         int      `json:"schema_version"`
		Profile        string   `json:"profile"`
		Status         string   `json:"status"`
		ArtifactKind   string   `json:"artifact_kind"`
		BinaryDigest   string   `json:"binary_sha256"`
		Source         string   `json:"source_sha"`
		Datastore      string   `json:"datastore"`
		Authentication string   `json:"authentication"`
		Elapsed        int64    `json:"elapsed_ms"`
		Required       []string `json:"required_tests"`
		Configuration  *struct {
			Transport           string `json:"transport"`
			AdmissionLimits     string `json:"admission_limits"`
			ServiceBudgets      string `json:"service_budgets"`
			FakeAdapterProvider *bool  `json:"fake_adapter_provider"`
			ChildEnvironment    string `json:"child_environment"`
			DevelopmentWindow   *int   `json:"development_disclosure_window_seconds"`
			ProtectedWindow     *int   `json:"protected_disclosure_window_seconds"`
			ExternalProviders   *bool  `json:"external_identity_providers"`
		} `json:"fixture_configuration"`
		Tests []struct {
			ID       string `json:"id"`
			Status   string `json:"status"`
			Duration int64  `json:"duration_ms"`
		} `json:"tests"`
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		return err
	}
	required := []string{"stored-content", "hostile-origin", "oracle-negative-controls"}
	if report.Schema != 1 || report.Profile != "hikyo-browser-security-v1" || report.Status != "passed" || report.ArtifactKind != "provided-binary" || report.BinaryDigest != binaryDigest || report.Source != source || report.Datastore != "sqlite" || report.Authentication != "native-password-totp" || report.Elapsed <= 0 || !sameSet(report.Required, required) {
		return errors.New("browser profile missing or mismatched identity")
	}
	c := report.Configuration
	if c == nil || c.Transport != "loopback-http-dev" || c.AdmissionLimits != "product-default" || c.ServiceBudgets != "product-default" || c.FakeAdapterProvider == nil || *c.FakeAdapterProvider || c.ChildEnvironment != "allowlist" || c.DevelopmentWindow == nil || *c.DevelopmentWindow != 300 || c.ProtectedWindow == nil || *c.ProtectedWindow != 0 || c.ExternalProviders == nil || *c.ExternalProviders {
		return errors.New("browser fixture configuration incomplete or outside the reviewed profile")
	}
	ids := []string{}
	for _, c := range report.Tests {
		if c.Status != "passed" || c.Duration <= 0 {
			return errors.New("browser profile skipped or failed")
		}
		ids = append(ids, c.ID)
	}
	if !sameSet(ids, required) {
		return errors.New("browser mandatory inventory incomplete or duplicated")
	}
	return nil
}
