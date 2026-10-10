package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestPrepareReportsCompletedContextsOnly(t *testing.T) {
	for _, failWindows := range []bool{false, true} {
		name := "success"
		if failWindows {
			name = "failed_windows"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			stub := "#!/bin/sh\nif [ \"${FAIL_WINDOWS:-}\" = 1 ] && [ \"${GOOS:-}\" = windows ]; then exit 42; fi\nexit 0\n"
			if err := os.WriteFile(filepath.Join(dir, "go"), []byte(stub), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("FAIL_WINDOWS", "")
			if failWindows {
				t.Setenv("FAIL_WINDOWS", "1")
			}
			output, err := os.CreateTemp(dir, "output-")
			if err != nil {
				t.Fatal(err)
			}
			previous := os.Stdout
			os.Stdout = output
			t.Cleanup(func() {
				os.Stdout = previous
				output.Close()
			})
			prepareErr := prepare()
			os.Stdout = previous
			if failWindows {
				var exitErr *exec.ExitError
				if !errors.As(prepareErr, &exitErr) || exitErr.ExitCode() != 42 || !strings.Contains(prepareErr.Error(), "windows:") {
					t.Fatalf("expected Windows compiler failure, got %v", prepareErr)
				}
			} else if prepareErr != nil {
				t.Fatal(prepareErr)
			}
			raw, err := os.ReadFile(output.Name())
			if err != nil {
				t.Fatal(err)
			}
			text := string(raw)
			contexts := []string{"default", "ui", "darwin", "windows"}
			for _, context := range contexts {
				pattern := regexp.MustCompile(`(?m)^prepare lint cache: ` + context + ` completed in [0-9]+\.[0-9]s$`)
				matches := pattern.FindAllString(text, -1)
				want := 1
				if failWindows && context == "windows" {
					want = 0
				}
				if len(matches) != want {
					t.Errorf("%s: want %d timing lines, got %d in %q", context, want, len(matches), text)
				}
			}
			if failWindows && strings.Contains(text, "windows completed") {
				t.Fatalf("failed preparation was reported complete: %s", text)
			}
		})
	}
}
