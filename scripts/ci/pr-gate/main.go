// Command pr-gate publishes a trusted, asynchronous PR validation check.
// It must run from a trusted default-branch checkout, never from PR code.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const workflowPath = ".github/workflows/ci-candidate.yml"
const externalPrefix = "hikyo-pr-gate:v2:"
const legacyExternalPrefix = "hikyo-pr-gate:v1:"
const externalIDLimit = 255
const actionsAppID = 15368

var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

type repository struct {
	ID            int64  `json:"id"`
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
}
type reference struct {
	SHA  string     `json:"sha"`
	Ref  string     `json:"ref"`
	Repo repository `json:"repo"`
}
type pull struct {
	Number         int       `json:"number"`
	State          string    `json:"state"`
	Head           reference `json:"head"`
	Base           reference `json:"base"`
	MergeCommitSHA string    `json:"merge_commit_sha"`
	Mergeable      *bool     `json:"mergeable"`
	User           struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	} `json:"user"`
}
type workflow struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}
type run struct {
	ID             int64      `json:"id"`
	Attempt        int64      `json:"run_attempt"`
	WorkflowID     int64      `json:"workflow_id"`
	Path           string     `json:"path"`
	Name           string     `json:"name"`
	DisplayTitle   string     `json:"display_title"`
	Event          string     `json:"event"`
	HeadSHA        string     `json:"head_sha"`
	HeadBranch     string     `json:"head_branch"`
	CreatedAt      string     `json:"created_at"`
	Status         string     `json:"status"`
	Conclusion     string     `json:"conclusion"`
	Repository     repository `json:"repository"`
	HeadRepository repository `json:"head_repository"`
	Pulls          []pull     `json:"pull_requests"`
	References     []struct {
		Path string `json:"path"`
		Ref  string `json:"ref"`
		SHA  string `json:"sha"`
	} `json:"referenced_workflows"`
}
type job struct {
	ID         int64  `json:"id"`
	RunID      int64  `json:"run_id"`
	Attempt    int64  `json:"run_attempt"`
	HeadSHA    string `json:"head_sha"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}
type check struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	HeadSHA     string `json:"head_sha"`
	ExternalID  string `json:"external_id"`
	Status      string `json:"status"`
	Conclusion  string `json:"conclusion"`
	CompletedAt string `json:"completed_at"`
	App         struct {
		ID   int64  `json:"id"`
		Slug string `json:"slug"`
	} `json:"app"`
	Output struct {
		Title   string `json:"title"`
		Summary string `json:"summary"`
	} `json:"output"`
}
type legacyBinding struct {
	RepoID  int64  `json:"repo"`
	PR      int    `json:"pr"`
	Head    string `json:"head"`
	BaseRef string `json:"base_ref"`
	Base    string `json:"base"`
	Run     int64  `json:"run"`
	Attempt int64  `json:"attempt"`
}

type binding struct {
	RepoID, Run, Attempt int64
	PR                   int
	Head, Base           string
	BaseRef              [sha256.Size]byte
}

func (b binding) externalID() string {
	// GitHub truncates external_id at 255 characters. Fixed-width metadata and
	// a full SHA-256 reference digest keep every binding at 156 characters,
	// independent of branch-name and numeric-ID length.
	data := make([]byte, 104)
	binary.BigEndian.PutUint64(data[0:8], uint64(b.RepoID))
	binary.BigEndian.PutUint64(data[8:16], uint64(b.PR))
	binary.BigEndian.PutUint64(data[16:24], uint64(b.Run))
	binary.BigEndian.PutUint64(data[24:32], uint64(b.Attempt))
	head, _ := hex.DecodeString(b.Head)
	base, _ := hex.DecodeString(b.Base)
	copy(data[32:52], head)
	copy(data[52:72], base)
	copy(data[72:], b.BaseRef[:])
	return externalPrefix + base64.RawURLEncoding.EncodeToString(data)
}
func decodeBinding(value string) (binding, error) {
	var b binding
	if strings.HasPrefix(value, legacyExternalPrefix) {
		data, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, legacyExternalPrefix))
		if err != nil {
			return b, errors.New("invalid legacy controller check binding")
		}
		var old legacyBinding
		if err := decode(data, &old); err != nil {
			return b, err
		}
		canonical, _ := json.Marshal(old)
		if legacyExternalPrefix+base64.RawURLEncoding.EncodeToString(canonical) != value || old.BaseRef == "" || !shaPattern.MatchString(old.Head) || !shaPattern.MatchString(old.Base) {
			return b, errors.New("noncanonical legacy controller check binding")
		}
		b = binding{RepoID: old.RepoID, PR: old.PR, Head: old.Head, Base: old.Base, BaseRef: sha256.Sum256([]byte(old.BaseRef)), Run: old.Run, Attempt: old.Attempt}
		if !b.valid() {
			return binding{}, errors.New("invalid legacy controller check binding")
		}
		return b, nil
	}
	if !strings.HasPrefix(value, externalPrefix) {
		return b, errors.New("not a controller check")
	}
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, externalPrefix))
	if err != nil || len(data) != 104 {
		return b, errors.New("invalid controller check binding")
	}
	b.RepoID = int64(binary.BigEndian.Uint64(data[0:8]))
	pr := binary.BigEndian.Uint64(data[8:16])
	b.PR = int(pr)
	b.Run = int64(binary.BigEndian.Uint64(data[16:24]))
	b.Attempt = int64(binary.BigEndian.Uint64(data[24:32]))
	b.Head = hex.EncodeToString(data[32:52])
	b.Base = hex.EncodeToString(data[52:72])
	copy(b.BaseRef[:], data[72:])
	if !b.valid() || uint64(b.PR) != pr {
		return b, errors.New("invalid controller check binding")
	}
	if b.externalID() != value {
		return b, errors.New("noncanonical controller check binding")
	}
	return b, nil
}

func (b binding) valid() bool {
	return b.RepoID > 0 && b.PR > 0 && b.Run >= 0 && b.Attempt >= 0 && (b.Run == 0) == (b.Attempt == 0)
}

// Recognize canonical v1 truncations only after the surviving complete repo,
// PR and head identities have been verified. Old base/attempt metadata grants
// only authority to supersede the broken check with a new queued barrier.
func truncatedLegacyBinding(value string, repoID int64, head string) bool {
	if len(value) != externalIDLimit || !strings.HasPrefix(value, legacyExternalPrefix) {
		return false
	}
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, legacyExternalPrefix))
	if err != nil || json.Valid(data) {
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	var identity struct {
		Repo int64
		PR   int
		Head string
	}
	if token, err := dec.Token(); err != nil || token != json.Delim('{') {
		return false
	}
	for _, member := range []struct {
		name  string
		value any
	}{{"repo", &identity.Repo}, {"pr", &identity.PR}, {"head", &identity.Head}} {
		if token, err := dec.Token(); err != nil || token != member.name || dec.Decode(member.value) != nil {
			return false
		}
	}
	if identity.Repo <= 0 || identity.Repo != repoID || identity.PR <= 0 || !shaPattern.MatchString(identity.Head) || identity.Head != head {
		return false
	}
	prefix := fmt.Sprintf(`{"repo":%d,"pr":%d,"head":%q`, identity.Repo, identity.PR, identity.Head)
	if !bytes.HasPrefix(data, []byte(prefix)) {
		return false
	}
	tail := data[len(prefix):]
	var run int64
	for _, field := range []string{"base_ref", "base", "run", "attempt"} {
		literal := []byte(`,"` + field + `":`)
		if len(tail) < len(literal) {
			return bytes.HasPrefix(literal, tail)
		}
		if !bytes.HasPrefix(tail, literal) {
			return false
		}
		tail = tail[len(literal):]
		if field == "base_ref" || field == "base" {
			var text string
			d := json.NewDecoder(bytes.NewReader(tail))
			if err := d.Decode(&text); err != nil {
				if !(errors.Is(err, io.ErrUnexpectedEOF) || (errors.Is(err, io.EOF) && len(tail) == 0)) || !canonicalStringPrefix(tail) {
					return false
				}
				return field == "base_ref" || len(tail) == 0 || (len(tail) <= 41 && strings.Trim(string(tail[1:]), "0123456789abcdef") == "")
			}
			canonical, _ := json.Marshal(text)
			if text == "" || (field == "base" && !shaPattern.MatchString(text)) || !bytes.HasPrefix(tail, canonical) {
				return false
			}
			tail = tail[len(canonical):]
			continue
		}
		i := bytes.IndexByte(tail, ',')
		if i < 0 {
			i = len(tail)
		}
		digits := string(tail[:i])
		if digits == "" {
			return i == len(tail)
		}
		number, err := strconv.ParseInt(digits, 10, 64)
		if err != nil || number < 0 || strconv.FormatInt(number, 10) != digits {
			return false
		}
		if field == "run" {
			run = number
		} else if (run == 0) != (number == 0) {
			return false
		}
		if i == len(tail) {
			return true
		}
		tail = tail[i:]
	}
	return false
}

func canonicalStringPrefix(data []byte) bool {
	if len(data) == 0 {
		return true
	}
	if data[0] != '"' {
		return false
	}
	for i := 1; i < len(data); {
		c := data[i]
		if c == '\\' {
			i++
			if i == len(data) {
				return true
			}
			if strings.ContainsRune(`"\bfnrt`, rune(data[i])) {
				i++
				continue
			}
			if data[i] != 'u' {
				return false
			}
			i++
			start := i
			for i < len(data) && i-start < 4 {
				if !strings.ContainsRune("0123456789abcdef", rune(data[i])) {
					return false
				}
				i++
			}
			code := string(data[start:i])
			allowed := []string{"0026", "003c", "003e", "2028", "2029"}
			for control := 0; control < 0x20; control++ {
				if control != '\b' && control != '\t' && control != '\n' && control != '\f' && control != '\r' {
					allowed = append(allowed, fmt.Sprintf("%04x", control))
				}
			}
			possible := false
			for _, escape := range allowed {
				possible = possible || strings.HasPrefix(escape, code)
			}
			if !possible {
				return false
			}
			if i-start < 4 {
				return true
			}
			continue
		}
		if c < 0x20 || c == '"' || c == '<' || c == '>' || c == '&' {
			return false
		}
		r, size := utf8.DecodeRune(data[i:])
		if r == utf8.RuneError && size == 1 {
			return !utf8.FullRune(data[i:])
		}
		if r == '\u2028' || r == '\u2029' {
			return false
		}
		i += size
	}
	return true
}

