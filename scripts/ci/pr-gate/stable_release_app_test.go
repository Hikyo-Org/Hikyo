package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

const testAppBotID = 424242

func appPull(f *queueFixture, number int, branch string) pull {
	p := f.pr
	p.Number = number
	p.User.ID = testAppBotID
	p.User.Login = "some-app[bot]"
	p.Head.Ref = branch
	p.Head.Repo = f.c.repo
	p.Base.Ref = f.c.repo.DefaultBranch
	return p
}

func setPullFiles(f *queueFixture, number int, files []pullFile) {
	f.api.responses[f.c.endpoint(fmt.Sprintf("pulls/%d/files?per_page=100&page=1", number))] = files
}

func trustFiles(names ...string) []pullFile {
	var files []pullFile
	for _, n := range names {
		files = append(files, pullFile{Name: "release/trust/" + n, Status: "modified"})
	}
	return files
}

func allTrustFiles() []pullFile {
	return trustFiles("metadata.json", "metadata.sigstore.json", "catalog.json", "catalog.sigstore.json")
}

func setPullCommits(f *queueFixture, number int, commits []pullCommit) {
	for page := 1; ; page++ {
		lo := (page - 1) * 100
		var chunk []pullCommit
		if lo < len(commits) {
			chunk = commits[lo:min(lo+100, len(commits))]
		}
		f.api.responses[f.c.endpoint(fmt.Sprintf("pulls/%d/commits?per_page=100&page=%d", number, page))] = chunk
		if len(chunk) < 100 {
			return
		}
	}
}

func cleanCommits(n int) []pullCommit {
	commits := make([]pullCommit, n)
	for i := range commits {
		commits[i].Author = &struct {
			ID int64 `json:"id"`
		}{ID: 49699333}
		commits[i].Commit.Author.Email = "49699333+dependabot[bot]@users.noreply.github.com"
		commits[i].Commit.Committer.Email = "noreply@github.com"
	}
	return commits
}

func TestStableReleaseAppMayOnlyOpenTheTrustSyncPullRequest(t *testing.T) {
	const syncBranch = "release/sync-trust-1.1.1"
	forkRepo := repository{ID: 999, FullName: "someone/fork", DefaultBranch: "main"}
	cases := []struct {
		name    string
		branch  string
		files   []pullFile
		mutate  func(*pull)
		allowed bool
	}{
		{name: "the four trust files", branch: syncBranch, files: allTrustFiles(), allowed: true},
		{name: "a prerelease trust sync", branch: "release/sync-trust-1.2.0-rc.1", files: allTrustFiles(), allowed: true},
		{name: "a subset of the trust files", branch: syncBranch, files: trustFiles("catalog.json"), allowed: true},
		{name: "added trust files", branch: syncBranch, files: []pullFile{{Name: "release/trust/metadata.json", Status: "added"}}, allowed: true},
		{name: "code beside the trust files", branch: syncBranch, files: append(allTrustFiles(), pullFile{Name: "cmd/hikyo/main.go", Status: "modified"})},
		{name: "a workflow", branch: syncBranch, files: []pullFile{{Name: ".github/workflows/release.yml", Status: "modified"}}},
		{name: "the trust root", branch: syncBranch, files: trustFiles("root.json")},
		{name: "a trust file in a subdirectory", branch: syncBranch, files: []pullFile{{Name: "release/trust/x/metadata.json", Status: "modified"}}},
		{name: "a deleted trust file", branch: syncBranch, files: []pullFile{{Name: "release/trust/metadata.json", Status: "removed"}}},
		{name: "a renamed trust file", branch: syncBranch, files: []pullFile{{Name: "release/trust/metadata.json", Previous: "cmd/hikyo/main.go", Status: "renamed"}}},
		{name: "no files at all", branch: syncBranch, files: nil},
		{name: "another branch name", branch: "feature/payload", files: allTrustFiles()},
		{name: "a sync-trust look-alike", branch: "release/sync-trust-1.1.1/../x", files: allTrustFiles()},
		{name: "a sync-trust prefix without a version", branch: "release/sync-trust-", files: allTrustFiles()},
		{name: "a dependabot branch", branch: "dependabot/go_modules/x", files: allTrustFiles()},
		{name: "a fork head", branch: syncBranch, files: allTrustFiles(), mutate: func(p *pull) { p.Head.Repo = forkRepo }},
		{name: "a base other than main", branch: syncBranch, files: allTrustFiles(), mutate: func(p *pull) { p.Base.Ref = "dev" }},
	}
	t.Setenv(stableReleaseAppBotEnv, fmt.Sprint(testAppBotID))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newQueueFixture(t)
			p := appPull(f, 7, tc.branch)
			if tc.mutate != nil {
				tc.mutate(&p)
			}
			setPullFiles(f, 7, tc.files)
			verified := false
			f.c.verify = func(context.Context, int, string) (policyDecision, error) {
				verified = true
				return policyDecision{}, nil
			}
			decision, err := f.q.verifyPR(context.Background(), p)
			if err != nil {
				t.Fatal(err)
			}
			if tc.allowed && (decision.Code != 0 || !verified) {
				t.Fatalf("a trust-sync pull request was refused: %+v verified=%v", decision, verified)
			}
			if !tc.allowed && (decision.Code != 1 || verified) {
				t.Fatalf("the stable-release App got a pull request through: %+v verified=%v", decision, verified)
			}
		})
	}
}

