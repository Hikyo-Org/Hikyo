package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// orderedStdin records whether the statement was already on stderr, and how
// many requests had reached the server, when the value was first read.
type orderedStdin struct {
	r           io.Reader
	stderr      *bytes.Buffer
	requests    *int
	read        bool
	statedFirst bool
	sentBefore  int
}

func (o *orderedStdin) Read(p []byte) (int, error) {
	if !o.read {
		o.read = true
		o.statedFirst = strings.Contains(o.stderr.String(), personalSetStatement)
		o.sentBefore = *o.requests
	}
	return o.r.Read(p)
}

// `personal set` states what the guarantee does not cover before it reads or
// sends a value; `--clear` sends none and states nothing.
func TestPersonalSetStatesTheGuaranteeBeforeTheValue(t *testing.T) {
	const want = "Hikyo does not show this to administrators. Your instance operator could read it, and anyone who can " +
		"change shared values here could capture it once your program runs. If someone gave you your password, " +
		"authenticator or recovery codes, replace them, regenerate your recovery codes and check your linked identities first."
	if personalSetStatement != want {
		t.Fatalf("statement = %q", personalSetStatement)
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	stateDir := t.TempDir()
	st := &State{dir: stateDir}
	if err := st.Trust().Put(TrustEntry{Name: "local", Origin: server.URL}); err != nil {
		t.Fatal(err)
	}
	run := func(stdin io.Reader, stderr *bytes.Buffer, args ...string) int {
		ios := IO{
			Stdin: stdin, Stdout: &bytes.Buffer{}, Stderr: stderr, Workdir: t.TempDir(),
			Env: Env{Getenv: func(key string) string {
				if key == "HIKYO_STATE_DIR" {
					return stateDir
				}
				return ""
			}},
		}
		target := []string{"--instance", "local", "--org", "org_1", "--project", "project_1", "--env", "env_1"}
		return Run(context.Background(), ios, append(append([]string{"personal", "set", "API_TOKEN"}, args...), target...))
	}

	var stderr bytes.Buffer
	stdin := &orderedStdin{r: strings.NewReader("synthetic\n"), stderr: &stderr, requests: &requests}
	run(stdin, &stderr, "--stdin")
	if !stdin.read || !stdin.statedFirst || stdin.sentBefore != 0 {
		t.Fatalf("statement before the value: read=%t stated=%t requests=%d", stdin.read, stdin.statedFirst, stdin.sentBefore)
	}
	if strings.Count(stderr.String(), personalSetStatement+"\n") != 1 {
		t.Fatalf("stderr = %q, want the statement once", stderr.String())
	}

	var cleared bytes.Buffer
	run(strings.NewReader(""), &cleared, "--clear")
	if strings.Contains(cleared.String(), "Hikyo does not show this to administrators") {
		t.Fatalf("--clear stated the guarantee: %q", cleared.String())
	}
}
