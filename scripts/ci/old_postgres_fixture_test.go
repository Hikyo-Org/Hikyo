package ci_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	oldPostgresStartName = "Start PostgreSQL 17 to prove the PostgreSQL 18 minimum is enforced"
	oldPostgresTestName  = "Verify PostgreSQL 17 refusal fixture startup"
)

// The PostgreSQL 17 refusal fixture (#1552) is one exact additive step, directly
// after the checked datastore start, in each job that runs the packages whose
// tests read HIKYO_TEST_POSTGRES_OLD_DSN and fail under CI without it. test_core
// also runs the fixture's own shell test right after the start step.
var oldPostgresFixtureJobs = map[string]bool{"test_core": true, "race_shard": false}

func normalizeOldPostgresFixture(jobs map[string]map[string]any) error {
	start := map[string]any{"name": oldPostgresStartName, "run": "./scripts/ci/start-old-postgres.sh"}
	test := map[string]any{"name": oldPostgresTestName, "run": "./scripts/ci/start-old-postgres_test.sh"}
	for name, wantTest := range oldPostgresFixtureJobs {
		steps, ok := jobs[name]["steps"].([]any)
		if !ok {
			return fmt.Errorf("%s has no steps", name)
		}
		kept := make([]any, 0, len(steps))
		starts, tests, startAt, testAt, afterDatastore := 0, 0, -1, -1, false
		for i, value := range steps {
			step, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("%s has a malformed step", name)
			}
			switch step["name"] {
			case start["name"]:
				if !reflect.DeepEqual(step, start) {
					return fmt.Errorf("%s PostgreSQL 17 refusal fixture changed", name)
				}
				if i > 0 {
					previous, _ := steps[i-1].(map[string]any)
					afterDatastore = previous["id"] == "ci-postgres"
				}
				starts++
				startAt = i
			case test["name"]:
				if !reflect.DeepEqual(step, test) {
					return fmt.Errorf("%s PostgreSQL 17 refusal fixture test changed", name)
				}
				tests++
				testAt = i
			default:
				kept = append(kept, step)
			}
		}
		if starts != 1 || !afterDatastore {
			return fmt.Errorf("%s must start the PostgreSQL 17 refusal fixture exactly once, right after the datastore", name)
		}
		if wantTest && (tests != 1 || testAt != startAt+1) || !wantTest && tests != 0 {
			return fmt.Errorf("%s must run the fixture shell test %v, right after the fixture start", name, wantTest)
		}
		jobs[name]["steps"] = kept
	}
	return nil
}

// Any assignment form to the image variable: plain, declaration builtins, +=, or ${image:=...}.
var imageAssignment = regexp.MustCompile(`(^|[\s;&|(])image\+?=|\$\{image:?=`)

// A moving tag would let a new PostgreSQL release change what CI proves. Each
// fixture script pins one digest, in the assignment its docker commands use, for
// the intended major version.
func TestPostgresFixtureImagesArePinnedToDigests(t *testing.T) {
	override := imageAssignment
	for _, line := range []string{"image=x", " image=x", "export image=x", "readonly image=x", "declare -r image=x", "typeset image=x", "local image=x", "image+=x", "a; image=x", "${image:=x}", ": ${image:=x}"} {
		if !override.MatchString(line) {
			t.Errorf("assignment %q is not detected", line)
		}
	}
	for _, line := range []string{`docker run "$image"`, `echo "${image:-x}"`, `--image=x`} {
		if override.MatchString(line) {
			t.Errorf("%q is not an assignment", line)
		}
	}
	for script, major := range map[string]string{"start-ci-postgres.sh": "18", "start-old-postgres.sh": "17"} {
		raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), "scripts/ci", script))
		if err != nil {
			t.Fatal(err)
		}
		pinned := regexp.MustCompile(`^image=postgres:` + major + `(-[\w.]+)?@sha256:[0-9a-f]{64}$`)
		assignments := 0
		for _, line := range strings.Split(string(raw), "\n") {
			if override.MatchString(line) {
				assignments++
				if !pinned.MatchString(line) {
					t.Errorf("%s: %q is not a column-0 digest pin of PostgreSQL %s", script, line, major)
				}
			}
		}
		if assignments != 1 {
			t.Errorf("%s: %d image assignments, want exactly 1", script, assignments)
		}
		uses := 0
		arg := regexp.MustCompile(`(^|\s)"\$image"(\)|\s|$)`)
		for _, command := range strings.Split(strings.ReplaceAll(string(raw), "\\\n", " "), "\n") {
			if !strings.Contains(command, "docker run") && !strings.Contains(command, "docker pull") {
				continue
			}
			uses++
			if !arg.MatchString(command) || strings.Contains(command, "postgres:") {
				t.Errorf("%s: %q does not take its image only from \"$image\"", script, strings.TrimSpace(command))
			}
		}
		if uses == 0 {
			t.Errorf("%s: no docker run or pull to check", script)
		}
	}
}

func TestOldPostgresFixtureContract(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	check := func(raw string) error {
		var workflow struct {
			Jobs map[string]map[string]any `yaml:"jobs"`
		}
		if err := yaml.Unmarshal([]byte(raw), &workflow); err != nil {
			return err
		}
		return normalizeOldPostgresFixture(workflow.Jobs)
	}
	ci := string(raw)
	if err := check(ci); err != nil {
		t.Fatal(err)
	}
	startStep := "      - name: " + oldPostgresStartName + "\n        run: ./scripts/ci/start-old-postgres.sh\n"
	testStep := "      - name: " + oldPostgresTestName + "\n        run: ./scripts/ci/start-old-postgres_test.sh\n"
	if !strings.Contains(ci, startStep) || !strings.Contains(ci, testStep) {
		t.Fatal("workflow steps are not in the expected two-line form")
	}
	for name, mutated := range map[string]string{
		"renamed start":          strings.Replace(ci, oldPostgresStartName, "removed fixture", 1),
		"fail-open start":        strings.Replace(ci, startStep, startStep[:len(startStep)-1]+" || true\n", 1),
		"continue-on-error":      strings.Replace(ci, startStep, startStep+"        continue-on-error: true\n", 1),
		"conditional start":      strings.Replace(ci, startStep, startStep+"        if: ${{ false }}\n", 1),
		"duplicate start":        strings.Replace(ci, startStep, startStep+startStep, 1),
		"start before datastore": strings.Replace(strings.Replace(ci, startStep, "", 1), "      - name: Start healthy PostgreSQL", startStep+"      - name: Start healthy PostgreSQL", 1),
		"start after test":       strings.Replace(strings.Replace(ci, startStep, "", 1), testStep, testStep+startStep, 1),
		"missing shell test":     strings.Replace(ci, testStep, "", 1),
		"fail-open shell test":   strings.Replace(ci, testStep, testStep[:len(testStep)-1]+" || true\n", 1),
		"conditional test":       strings.Replace(ci, testStep, testStep+"        if: ${{ false }}\n", 1),
		"duplicate test":         strings.Replace(ci, testStep, testStep+testStep, 1),
		"test not adjacent":      strings.Replace(strings.Replace(ci, testStep, "", 1), "      - uses: actions/setup-go", testStep+"      - uses: actions/setup-go", 1),
	} {
		if mutated == ci {
			t.Errorf("%s: mutation did not change the workflow", name)
			continue
		}
		if err := check(mutated); err == nil {
			t.Errorf("accepted mutation: %s", name)
		}
	}
}
