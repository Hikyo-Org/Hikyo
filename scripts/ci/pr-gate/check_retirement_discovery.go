package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

const retirementHistoryPages = 20
const retirementHistoryWindow = 1000
const retirementHistoryHeads = 100
const retirementReporterTargets = 100

type reporterRetirementTarget struct {
	Head    string `json:"head"`
	Run     int64  `json:"run"`
	Attempt int64  `json:"attempt"`
}

type retirementHistoryScope struct {
	Snapshot         string `json:"snapshot"`
	MaxCommits       int    `json:"max_commits"`
	ScannedCommits   int    `json:"scanned_commits"`
	CompleteAncestry bool   `json:"complete_ancestry"`
	WindowLimited    bool   `json:"window_limited"`
	NextCursor       string `json:"next_cursor,omitempty"`
}

type retirementTargets struct {
	HistoryScope       retirementHistoryScope     `json:"history_scope"`
	Queue              []string                   `json:"queue_heads"`
	Observation        []string                   `json:"observation_heads"`
	ObservationBatches [][]string                 `json:"observation_batches"`
	ReporterTargets    []reporterRetirementTarget `json:"reporter_targets"`
}

type retirementPageInfo struct {
	More *bool   `json:"hasNextPage"`
	End  *string `json:"endCursor"`
}

type retirementHistoryCommit struct {
	OID    string `json:"oid"`
	Rollup *struct {
		State string `json:"state"`
	} `json:"statusCheckRollup"`
}

func (c *controller) retirementHistoryQuery(head, after string, first int) string {
	parts := strings.Split(c.repoName, "/")
	args := fmt.Sprintf("first:%d", first)
	if after != "" {
		args += fmt.Sprintf(",after:%q", after)
	}
	return fmt.Sprintf(`query{repository(owner:%q,name:%q){databaseId object(oid:%q){... on Commit{oid history(%s){nodes{oid statusCheckRollup{state}} pageInfo{hasNextPage endCursor}}}}}}`, parts[0], parts[1], head, args)
}

