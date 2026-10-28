package githubactions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

// Configuration errors after explicit opt-in are failures, never provider proof.
// The runner supplies the pins from a separately approved, non-secret packet.
func parseGitHubContractConfig(getenv func(string) string) (githubContractConfig, error) {
	var cfg githubContractConfig
	fields := []struct {
		name  string
		value *string
	}{
		{"OWNER", &cfg.owner}, {"REPOSITORY", &cfg.repository}, {"ENVIRONMENT", &cfg.environment},
		{"WORKFLOW_PATH", &cfg.workflowPath}, {"REF", &cfg.ref}, {"WORKFLOW_SHA", &cfg.workflowSHA},
		{"CANDIDATE_SHA", &cfg.candidateSHA}, {"AUTHORIZATION_REF", &cfg.authorizationRef},
		{"REPOSITORY_TOKEN", &cfg.repositoryToken}, {"ORGANIZATION_TOKEN", &cfg.organizationToken},
		{"ENVIRONMENT_TOKEN", &cfg.environmentToken}, {"REPOSITORY_DENIED_TOKEN", &cfg.repositoryDeniedToken},
		{"ORGANIZATION_DENIED_TOKEN", &cfg.organizationDeniedToken}, {"ENVIRONMENT_DENIED_TOKEN", &cfg.environmentDeniedToken},
		{"HARNESS_TOKEN", &cfg.harnessToken},
	}
	for _, field := range fields {
		*field.value = getenv("HIKYO_GITHUB_CONTRACT_" + field.name)
		value := *field.value
		if value == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n\x00") {
			return cfg, fmt.Errorf("github contract: missing or malformed HIKYO_GITHUB_CONTRACT_%s", field.name)
		}
	}
	for _, field := range []struct {
		name  string
		value *int64
	}{
		{"REPOSITORY_ID", &cfg.repositoryID}, {"ORGANIZATION_ID", &cfg.organizationID}, {"ENVIRONMENT_ID", &cfg.environmentID},
	} {
		value, err := strconv.ParseInt(getenv("HIKYO_GITHUB_CONTRACT_"+field.name), 10, 64)
		if err != nil || value <= 0 {
			return cfg, fmt.Errorf("github contract: invalid HIKYO_GITHUB_CONTRACT_%s", field.name)
		}
		*field.value = value
	}
	for _, value := range []string{cfg.candidateSHA, cfg.workflowSHA} {
		if len(value) != 40 || strings.IndexFunc(value, func(r rune) bool { return !strings.ContainsRune("0123456789abcdef", r) }) >= 0 {
			return cfg, fmt.Errorf("github contract: candidate and workflow pins must be full lowercase commit SHAs")
		}
	}
	if strings.ContainsAny(cfg.owner+cfg.repository, "/\\?#% ") || strings.EqualFold(cfg.repository, "Hikyo") ||
		!strings.HasPrefix(cfg.workflowPath, ".github/workflows/") || strings.Count(cfg.workflowPath, "/") != 2 || !strings.HasSuffix(cfg.workflowPath, ".yml") {
		return cfg, fmt.Errorf("github contract: dedicated fixture repository and workflow path required; product repository is forbidden")
	}
	tokens := []string{cfg.repositoryToken, cfg.organizationToken, cfg.environmentToken, cfg.repositoryDeniedToken, cfg.organizationDeniedToken, cfg.environmentDeniedToken, cfg.harnessToken}
	for i, token := range tokens {
		if !strings.HasPrefix(token, "github_pat_") || strings.ContainsAny(token, " \t") {
			return cfg, fmt.Errorf("github contract: all seven credentials must be fine-grained PATs")
		}
		for _, other := range tokens[:i] {
			if token == other {
				return cfg, fmt.Errorf("github contract: minimum, one-permission-less, and harness tokens must all be distinct")
			}
		}
	}
	return cfg, nil
}

func contractEvidence(t *testing.T, record map[string]any) {
	t.Helper()
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("GITHUB_CONTRACT_EVIDENCE %s", encoded)
}

func ownContractName(t *testing.T, client *Client, destination adapter.Destination, surface adapter.Surface, name string) {
	t.Helper()
	contractEvidence(t, map[string]any{"kind": "owned_name", "destination": destination.Kind, "surface": surface, "name": name})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		var err error
		if surface == adapter.Secret {
			err = client.DeleteSecret(ctx, destination, name)
		} else {
			err = client.DeleteVariable(ctx, destination, name)
		}
		cleanupContractName(t, err, string(surface)+" "+name)
		if err == nil || IsStatus(err, http.StatusNotFound) {
			contractEvidence(t, map[string]any{"kind": "cleaned_name", "destination": destination.Kind, "surface": surface, "name": name})
		}
	})
}

// The API request is authenticated only at api.github.com. Artifact redirects
// use their signed download URL without forwarding the harness credential.
func contractRedirect(request *http.Request, via []*http.Request) error {
	request.Header.Del("Authorization")
	if len(via) >= 5 || request.URL.Scheme != "https" || request.URL.User != nil {
		return fmt.Errorf("github contract: unsafe artifact redirect")
	}
	return nil
}

type contractWorkflowRun struct {
	ID           int64     `json:"id"`
	DisplayTitle string    `json:"display_title"`
	Status       string    `json:"status"`
	Conclusion   string    `json:"conclusion"`
	CreatedAt    time.Time `json:"created_at"`
	HeadSHA      string    `json:"head_sha"`
	Event        string    `json:"event"`
	RunAttempt   int       `json:"run_attempt"`
	Repository   struct {
		ID int64 `json:"id"`
	} `json:"repository"`
}

