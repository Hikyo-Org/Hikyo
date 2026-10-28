// artifact-harness exercises externally supplied production Hikyo bytes. Artifact
// authenticity belongs to the release verifier; this program records its digest.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const profile = "hikyo-artifact-adversarial-v1"

var mandatoryCases = []string{"native-bootstrap", "authorized-controls", "cross-project-denial", "cross-tenant-denial", "denied-mutation-unchanged", "grant-removal", "grant-restoration", "credential-revocation", "cursor-binding", "projection-boundary", "revision-boundary", "restart-continuity", "audit-boundary", "generated-sequence"}

type options struct {
	binary, output, postgres, bundle string
	seed                             int64
	steps                            int
	timeout                          time.Duration
}
type caseResult struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	Expected   string `json:"expected"`
	Observed   string `json:"observed"`
	Assertions int    `json:"assertions"`
	Error      string `json:"error,omitempty"`
}
type backendResult struct {
	Engine        string       `json:"engine"`
	Cases         []caseResult `json:"cases"`
	Sequence      []string     `json:"sequence,omitempty"`
	FailurePrefix []string     `json:"failure_prefix,omitempty"`
	Cleanup       string       `json:"cleanup"`
}
type receipt struct {
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
	Backends        []backendResult   `json:"backends"`
	Limitations     []string          `json:"limitations"`
	Configuration   map[string]string `json:"configuration"`
	AssertionMetric string            `json:"assertion_metric"`
	Error           string            `json:"error,omitempty"`
}

func main() {
	var o options
	flag.StringVar(&o.binary, "binary", "", "verified Hikyo executable supplied by trusted release pipeline")
	flag.StringVar(&o.output, "output", "", "JSON evidence file (written even on campaign failure)")
	flag.StringVar(&o.postgres, "postgres-dsn", "", "disposable loopback PostgreSQL admin DSN with CREATEDB; an owned database is created and removed")
	flag.StringVar(&o.bundle, "bundle", "", "verified production upgrade bundle directory when required by executable")
	flag.Int64Var(&o.seed, "seed", 20261014, "reproducible action generator seed")
	flag.IntVar(&o.steps, "steps", 12, "bounded generated actions, 8 through 24")
	flag.DurationVar(&o.timeout, "timeout", 10*time.Minute, "total deadline, at most 15m")
	flag.Parse()
	if o.output == "" {
		fmt.Fprintln(os.Stderr, "--output is required")
		os.Exit(2)
	}
	r, err := execute(o)
	data, marshalErr := json.MarshalIndent(r, "", "  ")
	if marshalErr == nil {
		marshalErr = os.WriteFile(o.output, append(data, '\n'), 0600)
	}
	if marshalErr != nil {
		fmt.Fprintln(os.Stderr, "write evidence:", marshalErr)
		os.Exit(1)
	}
	if err != nil {
		emitReplay(os.Stderr, r)
		fmt.Fprintln(os.Stderr, "artifact campaign failed:", err)
		os.Exit(1)
	}
	fmt.Println("artifact campaign passed:", profile)
}

func emitReplay(w io.Writer, r receipt) {
	allowedActions := map[string]bool{"remove-read": true, "restore-read": true, "probe": true, "revoke-generated": true, "restart": true}
	allowedCases := map[string]bool{}
	for _, id := range mandatoryCases {
		allowedCases[id] = true
	}
	type replay struct {
		Profile       string   `json:"profile"`
		Artifact      string   `json:"artifact_sha256"`
		Seed          int64    `json:"seed"`
		Steps         int      `json:"steps"`
		Engine        string   `json:"backend"`
		Case          string   `json:"case"`
		Sequence      []string `json:"sequence,omitempty"`
		FailurePrefix []string `json:"failure_prefix,omitempty"`
	}
	filter := func(actions []string) []string {
		var out []string
		for _, action := range actions {
			if allowedActions[action] {
				out = append(out, action)
			}
		}
		return out
	}
	for _, b := range r.Backends {
		if b.Engine != "sqlite" && b.Engine != "postgres" {
			continue
		}
		id := "setup"
		for _, c := range b.Cases {
			if c.Status == "failed" && allowedCases[c.ID] {
				id = c.ID
			}
		}
		if b.Cleanup == "not-started" {
			continue
		}
		safe := replay{Profile: profile, Artifact: r.ArtifactSHA256, Seed: r.Seed, Steps: r.Steps, Engine: b.Engine, Case: id, Sequence: filter(b.Sequence), FailurePrefix: filter(b.FailurePrefix)}
		if raw, err := json.Marshal(safe); err == nil {
			fmt.Fprintln(w, "artifact replay:", string(raw))
		}
	}
}

