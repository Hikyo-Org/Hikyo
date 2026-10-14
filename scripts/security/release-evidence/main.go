// release-evidence extracts only authenticated subjects and publishes a bounded
// runtime-profile receipt. Release trust verification belongs to the existing
// stable/nightly verifiers; this command never replaces their signature policy.
package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
)

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

type prepared struct {
	Tag          string                   `json:"tag"`
	Identity     releaseidentity.Identity `json:"identity"`
	Archive      releasetrust.Artifact    `json:"archive"`
	BinarySHA256 string                   `json:"binary_sha256"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "security evidence:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("require prepare or publish")
	}
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	directory := f.String("directory", "", "authenticated release directory")
	identity := f.String("identity", "", "output from trusted release verifier")
	tag := f.String("tag", "", "exact published tag")
	source := f.String("source", "", "peeled tag commit")
	binary := f.String("binary", "", "new executable output path")
	prep := f.String("prepared", "", "prepared subject identity")
	result := f.String("result", "", "artifact harness output")
	browser := f.String("browser", "", "Playwright JSON output")
	scan := f.String("scan", "", "binary govulncheck JSON stream")
	scanCompleted := f.String("scan-completed-at", "", "recorded completion timestamp of successful binary scan")
	harness := f.String("harness", "", "trusted main harness SHA")
	runURL := f.String("run-url", "", "exact hosted run and attempt URL")
	cleanup := f.Bool("datastore-cleaned", false, "set only after successful PostgreSQL removal")
	out := f.String("out", "", "new output file or directory")
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	switch args[0] {
	case "prepare":
		var p prepared
		if err := readJSON(*identity, &p.Identity); err != nil {
			return err
		}
		p.Tag = *tag
		if p.Tag != "v"+p.Identity.Version || p.Identity.Commit != *source {
			return errors.New("release tag/source does not match authenticated identity")
		}
		if err := p.Identity.Validate(); err != nil {
			return err
		}
		manifestRaw, err := os.ReadFile(filepath.Join(*directory, "release-manifest.json"))
		if err != nil {
			return err
		}
		if hashBytes(manifestRaw) != string(p.Identity.ManifestSHA256) {
			return errors.New("authenticated manifest digest mismatch")
		}
		var manifest struct {
			Artifacts []releasetrust.Artifact `json:"artifacts"`
		}
		if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
			return err
		}
		matches := 0
		for _, artifact := range manifest.Artifacts {
			if artifact.Kind == "binary" && artifact.Platform == "linux/amd64" {
				p.Archive = artifact
				matches++
			}
		}
		if matches != 1 || !releaseidentity.SafeName(p.Archive.Name) || !strings.HasSuffix(p.Archive.Name, ".tar.gz") || !digestPattern.MatchString(p.Archive.SHA256) {
			return errors.New("require exactly one authenticated linux/amd64 archive")
		}
		archive := filepath.Join(*directory, p.Archive.Name)
		actual, err := fileHash(archive)
		if err != nil {
			return err
		}
		if actual != p.Archive.SHA256 {
			return errors.New("authenticated archive digest mismatch")
		}
		p.BinarySHA256, err = extractBinary(archive, *binary)
		if err != nil {
			return err
		}
		return writeJSON(*out, p)
	case "publish":
		var p prepared
		if err := readJSON(*prep, &p); err != nil {
			return err
		}
		if err := p.Identity.Validate(); err != nil {
			return err
		}
		if p.Tag != "v"+p.Identity.Version || !digestPattern.MatchString(p.BinarySHA256) || !digestPattern.MatchString(p.Archive.SHA256) || p.Archive.Platform != "linux/amd64" {
			return errors.New("invalid prepared subject")
		}
		if !*cleanup || !shaPattern.MatchString(*harness) || !regexp.MustCompile(`^https://github\.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+/actions/runs/[1-9][0-9]*/attempts/[1-9][0-9]*$`).MatchString(*runURL) {
			return errors.New("require cleanup, trusted harness SHA, and exact hosted run attempt")
		}
		var attacks attackResult
		if err := readJSON(*result, &attacks); err != nil {
			return err
		}
		if err := validateAttacks(attacks, p.BinarySHA256); err != nil {
			return err
		}
		browserRaw, err := os.ReadFile(*browser)
		if err != nil {
			return err
		}
		if err := validateBrowser(browserRaw, p.BinarySHA256, p.Identity.Commit); err != nil {
			return err
		}
		scanRaw, err := os.ReadFile(*scan)
		if err != nil {
			return err
		}
		scanner, err := validateScan(scanRaw)
		if err != nil {
			return err
		}
		scanTime, err := time.Parse(time.RFC3339, *scanCompleted)
		if err != nil || scanTime.IsZero() || scanTime.After(time.Now().Add(time.Minute)) {
			return errors.New("invalid scan completion timestamp")
		}
		scanner.ScanTime = scanTime
		receipt := struct {
			Schema           string          `json:"schema"`
			Status           string          `json:"status"`
			Profile          string          `json:"profile"`
			Subject          prepared        `json:"subject"`
			HarnessCommit    string          `json:"harness_commit"`
			RunURL           string          `json:"run_url"`
			PublishedAt      time.Time       `json:"published_at"`
			Runner           string          `json:"runner"`
			DatastoreCleaned bool            `json:"disposable_postgres_removed"`
			Attacks          attackResult    `json:"artifact_attacks"`
			Browser          json.RawMessage `json:"artifact_browser"`
			BinaryScanner    scanEvidence    `json:"binary_vulnerability_scan"`
			SourceChecks     string          `json:"source_checks"`
			Fuzz             string          `json:"active_fuzz"`
			CurrentFindings  string          `json:"current_repository_findings"`
			Limitations      []string        `json:"limitations"`
		}{"hikyo.dev/security-runtime-evidence/v1", "passed", "hikyo-release-runtime-v1", p, *harness, *runURL, time.Now().UTC(), runtime.GOOS + "/" + runtime.GOARCH, true, attacks, browserRaw, scanner,
			"Full source CI is a separate prepublication gate; this receipt does not infer its execution counts or findings from workflow success.",
			"No coverage-guided fuzzing is performed by this runtime profile. Source CI declares 100000x iterations per discovered target; that budget is not a measured count or a distinct attack count.",
			"CodeQL and Dependabot repository findings are not queried by this runtime profile. Their current status is unknown here; the binary scan is a historical, timestamped scan of these exact bytes.",
			[]string{"Only the linux/amd64 binary archive is executed; other binaries, packages and OCI images are authenticated but not executed by this profile.", "Generated bounded sequences are regression coverage, not an exhaustive attack inventory or security certification.", "SQLite and disposable PostgreSQL use synthetic data; external providers and a compromised trusted host are outside scope."}}
		// Create no PASS directory until every required input has validated.
		if err := os.Mkdir(*out, 0700); err != nil {
			return err
		}
		if err := writeJSON(filepath.Join(*out, "security-evidence.json"), receipt); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(*out, "binary-scan.jsonl"), scanRaw, 0600); err != nil {
			return err
		}
		raw, err := json.MarshalIndent(receipt, "", "  ")
		if err != nil {
			return err
		}
		html, err := os.OpenFile(filepath.Join(*out, "security-evidence.html"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		defer html.Close()
		t := template.Must(template.New("receipt").Parse(`<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Hikyo security evidence</title><style>body{font:16px system-ui;margin:3rem auto;max-width:80rem;padding:0 1rem;color:#17202a}pre{white-space:pre-wrap;overflow-wrap:anywhere;background:#f3f5f7;padding:1.5rem;border-radius:.5rem}h1{font-size:2rem}a{color:#245bb2}</style><h1>{{.Tag}}: runtime regression profile passed</h1><p>Historical evidence for the exact authenticated Linux amd64 binary. Scope, executed cases, exclusions and scan freshness appear below. This is not a security certification.</p><p><a href="{{.Run}}">Exact GitHub run attempt</a></p><pre>{{.JSON}}</pre></html>`))
		return t.Execute(html, struct{ Tag, Run, JSON string }{p.Tag, *runURL, string(raw)})
	default:
		return errors.New("unknown command")
	}
}