type event struct {
	Repository repository `json:"repository"`
	Action     string     `json:"action"`
	Before     string     `json:"before"`
	After      string     `json:"after"`
	Ref        string     `json:"ref"`
	Number     int        `json:"number"`
	Pull       pull       `json:"pull_request"`
	Run        run        `json:"workflow_run"`
	Inputs     struct {
		PR   string `json:"pr_number"`
		Work string `json:"work"`
		Head string `json:"head"`
	} `json:"inputs"`
}
type api interface {
	read(context.Context, string, any) error
	write(context.Context, string, string, any, any) error
}
type ghAPI struct{}

// Keep REST failure status separate from untrusted response text. Callers can
// recognize an absent ephemeral ref without hiding permission or API failures.
type githubReadError struct {
	path   string
	status int
	cause  error
	// GraphQL answered with an error payload. The server decided, so a retry
	// cannot help even though gh exited nonzero.
	rejected bool
}

func (e *githubReadError) Error() string {
	details := []string{}
	if e.status != 0 {
		details = append(details, fmt.Sprintf("HTTP %d", e.status))
	}
	var exit *exec.ExitError
	switch {
	case errors.Is(e.cause, context.DeadlineExceeded):
		details = append(details, context.DeadlineExceeded.Error())
	case errors.Is(e.cause, context.Canceled):
		details = append(details, context.Canceled.Error())
	case errors.As(e.cause, &exit):
		details = append(details, fmt.Sprintf("command exit status %d", exit.ExitCode()))
	case e.cause != nil:
		details = append(details, "command failed")
	}
	if len(details) != 0 {
		return fmt.Sprintf("GitHub read failed for %s (%s)", e.path, strings.Join(details, "; "))
	}
	return fmt.Sprintf("GitHub read failed for %s", e.path)
}

func (e *githubReadError) Unwrap() error { return e.cause }

func decode(data []byte, result any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	if err := d.Decode(result); err != nil {
		return fmt.Errorf("invalid GitHub JSON: %w", err)
	}
	var trailing any
	if err := d.Decode(&trailing); err != io.EOF {
		return errors.New("GitHub response contains extra JSON or malformed trailing data")
	}
	return nil
}

// Reads are idempotent, so retryingAPI retries brief GitHub 5xx/transport blips
// rather than letting them become terminal required-check failures. Writes
// never retry.
var readRetryBackoff = []time.Duration{time.Second, 3 * time.Second}
var readRetryWait = waitForReadRetry

func waitForReadRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// 404 and other 4xx stay terminal: callers give them meaning (for example an
// absent ephemeral ref). Status 0 means gh failed without an HTTP answer.
func transientGitHubRead(err error) bool {
	var failure *githubReadError
	if !errors.As(err, &failure) || failure.rejected {
		return false
	}
	switch failure.status {
	case 429, 500, 502, 503, 504:
		return true
	case 0:
		// Only a gh process that ran and failed without a status is transport
		// trouble; a missing or unstartable gh binary will not recover. An
		// ExitError never wraps a context error, so none needs excluding. gh
		// documents exit 2 (cancelled) and 4 (authentication required) as final.
		var exit *exec.ExitError
		return errors.As(failure.cause, &exit) && exit.ExitCode() != 2 && exit.ExitCode() != 4
	}
	return false
}

// gh prints "gh: <message> (HTTP 502)" for JSON errors and "gh: HTTP 502" when
// the body is not JSON. Match only a gh-prefixed line ending in a status so a
// quoted body cannot mask the status gh appended last; the last match wins.
var ghStderrStatus = regexp.MustCompile(`(?m)^gh: (?:.*\(HTTP ([45][0-9]{2})\)|HTTP ([45][0-9]{2}))[ \t\r]*$`)

func stderrHTTPStatus(stderr []byte) int {
	matches := ghStderrStatus.FindAllSubmatch(stderr, -1)
	if len(matches) == 0 {
		return 0
	}
	last := matches[len(matches)-1]
	code, _ := strconv.Atoi(string(last[1]) + string(last[2]))
	return code
}

// retryingAPI retries transient reads. Only the queue publisher opts in, since
// its group proof has nothing that retriggers it after a blip. Retirement
// discovery documents "no retries or backoff" and counts one request per read,
// and the legacy gate and lifecycle probe keep their single attempt.
type retryingAPI struct{ api api }

func (a retryingAPI) read(ctx context.Context, path string, result any) error {
	for attempt := 0; ; attempt++ {
		err := a.api.read(ctx, path, result)
		if err == nil || attempt >= len(readRetryBackoff) || !transientGitHubRead(err) {
			return err
		}
		delay := readRetryBackoff[attempt]
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= delay {
			return err
		}
		fmt.Fprintf(os.Stderr, "trusted PR gate: retrying GitHub read (attempt %d of %d): %v\n", attempt+2, len(readRetryBackoff)+1, err)
		if waitErr := readRetryWait(ctx, delay); waitErr != nil {
			return &githubReadError{path: path, cause: waitErr}
		}
	}
}

func (a retryingAPI) write(ctx context.Context, method, path string, payload, result any) error {
	return a.api.write(ctx, method, path, payload, result)
}

// Discovery and dispatch are broad inventories and keep single-attempt reads.
func controllerAPI(queueMode, dispatch bool) api {
	if queueMode && !dispatch {
		return retryingAPI{api: ghAPI{}}
	}
	return ghAPI{}
}

func (ghAPI) read(ctx context.Context, path string, result any) error {
	args := []string{"api", path}
	if strings.HasPrefix(path, "graphql:") {
		args = []string{"api", "graphql", "-f", "query=" + strings.TrimPrefix(path, "graphql:")}
	}
	data, err := gateCommand(ctx, "gh", args...).Output()
	if err != nil {
		// Process-group cancellation can surface as a killed command rather than
		// ctx.Err(). Keep the controlling deadline/cancellation recognizable.
		if cause := ctx.Err(); cause != nil {
			return &githubReadError{path: path, cause: cause}
		}
		// gh exits nonzero for HTTP-successful GraphQL error responses too.
		// Preserve bounded server diagnostics without exposing partial data.
		if strings.HasPrefix(path, "graphql:") {
			var response struct {
				Errors []json.RawMessage `json:"errors"`
			}
			if decode(data, &response) == nil && len(response.Errors) > 0 {
				return fmt.Errorf("%s: %w", graphQLDiagnostic(response.Errors), &githubReadError{path: path, cause: err, rejected: true})
			}
		}
		var failure struct {
			Status string `json:"status"`
		}
		status := 0
		if !strings.HasPrefix(path, "graphql:") && decode(data, &failure) == nil {
			if code, parseErr := strconv.Atoi(failure.Status); parseErr == nil && code >= 400 && code <= 599 {
				status = code
			}
		}
		// Output fills ExitError.Stderr (bounded) because gateCommand leaves
		// Stderr unset. Only the parsed status is kept, never the text.
		var exit *exec.ExitError
		if status == 0 && errors.As(err, &exit) {
			status = stderrHTTPStatus(exit.Stderr)
		}
		return &githubReadError{path: path, status: status, cause: err}
	}
	return decode(data, result)
}
func (ghAPI) write(ctx context.Context, method, path string, payload, result any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	args := []string{"api", "--method", method, path, "--input", "-"}
	dispatch := strings.HasSuffix(path, "/actions/workflows/"+queueWorkerFilename+"/dispatches")
	if dispatch {
		args = append(args, "-H", "X-GitHub-Api-Version: 2026-03-10", "--include")
	}
	cmd := gateCommand(ctx, "gh", args...)
	cmd.Stdin = bytes.NewReader(data)
	response, err := cmd.Output()
	// A lost response may follow a successful mutation. Never retry writes.
	if err != nil {
		return fmt.Errorf("GitHub %s failed for %s; no mutation retry attempted", method, path)
	}
	if dispatch {
		response, err = dispatchResponseBody(response)
		if err != nil {
			return err
		}
	}
	return decode(response, result)
}