func execute(o options) (r receipt, err error) {
	r = receipt{SchemaVersion: 1, Profile: profile, HarnessVersion: 1, Seed: o.seed, Steps: o.steps, StartedAt: time.Now().UTC(), Status: "failed", ExpectedCases: mandatoryCases,
		Configuration:   map[string]string{"trust_domain": "production", "transport": "private HTTPS with pinned synthetic certificate", "second_factor": "required; native enrolled WebAuthn software authenticator", "disclosure_window": "production default zero; each widening has native signed mint ceremony", "credential_issuance": "native local administrator bootstrap and public API issuance", "operator_custody": "fresh independent ECDSA public key and temporary upgrade state", "authentication_pacing": "at most eight authentication POSTs and fifty MCP calls per rolling minute; server policy unchanged"},
		AssertionMetric: "successful oracle groups, not individual predicates, HTTP requests, distinct attacks or coverage percentage",
		Limitations:     []string{"No external providers, detector/notification stack, backup restore, datastore fault injection, replica concurrency, or browser execution in this harness.", "Generated sequences cover explicit read grants and credential liveness, not every domain capability or concurrent schedule.", "MCP environment inspection uses a genuinely authorized missing-environment twin; list-definitions cursor denial uses a same-tool restricted missing-project uniform-refusal baseline.", "Failure prefix is the shortest observed failing prefix, not a delta-debugged minimum."}}
	defer func() {
		r.CompletedAt = time.Now().UTC()
		if err != nil {
			r.Error = err.Error()
			for _, engine := range []string{"sqlite", "postgres"} {
				found := false
				for _, b := range r.Backends {
					found = found || b.Engine == engine
				}
				if !found {
					b := backendResult{Engine: engine, Cleanup: "not-started"}
					for _, id := range mandatoryCases {
						b.Cases = append(b.Cases, caseResult{ID: id, Status: "skipped", Expected: "mandatory campaign case", Observed: "prior failure prevented backend execution"})
					}
					r.Backends = append(r.Backends, b)
				}
			}
		}
	}()
	if o.binary == "" || o.steps < 8 || o.steps > 24 || o.timeout <= 0 || o.timeout > 15*time.Minute {
		return r, errors.New("require binary, 8..24 steps and deadline <=15m")
	}
	o.binary, err = filepath.Abs(o.binary)
	if err != nil {
		return r, err
	}
	f, err := os.Open(o.binary)
	if err != nil {
		return r, errors.New("open supplied executable")
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return r, errors.New("hash supplied executable")
	}
	r.ArtifactSHA256 = hex.EncodeToString(h.Sum(nil))
	ctx, cancel := context.WithTimeout(context.Background(), o.timeout)
	defer cancel()
	v, err := command(ctx, o.binary, "", nil, "version")
	if err != nil {
		return r, errors.New("supplied executable version command failed")
	}
	r.BinaryVersion = string(v)
	r.Backends, err = runBackends(ctx, o, runBackend)
	if err != nil {
		return r, err
	}
	r.Status = "passed"
	return r, nil
}

// runBackends retains one result per independent datastore and joins every
// campaign's owned cleanup before returning deterministic receipt order.
func runBackends(ctx context.Context, o options, run func(context.Context, options, *backendResult) error) ([]backendResult, error) {
	backends := []backendResult{{Engine: "sqlite", Cleanup: "not-started"}, {Engine: "postgres", Cleanup: "not-started"}}
	errs := make([]error, len(backends))
	var workers sync.WaitGroup
	for i := range backends {
		b := &backends[i]
		if b.Engine == "postgres" && o.postgres == "" {
			for _, id := range mandatoryCases {
				b.Cases = append(b.Cases, caseResult{ID: id, Status: "skipped", Expected: "executed on PostgreSQL", Observed: "no DSN supplied"})
			}
			errs[i] = errors.New("PostgreSQL DSN required for complete profile")
			continue
		}
		workers.Go(func() { errs[i] = run(ctx, o, b) })
	}
	workers.Wait()
	return backends, errors.Join(errs...)
}

func record(b *backendResult, id, expected string, assertions int, fn func() error) error {
	c := caseResult{ID: id, Expected: expected, Status: "passed", Observed: "assertions satisfied"}
	err := fn()
	if err != nil {
		c.Status = "failed"
		c.Observed = "assertion failed"
		c.Error = err.Error()
	} else {
		c.Assertions = assertions
	}
	b.Cases = append(b.Cases, c)
	fmt.Fprintf(os.Stderr, "artifact case %s/%s: %s\n", b.Engine, id, c.Status)
	return err
}
