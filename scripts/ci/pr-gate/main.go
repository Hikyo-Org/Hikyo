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
	"encoding/json/jsontext"
	"encoding/json/v2"
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

// stableJSON reproduces encoding/json (v1) output bytes. The publisher is built
// from a sparse checkout of scripts/ci, so it cannot import internal/jsonwire and
// keeps its own copy of those options; internal/lint fails when the two option
// lists differ.
var stableJSON = json.JoinOptions(
	json.Deterministic(true),
	json.FormatNilSliceAsNull(true),
	json.FormatNilMapAsNull(true),
	jsontext.EscapeForHTML(true),
	jsontext.EscapeForJS(true),
	jsontext.PreserveRawStrings(true),
)

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
	Stack          stack     `json:"stack,omitzero"`
	StackInvalid   bool      `json:"-"`
	DevTip         string    `json:"-"`
	MergeCommitSHA string    `json:"merge_commit_sha"`
	Mergeable      *bool     `json:"mergeable"`
	User           struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	} `json:"user"`
}

// GitHub evaluates every PR of a stack against the rules of the stack base, so
// a layer's own base is a lower layer's branch. stack.base names the branch the
// stack merges into.
type stack struct {
	Base struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"base"`
}

// UnmarshalJSON keeps the real GitHub base and records a stack beside it. A
// malformed stack is not an error here, so it cannot stall the main queue, which
// never reads it; dev admission refuses a PR whose StackInvalid is set.
func (p *pull) UnmarshalJSON(data []byte) error {
	type plain pull
	var raw struct {
		plain
		Stack jsontext.Value `json:"stack"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*p = pull(raw.plain)
	if len(raw.Stack) == 0 || string(raw.Stack) == "null" {
		return nil
	}
	var s stack
	if err := json.Unmarshal(raw.Stack, &s); err != nil || s.Base.Ref == "" || !shaPattern.MatchString(s.Base.SHA) {
		p.StackInvalid = true
		return nil
	}
	p.Stack = s
	return nil
}

// stackedOnDev is a layer above the bottom of a stack that merges into dev. GitHub
// judges it by dev's rules although its own base is a lower layer's branch. The
// bottom layer is a plain dev PR.
func (p pull) stackedOnDev() bool { return p.Stack.Base.Ref == "dev" && p.Base.Ref != "dev" }

// admission is the one place dev admission reads a PR's base: dev and the live dev
// tip for a stacked layer, the PR's own base otherwise. GitHub-identity checks
// (run association, role matching, movement) use Base itself.
func (p pull) admission() reference {
	if p.stackedOnDev() {
		return reference{Ref: "dev", SHA: p.DevTip, Repo: p.Base.Repo}
	}
	return p.Base
}

func (p *pull) setLiveBase(sha string) {
	if p.stackedOnDev() {
		p.DevTip = sha
	} else {
		p.Base.SHA = sha
	}
}