func TestStableReleaseAppRefusalIsBoundToTheConfiguredBotOnly(t *testing.T) {
	f := newQueueFixture(t)
	f.c.verify = func(context.Context, int, string) (policyDecision, error) { return policyDecision{}, nil }
	// Anyone else, whatever the name, is judged by the ordinary policy; the only
	// extra read is the check that none of their commits belongs to the App.
	setPullCommits(f, 7, cleanCommits(3))
	for _, login := range []string{"some-app[bot]", "dependabot[bot]", "octocat"} {
		p := appPull(f, 7, "feature/x")
		p.User.ID = 1
		p.User.Login = login
		t.Setenv(stableReleaseAppBotEnv, fmt.Sprint(testAppBotID))
		if decision, err := f.q.verifyPR(context.Background(), p); err != nil || decision.Code != 0 {
			t.Fatalf("%s was refused as the stable-release App: %+v %v", login, decision, err)
		}
	}
	for read := range f.api.reads {
		if !strings.Contains(read, "pulls/7/commits") {
			t.Fatalf("a pull request of someone else cost an API read besides its commits: %v", f.api.reads)
		}
	}
	// Inert until the owner configures the App.
	t.Setenv(stableReleaseAppBotEnv, "")
	p := appPull(f, 7, "feature/x")
	if decision, err := f.q.verifyPR(context.Background(), p); err != nil || decision.Code != 0 {
		t.Fatalf("an unconfigured gate refused a pull request: %+v %v", decision, err)
	}
	// A malformed configuration fails the gate rather than disabling the rule.
	for _, bad := range []string{"some-app", "0", "-5", "12x"} {
		t.Setenv(stableReleaseAppBotEnv, bad)
		if _, err := f.q.verifyPR(context.Background(), appPull(f, 7, "feature/x")); err == nil {
			t.Fatalf("malformed bot id %q disabled the rule", bad)
		}
	}
}

func TestStableReleaseAppRefusalFailsClosedOnUnreadableOrHugeFileLists(t *testing.T) {
	t.Setenv(stableReleaseAppBotEnv, fmt.Sprint(testAppBotID))
	f := newQueueFixture(t)
	f.c.verify = func(context.Context, int, string) (policyDecision, error) { return policyDecision{}, nil }
	p := appPull(f, 7, "release/sync-trust-1.1.1")
	// The file list cannot be read: the gate fails instead of admitting.
	if _, err := f.q.verifyPR(context.Background(), p); err == nil {
		t.Fatal("an unreadable file list admitted the pull request")
	}
	// More pages than GitHub lists: refused, never admitted on a partial list.
	for page := 1; page <= pullFilePages; page++ {
		files := make([]pullFile, 100)
		for i := range files {
			files[i] = pullFile{Name: "release/trust/metadata.json", Status: "modified"}
		}
		f.api.responses[f.c.endpoint(fmt.Sprintf("pulls/7/files?per_page=100&page=%d", page))] = files
	}
	if decision, err := f.q.verifyPR(context.Background(), p); err != nil || decision.Code != 1 {
		t.Fatalf("a file list beyond GitHub's limit was admitted: %+v %v", decision, err)
	}
}

func TestStableReleaseAppIsRefusedWhenTheQueueGroupIsVerifiedToo(t *testing.T) {
	t.Setenv(stableReleaseAppBotEnv, fmt.Sprint(testAppBotID))
	f := newQueueFixture(t)
	member := appPull(f, 7, "feature/payload")
	f.api.responses[f.c.endpoint("pulls/7")] = member
	setPullFiles(f, 7, []pullFile{{Name: "cmd/hikyo/main.go", Status: "modified"}})
	f.c.verify = func(context.Context, int, string) (policyDecision, error) { return policyDecision{}, nil }
	_, _, err := f.q.groupMembers(context.Background(), queueSnapshot{Entries: []queueEntry{f.entry}})
	if err == nil || !strings.Contains(err.Error(), "exact-head authority is not approved") {
		t.Fatalf("a queued App pull request with code was collected into a group: %v", err)
	}
}