func hashBytes(raw []byte) string { digest := sha256.Sum256(raw); return hex.EncodeToString(digest[:]) }
func fileHash(name string) (string, error) {
	f, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
func readJSON(name string, out any) error {
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 16<<20))
	if err := d.Decode(out); err != nil {
		return err
	}
	var trailing any
	if d.Decode(&trailing) != io.EOF {
		return errors.New("trailing JSON input")
	}
	return nil
}
func writeJSON(name string, value any) error {
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(value)
}
func extractBinary(archive, destination string) (string, error) {
	f, err := os.Open(archive)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	var binary []byte
	found := false
	for {
		h, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if h.Name != "hikyo" {
			continue
		}
		if found || h.Typeflag != tar.TypeReg || h.Size <= 0 || h.Size > 256<<20 {
			return "", errors.New("invalid or duplicate archive executable")
		}
		found = true
		binary, err = io.ReadAll(io.LimitReader(reader, 256<<20+1))
		if err != nil {
			return "", err
		}
		if int64(len(binary)) != h.Size {
			return "", errors.New("truncated executable")
		}
	}
	if !found {
		return "", errors.New("archive has no regular root hikyo executable")
	}
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if err != nil {
		return "", err
	}
	defer out.Close()
	if _, err := out.Write(binary); err != nil {
		return "", err
	}
	return hashBytes(binary), nil
}
