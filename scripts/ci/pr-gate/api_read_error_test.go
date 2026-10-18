package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGitHubReadPreservesContextFailure(t *testing.T) {
	for _, path := range []string{"repos/Hikyo-Org/Hikyo", "graphql:query{repository{name}}"} {
		for _, failure := range []string{"cancelled", "deadline"} {
			t.Run(path+"/"+failure, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				want := context.Canceled
				if failure == "deadline" {
					cancel()
					ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
					want = context.DeadlineExceeded
				}
				cancel()
				var result map[string]any
				err := (ghAPI{}).read(ctx, path, &result)
				if !errors.Is(err, want) || !strings.Contains(err.Error(), want.Error()) {
					t.Fatalf("GitHub read concealed context failure %v: %v", want, err)
				}
				if result != nil {
					t.Fatal("failed GitHub read exposed a partial response")
				}
			})
		}
	}
}

func TestGitHubReadPreservesCommandFailureWithoutLoggingResponse(t *testing.T) {
	tools := t.TempDir()
	script := "#!/bin/sh\nprintf '%s\\n' '{\"status\":\"429\",\"message\":\"DO_NOT_LOG_BODY\"}'\nprintf '%s\\n' 'DO_NOT_LOG_STDERR' >&2\nexit 42\n"
	if err := os.WriteFile(filepath.Join(tools, "gh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	var result map[string]any
	err := (ghAPI{}).read(context.Background(), "repos/Hikyo-Org/Hikyo", &result)
	var commandFailure *exec.ExitError
	var readFailure *githubReadError
	if !errors.As(err, &readFailure) || readFailure.status != 429 || !errors.As(err, &commandFailure) || commandFailure.ExitCode() != 42 {
		t.Fatalf("GitHub read concealed HTTP or command failure: %v", err)
	}
	if !strings.Contains(err.Error(), "HTTP 429") || !strings.Contains(err.Error(), "exit status 42") {
		t.Fatalf("GitHub read diagnostic omitted validated status: %v", err)
	}
	for _, secret := range []string{"DO_NOT_LOG_BODY", "DO_NOT_LOG_STDERR", "\n"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("GitHub read diagnostic exposed response data %q", secret)
		}
	}
	if result != nil {
		t.Fatal("failed GitHub read exposed partial response data")
	}
}

func TestGitHubReadPreservesGraphQLCommandCauseAndSanitizedDiagnostic(t *testing.T) {
	tools := t.TempDir()
	script := "#!/bin/sh\nprintf '%s\\n' '{\"errors\":[{\"message\":\"Resource not accessible by integration\",\"extensions\":{\"code\":\"FORBIDDEN\",\"private\":\"DO_NOT_LOG_EXTENSION\"}}]}'\nprintf '%s\\n' 'DO_NOT_LOG_STDERR' >&2\nexit 42\n"
	if err := os.WriteFile(filepath.Join(tools, "gh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	var result map[string]any
	err := (ghAPI{}).read(context.Background(), "graphql:query{repository{name}}", &result)
	var commandFailure *exec.ExitError
	if !errors.As(err, &commandFailure) || commandFailure.ExitCode() != 42 || !strings.Contains(err.Error(), "Resource not accessible by integration") || !strings.Contains(err.Error(), "FORBIDDEN") {
		t.Fatalf("GitHub read concealed GraphQL command cause or sanitized diagnostic: %v", err)
	}
	for _, secret := range []string{"DO_NOT_LOG_EXTENSION", "DO_NOT_LOG_STDERR", "\n"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("GraphQL command diagnostic exposed response data %q", secret)
		}
	}
	if result != nil {
		t.Fatal("failed GraphQL read exposed partial response data")
	}
}
