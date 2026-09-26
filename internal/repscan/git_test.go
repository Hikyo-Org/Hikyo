package repscan

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fixtureRepo is a throwaway repository built with the real git executable,
// isolated from the developer's and runner's git configuration.
type fixtureRepo struct {
	t   *testing.T
	dir string
}

func newFixtureRepo(t *testing.T) *fixtureRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	empty := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", empty)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, k := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	r := &fixtureRepo{t: t, dir: t.TempDir()}
	r.git("init", "-q", "-b", "main")
	r.git("config", "user.email", "fixture@example.invalid")
	r.git("config", "user.name", "fixture")
	r.git("config", "commit.gpgsign", "false")
	r.git("config", "core.autocrlf", "false")
	return r
}

func (r *fixtureRepo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *fixtureRepo) write(rel, content string) { write(r.t, r.dir, rel, content) }

func (r *fixtureRepo) commit(msg string) string {
	r.git("add", "-A")
	r.git("commit", "-q", "-m", msg)
	return r.git("rev-parse", "HEAD")
}

func (r *fixtureRepo) options(mode Mode) Options {
	return Options{Mode: mode, Workdir: r.dir, Limits: DefaultLimits()}
}

func TestGitModes(t *testing.T) {
	repo := newFixtureRepo(t)
	repo.write("README.md", "hello\n")
	repo.write("config/app.env", "A=1\nTOKEN="+githubPAT+"\n")
	first := repo.commit("first")
	repo.write("config/app.env", "A=1\nB=2\nTOKEN="+githubPAT+"\n")
	repo.write("keys/aws.txt", "x\n"+awsKey+"\n")
	second := repo.commit("second")

	// Staged: a new secret staged, then a different one left unstaged on top.
	repo.write("staged.txt", "s\n"+githubPAT+"\n")
	repo.git("add", "staged.txt")
	repo.write("staged.txt", "s\nclean now\n")
	repo.write("untracked.txt", awsKey+"\n")
	repo.write(".gitignore", "ignored.txt\n")
	repo.write("ignored.txt", githubPAT+"\n")

	t.Run("history", func(t *testing.T) {
		r := scan(t, repo.options(ModeHistory))
		want := []loc{
			{rule: "github-pat", path: "config/app.env", line: 2, commit: first},
			{rule: "github-pat", path: "config/app.env", line: 3, commit: second},
			{rule: "aws-access-token", path: "keys/aws.txt", line: 2, commit: second},
		}
		if got := locs(r); !slices.Equal(got, want) {
			t.Fatalf("findings:\n got %v\nwant %v", got, want)
		}
		if r.Summary.CommitsScanned != 2 {
			t.Fatalf("commits = %d", r.Summary.CommitsScanned)
		}
		for _, f := range r.Findings {
			if f.Fingerprint != Fingerprint(f.RuleDigest, f.Path, f.Line, f.Commit) {
				t.Fatal("history fingerprint is not scoped by commit")
			}
		}
	})
	t.Run("range", func(t *testing.T) {
		opts := repo.options(ModeRange)
		opts.Range = first + "..HEAD"
		r := scan(t, opts)
		want := []loc{
			{rule: "github-pat", path: "config/app.env", line: 3, commit: second},
			{rule: "aws-access-token", path: "keys/aws.txt", line: 2, commit: second},
		}
		if got := locs(r); !slices.Equal(got, want) {
			t.Fatalf("findings:\n got %v\nwant %v", got, want)
		}
	})
	t.Run("staged reads the index, not the working tree", func(t *testing.T) {
		r := scan(t, repo.options(ModeStaged))
		want := []loc{{rule: "github-pat", path: "staged.txt", line: 2}}
		if got := locs(r); !slices.Equal(got, want) {
			t.Fatalf("findings:\n got %v\nwant %v", got, want)
		}
		// Same identity as a working-tree scan of the same location.
		if r.Findings[0].Fingerprint != Fingerprint(r.Findings[0].RuleDigest, "staged.txt", 2, worktreeScope) {
			t.Fatal("staged fingerprint differs from the working-tree construction")
		}
	})
	t.Run("unstaged includes untracked, honours .gitignore", func(t *testing.T) {
		r := scan(t, repo.options(ModeUnstaged))
		want := []loc{{rule: "aws-access-token", path: "untracked.txt", line: 1}}
		if got := locs(r); !slices.Equal(got, want) {
			t.Fatalf("findings:\n got %v\nwant %v", got, want)
		}
	})
	t.Run("commit budget refuses", func(t *testing.T) {
		opts := repo.options(ModeHistory)
		opts.Limits.MaxCommits = 1
		refusal(t, opts, KindRefused, "more than 1 commits")
	})
	t.Run("git modes take no paths", func(t *testing.T) {
		opts := repo.options(ModeStaged)
		opts.Paths = []string{"."}
		refusal(t, opts, KindConfig, "takes no paths")
	})
	t.Run("an unknown revision refuses", func(t *testing.T) {
		opts := repo.options(ModeRange)
		opts.Range = "nope..HEAD"
		refusal(t, opts, KindRefused, "git rev-list failed")
	})
}

