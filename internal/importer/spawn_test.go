package importer

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestSanitizedEnvScrubsNamespaceInEveryCase(t *testing.T) {
	input := []string{"HIKYO_TOKEN=upper", "hikyo_token=lower", "HiKyO_ROOT_KEY=mixed", "SOPS_AGE_KEY=foreign", "HIKYOLOGY=value"}
	want := []string{"SOPS_AGE_KEY=foreign", "HIKYOLOGY=value"}
	if got := SanitizedEnv(input); !reflect.DeepEqual(got, want) {
		t.Fatalf("sanitized environment=%v, want %v", got, want)
	}
	if input[0] != "HIKYO_TOKEN=upper" {
		t.Fatal("sanitization modified its input")
	}
}

func TestWithSanitizedRestoresEveryCaseAfterReturnErrorAndPanic(t *testing.T) {
	for _, outcome := range []string{"success", "error", "panic"} {
		t.Run(outcome, func(t *testing.T) {
			values := map[string]string{"HIKYO_UPPER_TEST": "upper", "hikyo_lower_test": "lower", "HiKyO_Mixed_Test": "mixed"}
			for name, value := range values {
				t.Setenv(name, value)
			}
			t.Setenv("IMPORTER_FOREIGN_TEST", "foreign")
			sentinel := errors.New("scope failed")
			var returned error
			var recovered any
			func() {
				defer func() { recovered = recover() }()
				returned = WithSanitized(func() error {
					for name := range values {
						if _, present := os.LookupEnv(name); present {
							t.Errorf("%s survived sanitization", name)
						}
					}
					if os.Getenv("IMPORTER_FOREIGN_TEST") != "foreign" {
						t.Error("foreign connector variable was removed")
					}
					switch outcome {
					case "error":
						return sentinel
					case "panic":
						panic(sentinel)
					}
					return nil
				})
			}()
			if outcome == "error" && !errors.Is(returned, sentinel) {
				t.Fatalf("scope error=%v, want sentinel", returned)
			}
			if outcome == "panic" && recovered != sentinel {
				t.Fatalf("scope panic=%v, want sentinel", recovered)
			}
			if outcome != "panic" && recovered != nil {
				t.Fatalf("unexpected scope panic: %v", recovered)
			}
			if outcome != "error" && returned != nil {
				t.Fatalf("unexpected scope error: %v", returned)
			}
			for name, want := range values {
				if got := os.Getenv(name); got != want {
					t.Errorf("restored %s=%q, want %q", name, got, want)
				}
			}
		})
	}
}

func TestMixedCaseCredentialsCannotReachConnectorChild(t *testing.T) {
	for _, name := range []string{"HIKYO_UPPER_TEST", "hikyo_lower_test", "HiKyO_Mixed_Test"} {
		t.Setenv(name, "credential")
	}
	t.Setenv("IMPORTER_ENV_CHILD", "1")
	t.Setenv("IMPORTER_FOREIGN_TEST", "foreign")
	child := func(env []string) error {
		cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSanitizedEnvironmentChild$")
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("connector child: %w: %s", err, out)
		}
		return nil
	}
	if err := child(SanitizedEnv(os.Environ())); err != nil {
		t.Fatal(err)
	}
	if err := WithSanitized(func() error { return child(nil) }); err != nil {
		t.Fatal(err)
	}
}

func TestSanitizedEnvironmentChild(t *testing.T) {
	if os.Getenv("IMPORTER_ENV_CHILD") != "1" {
		return
	}
	for _, variable := range os.Environ() {
		name, _, _ := strings.Cut(variable, "=")
		if strings.HasPrefix(strings.ToUpper(name), "HIKYO_") {
			t.Errorf("Hikyo variable %s reached connector child", name)
		}
	}
	if os.Getenv("IMPORTER_FOREIGN_TEST") != "foreign" {
		t.Error("connector child lost foreign environment variable")
	}
}