type policyDecision struct {
	Code                     int
	WorkflowChangeAuthorized bool
}

type verifier func(context.Context, int, string) (policyDecision, error)

func verifyPolicy(ctx context.Context, pr int, head string) (policyDecision, error) {
	cmd := gateCommand(ctx, "sh", "scripts/ci/check-fork-validation.sh")
	// Override only the controller-owned settings, retaining gh authentication.
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "PR_NUMBER=") && !strings.HasPrefix(value, "HEAD_SHA=") && !strings.HasPrefix(value, "PR_GATE_ONCE=") && !strings.HasPrefix(value, "PR_GATE_POLICY_ONLY=") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "PR_NUMBER="+strconv.Itoa(pr), "HEAD_SHA="+head, "PR_GATE_ONCE=1", "PR_GATE_POLICY_ONLY=1")
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, os.Stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && (exit.ExitCode() == 1 || exit.ExitCode() == 75) {
			return policyDecision{Code: exit.ExitCode()}, nil
		}
		return policyDecision{}, errors.New("trusted one-shot policy verifier could not execute")
	}
	var response struct {
		WorkflowChangeAuthorized *bool `json:"workflow_change_authorized"`
	}
	if err := decode(output.Bytes(), &response); err != nil || response.WorkflowChangeAuthorized == nil {
		return policyDecision{}, errors.New("trusted policy verifier omitted its typed workflow authority decision")
	}
	return policyDecision{WorkflowChangeAuthorized: *response.WorkflowChangeAuthorized}, nil
}

type controller struct {
	api           api
	verify        verifier
	repoName      string
	repo          repository
	workflow      workflow
	checkName     string
	expectedHead  string
	queueDeadline time.Time
	queueRefresh  *queueReassessment
	dryRun        bool
	output        io.Writer
	diagnostics   io.Writer
}

func (c *controller) endpoint(path string) string { return "repos/" + c.repoName + "/" + path }
func validWorkflow(w workflow) bool {
	return w.ID > 0 && w.Path == workflowPath && w.Name == "pr-candidate"
}
func (c *controller) init(ctx context.Context) error {
	if !repoPattern.MatchString(c.repoName) {
		return errors.New("invalid GH_REPO")
	}
	if c.checkName != "ci-required" && c.checkName != "ci-required-next" && c.checkName != queueCheckName {
		return errors.New("invalid PR_GATE_CHECK_NAME")
	}
	if err := c.api.read(ctx, "repos/"+c.repoName, &c.repo); err != nil {
		return err
	}
	if c.repo.ID <= 0 || !strings.EqualFold(c.repo.FullName, c.repoName) {
		return errors.New("repository identity mismatch")
	}
	if err := c.api.read(ctx, c.endpoint("actions/workflows/ci-candidate.yml"), &c.workflow); err != nil {
		return err
	}
	if !validWorkflow(c.workflow) {
		return errors.New("validation workflow identity mismatch")
	}
	return nil
}
func validReference(r reference) bool {
	return shaPattern.MatchString(r.SHA) && r.Ref != "" && r.Repo.ID > 0 && repoPattern.MatchString(r.Repo.FullName)
}
func (c *controller) readPRMetadata(ctx context.Context, number int) (pull, error) {
	var p pull
	if number <= 0 {
		return p, errors.New("invalid PR number")
	}
	if err := c.api.read(ctx, c.endpoint(fmt.Sprintf("pulls/%d", number)), &p); err != nil {
		return p, err
	}
	if p.Number != number || (p.State != "open" && p.State != "closed") || !validReference(p.Head) || !validReference(p.Base) || p.Base.Repo.ID != c.repo.ID || !strings.EqualFold(p.Base.Repo.FullName, c.repoName) {
		return p, errors.New("invalid current PR metadata")
	}
	return p, nil
}
func (c *controller) readPR(ctx context.Context, number int) (pull, error) {
	p, err := c.readPRMetadata(ctx, number)
	if err != nil {
		return p, err
	}
	// REST pull.base.sha may remain the branch divergence commit after main
	// advances. Resolve the authenticated branch ref on every read, including
	// final publication, instead of treating that PR metadata as the live tip.
	if p.State == "open" && p.Base.Ref == c.repo.DefaultBranch {
		var ref struct {
			Ref    string `json:"ref"`
			Object struct {
				Type string `json:"type"`
				SHA  string `json:"sha"`
			} `json:"object"`
		}
		if err := c.api.read(ctx, c.endpoint("git/ref/heads/"+url.PathEscape(c.repo.DefaultBranch)), &ref); err != nil {
			return p, err
		}
		if ref.Ref != "refs/heads/"+c.repo.DefaultBranch || ref.Object.Type != "commit" || !shaPattern.MatchString(ref.Object.SHA) {
			return p, errors.New("invalid live default-branch ref")
		}
		p.Base.SHA = ref.Object.SHA
	}
	return p, nil
}
func (c *controller) openPRs(ctx context.Context) ([]pull, error) {
	var pulls []pull
	seen := make(map[int]bool)
	for page := 1; ; page++ {
		var entries []pull
		if err := c.api.read(ctx, c.endpoint(fmt.Sprintf("pulls?state=open&per_page=100&page=%d", page)), &entries); err != nil {
			return nil, err
		}
		if entries == nil || len(entries) > 100 || len(pulls)+len(entries) > 2000 {
			return nil, errors.New("invalid open PR list")
		}
		for _, p := range entries {
			if p.Number <= 0 || seen[p.Number] || p.State != "open" || !validReference(p.Head) || !validReference(p.Base) || p.Base.Repo.ID != c.repo.ID || !strings.EqualFold(p.Base.Repo.FullName, c.repoName) {
				return nil, errors.New("invalid open PR metadata")
			}
			seen[p.Number] = true
		}
		pulls = append(pulls, entries...)
		if len(entries) < 100 {
			return pulls, nil
		}
	}
}
func associated(r run, p pull, exactBase bool) bool {
	if len(r.Pulls) == 0 {
		// GitHub omits pull_requests for some fork runs. A GitHub-recorded
		// reusable workflow merge ref and exact current merge SHA bind that PR
		// without accepting its PR-controlled title. Acceptance additionally
		// verifies the immutable merge commit and all author/workflow policy.
		if r.HeadSHA != p.Head.SHA || r.HeadRepository.ID != p.Head.Repo.ID || !shaPattern.MatchString(p.MergeCommitSHA) {
			return false
		}
		count := 0
		for _, ref := range r.References {
			parts := strings.Split(ref.Path, "@")
			if len(parts) == 2 && strings.EqualFold(parts[0], p.Base.Repo.FullName+"/.github/workflows/ci.yml") && parts[1] == ref.SHA && ref.SHA == p.MergeCommitSHA && ref.Ref == fmt.Sprintf("refs/pull/%d/merge", p.Number) {
				count++
			}
		}
		return count == 1
	}
	count := 0
	for _, a := range r.Pulls {
		if a.Number == p.Number && a.Head.SHA == p.Head.SHA && a.Head.Repo.ID == p.Head.Repo.ID && a.Base.Repo.ID == p.Base.Repo.ID {
			if !exactBase || (a.Base.Ref == p.Base.Ref) {
				count++
			}
		}
	}
	return count == 1
}
func (c *controller) validRun(r run) bool {
	if r.ID <= 0 || r.Attempt <= 0 || r.WorkflowID != c.workflow.ID || r.Path != workflowPath || r.Event != "pull_request" || r.Repository.ID != c.repo.ID || !strings.EqualFold(r.Repository.FullName, c.repoName) || !shaPattern.MatchString(r.HeadSHA) || r.HeadRepository.ID <= 0 {
		return false
	}
	switch r.Status {
	case "queued", "in_progress", "completed", "waiting", "pending", "requested":
	default:
		return false
	}
	if r.Status == "completed" && r.Conclusion == "" {
		return false
	}
	if r.Status != "completed" && r.Conclusion != "" {
		return false
	}
	return true
}
func (c *controller) latest(ctx context.Context, p pull) (*run, error) {
	var latest *run
	seen := make(map[int64]bool)
	for page := 1; page <= 10; page++ {
		var list struct {
			Total int   `json:"total_count"`
			Runs  []run `json:"workflow_runs"`
		}
		path := fmt.Sprintf("actions/workflows/ci-candidate.yml/runs?event=pull_request&head_sha=%s&per_page=100&page=%d", p.Head.SHA, page)
		if err := c.api.read(ctx, c.endpoint(path), &list); err != nil {
			return nil, err
		}
		if list.Runs == nil || list.Total < 0 || list.Total > 1000 {
			return nil, errors.New("invalid or incomplete validation run list")
		}
		for _, r := range list.Runs {
			if !c.validRun(r) || r.HeadSHA != p.Head.SHA || seen[r.ID] {
				return nil, errors.New("invalid or duplicate validation run metadata")
			}
			seen[r.ID] = true
			if r.HeadRepository.ID != p.Head.Repo.ID || !associated(r, p, false) || r.DisplayTitle != fmt.Sprintf("candidate-ci #%d", p.Number) {
				continue
			}
			if latest == nil || r.ID > latest.ID {
				copy := r
				latest = &copy
			}
		}
		if len(list.Runs) < 100 {
			if len(seen) != list.Total {
				return nil, errors.New("incomplete validation run list")
			}
			if latest != nil {
				var live run
				if err := c.api.read(ctx, c.endpoint(fmt.Sprintf("actions/runs/%d", latest.ID)), &live); err != nil {
					return nil, err
				}
				if !c.validRun(live) || !sameSourceEvent(*latest, live) || !associated(live, p, false) {
					return nil, errors.New("source run changed during selection")
				}
				latest = &live
			}
			return latest, nil
		}
	}
	return nil, errors.New("validation run pagination limit reached")
}
func (c *controller) jobs(ctx context.Context, r run) ([]job, error) {
	return c.listJobs(ctx, r, false)
}
func (c *controller) listJobs(ctx context.Context, r run, latest bool) ([]job, error) {
	var all []job
	seen := make(map[int64]bool)
	for page := 1; page <= 10; page++ {
		var list struct {
			Total int   `json:"total_count"`
			Jobs  []job `json:"jobs"`
		}
		path := fmt.Sprintf("actions/runs/%d/attempts/%d/jobs?per_page=100&page=%d", r.ID, r.Attempt, page)
		if latest {
			path = fmt.Sprintf("actions/runs/%d/jobs?filter=latest&per_page=100&page=%d", r.ID, page)
		}
		if err := c.api.read(ctx, c.endpoint(path), &list); err != nil {
			return nil, err
		}
		if list.Jobs == nil || list.Total < 0 || list.Total > 1000 {
			return nil, errors.New("invalid validation job list")
		}
		for _, j := range list.Jobs {
			if j.ID <= 0 || j.RunID != r.ID || j.Attempt <= 0 || j.Attempt > r.Attempt || (!latest && j.Attempt != r.Attempt) || j.Name == "" || seen[j.ID] {
				return nil, errors.New("invalid exact-attempt validation job")
			}
			seen[j.ID] = true
			all = append(all, j)
		}
		if len(list.Jobs) < 100 {
			if len(all) != list.Total {
				return nil, errors.New("incomplete validation job list")
			}
			return all, nil
		}
	}
	return nil, errors.New("validation job pagination limit reached")
}

