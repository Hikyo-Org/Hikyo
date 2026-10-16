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
const retirementContextBatchSize = 10
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

type retirementHistoryContext struct {
	Kind       string `json:"__typename"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	ExternalID string `json:"externalId"`
	Suite      *struct {
		App *struct {
			ID   int64  `json:"databaseId"`
			Slug string `json:"slug"`
		} `json:"app"`
	} `json:"checkSuite"`
}

type retirementContexts struct {
	Total *int                       `json:"totalCount"`
	Nodes []retirementHistoryContext `json:"nodes"`
	Page  retirementPageInfo         `json:"pageInfo"`
}

type retirementHistoryCommit struct {
	OID    string `json:"oid"`
	Rollup *struct {
		State    string             `json:"state"`
		Contexts retirementContexts `json:"contexts"`
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
func (c *controller) discoverRetirements(ctx context.Context, name string, e event) (retirementTargets, error) {
	targets := retirementTargets{Queue: []string{}, Observation: []string{}, ObservationBatches: [][]string{}, ReporterTargets: []reporterRetirementTarget{}}
	if err := c.init(ctx); err != nil {
		return targets, err
	}
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
		for start := 0; start < len(candidates); start += retirementContextBatchSize {
			batch := candidates[start:min(start+retirementContextBatchSize, len(candidates))]
			commits, err := c.retirementContextBatch(ctx, batch)
			if err != nil {
				return retirementTargets{}, err
			}
			for _, commit := range commits {
				q, o, reporterWork, err := c.retirementPendingContexts(ctx, commit)
				if err != nil {
					return retirementTargets{}, err
				}
				if !q && !o && len(reporterWork) == 0 {
					continue
				}
				seenHeads[commit.OID] = true
				if len(seenHeads) > retirementHistoryHeads {
					return retirementTargets{}, errors.New("retirement discovery exceeds 100 heads; operator backfill required before retry")
				}
				if q {
					queue[commit.OID] = true
				}
				if o {
					observation[commit.OID] = true
				}
				for _, target := range reporterWork {
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

func (c *controller) retirementContextsQuery(head, after string) string {
	parts := strings.Split(c.repoName, "/")
	return fmt.Sprintf(`query{repository(owner:%q,name:%q){databaseId object(oid:%q){... on Commit{oid statusCheckRollup{contexts(first:100,after:%q){totalCount nodes{__typename ... on CheckRun{name status externalId checkSuite{app{databaseId slug}}}} pageInfo{hasNextPage endCursor}}}}}}}`, parts[0], parts[1], head, after)
}

// A rollup enumerates effective contexts, rather than every historical check
// run. Follow its connection completely; the selected head's worker later
// inventories and authenticates all owned REST history before cancellation.
func (c *controller) completeRetirementContexts(ctx context.Context, head string, part retirementContexts) ([]retirementHistoryContext, error) {
	all := []retirementHistoryContext{}
	total := -1
	seen := make(map[string]bool)
	for page := 1; page <= 10; page++ {
		if part.Total == nil || *part.Total < 0 || part.Nodes == nil || len(part.Nodes) > 100 || part.Page.More == nil || total != -1 && total != *part.Total {
			return nil, errors.New("incomplete or changing retirement status context inventory")
		}
		total = *part.Total
		all = append(all, part.Nodes...)
		if len(all) > total || total > 1000 {
			return nil, fmt.Errorf("retirement contexts exceed complete 1000-record budget on %s; operator exact-head backfill required", head)
		}
		if !*part.Page.More {
			if len(all) != total {
				return nil, errors.New("incomplete retirement status context pagination")
			}
			return all, nil
		}
		if len(part.Nodes) == 0 || len(all) >= total || part.Page.End == nil || *part.Page.End == "" || len(*part.Page.End) > 1024 || seen[*part.Page.End] {
			return nil, errors.New("invalid or repeated retirement status context cursor")
		}
		cursor := *part.Page.End
		seen[cursor] = true
		if page == 10 {
			return nil, errors.New("retirement contexts exceed complete ten-page budget; operator exact-head backfill required")
		}
		var response struct {
			Errors []json.RawMessage `json:"errors"`
			Data   struct {
				Repo *struct {
					ID     int64 `json:"databaseId"`
					Object *struct {
						OID    string `json:"oid"`
						Rollup *struct {
							Contexts retirementContexts `json:"contexts"`
						} `json:"statusCheckRollup"`
					} `json:"object"`
				} `json:"repository"`
			} `json:"data"`
		}
		if err := c.api.read(ctx, "graphql:"+c.retirementContextsQuery(head, cursor), &response); err != nil {
			return nil, err
		}
		if len(response.Errors) != 0 {
			return nil, graphQLDiagnostic(response.Errors)
		}
		repo := response.Data.Repo
		if repo == nil || repo.ID != c.repo.ID || repo.Object == nil || repo.Object.OID != head || repo.Object.Rollup == nil {
			return nil, errors.New("missing or foreign retirement context page")
		}
		part = repo.Object.Rollup.Contexts
	}
	return nil, errors.New("retirement contexts exceed complete ten-page budget; operator exact-head backfill required")
}

func (c *controller) retirementContextBatchQuery(heads []string) string {
	parts := strings.Split(c.repoName, "/")
	var query strings.Builder
	fmt.Fprintf(&query, `query{repository(owner:%q,name:%q){databaseId `, parts[0], parts[1])
	for i, head := range heads {
		fmt.Fprintf(&query, `h%d:object(oid:%q){... on Commit{oid statusCheckRollup{contexts(first:100){totalCount nodes{__typename ... on CheckRun{name status externalId checkSuite{app{databaseId slug}}}} pageInfo{hasNextPage endCursor}}}}} `, i, head)
	}
	query.WriteString("}}")
	return query.String()
}
func (c *controller) retirementContextBatch(ctx context.Context, heads []string) ([]retirementHistoryCommit, error) {
	if len(heads) == 0 || len(heads) > retirementContextBatchSize {
		return nil, errors.New("invalid retirement context batch")
	}
	var response struct {
		Errors []json.RawMessage `json:"errors"`
		Data   struct {
			Repo map[string]json.RawMessage `json:"repository"`
		} `json:"data"`
	}
	if err := c.api.read(ctx, "graphql:"+c.retirementContextBatchQuery(heads), &response); err != nil {
		return nil, err
	}
	if len(response.Errors) != 0 {
		return nil, graphQLDiagnostic(response.Errors)
	}
	var repoID int64
	if err := decode(response.Data.Repo["databaseId"], &repoID); err != nil || repoID != c.repo.ID {
		return nil, errors.New("missing or foreign retirement context batch repository")
	}
	commits := make([]retirementHistoryCommit, 0, len(heads))
	for i, head := range heads {
		var commit *retirementHistoryCommit
		if err := decode(response.Data.Repo[fmt.Sprintf("h%d", i)], &commit); err != nil || commit == nil || commit.OID != head {
			return nil, errors.New("missing or foreign retirement context batch commit")
		}
		commits = append(commits, *commit)
	}
	return commits, nil
}
func (c *controller) retirementPendingContexts(ctx context.Context, commit retirementHistoryCommit) (bool, bool, []reporterRetirementTarget, error) {
	if commit.Rollup == nil {
		return false, false, nil, errors.New("retirement candidate rollup disappeared during discovery; retry a complete snapshot")
	}
	contexts, err := c.completeRetirementContexts(ctx, commit.OID, commit.Rollup.Contexts)
	if err != nil {
		return false, false, nil, err
	}
	queue, observation := false, false
	reporters := []reporterRetirementTarget{}
	for _, check := range contexts {
		if check.Kind == "StatusContext" {
			continue
		}
		if check.Kind != "CheckRun" {
			return false, false, nil, errors.New("unknown retirement status context kind")
		}
		isReporter := strings.HasPrefix(check.Name, "Merge queue validation (run ") || strings.HasPrefix(check.ExternalID, "hikyo-queue-report:")
		if check.Name != queueCheckName && check.Name != "ci-required-next" && !isReporter || check.Suite == nil || check.Suite.App == nil || check.Suite.App.ID != actionsAppID || check.Suite.App.Slug != "github-actions" {
			continue
		}
		switch check.Status {
		case "COMPLETED":
			continue
		case "QUEUED", "IN_PROGRESS", "WAITING", "PENDING", "REQUESTED":
		default:
			return false, false, nil, errors.New("unknown owned synthetic check status")
		}
		if isReporter {
			target, err := c.reporterRetirementHint(commit.OID, check)
			if err != nil {
				return false, false, nil, err
			}
			reporters = append(reporters, target)
			continue
		}
		b, err := decodeBinding(check.ExternalID)
		if err != nil || b.RepoID != c.repo.ID || b.Head != commit.OID {
			return false, false, nil, fmt.Errorf("unfinished synthetic context has ambiguous ownership on %s; operator review required", commit.OID)
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
func (c *controller) reporterRetirementHint(head string, check retirementHistoryContext) (reporterRetirementTarget, error) {
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
