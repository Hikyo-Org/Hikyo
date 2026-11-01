package securitydeployment

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestCommandHelperProcess(t *testing.T) {
	index := slices.Index(os.Args, "--")
	if index < 0 {
		return
	}
	switch os.Args[index+1] {
	case "failure":
		fmt.Fprintln(os.Stdout, "CANARY_SECRET_STDOUT")
		fmt.Fprintln(os.Stderr, "CANARY_SECRET_STDERR")
		os.Exit(3)
	case "large":
		fmt.Fprint(os.Stdout, strings.Repeat("x", maxRecordBytes+1024))
		os.Exit(0)
	case "success":
		fmt.Fprint(os.Stdout, `{"result":"ok"}`)
		os.Exit(0)
	}
	os.Exit(2)
}

func TestCommandRunnerBoundsOutputAndRedactsToolFailures(t *testing.T) {
	runner := CommandRunner{}
	for _, kind := range []string{"failure", "large", "success"} {
		t.Run(kind, func(t *testing.T) {
			out, err := runner.Run(t.Context(), Command{Path: os.Args[0], Args: []string{"-test.run=^TestCommandHelperProcess$", "--", kind}})
			if kind == "success" {
				if err != nil || string(out) != `{"result":"ok"}` {
					t.Fatalf("successful output changed: %s %v", out, err)
				}
				return
			}
			if err == nil || len(out) != 0 {
				t.Fatal("failure or oversized output admitted")
			}
			if strings.Contains(err.Error(), "CANARY_SECRET") {
				t.Fatal("tool failure exposed secret output")
			}
		})
	}
}
