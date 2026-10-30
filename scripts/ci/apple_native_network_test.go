package ci_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Execute the workflow command with the real network wrapper and synthetic
// privileged tools. The installation probe models the observed CloudKit failure
// when TCP fallback is absent, without touching the host firewall or Apple.
func TestAppleNativeAcceptanceTicketNetwork(t *testing.T) {
	workflow := readConsolidatedWorkflow(t, "nightly")
	var command string
	for _, step := range workflow.Jobs["apple-acceptance"].Steps {
		if step.Name == "Verify native installation with quarantine retained" {
			command = step.Run
		}
	}
	if command == "" {
		t.Fatal("missing native installation command")
	}
	wrapper, err := os.ReadFile("../release/with-apple-ticket-network.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, arch := range []string{"arm64", "x86_64"} {
		for _, scenario := range []struct {
			name          string
			installStatus int
			cleanupStatus int
			wantStatus    int
		}{
			{"accepted", 0, 0, 0},
			{"invalid ticket", 68, 0, 68},
			{"cleanup failure", 0, 1, 1},
		} {
			t.Run(arch+"/"+scenario.name, func(t *testing.T) {
				root := t.TempDir()
				write := func(name, source string) {
					t.Helper()
					path := filepath.Join(root, name)
					if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(source), 0700); err != nil {
						t.Fatal(err)
					}
				}
				write("scripts/release/with-apple-ticket-network.sh", string(wrapper))
				write("bin/uname", "#!/bin/sh\necho Darwin\n")
				write("bin/sudo", "#!/bin/sh\n[ \"$1\" = -n ] || exit 1\nshift\nexec \"$@\"\n")
				write("bin/pfctl", `#!/bin/sh
set -eu
printf '%s\n' "$*" >>"$FIXTURE_ROOT/network.log"
case "$*" in
  '-sr') printf 'anchor "com.apple/*" all\n' ;;
  '-a com.apple/hikyo-notary -f -')
    IFS= read -r rule
    [ "$rule" = 'block return out quick proto udp from any to any port 443' ]
    touch "$FIXTURE_ROOT/tcp-rule" ;;
  '-E') touch "$FIXTURE_ROOT/enabled"; echo 'Token : 123456' ;;
  '-a com.apple/hikyo-notary -F rules') rm "$FIXTURE_ROOT/tcp-rule"; exit "$CLEANUP_STATUS" ;;
  '-X 123456') rm "$FIXTURE_ROOT/enabled" ;;
  *) exit 1 ;;
esac
`)
				write("scripts/release/verify-apple-install.sh", `#!/bin/sh
set -eu
if [ ! -f "$FIXTURE_ROOT/tcp-rule" ] || [ ! -f "$FIXTURE_ROOT/enabled" ]; then
  echo 'NSURLErrorDomain Code=-1005: CloudKit connection lost' >&2
  exit 68
fi
[ "$#" = 4 ] && [ "$1" = dist ] && [ "$2" = '1.2.3-nightly.1' ] &&
  [ "$3" = "$ARCH" ] && [ "$4" = "$RUNNER_TEMP/install-$ARCH.json" ]
touch "$FIXTURE_ROOT/installation-ran"
exit "$INSTALL_STATUS"
`)
				cmd := exec.Command("bash", "-e", "-o", "pipefail", "-c", command)
				cmd.Dir = root
				cmd.Env = append(os.Environ(), "PATH="+filepath.Join(root, "bin")+":"+os.Getenv("PATH"),
					"FIXTURE_ROOT="+root, "RUNNER_TEMP="+root, "ARCH="+arch, "VERSION=1.2.3-nightly.1",
					"GITHUB_ACTIONS=true", "RUNNER_ENVIRONMENT=github-hosted",
					fmt.Sprintf("INSTALL_STATUS=%d", scenario.installStatus),
					fmt.Sprintf("CLEANUP_STATUS=%d", scenario.cleanupStatus))
				output, err := cmd.CombinedOutput()
				if cmd.ProcessState == nil {
					t.Fatalf("start native acceptance: %v", err)
				}
				if got := cmd.ProcessState.ExitCode(); got != scenario.wantStatus {
					t.Fatalf("native acceptance exit=%d, want=%d: %v\n%s", got, scenario.wantStatus, err, output)
				}
				if _, err := os.Stat(filepath.Join(root, "installation-ran")); err != nil {
					t.Fatal("native installation did not execute with TCP fallback")
				}
				for _, name := range []string{"tcp-rule", "enabled"} {
					if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
						t.Fatalf("network state %s was not cleaned up: %v", name, err)
					}
				}
				log, err := os.ReadFile(filepath.Join(root, "network.log"))
				if err != nil || !strings.HasSuffix(string(log), "-a com.apple/hikyo-notary -F rules\n-X 123456\n") {
					t.Fatalf("missing scoped rule and enablement cleanup: %v\n%s", err, log)
				}
			})
		}
	}
}
