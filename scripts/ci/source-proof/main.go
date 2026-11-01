// Command source-proof verifies reusable, exact-source full CI evidence.
// It reads only GitHub metadata and immutable repository files. It never treats
// a candidate-authored artifact or a same-name check as validation authority.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const proofSchema = "hikyo.dev/source-validation/v1"

var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

type proof struct {
	Schema              string `json:"schema"`
	Repository          string `json:"repository"`
	RepositoryID        int64  `json:"repository_id"`
	Commit              string `json:"commit"`
	Ref                 string `json:"ref"`
	WorkflowPath        string `json:"workflow_path"`
	Event               string `json:"event"`
	RunID               int64  `json:"run_id"`
	RunAttempt          int64  `json:"run_attempt"`
	WorkflowID          int64  `json:"workflow_id"`
	AuthorityCommit     string `json:"authority_commit"`
	ConfigurationSHA256 string `json:"configuration_sha256"`
}

type options struct {
	Repository, Mode, Commit, Authority, Output, GitHubOutput string
	Latest                                                    bool
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := execute(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "source proof:", err)
		os.Exit(1)
	}
}

func execute(ctx context.Context, args []string, stdout io.Writer) error {
	var opt options
	flags := flag.NewFlagSet("source-proof", flag.ContinueOnError)
	flags.StringVar(&opt.Repository, "repository", "", "canonical OWNER/REPO")
	flags.StringVar(&opt.Mode, "mode", "", "main or dev")
	flags.StringVar(&opt.Commit, "commit", "", "exact source SHA")
	flags.StringVar(&opt.Authority, "authority-commit", "", "immutable approved main definition SHA")
	flags.StringVar(&opt.Output, "output", "", "small verified JSON receipt")
	flags.StringVar(&opt.GitHubOutput, "github-output", "", "append validated Actions outputs")
	flags.BoolVar(&opt.Latest, "latest", false, "select last fully validated dev ancestor")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || !repoPattern.MatchString(opt.Repository) || (opt.Mode != "main" && opt.Mode != "dev") ||
		(opt.Latest && (opt.Mode != "dev" || opt.Commit != "")) || (!opt.Latest && !shaPattern.MatchString(opt.Commit)) ||
		(opt.Authority != "" && !shaPattern.MatchString(opt.Authority)) {
		return errors.New("require --repository OWNER/REPO --mode main|dev and either --commit SHA or --mode dev --latest")
	}
	gh := os.Getenv("GH_BIN")
	if gh == "" {
		gh = "gh"
	}
	r := resolver{api: githubCLI{binary: gh}, requested: opt.Repository}
	verified, err := r.resolve(ctx, opt)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(verified, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if opt.Output != "" {
		if err := os.WriteFile(opt.Output, data, 0o600); err != nil {
			return fmt.Errorf("write receipt: %w", err)
		}
	}
	if opt.GitHubOutput != "" {
		f, err := os.OpenFile(opt.GitHubOutput, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
		if err != nil {
			return err
		}
		_, writeErr := fmt.Fprintf(f, "commit=%s\nrun_id=%d\nrun_attempt=%d\nworkflow_path=%s\nconfiguration_sha256=%s\n", verified.Commit, verified.RunID, verified.RunAttempt, verified.WorkflowPath, verified.ConfigurationSHA256)
		if err := errors.Join(writeErr, f.Close()); err != nil {
			return err
		}
	}
	_, err = stdout.Write(data)
	return err
}

type reader interface {
	read(context.Context, string, any) error
}

type githubCLI struct{ binary string }

type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	if len(data) > b.limit-b.Len() {
		return 0, errors.New("GitHub response exceeded limit")
	}
	return b.Buffer.Write(data)
}

func (g githubCLI) read(ctx context.Context, endpoint string, target any) error {
	cmd := exec.CommandContext(ctx, g.binary, "api", "--hostname", "github.com", "--method", "GET", endpoint)
	var stdout = boundedBuffer{limit: 16 << 20}
	var stderr = boundedBuffer{limit: 8 << 10}
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		// Do not forward raw command output, which can contain credentials or
		// provider-controlled terminal control sequences.
		return fmt.Errorf("GitHub read failed for %s: %w", endpoint, err)
	}
	return decode(stdout.Bytes(), target)
}

func decode(data []byte, target any) error {
	if err := uniqueMembers(data); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	if err := d.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("GitHub response contains trailing data")
	}
	return nil
}

func uniqueMembers(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var value func() error
	value = func() error {
		token, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				name, err := d.Token()
				if err != nil {
					return err
				}
				key, ok := name.(string)
				if !ok || seen[key] {
					return errors.New("duplicate or invalid JSON member")
				}
				seen[key] = true
				if err := value(); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := value(); err != nil {
					return err
				}
			}
		default:
			return errors.New("invalid JSON delimiter")
		}
		_, err = d.Token()
		return err
	}
	if err := value(); err != nil {
		return err
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return errors.New("GitHub response contains multiple JSON documents")
	}
	return nil
}

func equalRepository(a repository, b repository) bool {
	return a.ID == b.ID && strings.EqualFold(a.FullName, b.FullName)
}