func TestStableReleaseAppCommitsAreRefusedInAnyoneElsesPullRequest(t *testing.T) {
	t.Setenv(stableReleaseAppBotEnv, fmt.Sprint(testAppBotID))
	bot := &struct {
		ID int64 `json:"id"`
	}{ID: testAppBotID}
	cases := map[string]struct {
		mutate  func([]pullCommit)
		count   int
		allowed bool
	}{
		"clean commits":                  {count: 3, allowed: true},
		"249 clean commits":              {count: 249, allowed: true},
		"an App author":                  {count: 3, mutate: func(c []pullCommit) { c[1].Author = bot }},
		"an App committer":               {count: 3, mutate: func(c []pullCommit) { c[2].Committer = bot }},
		"an App author on the last page": {count: 130, mutate: func(c []pullCommit) { c[129].Author = bot }},
		"an App noreply author email": {count: 3, mutate: func(c []pullCommit) {
			c[0].Author = nil
			c[0].Commit.Author.Email = fmt.Sprintf("%d+some-app[bot]@users.noreply.github.com", testAppBotID)
		}},
		"an App noreply committer email": {count: 3, mutate: func(c []pullCommit) {
			c[0].Commit.Committer.Email = fmt.Sprintf("%d+some-app[bot]@users.noreply.github.com", testAppBotID)
		}},
		"another bot's noreply email": {count: 3, allowed: true, mutate: func(c []pullCommit) {
			c[0].Commit.Author.Email = "99+other[bot]@users.noreply.github.com"
		}},
		"as many commits as GitHub lists": {count: 250},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newQueueFixture(t)
			verified := false
			f.c.verify = func(context.Context, int, string) (policyDecision, error) {
				verified = true
				return policyDecision{}, nil
			}
			commits := cleanCommits(tc.count)
			if tc.mutate != nil {
				tc.mutate(commits)
			}
			setPullCommits(f, 7, commits)
			p := appPull(f, 7, "dependabot/go_modules/x")
			p.User.ID = 49699333
			p.User.Login = "dependabot[bot]"
			decision, err := f.q.verifyPR(context.Background(), p)
			if err != nil {
				t.Fatal(err)
			}
			if tc.allowed && (decision.Code != 0 || !verified) {
				t.Fatalf("a pull request without the App's commits was refused: %+v", decision)
			}
			if !tc.allowed && (decision.Code != 1 || verified) {
				t.Fatalf("a pull request carrying the App's commit got through: %+v verified=%v", decision, verified)
			}
		})
	}
	// An unreadable commit list fails the gate.
	f := newQueueFixture(t)
	f.c.verify = func(context.Context, int, string) (policyDecision, error) { return policyDecision{}, nil }
	p := appPull(f, 7, "dependabot/go_modules/x")
	p.User.ID = 49699333
	if _, err := f.q.verifyPR(context.Background(), p); err == nil {
		t.Fatal("an unreadable commit list admitted the pull request")
	}
}

func TestStableReleaseAppConfiguredWithoutItsBotIDFailsTheGate(t *testing.T) {
	f := newQueueFixture(t)
	f.c.verify = func(context.Context, int, string) (policyDecision, error) { return policyDecision{}, nil }
	p := appPull(f, 7, "feature/x")
	p.User.ID = 1
	// Nothing configured: inert. Only the client id: the App exists, so fail closed.
	t.Setenv(stableReleaseAppBotEnv, "")
	t.Setenv(stableReleaseAppClientEnv, "")
	if decision, err := f.q.verifyPR(context.Background(), p); err != nil || decision.Code != 0 {
		t.Fatalf("an unconfigured gate refused a pull request: %+v %v", decision, err)
	}
	t.Setenv(stableReleaseAppClientEnv, "Iv23liExample")
	if _, err := f.q.verifyPR(context.Background(), p); err == nil {
		t.Fatal("a configured App without a bot id left the gate open")
	}
	// Only the bot id (the reconciler still off): the rule is armed.
	t.Setenv(stableReleaseAppClientEnv, "")
	t.Setenv(stableReleaseAppBotEnv, fmt.Sprint(testAppBotID))
	setPullCommits(f, 7, cleanCommits(1))
	if decision, err := f.q.verifyPR(context.Background(), p); err != nil || decision.Code != 0 {
		t.Fatalf("an armed gate refused a clean pull request: %+v %v", decision, err)
	}
}