func (c *controller) reusedVouch(ctx context.Context, r run) (bool, error) {
	if r.Attempt <= 1 {
		return false, nil
	}
	// A partial rerun can retain successful admission dependencies. Accept only
	// the vouch GitHub still lists in this run's current execution, never a job
	// found by searching arbitrary historical attempts. The aggregate remains
	// exact-attempt and current default-branch admission is checked separately.
	jobs, err := c.listJobs(ctx, r, true)
	if err != nil {
		return false, err
	}
	count, successful := 0, false
	for _, j := range jobs {
		if j.Name == "vouch" {
			count++
			successful = j.Attempt < r.Attempt && j.HeadSHA == r.HeadSHA && j.Status == "completed" && j.Conclusion == "success"
		}
	}
	return count == 1 && successful, nil
}
func (c *controller) ownedChecks(ctx context.Context, p pull) (checks []check, err error) {
	defer func() {
		if err != nil {
			err = c.refuseCheckIdentity(ctx, p, err)
		}
	}()
	return c.listOwnedChecks(ctx, p.Head.SHA)
}

func (c *controller) listOwnedChecks(ctx context.Context, head string) ([]check, error) {
	var own []check
	listed := 0
	seen := make(map[int64]bool)
	for page := 1; page <= 10; page++ {
		var list struct {
			Total  int     `json:"total_count"`
			Checks []check `json:"check_runs"`
		}
		if err := c.api.read(ctx, c.endpoint(fmt.Sprintf("commits/%s/check-runs?check_name=%s&filter=all&per_page=100&page=%d", head, c.checkName, page)), &list); err != nil {
			return nil, err
		}
		if list.Checks == nil || list.Total < 0 || list.Total > 1000 {
			return nil, errors.New("invalid check list")
		}
		for _, v := range list.Checks {
			if v.ID <= 0 || seen[v.ID] {
				return nil, errors.New("invalid or duplicate current check identity")
			}
			seen[v.ID] = true
			listed++
			if v.Name != c.checkName {
				continue
			}
			if !strings.HasPrefix(v.ExternalID, externalPrefix) && !strings.HasPrefix(v.ExternalID, legacyExternalPrefix) {
				if v.App.ID == actionsAppID || v.App.Slug == "github-actions" {
					return nil, errors.New("ambiguous same-name GitHub Actions check outside the controller")
				}
				continue
			}
			if v.ID <= 0 || v.App.ID != actionsAppID || v.App.Slug != "github-actions" || v.HeadSHA != head {
				return nil, errors.New("untrusted controller check identity")
			}
			b, err := decodeBinding(v.ExternalID)
			if err != nil {
				if truncatedLegacyBinding(v.ExternalID, c.repo.ID, head) {
					own = append(own, v)
					continue
				}
				return nil, err
			}
			if b.RepoID != c.repo.ID || b.Head != head {
				return nil, errors.New("controller check repository/head mismatch")
			}
			// Check contexts belong to a commit. Even a closed PR's controller
			// success must be superseded before evaluating a new PR at that SHA.
			own = append(own, v)
		}
		if listed == list.Total {
			return own, nil
		}
		if len(list.Checks) < 100 || listed > list.Total {
			return nil, errors.New("incomplete current check listing")
		}
	}
	return nil, errors.New("check pagination limit reached")
}

type snapshot struct {
	PR        pull
	Run       *run
	Collision bool
}

func (c *controller) snapshot(ctx context.Context, number int) (snapshot, error) {
	var s snapshot
	p, err := c.readPR(ctx, number)
	if err != nil {
		return s, err
	}
	s.PR = p
	if p.State != "open" {
		return s, nil
	}
	all, err := c.openPRs(ctx)
	if err != nil {
		return s, err
	}
	found := false
	for _, other := range all {
		if other.Number == p.Number {
			found = true
			if other.Head != p.Head || other.Base.Ref != p.Base.Ref || other.Base.Repo != p.Base.Repo {
				return s, &prStateMovement{phase: "collision scan"}
			}
		}
		if other.Number != p.Number && other.Head.SHA == p.Head.SHA {
			s.Collision = true
		}
	}
	if !found {
		return s, errors.New("open PR missing from collision scan")
	}
	s.Run, err = c.latest(ctx, p)
	return s, err
}
func (s snapshot) binding(repoID int64) binding {
	b := binding{RepoID: repoID, PR: s.PR.Number, Head: s.PR.Head.SHA, BaseRef: sha256.Sum256([]byte(s.PR.Base.Ref)), Base: s.PR.Base.SHA}
	if s.Run != nil {
		b.Run, b.Attempt = s.Run.ID, s.Run.Attempt
	}
	return b
}

type outcome struct{ Status, Conclusion, Title, Summary string }