// sameBase compares the whole base tuple, including a stacked layer's dev tip.
func (p pull) sameBase(q pull) bool { return p.Base == q.Base && p.DevTip == q.DevTip }

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
		canonical, _ := json.Marshal(old, stableJSON)
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
	if err != nil || jsontext.Value(data).IsValid() {
		return false
	}
	dec := jsontext.NewDecoder(bytes.NewReader(data))
	var identity struct {
		Repo int64
		PR   int
		Head string
	}
	if token, err := dec.ReadToken(); err != nil || token.Kind() != '{' {
		return false
	}
	for _, member := range []struct {
		name  string
		value any
	}{{"repo", &identity.Repo}, {"pr", &identity.PR}, {"head", &identity.Head}} {
		if token, err := dec.ReadToken(); err != nil || token.Kind() != '"' || token.String() != member.name || json.UnmarshalDecode(dec, member.value) != nil {
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
			d := jsontext.NewDecoder(bytes.NewReader(tail))
			if err := json.UnmarshalDecode(d, &text); err != nil {
				if !(errors.Is(err, io.ErrUnexpectedEOF) || (errors.Is(err, io.EOF) && len(tail) == 0)) || !canonicalStringPrefix(tail) {
					return false
				}
				return field == "base_ref" || len(tail) == 0 || (len(tail) <= 41 && strings.Trim(string(tail[1:]), "0123456789abcdef") == "")
			}
			canonical, _ := json.Marshal(text, stableJSON)
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
	d := jsontext.NewDecoder(bytes.NewReader(data))
	if err := json.UnmarshalDecode(d, result); err != nil {
		return fmt.Errorf("invalid GitHub JSON: %w", err)
	}
	if _, err := d.ReadValue(); !errors.Is(err, io.EOF) {
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
// and the check retirement commands keep their single attempt.
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
				Errors []jsontext.Value `json:"errors"`
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
	data, err := json.Marshal(payload, stableJSON)
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

type policyFilesBase struct{}

// verifyPR runs the policy verifier for one PR. A stacked layer's file list is
// judged over dev-tip...head, the tuple the Go policy reads, passed to the shell
// verifier as PR_GATE_FILES_BASE.
func (c *controller) verifyPR(ctx context.Context, p pull) (policyDecision, error) {
	if p.stackedOnDev() {
		ctx = context.WithValue(ctx, policyFilesBase{}, p.admission().SHA)
	}
	return c.verify(ctx, p.Number, p.Head.SHA)
}

func verifyPolicy(ctx context.Context, pr int, head string) (policyDecision, error) {
	cmd := gateCommand(ctx, "sh", "scripts/ci/check-fork-validation.sh")
	// Override only the controller-owned settings, retaining gh authentication.
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "PR_NUMBER=") && !strings.HasPrefix(value, "HEAD_SHA=") && !strings.HasPrefix(value, "PR_GATE_ONCE=") && !strings.HasPrefix(value, "PR_GATE_POLICY_ONLY=") && !strings.HasPrefix(value, "PR_GATE_FILES_BASE=") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "PR_NUMBER="+strconv.Itoa(pr), "HEAD_SHA="+head, "PR_GATE_ONCE=1", "PR_GATE_POLICY_ONLY=1")
	if base, ok := ctx.Value(policyFilesBase{}).(string); ok {
		cmd.Env = append(cmd.Env, "PR_GATE_FILES_BASE="+base)
	}
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
	api            api
	verify         verifier
	repoName       string
	repo           repository
	checkName      string
	expectedHead   string
	queueDeadline  time.Time
	reconciliation *queueReconciliation
	dryRun         bool
	output         io.Writer
	diagnostics    io.Writer
}

func (c *controller) endpoint(path string) string { return "repos/" + c.repoName + "/" + path }
func (c *controller) initRepository(ctx context.Context) error {
	if !repoPattern.MatchString(c.repoName) {
		return errors.New("invalid GH_REPO")
	}
	if c.checkName != "ci-required-next" && c.checkName != queueCheckName && c.checkName != devCheckName {
		return errors.New("invalid PR_GATE_CHECK_NAME")
	}
	if err := c.api.read(ctx, "repos/"+c.repoName, &c.repo); err != nil {
		return err
	}
	if c.repo.ID <= 0 || !strings.EqualFold(c.repo.FullName, c.repoName) {
		return errors.New("repository identity mismatch")
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
	if c.checkName == devCheckName && p.StackInvalid {
		return p, errors.New("malformed PR stack; dev admission refuses it")
	}
	if p.State == "open" && p.admission().Ref == c.admissionBranch() {
		var ref struct {
			Ref    string `json:"ref"`
			Object struct {
				Type string `json:"type"`
				SHA  string `json:"sha"`
			} `json:"object"`
		}
		if err := c.api.read(ctx, c.endpoint("git/ref/heads/"+url.PathEscape(c.admissionBranch())), &ref); err != nil {
			return p, err
		}
		if ref.Ref != "refs/heads/"+c.admissionBranch() || ref.Object.Type != "commit" || !shaPattern.MatchString(ref.Object.SHA) {
			return p, errors.New("invalid live default-branch ref")
		}
		p.setLiveBase(ref.Object.SHA)
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
func (c *controller) jobs(ctx context.Context, r run) ([]job, error) {
	var all []job
	seen := make(map[int64]bool)
	for page := 1; page <= 10; page++ {
		var list struct {
			Total int   `json:"total_count"`
			Jobs  []job `json:"jobs"`
		}
		path := fmt.Sprintf("actions/runs/%d/attempts/%d/jobs?per_page=100&page=%d", r.ID, r.Attempt, page)
		if err := c.api.read(ctx, c.endpoint(path), &list); err != nil {
			return nil, err
		}
		if list.Jobs == nil || list.Total < 0 || list.Total > 1000 {
			return nil, errors.New("invalid validation job list")
		}
		for _, j := range list.Jobs {
			if j.ID <= 0 || j.RunID != r.ID || j.Attempt <= 0 || j.Attempt != r.Attempt || j.Name == "" || seen[j.ID] {
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
	PR  pull
	Run *run
}

func (s snapshot) binding(repoID int64) binding {
	// A stacked layer binds to dev's tip and to the base it really has, so
	// retargeting it to dev itself is a new tuple.
	base := s.PR.admission()
	ref := base.Ref
	if s.PR.stackedOnDev() {
		ref += "\x00stacked-on:" + s.PR.Base.Ref
	}
	b := binding{RepoID: repoID, PR: s.PR.Number, Head: s.PR.Head.SHA, BaseRef: sha256.Sum256([]byte(ref)), Base: base.SHA}
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
	if current.Head != p.Head || !current.sameBase(p) {
		return errors.New("PR changed during pending recovery; current identity could not be confirmed blocked")
	}
	_, err = fmt.Fprintf(c.output, "PR #%d: pending after authenticated state movement during %s\n", number, phase)
	return err
}

func (c *controller) publish(ctx context.Context, p pull, b binding, selected *check, result outcome) error {
	if c.checkName != queueCheckName && c.checkName != devCheckName {
		return errors.New("only the queue and dev contexts can publish; the legacy context is retirement-only")
	}
	session := c.reconciliation
	if session == nil {
		// The independently bounded admission refusal may publish outside
		// a normal pass, using the same immutable publication protocol.
		session = &queueReconciliation{q: &queueController{controller: c}}
	}
	return session.publishVerified(ctx, p, b, selected, result)
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
func sameSourceEvent(a, b run) bool {
	return a.ID == b.ID && a.Attempt == b.Attempt && a.WorkflowID == b.WorkflowID && a.Path == b.Path && a.Event == b.Event && a.HeadSHA == b.HeadSHA && a.Repository.ID == b.Repository.ID && a.HeadRepository.ID == b.HeadRepository.ID
}
func main() {
	discoverRetirement := flag.Bool("discover-retirements", false, "discover complete bounded main-history retirement targets without writing checks")
	retire := flag.Bool("retire-checks", false, "retire only owned unfinished checks on an exact inactive SHA under its existing writer lease")
	dryRun := flag.Bool("dry-run", false, "read policy and live metadata without publishing checks")
	devMode := flag.Bool("dev", false, "publish trusted fast admission for the dev integration branch")
	devPR := flag.Int("dev-pr", 0, "current dev PR routing hint under an exact head lease")
	queueMode := flag.Bool("queue", false, "publish the separately protected merge-queue context")
	targeted := flag.Bool("targeted", false, "bound only a targeted queue admission invocation")
	repairOnly := flag.Bool("repair-groups-only", false, "repair queue groups only on authenticated manual dispatch")
	expectedHead := flag.String("expected-head", "", "exact SHA held by the protected per-head publisher concurrency lease")
	dispatch := flag.Bool("dispatch-workers", false, "discover complete work and dispatch only fixed default-branch head workers")
	headWorker := flag.String("head-worker", "", "reconcile one typed discovery record under its exact head lease")
	flag.Parse()
	if *devMode || *devPR != 0 {
		if !*devMode || *devPR <= 0 || *discoverRetirement || *retire || *queueMode || *targeted || *repairOnly || *dispatch || *headWorker != "" || flag.NArg() != 0 || !shaPattern.MatchString(*expectedHead) {
			fmt.Fprintln(os.Stderr, "dev admission requires one PR and exact held head and cannot mix producer roles")
			os.Exit(1)
		}
		if err := runDevCommand(*devPR, *expectedHead, *dryRun); err != nil {
			if onlyStaleHeadWork(err) {
				fmt.Fprintln(os.Stdout, err)
				return
			}
			fmt.Fprintln(os.Stderr, "trusted dev gate:", err)
			os.Exit(1)
		}
		return
	}
	if *discoverRetirement && (*retire || *queueMode || *targeted || *repairOnly || *dispatch || *headWorker != "" || *expectedHead != "") {
		fmt.Fprintln(os.Stderr, "retirement discovery is read-only and cannot hold or mix writer roles")
		os.Exit(1)
	}
	if *retire && (*queueMode || *targeted || *repairOnly || *dispatch || *headWorker != "" || !shaPattern.MatchString(*expectedHead)) {
		fmt.Fprintln(os.Stderr, "retirement requires an exact head and cannot mix producer roles")
		os.Exit(1)
	}
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected positional arguments")
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
		// Read-only retirement discovery sets no context; the name only passes validation.
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
				var b []byte
				if b, err = json.Marshal(targets, stableJSON); err == nil {
					_, err = c.output.Write(append(b, '\n'))
				}
			}
		} else if *retire {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			err = c.retireEvent(ctx, os.Getenv("GITHUB_EVENT_NAME"), e)
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
			err = errors.New("pr-gate requires --queue, --dev, --retire-checks or --discover-retirements")
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
