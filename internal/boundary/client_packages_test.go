package boundary

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The desktop shell (a nested module, desktop-app ADR D3) imports these
// packages for sign-in and HTTPS trust. Importing internal/cli instead would
// pull in most of the 88.8 MB binary, so a transitive edge to the CLI or to
// any server-side package fails here. The check is transitive, per platform,
// because the loopback socket options differ by GOOS.
func TestClientPackagesStayOffTheCLIAndServer(t *testing.T) {
	roots := []string{
		module + "/internal/clienterr",
		module + "/internal/clientauth",
		module + "/internal/clienttrust",
	}
	forbiddenPrefixes := []string{
		module + "/internal/cli",
		module + "/internal/server",
		module + "/internal/service",
		module + "/internal/store",
		module + "/internal/app",
		module + "/internal/authz",
		module + "/internal/webui",
		module + "/internal/mcpserver",
		module + "/internal/operator",
		module + "/internal/pki",
		module + "/internal/updater",
		module + "/internal/importer",
		module + "/internal/compose",
		module + "/cmd",
	}
	for _, goos := range []string{"linux", "darwin", "windows"} {
		t.Run(goos, func(t *testing.T) {
			command := exec.CommandContext(t.Context(), "go", append([]string{"list", "-deps", "-f", "{{.ImportPath}}"}, roots...)...)
			command.Env = append(os.Environ(), "GOOS="+goos, "CGO_ENABLED=0")
			out, err := command.Output()
			if err != nil {
				t.Fatalf("go list -deps for %s: %v", goos, err)
			}
			for _, dep := range strings.Fields(string(out)) {
				for _, prefix := range forbiddenPrefixes {
					if dep == prefix || strings.HasPrefix(dep, prefix+"/") {
						t.Errorf("%s: the client auth and trust packages depend on %s; they must stay importable without internal/cli or the server", goos, dep)
					}
				}
			}
		})
	}
}