func pending(summary string) outcome {
	return outcome{Status: "queued", Title: "PR validation pending", Summary: summary}
}
func refused(summary string) outcome {
	return outcome{Status: "completed", Conclusion: "failure", Title: "PR validation refused", Summary: summary}
}
func (c *controller) evaluate(ctx context.Context, s snapshot) (outcome, error) {
	if s.PR.Base.Ref != c.repo.DefaultBranch {
		return refused("The full candidate gate only authorizes the current default branch."), nil
	}
	policy, err := c.verify(ctx, s.PR.Number, s.PR.Head.SHA)
	if err != nil {
		return outcome{}, err
	}
	code := policy.Code
	if code != 0 && code != 1 && code != 75 {
		return outcome{}, errors.New("invalid one-shot verifier result")
	}
	if s.Collision {
		return refused("Another open PR shares this head. A commit-level check cannot safely authorize distinct PR policy and integration candidates."), nil
	}
	if code == 75 {
		return pending("Trusted policy verification is still pending; validation success alone cannot authorize the merge."), nil
	}
	if code == 1 {
		return refused("The trusted exact-head policy verifier refused this PR. Inspect the controller workflow log for the policy or validation failure."), nil
	}
	if s.Run == nil {
		return pending("No API-associated PR validation run exists yet. The completion callback or scheduled repair will reconcile it."), nil
	}
	if !associated(*s.Run, s.PR, true) {
		return pending("The latest validation run targets a different base revision. Fresh validation of the current integration candidate is required."), nil
	}
	r := *s.Run
	if r.Status != "completed" || r.Conclusion == "action_required" {
		return pending(fmt.Sprintf("Validation run %d attempt %d is %s. No runner is held waiting for completion.", r.ID, r.Attempt, r.Status)), nil
	}
	jobs, err := c.jobs(ctx, r)
	if err != nil {
		return outcome{}, err
	}
	aggregates, candidates, vouches := 0, 0, 0
	aggregateOK, candidateOK, candidateSourceOK, vouchOK := false, false, false, false
	for _, j := range jobs {
		if j.Name == "validation / ci-required" {
			aggregates++
			aggregateOK = j.Status == "completed" && j.Conclusion == "success"
		}
		if j.Name == "candidate-proof" {
			candidates++
			candidateSourceOK = j.Status == "completed" && j.HeadSHA == r.HeadSHA
			candidateOK = candidateSourceOK && j.Conclusion == "success"
		}
		if j.Name == "vouch" {
			vouches++
			vouchOK = j.Status == "completed" && j.Conclusion == "success"
		}
	}
	if aggregates != 1 {
		return refused("Validation must contain exactly one validation / ci-required aggregate for the current attempt."), nil
	}
	if candidates != 1 || !candidateSourceOK {
		return refused("Validation requires exactly one successful candidate-proof job for this source head and attempt. Earlier selective or head-only runs cannot authorize merge."), nil
	}
	// A completed marker from this exact source/attempt can fail because main
	// moved during validation. Check its immutable integration before deciding
	// whether that failure belongs to the current candidate. Invalid marker
	// identity and unreadable provenance still fail closed above and below.
	proved, err := c.proveIntegration(ctx, r, s.PR)
	if err != nil {
		return outcome{}, err
	}
	if !proved {
		return pending("The validation run does not prove the current base/head integration candidate. Fresh validation is required."), nil
	}
	if !candidateOK {
		return refused("Validation requires a successful candidate-proof job for the current integration candidate."), nil
	}
	if r.Conclusion != "success" || !aggregateOK {
		return refused(fmt.Sprintf("Validation run %d attempt %d failed or did not produce a successful aggregate.", r.ID, r.Attempt)), nil
	}
	if s.PR.Head.Repo.ID != c.repo.ID {
		if vouches == 0 {
			vouchOK, err = c.reusedVouch(ctx, r)
			if err != nil {
				return outcome{}, err
			}
		} else if vouches != 1 {
			vouchOK = false
		}
		if !vouchOK {
			return refused("A fork requires exactly one successful author-vouch job in the current attempt or proved reused by GitHub's current execution."), nil
		}
	}
	if s.PR.Head.Repo.ID != c.repo.ID {
		vouched, err := c.currentVouch(ctx, s.PR)
		if err != nil {
			return outcome{}, err
		}
		if !vouched {
			return refused("The fork author is no longer vouched by current default-branch policy or current collaborator permission."), nil
		}
	}
	contract, err := c.proveWorkflowContract(ctx, s.PR, policy.WorkflowChangeAuthorized)
	if err != nil {
		return outcome{}, err
	}
	if !contract {
		return refused("Candidate workflow or reusable validation differs from the trusted base without current exact-head workflow-change authority."), nil
	}
	return outcome{Status: "completed", Conclusion: "success", Title: "PR validation passed", Summary: fmt.Sprintf("Trusted policy and validation run %d attempt %d passed for PR #%d head %s against %s at %s.", r.ID, r.Attempt, s.PR.Number, s.PR.Head.SHA, s.PR.Base.Ref, s.PR.Base.SHA)}, nil
}