// This read-only scheduled scan never acquires a writer lease or publishes a
// verdict. It inspects at most the newest 1000 commits of an immutable main
// snapshot and explicitly reports when older ancestry remains outside that window.
// Each discovered head is merely a hint for the existing independent
// per-context ownership, live-role and readback proof under the proper lease.
func (c *controller) discoverRetirements(ctx context.Context, name string, e event) (targets retirementTargets, err error) {
	scan := newRetirementDiscoveryScan()
	defer func() {
		if reportErr := scan.report(c.diagnostics, err); reportErr != nil {
			err = errors.Join(err, reportErr)
		}
		if err != nil {
			targets = retirementTargets{}
		}
	}()
	targets = retirementTargets{Queue: []string{}, Observation: []string{}, ObservationBatches: [][]string{}, ReporterTargets: []reporterRetirementTarget{}}
	if err := c.init(ctx); err != nil {
		return targets, err
	}
	reader := *c
	reader.api = retirementDiscoveryReadAPI{api: c.api, scan: scan}
	c = &reader
	trustedSource := name == "schedule" || name == "workflow_dispatch" && e.Ref == "refs/heads/"+c.repo.DefaultBranch && e.Inputs.Head == "" && e.Inputs.PR == "" && e.Inputs.Work == ""
	if !trustedSource || c.expectedHead != "" || e.Repository.ID != c.repo.ID || !strings.EqualFold(e.Repository.FullName, c.repoName) || e.Repository.DefaultBranch != c.repo.DefaultBranch || os.Getenv("GITHUB_REF") != "refs/heads/"+c.repo.DefaultBranch {
		return targets, errors.New("retirement discovery requires a trusted default-branch schedule or empty-head dispatch without a writer lease")
	}
	var ref struct {
		Ref    string `json:"ref"`
		Object struct {
			SHA  string `json:"sha"`
			Type string `json:"type"`
		} `json:"object"`
	}
	if err := c.api.read(ctx, c.endpoint("git/ref/heads/"+c.repo.DefaultBranch), &ref); err != nil {
		return targets, err
	}
	if ref.Ref != "refs/heads/"+c.repo.DefaultBranch || ref.Object.Type != "commit" || !shaPattern.MatchString(ref.Object.SHA) {
		return targets, errors.New("retirement discovery could not authenticate current main")
	}
	head, after := ref.Object.SHA, ""
	scan.snapshot = head
	seenCommits, seenCursors := make(map[string]bool), make(map[string]bool)
	queue, observation := make(map[string]bool), make(map[string]bool)
	seenHeads := make(map[string]bool)
	reporters := make(map[string]reporterRetirementTarget)
	for page := 1; page <= retirementHistoryPages; page++ {
		first := min(100, retirementHistoryWindow-len(seenCommits))
		var response struct {
			Errors []json.RawMessage `json:"errors"`
			Data   struct {
				Repo *struct {
					ID     int64 `json:"databaseId"`
					Object *struct {
						OID     string `json:"oid"`
						History struct {
							Nodes []retirementHistoryCommit `json:"nodes"`
							Page  retirementPageInfo        `json:"pageInfo"`
						} `json:"history"`
					} `json:"object"`
				} `json:"repository"`
			} `json:"data"`
		}
		if err := c.api.read(ctx, "graphql:"+c.retirementHistoryQuery(head, after, first), &response); err != nil {
			return retirementTargets{}, fmt.Errorf("retirement history page %d: %w", page, err)
		}
		scan.historyPages++
		if len(response.Errors) != 0 {
			return retirementTargets{}, graphQLDiagnostic(response.Errors)
		}
		repo := response.Data.Repo
		if repo == nil || repo.ID != c.repo.ID || repo.Object == nil || repo.Object.OID != head || repo.Object.History.Nodes == nil || len(repo.Object.History.Nodes) > first || repo.Object.History.Page.More == nil {
			return retirementTargets{}, errors.New("incomplete or foreign retirement main-history snapshot")
		}
		part := repo.Object.History
		if len(part.Nodes) == 0 {
			return retirementTargets{}, errors.New("empty retirement history page cannot establish complete ancestry")
		}
		if page == 1 && part.Nodes[0].OID != head {
			return retirementTargets{}, errors.New("retirement history did not start at authenticated main")
		}
		candidates := []string{}
		for _, commit := range part.Nodes {
			scan.commitsExamined++
			if !shaPattern.MatchString(commit.OID) || seenCommits[commit.OID] {
				return retirementTargets{}, errors.New("duplicate or invalid retirement history commit")
			}
			seenCommits[commit.OID] = true
			if commit.Rollup == nil {
				continue
			}
			switch commit.Rollup.State {
			case "SUCCESS":
				continue
			case "ERROR", "EXPECTED", "FAILURE", "PENDING":
				candidates = append(candidates, commit.OID)
			default:
				return retirementTargets{}, errors.New("unknown aggregate retirement rollup state")
			}
		}
		inventories, err := c.retirementCandidateInventories(ctx, candidates, scan)
		if err != nil {
			return retirementTargets{}, err
		}
		for _, inventory := range inventories {
			if !inventory.queue && !inventory.observation && len(inventory.reporters) == 0 {
				continue
			}
			seenHeads[inventory.head] = true
			if len(seenHeads) > retirementHistoryHeads {
				return retirementTargets{}, errors.New("retirement discovery exceeds 100 heads; operator backfill required before retry")
			}
			if inventory.queue {
				queue[inventory.head] = true
			}
			if inventory.observation {
				observation[inventory.head] = true
			}
			for _, target := range inventory.reporters {
				key := fmt.Sprintf("%d:%d", target.Run, target.Attempt)
				if old, exists := reporters[key]; exists && old.Head != target.Head {
					return retirementTargets{}, errors.New("reporter source attempt has ambiguous discovery heads")
				}
				reporters[key] = target
				if len(reporters) > retirementReporterTargets {
					return retirementTargets{}, errors.New("reporter discovery exceeds 100 source attempts; operator backfill required")
				}
			}
		}
		if !*part.Page.More {
			targets.HistoryScope.CompleteAncestry = true
			break
		}
		if part.Page.End == nil || *part.Page.End == "" || len(*part.Page.End) > 1024 || seenCursors[*part.Page.End] {
			return retirementTargets{}, errors.New("missing or repeated retirement history cursor")
		}
		after = *part.Page.End
		seenCursors[after] = true
		if len(seenCommits) == retirementHistoryWindow {
			targets.HistoryScope.WindowLimited = true
			targets.HistoryScope.NextCursor = after
			break
		}
		if page == retirementHistoryPages {
			return retirementTargets{}, errors.New("retirement discovery could not complete its recent window within the 20-page read budget; operator review required")
		}
	}
	targets.HistoryScope.Snapshot = head
	targets.HistoryScope.MaxCommits = retirementHistoryWindow
	targets.HistoryScope.ScannedCommits = len(seenCommits)
	for head := range queue {
		targets.Queue = append(targets.Queue, head)
	}
	for head := range observation {
		targets.Observation = append(targets.Observation, head)
	}
	sort.Strings(targets.Queue)
	sort.Strings(targets.Observation)
	targets.ObservationBatches = retirementObservationBatches(targets.Observation)
	for _, target := range reporters {
		targets.ReporterTargets = append(targets.ReporterTargets, target)
	}
	sort.Slice(targets.ReporterTargets, func(i, j int) bool {
		a, b := targets.ReporterTargets[i], targets.ReporterTargets[j]
		if a.Run != b.Run {
			return a.Run < b.Run
		}
		return a.Attempt < b.Attempt
	})
	return targets, nil
}