func (c *contractHTTP) dispatchRuns(ctx context.Context, workflowID, nonce string, started time.Time) ([]contractWorkflowRun, error) {
	var matches []contractWorkflowRun
	for page := 1; page <= 100; page++ {
		var response struct {
			Total *int                  `json:"total_count"`
			Runs  []contractWorkflowRun `json:"workflow_runs"`
		}
		endpoint := c.repoPath("/actions/workflows/"+workflowID+"/runs") + "?event=workflow_dispatch&per_page=100&page=" + strconv.Itoa(page)
		if _, err := c.api(ctx, http.MethodGet, endpoint, nil, &response); err != nil {
			return nil, err
		}
		if response.Total == nil || *response.Total < 0 || response.Runs == nil || len(response.Runs) != min(100, max(0, *response.Total-(page-1)*100)) {
			return nil, fmt.Errorf("incomplete dispatch run inventory")
		}
		for _, run := range response.Runs {
			if run.DisplayTitle == "hikyo-contract-"+nonce && !run.CreatedAt.Before(started) {
				if run.ID <= 0 || run.Repository.ID != c.cfg.repositoryID {
					return nil, fmt.Errorf("workflow run repository identity mismatch")
				}
				matches = append(matches, run)
			}
		}
		if page*100 >= *response.Total {
			return matches, nil
		}
		if len(response.Runs) != 100 {
			return nil, fmt.Errorf("incomplete dispatch run inventory")
		}
	}
	return nil, fmt.Errorf("dispatch run inventory exceeded bound")
}

func (c *contractHTTP) cleanupDispatch(ctx context.Context, workflowID, nonce string, started time.Time, owned map[int64]bool, record func(int64, string)) error {
	runs, err := c.dispatchRuns(ctx, workflowID, nonce, started)
	var failures []error
	if err != nil {
		failures = append(failures, err)
	}
	for _, run := range runs {
		if !owned[run.ID] {
			owned[run.ID] = true
			record(run.ID, "owned_run")
		}
	}
	// Include already observed runs even if a listing is momentarily lagging.
	for id := range owned {
		if err := c.cleanupWorkflowRun(ctx, id); err != nil {
			failures = append(failures, fmt.Errorf("run %d: %w", id, err))
			continue
		}
		record(id, "cleaned_run")
	}
	if len(failures) != 0 {
		return errors.Join(failures...)
	}
	if len(owned) == 0 {
		return fmt.Errorf("dispatch outcome unobserved; retain nonce for later reconciliation")
	}
	return nil
}

func (c *contractHTTP) cleanupWorkflowRun(ctx context.Context, runID int64) error {
	if runID <= 0 {
		return fmt.Errorf("invalid owned run id")
	}
	runPath := c.repoPath("/actions/runs/" + strconv.FormatInt(runID, 10))
	var run struct {
		Status string `json:"status"`
	}
	if _, err := c.api(ctx, http.MethodGet, runPath, nil, &run); err != nil {
		return err
	}
	if run.Status != "completed" {
		if _, err := c.api(ctx, http.MethodPost, runPath+"/cancel", nil, nil); err != nil {
			return err
		}
		for run.Status != "completed" {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
			}
			if _, err := c.api(ctx, http.MethodGet, runPath, nil, &run); err != nil {
				return err
			}
		}
	}
	// Re-read page one after deletion, including artifacts uploaded during
	// cancellation. Never delete another run's artifacts or read variable values.
	for range 11 {
		var artifacts struct {
			Total     *int `json:"total_count"`
			Artifacts []struct {
				ID int64 `json:"id"`
			} `json:"artifacts"`
		}
		if _, err := c.api(ctx, http.MethodGet, runPath+"/artifacts?per_page=100", nil, &artifacts); err != nil {
			return err
		}
		if artifacts.Total == nil || artifacts.Artifacts == nil {
			return fmt.Errorf("missing owned-run artifact inventory")
		}
		if *artifacts.Total == 0 && len(artifacts.Artifacts) == 0 {
			return nil
		}
		if *artifacts.Total < len(artifacts.Artifacts) || len(artifacts.Artifacts) == 0 {
			return fmt.Errorf("incomplete owned-run artifact inventory")
		}
		var failures []error
		for _, artifact := range artifacts.Artifacts {
			if artifact.ID <= 0 {
				failures = append(failures, fmt.Errorf("invalid owned artifact id"))
				continue
			}
			if err := c.cleanupArtifact(ctx, artifact.ID); err != nil {
				failures = append(failures, err)
			}
		}
		if len(failures) != 0 {
			return errors.Join(failures...)
		}
	}
	return fmt.Errorf("owned-run artifact cleanup exceeded bounded inventory")
}

func (c *contractHTTP) cleanupArtifact(ctx context.Context, artifactID int64) error {
	if artifactID <= 0 {
		return fmt.Errorf("invalid owned artifact id")
	}
	status, err := c.api(ctx, http.MethodDelete, c.repoPath("/actions/artifacts/"+strconv.FormatInt(artifactID, 10)), nil, nil)
	if status == http.StatusNotFound {
		return nil
	}
	if err != nil {
		return err
	}
	if status != http.StatusNoContent {
		return fmt.Errorf("owned artifact deletion status %d", status)
	}
	return nil
}