// Trustdown parsing follows mitchellh/vouch's file.nu and lib.nu at
// d66fa29a64600490892131ad87597c30c91fcac4: platformless entries default to
// github; matching is case-insensitive; the first match decides. Unsupported
// malformed handles are rejected instead of silently authorizing a user.
func trustdownVouched(contents, login string) (bool, error) {
	type entry struct {
		user, platform string
		deny           bool
	}
	var entries []entry
	for _, line := range strings.Split(contents, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		deny := strings.HasPrefix(line, "-")
		if deny {
			line = strings.TrimPrefix(line, "-")
		}
		handle, _, _ := strings.Cut(line, " ")
		parts := strings.Split(strings.ToLower(handle), ":")
		platform, user := "github", parts[0]
		if len(parts) == 2 {
			platform, user = parts[0], parts[1]
		}
		if len(parts) > 2 || platform == "" || user == "" || strings.ContainsAny(handle, "\t\r\x00") || strings.HasPrefix(user, "-") {
			return false, errors.New("unsupported malformed Trustdown entry")
		}
		entries = append(entries, entry{user: user, platform: platform, deny: deny})
	}
	for _, e := range entries {
		if e.platform == "github" && e.user == strings.ToLower(login) {
			return !e.deny, nil
		}
	}
	return false, nil
}
func (c *controller) currentVouch(ctx context.Context, p pull) (bool, error) {
	login := p.User.Login
	if p.User.ID <= 0 || !regexp.MustCompile(`^[A-Za-z0-9-]+(\[bot\])?$`).MatchString(login) {
		return false, errors.New("invalid fork author identity")
	}
	if strings.HasSuffix(login, "[bot]") {
		return true, nil
	}
	var permission struct {
		Permission string `json:"permission"`
		User       struct {
			ID int64 `json:"id"`
		} `json:"user"`
	}
	if err := c.api.read(ctx, c.endpoint("collaborators/"+login+"/permission"), &permission); err != nil {
		return false, err
	}
	if permission.User.ID != p.User.ID || permission.Permission == "" {
		return false, errors.New("invalid current collaborator permission response")
	}
	if permission.Permission == "admin" || permission.Permission == "write" {
		return true, nil
	}
	if c.repo.DefaultBranch == "" {
		return false, errors.New("missing trusted default branch for current vouch policy")
	}
	var file struct {
		Type     string `json:"type"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
		Path     string `json:"path"`
	}
	if err := c.api.read(ctx, c.endpoint("contents/.github/VOUCHED.td?ref="+url.QueryEscape(c.repo.DefaultBranch)), &file); err != nil {
		return false, err
	}
	if file.Type != "file" || file.Encoding != "base64" || file.Path != ".github/VOUCHED.td" {
		return false, errors.New("invalid default-branch vouch file metadata")
	}
	contents, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(file.Content, "\n", ""))
	if err != nil {
		return false, errors.New("invalid default-branch vouch file contents")
	}
	return trustdownVouched(string(contents), login)
}
func (c *controller) proveIntegration(ctx context.Context, r run, p pull) (bool, error) {
	// API PR associations can change after retargets. GitHub records the actual
	// reusable workflow merge commit. Compare it with GitHub's current merge
	// candidate. The first parent must be the current base itself: a synthetic
	// stacked base is not proof of testing the exact current-main candidate.
	var mergeSHA string
	count := 0
	for _, ref := range r.References {
		parts := strings.Split(ref.Path, "@")
		if len(parts) != 2 || !strings.EqualFold(parts[0], c.repoName+"/.github/workflows/ci.yml") {
			continue
		}
		count++
		if ref.Ref != fmt.Sprintf("refs/pull/%d/merge", p.Number) || !shaPattern.MatchString(ref.SHA) || parts[1] != ref.SHA {
			return false, errors.New("invalid reusable validation workflow source")
		}
		mergeSHA = ref.SHA
	}
	if count == 0 {
		return false, nil
	}
	if count != 1 {
		return false, errors.New("ambiguous reusable validation workflow sources")
	}
	if p.Mergeable == nil || !*p.Mergeable || !shaPattern.MatchString(p.MergeCommitSHA) || mergeSHA != p.MergeCommitSHA {
		return false, nil
	}
	var commit struct {
		SHA     string `json:"sha"`
		Parents []struct {
			SHA string `json:"sha"`
		} `json:"parents"`
	}
	if err := c.api.read(ctx, c.endpoint("git/commits/"+mergeSHA), &commit); err != nil {
		return false, err
	}
	if commit.SHA != mergeSHA || commit.Parents == nil {
		return false, errors.New("invalid immutable integration commit")
	}
	return len(commit.Parents) == 2 && commit.Parents[0].SHA == p.Base.SHA && commit.Parents[1].SHA == p.Head.SHA, nil
}

func (c *controller) proveWorkflowContract(ctx context.Context, p pull, authorizedChange bool) (bool, error) {
	// A forged marker and aggregate are not proof. Normally the immutable
	// candidate's workflow contract must equal the trusted base byte for byte.
	// Differences retain the existing exact-head workflow approval/BDFL policy;
	// they never obtain authority just by producing successful job names.
	for _, path := range []string{workflowPath, ".github/workflows/ci.yml"} {
		var versions [][]byte
		for _, revision := range []string{p.Base.SHA, p.MergeCommitSHA} {
			var file struct {
				Type     string `json:"type"`
				Path     string `json:"path"`
				Encoding string `json:"encoding"`
				Content  string `json:"content"`
			}
			if err := c.api.read(ctx, c.endpoint("contents/"+path+"?ref="+revision), &file); err != nil {
				return false, err
			}
			if file.Type != "file" || file.Path != path || file.Encoding != "base64" {
				return false, errors.New("invalid immutable workflow contract response")
			}
			data, err := base64.StdEncoding.DecodeString(file.Content)
			if err != nil || len(data) == 0 {
				return false, errors.New("invalid immutable workflow contract bytes")
			}
			versions = append(versions, data)
		}
		if !bytes.Equal(versions[0], versions[1]) && !authorizedChange {
			return false, nil
		}
	}
	return true, nil
}
func sameSnapshot(a, b snapshot, repoID int64) bool {
	if a.PR.State != b.PR.State || a.PR.Head != b.PR.Head || a.PR.Base != b.PR.Base || a.PR.MergeCommitSHA != b.PR.MergeCommitSHA || a.Collision != b.Collision || a.binding(repoID) != b.binding(repoID) {
		return false
	}
	if a.Run == nil {
		return b.Run == nil
	}
	return b.Run != nil && a.Run.Status == b.Run.Status && a.Run.Conclusion == b.Run.Conclusion && associated(*b.Run, b.PR, true) == associated(*a.Run, a.PR, true)
}

// prStateMovement identifies only observed authenticated metadata movement.
// API, malformed identity and immutable proof failures never use this type.
type prStateMovement struct{ phase string }

func (e *prStateMovement) Error() string { return "PR state changed during " + e.phase }

func (c *controller) reconcile(ctx context.Context, number int, callback *run) error {
	err := c.reconcileCurrent(ctx, number, callback)
	var moved *prStateMovement
	if !errors.As(err, &moved) {
		return err
	}
	// Recover once, without evaluating success or retrying a mutable proof. An
	// expected transition is operationally healthy only after current PR policy
	// is blocked; publication/listing failures still fail the controller.
	return c.blockMovement(ctx, number, moved.phase)
}

func (c *controller) blockMovement(ctx context.Context, number int, phase string) error {
	p, err := c.readPRMetadata(ctx, number)
	if err != nil {
		return err
	}
	if p.State != "open" {
		return nil
	}
	own, err := c.ownedChecks(ctx, p)
	if err != nil {
		return err
	}
	var selected *check
	for _, v := range own {
		if selected == nil || v.ID > selected.ID {
			copy := v
			selected = &copy
		}
	}
	// The raw PR base may precede live main. This Run0 barrier carries no
	// validation authority; a later reconciliation must prove the live candidate.
	b := (snapshot{PR: p}).binding(c.repo.ID)
	if err := c.publish(ctx, p, b, selected, pending("PR state changed during "+phase+"; fresh candidate reconciliation is required.")); err != nil {
		return err
	}
	current, err := c.readPRMetadata(ctx, number)
	if err != nil {
		return err
	}
	if current.State != "open" {
		return nil
	}
	if current.Head != p.Head || current.Base != p.Base {
		return errors.New("PR changed during pending recovery; current identity could not be confirmed blocked")
	}
	_, err = fmt.Fprintf(c.output, "PR #%d: pending after authenticated state movement during %s\n", number, phase)
	return err
}

func (c *controller) reconcileCurrent(ctx context.Context, number int, callback *run) error {
	// Revoke our prior success before querying mutable source/policy APIs. If
	// those later reads fail, an earlier result must not keep authorizing merge.
	if err := c.invalidateSuccess(ctx, number); err != nil {
		return err
	}
	s, err := c.snapshot(ctx, number)
	if err != nil {
		return err
	}
	if s.PR.State != "open" {
		return nil
	}
	if callback != nil && (s.Run == nil || s.Run.ID != callback.ID || s.Run.Attempt != callback.Attempt || s.PR.Head.SHA != callback.HeadSHA) {
		own, err := c.ownedChecks(ctx, s.PR)
		if err != nil {
			return err
		}
		if len(own) > 0 {
			return c.reconcileCurrent(ctx, number, nil)
		}
		return nil
	}
	own, err := c.ownedChecks(ctx, s.PR)
	if err != nil {
		return err
	}
	result, err := c.evaluate(ctx, s)
	if err != nil {
		return err
	}
	// Repeat authorization just before publication, including current maintainer
	// approval. Both reads and checks are intentionally bounded; this gate has
	// no read retries (those are queue-only) and never sleeps.
	var finalPolicy policyDecision
	if result.Conclusion == "success" {
		decision, err := c.verify(ctx, number, s.PR.Head.SHA)
		if err != nil {
			return err
		}
		finalPolicy = decision
		switch decision.Code {
		case 0:
		case 1:
			result = refused("Trusted policy was refused during the final authorization check.")
		case 75:
			result = pending("Trusted policy is pending during the final authorization check.")
		default:
			return errors.New("invalid final one-shot verifier result")
		}
		if result.Conclusion == "success" && s.PR.Head.Repo.ID != c.repo.ID {
			vouched, err := c.currentVouch(ctx, s.PR)
			if err != nil {
				return err
			}
			if !vouched {
				result = refused("Fork author admission was revoked before publication.")
			}
		}
	}
	final, err := c.snapshot(ctx, number)
	if err != nil {
		return err
	}
	if !sameSnapshot(s, final, c.repo.ID) {
		return &prStateMovement{phase: "final publication"}
	}
	if result.Conclusion == "success" {
		proved, err := c.proveIntegration(ctx, *final.Run, final.PR)
		if err != nil {
			return err
		}
		if !proved {
			return errors.New("integration candidate changed before success publication")
		}
		contract, err := c.proveWorkflowContract(ctx, final.PR, finalPolicy.WorkflowChangeAuthorized)
		if err != nil {
			return err
		}
		if !contract {
			return errors.New("workflow contract authority changed before success publication")
		}
	}
	own, err = c.ownedChecks(ctx, final.PR)
	if err != nil {
		return err
	}
	var selected *check
	currentBinding := final.binding(c.repo.ID)
	for _, v := range own {
		prior, decodeErr := decodeBinding(v.ExternalID)
		if decodeErr == nil && prior.PR == currentBinding.PR && knownNewerLegacySource(prior, currentBinding) {
			return errors.New("owned observation proves a newer source generation than the final source snapshot")
		}
		if selected == nil || v.ID > selected.ID {
			copy := v
			selected = &copy
		}
	}
	externalID := final.binding(c.repo.ID).externalID()
	if selected != nil && selected.ExternalID == externalID && selected.Status == result.Status && selected.Conclusion == result.Conclusion && selected.Output.Summary == result.Summary {
		return nil
	}
	return c.publish(ctx, final.PR, final.binding(c.repo.ID), selected, result)
}
func (c *controller) publish(ctx context.Context, p pull, b binding, selected *check, result outcome) error {
	if c.checkName == queueCheckName || c.checkName == lifecycleName {
		_, err := c.publishQueueRecord(ctx, p, b, selected, result)
		if err == nil && c.queueRefresh != nil && result.Status == "completed" {
			c.queueRefresh.verifiedTerminal = true
		}
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !shaPattern.MatchString(b.Head) || !shaPattern.MatchString(b.Base) {
		return errors.New("refuse invalid publication binding")
	}
	externalID := b.externalID()
	decoded, err := decodeBinding(externalID)
	if err != nil {
		return fmt.Errorf("refuse invalid publication binding: %w", err)
	}
	if decoded != b {
		return errors.New("refuse invalid publication binding")
	}
	if selected != nil {
		old, oldErr := decodeBinding(selected.ExternalID)
		sameIdentity := oldErr == nil && c.authenticatedQueueCheck(selected, b.Head) && old.RepoID == b.RepoID && old.PR == b.PR && old.Head == b.Head
		if sameIdentity && knownNewerLegacySource(old, b) {
			return errors.New("legacy publication cannot replace a known newer source generation")
		}
		// The pre-ref-read barrier carries PR metadata, whose base SHA can be
		// stale. An unbound barrier can acquire its first proved binding, but
		// a bound observation retains its original run/attempt provenance.
		placeholder := sameIdentity && selected.Status != "completed" && blockingQueueCheck(selected) && old.Run == 0 && old.BaseRef == b.BaseRef
		if selected.Status == "completed" || !sameIdentity {
			selected = nil
		} else if !blockingQueueCheck(selected) {
			return errors.New("legacy publication requires an authenticated nonterminal observation")
		} else if selected.ExternalID != externalID && !placeholder {
			// Complete the superseded generation before creating its successor.
			// Otherwise a later terminal rollup can hide this unfinished record
			// permanently when a close/synchronize callback is missed.
			if err := c.supersedeLegacyObservation(ctx, old, *selected); err != nil {
				return err
			}
			selected = nil
		}
	}
	payload, err := c.checkPayload(b, result)
	if err != nil {
		return err
	}
	method, path := "POST", c.endpoint("check-runs")
	if selected != nil {
		method, path = "PATCH", c.endpoint(fmt.Sprintf("check-runs/%d", selected.ID))
	} else {
		payload["head_sha"] = p.Head.SHA
	}
	if c.dryRun {
		_, err := fmt.Fprintf(c.output, "DRY RUN PR #%d: %s %s (%s), %s\n", p.Number, c.checkName, result.Status, result.Conclusion, result.Summary)
		return err
	}
	var written check
	if err := c.api.write(ctx, method, path, payload, &written); err != nil {
		return err
	}
	if written.ID <= 0 || written.App.ID != actionsAppID || written.App.Slug != "github-actions" || written.ExternalID != externalID || written.HeadSHA != p.Head.SHA || written.Name != c.checkName || written.Status != result.Status || written.Conclusion != result.Conclusion || selected != nil && written.ID != selected.ID {
		return errors.New("GitHub returned an unexpected check identity/result; no mutation retry attempted")
	}
	return nil
}

// latest already orders independent validation runs by ID. An existing owned
// binding can reveal a later run/attempt than lagging source API snapshots.
// Run0 carries no source authority and may only establish a pending barrier.
func knownNewerLegacySource(prior, current binding) bool {
	return prior.RepoID == current.RepoID && prior.Head == current.Head && prior.BaseRef == current.BaseRef && prior.Run > 0 && current.Run > 0 && (prior.Run > current.Run || prior.Run == current.Run && prior.Attempt > current.Attempt)
}

func (c *controller) supersedeLegacyObservation(ctx context.Context, b binding, selected check) error {
	if c.dryRun {
		return nil
	}
	path := c.endpoint(fmt.Sprintf("check-runs/%d", selected.ID))
	var current check
	if err := c.api.read(ctx, path, &current); err != nil {
		return err
	}
	if current != selected {
		return errors.New("superseded observation changed before retirement; no mutation attempted")
	}
	result := outcome{Status: "completed", Conclusion: "cancelled", Title: "Superseded observation", Summary: "A new source binding supersedes this unfinished observation. Retiring its original generation without granting validation proof."}
	payload, err := c.checkPayload(b, result)
	if err != nil {
		return err
	}
	payload["external_id"] = selected.ExternalID
	delete(payload, "details_url")
	var written check
	if err := c.api.write(ctx, "PATCH", path, payload, &written); err != nil {
		return err // Never retry an uncertain mutation or create its successor.
	}
	if !c.authenticatedQueueCheck(&written, b.Head) || written.ID != selected.ID || written.ExternalID != selected.ExternalID || written.Status != "completed" || written.Conclusion != "cancelled" {
		return errors.New("superseded observation retirement returned an unexpected identity/result")
	}
	var fresh check
	if err := c.api.read(ctx, path, &fresh); err != nil {
		return err
	}
	if fresh != written {
		return errors.New("superseded observation retirement readback disagrees")
	}
	return nil
}
func (c *controller) refuseCheckIdentity(ctx context.Context, p pull, err error) error {
	// A native same-name check can hide an older owned success. Listing or
	// identity refusal must still leave a fresh owned pending barrier; never
	// modify the foreign check or treat its green result as proof.
	current, readErr := c.readPRMetadata(ctx, p.Number)
	if readErr != nil {
		// The caller already authenticated p. A refresh failure cannot make
		// its prior green trustworthy; pending against that identity is safe.
		err = errors.Join(err, readErr)
		current = p
	}
	if current.State != "open" {
		return err
	}
	// A concurrent retarget/head change is also pending. Bind only the fresh
	// authenticated identity; a pending barrier never authorizes merge.
	b := (snapshot{PR: current}).binding(c.repo.ID)
	if writeErr := c.publish(ctx, current, b, nil, pending("Check identity or listing could not be verified; refusing merge proof.")); writeErr != nil {
		return errors.Join(err, writeErr)
	}
	return err
}
func (c *controller) invalidateSuccess(ctx context.Context, number int) error {
	p, err := c.readPRMetadata(ctx, number)
	if err != nil {
		return err
	}
	if p.State != "open" {
		return nil
	}
	own, err := c.ownedChecks(ctx, p)
	if err != nil {
		return err
	}
	var latest *check
	for _, v := range own {
		if latest == nil || v.ID > latest.ID {
			copy := v
			latest = &copy
		}
	}
	if latest == nil || strings.HasPrefix(latest.ExternalID, legacyExternalPrefix) || (latest.Status == "completed" && latest.Conclusion == "success") {
		current, err := c.readPRMetadata(ctx, number)
		if err != nil {
			return err
		}
		if current.State != "open" || current.Head != p.Head || current.Base != p.Base {
			return &prStateMovement{phase: "initial pending barrier"}
		}
		b := (snapshot{PR: p}).binding(c.repo.ID)
		if err := c.publish(ctx, p, b, latest, pending("Rechecking current policy, validation attempt and integration candidate.")); err != nil {
			return err
		}
	}
	return nil
}
func sameSourceEvent(a, b run) bool {
	return a.ID == b.ID && a.Attempt == b.Attempt && a.WorkflowID == b.WorkflowID && a.Path == b.Path && a.Event == b.Event && a.HeadSHA == b.HeadSHA && a.Repository.ID == b.Repository.ID && a.HeadRepository.ID == b.HeadRepository.ID
}
func (c *controller) execute(ctx context.Context, eventName string, e event) (err error) {
	if err := c.init(ctx); err != nil {
		return err
	}
	if e.Repository.ID != c.repo.ID || !strings.EqualFold(e.Repository.FullName, c.repoName) {
		return errors.New("event repository identity mismatch")
	}
	// Reuse this observer's existing global writer lease and setup. Retirement
	// follows current-head publication so old-head maintenance cannot delay it.
	if eventName == "pull_request_target" && c.checkName == "ci-required-next" && (e.Action == "synchronize" || e.Action == "closed") {
		head, routeErr := retirementHead(eventName, e)
		if routeErr != nil {
			return routeErr
		}
		defer func() { err = errors.Join(err, c.retireChecks(ctx, head)) }()
	}
	switch eventName {
	case "pull_request_target":
		if e.Number <= 0 || e.Pull.Number != e.Number {
			return errors.New("invalid pull_request_target event")
		}
		p, err := c.readPRMetadata(ctx, e.Number)
		if err != nil {
			return err
		}
		if p.Head.SHA != e.Pull.Head.SHA || p.Base.Ref != e.Pull.Base.Ref || p.Head.Repo.ID != e.Pull.Head.Repo.ID || p.Base.Repo.ID != e.Pull.Base.Repo.ID {
			return nil
		}
		return c.reconcile(ctx, e.Number, nil)
	case "workflow_dispatch":
		number, err := strconv.Atoi(e.Inputs.PR)
		if err != nil || number <= 0 {
			return errors.New("workflow_dispatch requires inputs.pr_number")
		}
		return c.reconcile(ctx, number, nil)
	case "workflow_run":
		if !c.validRun(e.Run) {
			return errors.New("invalid callback source metadata")
		}
		if strings.HasPrefix(e.Run.DisplayTitle, "pr-metadata #") {
			return nil
		}
		// Event associations are hints for revocation only. They never authorize
		// success; the live source and current PR are checked below.
		hints := e.Run.Pulls
		if len(hints) == 0 {
			all, err := c.openPRs(ctx)
			if err != nil {
				return err
			}
			for _, p := range all {
				if p.Head.SHA == e.Run.HeadSHA && p.Head.Repo.ID == e.Run.HeadRepository.ID {
					hints = append(hints, p)
				}
			}
		}
		for _, a := range hints {
			if a.Number <= 0 {
				return errors.New("invalid callback PR hint")
			}
			p, err := c.readPRMetadata(ctx, a.Number)
			if err != nil {
				return err
			}
			if p.Head.SHA != e.Run.HeadSHA || p.Head.Repo.ID != e.Run.HeadRepository.ID {
				continue
			}
			if err := c.invalidateSuccess(ctx, a.Number); err != nil {
				return err
			}
		}
		var live run
		if err := c.api.read(ctx, c.endpoint(fmt.Sprintf("actions/runs/%d", e.Run.ID)), &live); err != nil {
			return err
		}
		if !c.validRun(live) {
			return errors.New("invalid live callback source")
		}
		if live.Attempt != e.Run.Attempt {
			return c.repairHints(ctx, hints)
		}
		if !sameSourceEvent(e.Run, live) {
			return errors.New("callback differs from live source identity")
		}
		if len(live.Pulls) == 0 {
			// Routing by head alone authorizes only reevaluation. A terminal pass
			// still needs associated's immutable reusable merge-ref fallback.
			for _, p := range hints {
				if err := c.reconcile(ctx, p.Number, nil); err != nil {
					return err
				}
			}
			return nil
		}
		seen := make(map[int]bool)
		for _, a := range live.Pulls {
			if a.Number <= 0 || seen[a.Number] {
				return errors.New("invalid or ambiguous callback PR association")
			}
			seen[a.Number] = true
			p, err := c.readPRMetadata(ctx, a.Number)
			if err != nil {
				return err
			}
			if p.State != "open" || p.Head.SHA != live.HeadSHA {
				if err := c.repairHints(ctx, []pull{a}); err != nil {
					return err
				}
				continue
			}
			if !associated(live, p, true) || live.HeadRepository.ID != p.Head.Repo.ID {
				if err := c.repairHints(ctx, []pull{a}); err != nil {
					return err
				}
				continue
			}
			if err := c.reconcile(ctx, a.Number, &live); err != nil {
				return err
			}
		}
		return nil
	case "schedule", "push":
		if eventName == "push" && e.Ref != "refs/heads/"+c.repo.DefaultBranch {
			return nil
		}
		pulls, err := c.openPRs(ctx)
		if err != nil {
			return err
		}
		var failures []error
		// Repair existing terminal checks too: a missed rerun/retarget event must
		// not leave an earlier success authorizing a newer integration candidate.
		for _, p := range pulls {
			if err := c.reconcile(ctx, p.Number, nil); err != nil {
				failures = append(failures, fmt.Errorf("PR #%d: %w", p.Number, err))
			}
		}
		return errors.Join(failures...)
	default:
		return errors.New("unsupported trusted controller event")
	}
}
func (c *controller) repairHints(ctx context.Context, hints []pull) error {
	for _, hint := range hints {
		p, err := c.readPRMetadata(ctx, hint.Number)
		if err != nil {
			return err
		}
		if p.State != "open" {
			continue
		}
		own, err := c.ownedChecks(ctx, p)
		if err != nil {
			return err
		}
		if len(own) > 0 {
			if err := c.reconcile(ctx, p.Number, nil); err != nil {
				return err
			}
		}
	}
	return nil
}
func main() {
	discoverRetirement := flag.Bool("discover-retirements", false, "discover complete bounded main-history retirement targets without writing checks")
	retire := flag.Bool("retire-checks", false, "retire only owned unfinished checks on an exact inactive SHA under its existing writer lease")
	lifecycleProbe := flag.Bool("check-lifecycle-probe", false, "verify only the fixed non-required check lifecycle on trusted main")
	dryRun := flag.Bool("dry-run", false, "read policy and live metadata without publishing checks")
	queueMode := flag.Bool("queue", false, "publish the separately protected merge-queue context")
	targeted := flag.Bool("targeted", false, "bound only a targeted queue admission invocation")
	repairOnly := flag.Bool("repair-groups-only", false, "repair queue groups only on authenticated manual dispatch")
	expectedHead := flag.String("expected-head", "", "exact SHA held by the protected per-head publisher concurrency lease")
	dispatch := flag.Bool("dispatch-workers", false, "discover complete work and dispatch only fixed default-branch head workers")
	headWorker := flag.String("head-worker", "", "reconcile one typed discovery record under its exact head lease")
	flag.Parse()
	if *discoverRetirement && (*retire || *lifecycleProbe || *queueMode || *targeted || *repairOnly || *dispatch || *headWorker != "" || *expectedHead != "") {
		fmt.Fprintln(os.Stderr, "retirement discovery is read-only and cannot hold or mix writer roles")
		os.Exit(1)
	}
	if *retire && (*lifecycleProbe || *queueMode || *targeted || *repairOnly || *dispatch || *headWorker != "" || !shaPattern.MatchString(*expectedHead)) {
		fmt.Fprintln(os.Stderr, "retirement requires an exact head and cannot mix producer roles")
		os.Exit(1)
	}
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected positional arguments")
		os.Exit(1)
	}
	if *lifecycleProbe && (*dryRun || *queueMode || *targeted || *repairOnly || *dispatch || *headWorker != "" || *expectedHead != "") {
		fmt.Fprintln(os.Stderr, "lifecycle probe cannot mix publication roles or caller-selected heads")
		os.Exit(1)
	}
	if (*targeted || *repairOnly) && !*queueMode || *targeted && *repairOnly {
		fmt.Fprintln(os.Stderr, "targeted and group-only modes require queue mode and cannot be combined")
		os.Exit(1)
	}
	if (*dispatch || *headWorker != "") && !*queueMode || *dispatch && (*targeted || *repairOnly || *headWorker != "" || *expectedHead != "") || *headWorker != "" && (*targeted || *repairOnly) {
		fmt.Fprintln(os.Stderr, "discovery/head worker modes require queue mode and cannot mix producer roles")
		os.Exit(1)
	}
	if *headWorker != "" && *headWorker != "admission" && *headWorker != "group" && *headWorker != "ambiguous" {
		fmt.Fprintln(os.Stderr, "invalid head worker role")
		os.Exit(1)
	}
	if *queueMode && !*dispatch && !shaPattern.MatchString(*expectedHead) {
		fmt.Fprintln(os.Stderr, "queue writers require a validated expected head; unleased batch publication is disabled")
		os.Exit(1)
	}
	boundedAdmission := *targeted || *headWorker != "" && *headWorker != "group"
	if boundedAdmission && runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		fmt.Fprintln(os.Stderr, "targeted publisher requires supported Unix process-group cancellation")
		os.Exit(1)
	}
	checkName := os.Getenv("PR_GATE_CHECK_NAME")
	if checkName == "" {
		checkName = "ci-required-next"
	}
	if *queueMode {
		checkName = queueCheckName
	}
	data, err := os.ReadFile(os.Getenv("GITHUB_EVENT_PATH"))
	var e event
	if err == nil {
		err = decode(data, &e)
	}
	if err == nil {
		c := controller{api: controllerAPI(*queueMode, *dispatch), verify: verifyPolicy, repoName: os.Getenv("GH_REPO"), checkName: checkName, expectedHead: *expectedHead, dryRun: *dryRun, output: os.Stdout, diagnostics: os.Stderr}
		if *discoverRetirement {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			var targets retirementTargets
			targets, err = c.discoverRetirements(ctx, os.Getenv("GITHUB_EVENT_NAME"), e)
			if err == nil {
				err = json.NewEncoder(c.output).Encode(targets)
			}
		} else if *retire {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			err = c.retireEvent(ctx, os.Getenv("GITHUB_EVENT_NAME"), e)
		} else if *lifecycleProbe {
			receipt := lifecycleReceipt{Version: 2, Protocol: lifecycleProtocol}
			path := os.Getenv("PR_GATE_PROBE_RECEIPT")
			if path == "" {
				err = errors.New("lifecycle probe receipt path is required before mutation")
			} else {
				err = c.checkLifecycle(context.Background(), os.Getenv("GITHUB_EVENT_NAME"), e, &receipt)
				if err != nil {
					receipt.Error = err.Error()
				}
				body, encodeErr := json.MarshalIndent(receipt, "", "  ")
				if encodeErr == nil {
					encodeErr = os.WriteFile(path, append(body, '\n'), 0600)
				}
				err = errors.Join(err, encodeErr)
			}
		} else if *queueMode {
			q := &queueController{controller: &c, targeted: *targeted, repairOnly: *repairOnly}
			if *dispatch {
				err = q.dispatchWorkers(context.Background(), os.Getenv("GITHUB_EVENT_NAME"), e)
			} else if boundedAdmission {
				var budget admissionBudget
				budget, err = parseAdmissionBudget(os.Getenv("PR_GATE_STARTED_AT"), os.Getenv("PR_GATE_DEADLINE"), time.Now())
				if err == nil {
					if *headWorker != "" {
						err = q.withAdmissionBudget(context.Background(), budget, func(ctx context.Context) error { return q.runHead(ctx, os.Getenv("GITHUB_EVENT_NAME"), e, *headWorker) })
					} else {
						err = q.executeTargeted(context.Background(), os.Getenv("GITHUB_EVENT_NAME"), e, budget)
					}
				}
			} else {
				if *headWorker != "" {
					err = q.runHead(context.Background(), os.Getenv("GITHUB_EVENT_NAME"), e, *headWorker)
				} else {
					err = q.execute(context.Background(), os.Getenv("GITHUB_EVENT_NAME"), e)
				}
			}
		} else {
			if checkName == queueCheckName {
				err = errors.New("queue context requires --queue")
			} else {
				err = c.execute(context.Background(), os.Getenv("GITHUB_EVENT_NAME"), e)
			}
		}
	}
	if err != nil {
		if onlyStaleHeadWork(err) {
			fmt.Fprintln(os.Stdout, err)
			return
		}
		fmt.Fprintln(os.Stderr, "trusted PR gate:", err)
		os.Exit(1)
	}
}