// REST exposes each check's app identity without traversing a foreign app's
// GraphQL object, which can be denied by that app owner's IP allow list. Latest
// preserves effective-context discovery; the worker later authenticates all owned
// historical checks before cancellation. Never return a partial inventory.
func (c *controller) completeRetirementChecks(ctx context.Context, head string) ([]check, error) {
	all := []check{}
	total := -1
	seen := make(map[int64]bool)
	for page := 1; page <= 10; page++ {
		var part struct {
			Total  *int    `json:"total_count"`
			Checks []check `json:"check_runs"`
		}
		path := fmt.Sprintf("commits/%s/check-runs?filter=latest&per_page=100&page=%d", head, page)
		if err := c.api.read(ctx, c.endpoint(path), &part); err != nil {
			return nil, fmt.Errorf("retirement checks for %s page %d: %w", head, page, err)
		}
		if part.Total == nil || *part.Total < 0 || part.Checks == nil || len(part.Checks) > 100 || total != -1 && total != *part.Total {
			return nil, errors.New("incomplete or changing retirement check inventory")
		}
		total = *part.Total
		if total > 1000 {
			return nil, fmt.Errorf("retirement checks exceed complete 1000-record budget on %s; operator exact-head backfill required", head)
		}
		for _, check := range part.Checks {
			if check.ID <= 0 || seen[check.ID] || check.HeadSHA != head || check.Name == "" || check.App.ID <= 0 || check.App.Slug == "" {
				return nil, errors.New("incomplete, duplicate or foreign retirement check identity")
			}
			seen[check.ID] = true
			switch check.Status {
			case "completed", "queued", "in_progress", "waiting", "pending", "requested":
			default:
				return nil, errors.New("unknown retirement check status")
			}
		}
		all = append(all, part.Checks...)
		if len(all) == total {
			return all, nil
		}
		if len(part.Checks) < 100 || len(all) > total {
			return nil, errors.New("incomplete retirement check pagination")
		}
	}
	return nil, errors.New("retirement checks exceed complete ten-page budget; operator exact-head backfill required")
}

func (c *controller) retirementPendingChecks(ctx context.Context, head string) (bool, bool, []reporterRetirementTarget, error) {
	checks, err := c.completeRetirementChecks(ctx, head)
	if err != nil {
		return false, false, nil, err
	}
	queue, observation := false, false
	reporters := []reporterRetirementTarget{}
	for _, check := range checks {
		isReporter := strings.HasPrefix(check.Name, "Merge queue validation (run ") || strings.HasPrefix(check.ExternalID, "hikyo-queue-report:")
		if check.Name != queueCheckName && check.Name != "ci-required-next" && !isReporter || check.App.ID != actionsAppID || check.App.Slug != "github-actions" {
			continue
		}
		if check.Status == "completed" {
			continue
		}
		if isReporter {
			target, err := c.reporterRetirementHint(head, check)
			if err != nil {
				return false, false, nil, err
			}
			reporters = append(reporters, target)
			continue
		}
		b, err := decodeBinding(check.ExternalID)
		if err != nil || b.RepoID != c.repo.ID || b.Head != head {
			return false, false, nil, fmt.Errorf("unfinished synthetic context has ambiguous ownership on %s; operator review required", head)
		}
		if check.Name == queueCheckName {
			queue = true
		} else {
			observation = true
		}
	}
	return queue, observation, reporters, nil
}

func retirementObservationBatches(heads []string) [][]string {
	batches := [][]string{}
	for start := 0; start < len(heads); start += 10 {
		batches = append(batches, heads[start:min(start+10, len(heads))])
	}
	return batches
}

// This validates only a canonical routing hint. The reporter owner independently
// reauthenticates the source workflow/attempt, immutable merge parents, original
// PR-head check and terminal readback before mutating any record.
func (c *controller) reporterRetirementHint(head string, check check) (reporterRetirementTarget, error) {
	parts := strings.Split(check.ExternalID, ":")
	if len(parts) != 8 || parts[0] != "hikyo-queue-report" || parts[1] != "v1" || !shaPattern.MatchString(head) || !shaPattern.MatchString(parts[6]) || !shaPattern.MatchString(parts[7]) {
		return reporterRetirementTarget{}, errors.New("malformed reporter discovery binding; operator review required")
	}
	ids := make([]int64, 4)
	for i := range ids {
		n, err := strconv.ParseInt(parts[i+2], 10, 64)
		if err != nil || n <= 0 || strconv.FormatInt(n, 10) != parts[i+2] {
			return reporterRetirementTarget{}, errors.New("noncanonical reporter discovery identity")
		}
		ids[i] = n
	}
	if ids[0] != c.repo.ID || check.Name != fmt.Sprintf("Merge queue validation (run %d, attempt %d)", ids[2], ids[3]) {
		return reporterRetirementTarget{}, errors.New("foreign reporter discovery repository or context name")
	}
	return reporterRetirementTarget{Head: head, Run: ids[2], Attempt: ids[3]}, nil
}
