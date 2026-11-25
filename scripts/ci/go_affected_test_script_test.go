package ci_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const goAffectedPackagedPattern = `Packaged|^TestActualDefaultDevCLIStartsAndRestarts$`

// The pre-push runner splits internal/app exactly as the test_core_serial job does.
func TestGoAffectedRunnerAppSplitMatchesTestCore(t *testing.T) {
	root := repositoryRoot(t)
	for _, name := range []string{"scripts/ci/go-affected-test.sh", "scripts/ci/test-core-packages.sh"} {
		raw, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		match := regexp.MustCompile(`packaged='([^']+)'`).FindSubmatch(raw)
		if match == nil || string(match[1]) != goAffectedPackagedPattern {
			t.Fatalf("%s: packaged-test pattern differs from %q", name, goAffectedPackagedPattern)
		}
	}
}

// runGoAffected runs the real runner against a stub selector and a recording
// fake go, with CI set in the caller's environment.
func runGoAffected(t *testing.T, dsn, failing string) (log, stderr string, code int) {
	t.Helper()
	root := repositoryRoot(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "scripts/ci"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner, err := os.ReadFile(filepath.Join(root, "scripts/ci/go-affected-test.sh"))
	if err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "internal/webui"), 0o755); err != nil {
		t.Fatal(err)
	}
	write("scripts/ci/go-affected-test.sh", string(runner))
	write("scripts/ci/go-affected-packages.sh", "#!/bin/sh\nprintf 'm/internal/app\\nm/internal/lint\\nm/internal/isolation\\nm/scripts/ci\\nm/pkg/a\\nm/pkg/b\\n'\n")
	write("bin/go", `#!/bin/sh
case "$1" in
list) shift; for p in "$@"; do echo "m/${p#./}"; done ;;
test)
	echo "test CI=${CI-unset} $*" >>"$LOG"
	case " $* " in *" $FAILING "*) exit 1 ;; esac
	;;
run)
	echo "run $*" >>"$LOG"
	[ "$FAILING" != prepare ] || exit 1
	;;
esac
`)
	logFile := filepath.Join(dir, "log")
	cmd := exec.CommandContext(t.Context(), "sh", filepath.Join(dir, "scripts/ci/go-affected-test.sh"), "base")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+filepath.Join(dir, "bin")+":"+os.Getenv("PATH"),
		"CI=1", "LOG="+logFile, "FAILING="+failing, "HIKYO_TEST_POSTGRES_DSN="+dsn)
	var errBuf strings.Builder
	cmd.Stderr = &errBuf
	err = cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "internal/webui/dist/.placeholder")); statErr != nil {
		t.Errorf("no placeholder for the missing SPA build: %v", statErr)
	}
	raw, _ := os.ReadFile(logFile)
	return string(raw), errBuf.String(), code
}

func TestGoAffectedRunnerOrderAndEnvironment(t *testing.T) {
	log, stderr, code := runGoAffected(t, "", "")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	want := strings.Join([]string{
		"test CI=unset test -p 2 m/pkg/a m/pkg/b",
		"test CI=unset test -p 2 -skip " + goAffectedPackagedPattern + " m/internal/app",
		"test CI=unset test -p 2 -run " + goAffectedPackagedPattern + " m/internal/app",
		"run run ./scripts/ci/prepare-lint-cache",
		"test CI=unset test -p 2 -timeout=20m m/internal/lint",
		"",
	}, "\n")
	if log != want {
		t.Fatalf("go calls:\n%s\nwant:\n%s", log, want)
	}
	if strings.Contains(log, "isolation") || strings.Contains(log, "m/scripts/ci") {
		t.Fatalf("an excluded package ran:\n%s", log)
	}
	if !strings.Contains(stderr, "leaving 2 to CI") || !strings.Contains(stderr, "internal/isolation") || !strings.Contains(stderr, "scripts/ci") {
		t.Fatalf("no notice naming the excluded packages:\n%s", stderr)
	}
	if !strings.Contains(stderr, "4 affected packages") || !strings.Contains(stderr, "PostgreSQL tests are skipped") {
		t.Fatalf("missing count or DSN notice:\n%s", stderr)
	}
	_, stderr, _ = runGoAffected(t, "postgres://x", "")
	if strings.Contains(stderr, "PostgreSQL tests are skipped") {
		t.Fatalf("DSN notice printed with a DSN set:\n%s", stderr)
	}
}

func TestGoAffectedRunnerExitCodes(t *testing.T) {
	log, _, code := runGoAffected(t, "", "m/pkg/a")
	if code != 1 || !strings.Contains(log, "m/internal/lint") {
		t.Fatalf("a failing test: exit %d, want 1 with the later packages still run\n%s", code, log)
	}
	if _, _, code = runGoAffected(t, "", "prepare"); code != 64 {
		t.Fatalf("a failing lint cache: exit %d, want 64", code)
	}
}