func TestHistorySkipsSymlinksBinariesAndOversize(t *testing.T) {
	repo := newFixtureRepo(t)
	repo.write("bin.dat", "\x00"+githubPAT)
	repo.write("big.txt", strings.Repeat("y", MaxFileBytes+1)+githubPAT)
	repo.write("pkg.tar.gz", githubPAT)
	repo.write("target.txt", "clean\n")
	_ = os.Symlink("target.txt", filepath.Join(repo.dir, "link")) // absent on unprivileged Windows
	repo.commit("skips")
	r := scan(t, repo.options(ModeHistory))
	if len(r.Findings) != 0 {
		t.Fatalf("findings = %v", locs(r))
	}
	s := r.Summary.Skipped
	if s.Binary != 1 || s.Oversize != 1 || s.Archive != 1 {
		t.Fatalf("skipped = %+v", s)
	}
	if _, err := os.Lstat(filepath.Join(repo.dir, "link")); err == nil && s.Symlink != 1 {
		t.Fatalf("symlink blob not skipped: %+v", s)
	}
}

// TestMergesAttributeOnce: a blob a side branch introduced is reported once,
// against the side-branch commit, not again at the merge that brought it in.
func TestMergesAttributeOnce(t *testing.T) {
	repo := newFixtureRepo(t)
	repo.write("a.txt", "base\n")
	repo.commit("base")
	repo.git("checkout", "-q", "-b", "side")
	repo.write("side.txt", githubPAT+"\n")
	side := repo.commit("side")
	repo.git("checkout", "-q", "main")
	repo.write("b.txt", "main\n")
	repo.commit("main")
	repo.git("merge", "-q", "--no-ff", "-m", "merge", "side")
	r := scan(t, repo.options(ModeHistory))
	want := []loc{{rule: "github-pat", path: "side.txt", line: 1, commit: side}}
	if got := locs(r); !slices.Equal(got, want) {
		t.Fatalf("findings:\n got %v\nwant %v", got, want)
	}
	if r.Summary.CommitsScanned != 4 {
		t.Fatalf("commits = %d", r.Summary.CommitsScanned)
	}
}

// TestUnstagedSkipsEmbeddedRepositories: git lists an untracked nested
// repository as "dir/"; it is counted and never descended, not a refusal.
func TestUnstagedSkipsEmbeddedRepositories(t *testing.T) {
	repo := newFixtureRepo(t)
	repo.write("a.txt", "clean\n")
	repo.commit("a")
	nested := exec.Command("git", "init", "-q", filepath.Join(repo.dir, "nested"))
	if out, err := nested.CombinedOutput(); err != nil {
		t.Fatalf("git init nested: %v\n%s", err, out)
	}
	repo.write("nested/secret.txt", githubPAT+"\n")
	r := scan(t, repo.options(ModeUnstaged))
	if len(r.Findings) != 0 || r.Summary.Skipped.Submodule != 1 {
		t.Fatalf("findings %v, skipped %+v", locs(r), r.Summary.Skipped)
	}
}

func TestMalformedRepositoriesRefuse(t *testing.T) {
	t.Run("not a repository", func(t *testing.T) {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git is not installed")
		}
		dir := t.TempDir()
		write(t, dir, ".git", "gitdir: /nonexistent/elsewhere\n")
		t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
		opts := Options{Mode: ModeHistory, Workdir: dir, Limits: DefaultLimits()}
		refusal(t, opts, KindRefused, "git rev-parse failed")
	})
	t.Run("missing object", func(t *testing.T) {
		repo := newFixtureRepo(t)
		repo.write("a.txt", githubPAT+"\n")
		repo.commit("a")
		blob := repo.git("rev-parse", "HEAD:a.txt")
		if err := os.Remove(filepath.Join(repo.dir, ".git", "objects", blob[:2], blob[2:])); err != nil {
			t.Fatal(err)
		}
		refusal(t, repo.options(ModeHistory), KindRefused, "missing object")
	})
}

func TestMissingGitIsUnavailable(t *testing.T) {
	opts := Options{Mode: ModeStaged, Workdir: t.TempDir(), Limits: DefaultLimits(), Git: "hikyo-no-such-git-binary"}
	refusal(t, opts, KindUnavailable, "git is required")
}

// TestPathsAndGitAgreeOnIdentity: a paths scan inside a repository reports
// paths from the repository top level, so its fingerprints match the staged
// scan of the same content, wherever in the tree it is run from.
func TestPathsAndGitAgreeOnIdentity(t *testing.T) {
	repo := newFixtureRepo(t)
	repo.write("sub/dir/a.txt", "x\n"+githubPAT+"\n")
	repo.git("add", "-A")
	staged := scan(t, repo.options(ModeStaged))
	opts := pathsOptions(filepath.Join(repo.dir, "sub"), "dir")
	paths := scan(t, opts)
	if len(staged.Findings) != 1 || len(paths.Findings) != 1 {
		t.Fatalf("staged %v, paths %v", locs(staged), locs(paths))
	}
	if staged.Findings[0] != paths.Findings[0] {
		t.Fatalf("identity differs:\n staged %+v\n paths  %+v", staged.Findings[0], paths.Findings[0])
	}
}
