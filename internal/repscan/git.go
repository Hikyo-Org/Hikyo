package repscan

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// git is the repository access layer. It shells out to the git executable
// rather than linking a Git implementation, reads blob contents only (never
// checks anything out), and disables every repository-configured command hook
// it can reach: an untrusted repository's config must not run code because it
// was scanned.
type git struct {
	exe string
	dir string
}

// gitSafetyArgs precede every git invocation.
var gitSafetyArgs = []string{
	"--no-pager", "--no-optional-locks",
	"-c", "core.fsmonitor=false",
	"-c", "core.quotepath=off",
}

func lookGit(name string) (string, error) {
	if name == "" {
		name = "git"
	}
	exe, err := exec.LookPath(name)
	if err != nil {
		return "", &Error{Kind: KindUnavailable, Err: fmt.Errorf("git is required for this scan mode and was not found: %v", err)}
	}
	return exe, nil
}

func (g git) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, g.exe, append(slices.Clone(gitSafetyArgs), args...)...)
	cmd.Dir = g.dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	return cmd
}

// maxGitStderr bounds the diagnostic kept from a failing git command.
const maxGitStderr = 4 << 10

type cappedBuffer struct {
	buf bytes.Buffer
	max int
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := c.max - c.buf.Len(); room > 0 {
		if len(p) > room {
			c.buf.Write(p[:room])
		} else {
			c.buf.Write(p)
		}
	}
	return len(p), nil
}

// gitFailure turns a git failure into a refusal naming the command. The
// diagnostic is git's own first stderr line with control characters removed,
// since it may echo an untrusted path.
func gitFailure(ctx context.Context, what string, stderr *cappedBuffer, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return timeoutRefusal(ctxErr)
	}
	msg := strings.TrimSpace(stderr.buf.String())
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	msg = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, msg)
	if msg == "" {
		msg = err.Error()
	}
	return refusedf("scan refused: git %s failed: %s", what, msg)
}

func timeoutRefusal(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return refusedf("scan refused: the runtime budget was exhausted before the scan completed")
	}
	return refusedf("scan refused: %v", err)
}

// output runs a git command whose output is small and bounded by maxOut.
func (g git) output(ctx context.Context, what string, maxOut int, args ...string) ([]byte, error) {
	cmd := g.command(ctx, args...)
	stderr := &cappedBuffer{max: maxGitStderr}
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, gitFailure(ctx, what, stderr, err)
	}
	out, readErr := io.ReadAll(io.LimitReader(stdout, int64(maxOut)+1))
	if len(out) > maxOut {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, refusedf("scan refused: git %s produced more than %d bytes", what, maxOut)
	}
	if err := cmd.Wait(); err != nil {
		return nil, gitFailure(ctx, what, stderr, err)
	}
	if readErr != nil {
		return nil, readErr
	}
	return out, nil
}

// toplevel resolves the repository root, refusing a directory that is not
// inside a usable work tree.
func (g git) toplevel(ctx context.Context) (string, error) {
	out, err := g.output(ctx, "rev-parse", 64<<10, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	top := strings.TrimRight(string(out), "\r\n")
	if top == "" {
		return "", refusedf("scan refused: not inside a git work tree")
	}
	return top, nil
}

// blobEntry is one path/blob pair a git listing produced.
type blobEntry struct {
	commit string // empty for the index and the working tree
	mode   string
	blob   string // all zeros when the content lives only in the working tree
	path   string
}

const (
	modeFile       = "100644"
	modeExecutable = "100755"
	modeSymlink    = "120000"
	modeGitlink    = "160000"
)

// parseRaw parses `--raw -z` output, optionally interleaved with commit ids
// (diff-tree --stdin). Each entry is ":srcmode dstmode srcsha dstsha status"
// followed by one path field (renames are disabled, so there is never two).
func parseRaw(r io.Reader, emit func(blobEntry) error) error {
	br := bufio.NewReaderSize(r, 64<<10)
	commit := ""
	for {
		field, err := br.ReadString(0)
		if err == io.EOF {
			if field != "" {
				return refusedf("scan refused: truncated git output")
			}
			return nil
		}
		if err != nil {
			return err
		}
		field = strings.TrimSuffix(field, "\x00")
		field = strings.TrimLeft(field, "\n")
		if field == "" {
			continue
		}
		if !strings.HasPrefix(field, ":") {
			if !isObjectID(field) {
				return refusedf("scan refused: unexpected git output")
			}
			commit = field
			continue
		}
		meta := strings.Fields(field[1:])
		if len(meta) != 5 {
			return refusedf("scan refused: unexpected git output")
		}
		p, err := br.ReadString(0)
		if err != nil {
			return refusedf("scan refused: truncated git output")
		}
		if err := emit(blobEntry{commit: commit, mode: meta[1], blob: meta[3], path: strings.TrimSuffix(p, "\x00")}); err != nil {
			return err
		}
	}
}

func isObjectID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func isZeroID(s string) bool { return strings.Trim(s, "0") == "" }

// stream runs a git command and hands its stdout to consume, with stdin fed
// from the given lines when non-nil.
func (g git) stream(ctx context.Context, what string, stdin []string, consume func(io.Reader) error, args ...string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := g.command(ctx, args...)
	stderr := &cappedBuffer{max: maxGitStderr}
	cmd.Stderr = stderr
	if stdin != nil {
		cmd.Stdin = strings.NewReader(strings.Join(stdin, "\n") + "\n")
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return gitFailure(ctx, what, stderr, err)
	}
	consumeErr := consume(stdout)
	if consumeErr != nil {
		cancel()
		_ = cmd.Wait()
		return consumeErr
	}
	if err := cmd.Wait(); err != nil {
		return gitFailure(ctx, what, stderr, err)
	}
	return nil
}

// revList lists the commits of a revision selection oldest-first (parents
// before children), refusing past maxCommits.
func (g git) revList(ctx context.Context, maxCommits int, selection ...string) ([]string, error) {
	var commits []string
	args := append([]string{"rev-list", "--topo-order", "--reverse"}, selection...)
	err := g.stream(ctx, "rev-list", nil, func(r io.Reader) error {
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			id := strings.TrimSpace(sc.Text())
			if !isObjectID(id) {
				return refusedf("scan refused: unexpected git rev-list output")
			}
			commits = append(commits, id)
			if len(commits) > maxCommits {
				return refusedf("scan refused: more than %d commits; scan an explicit --range", maxCommits)
			}
		}
		return sc.Err()
	}, args...)
	return commits, err
}

// catFile is a long-running `git cat-file --batch` reading blobs by id.
type catFile struct {
	cmd    *exec.Cmd
	in     io.WriteCloser
	out    *bufio.Reader
	stderr *cappedBuffer
	ctx    context.Context
}

func (g git) catFile(ctx context.Context) (*catFile, error) {
	cmd := g.command(ctx, "cat-file", "--batch")
	stderr := &cappedBuffer{max: maxGitStderr}
	cmd.Stderr = stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, gitFailure(ctx, "cat-file", stderr, err)
	}
	return &catFile{cmd: cmd, in: in, out: bufio.NewReaderSize(out, 64<<10), stderr: stderr, ctx: ctx}, nil
}

// read returns the blob's content, or skipOversize without buffering a blob
// larger than maxBytes.
func (c *catFile) read(id string, maxBytes int64) ([]byte, skipReason, error) {
	if _, err := io.WriteString(c.in, id+"\n"); err != nil {
		return nil, 0, gitFailure(c.ctx, "cat-file", c.stderr, err)
	}
	header, err := c.out.ReadString('\n')
	if err != nil {
		return nil, 0, gitFailure(c.ctx, "cat-file", c.stderr, err)
	}
	fields := strings.Fields(header)
	if len(fields) == 2 && fields[1] == "missing" {
		return nil, 0, refusedf("scan refused: the repository is missing object %s", id)
	}
	if len(fields) != 3 || fields[0] != id {
		return nil, 0, refusedf("scan refused: unexpected git cat-file output")
	}
	if fields[1] != "blob" {
		return nil, 0, refusedf("scan refused: object %s is a %s, not a blob", id, fields[1])
	}
	size, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || size < 0 {
		return nil, 0, refusedf("scan refused: unexpected git cat-file output")
	}
	if size > maxBytes {
		if _, err := io.CopyN(io.Discard, c.out, size+1); err != nil {
			return nil, 0, gitFailure(c.ctx, "cat-file", c.stderr, err)
		}
		return nil, skipOversize, nil
	}
	data := make([]byte, size+1)
	if _, err := io.ReadFull(c.out, data); err != nil {
		return nil, 0, gitFailure(c.ctx, "cat-file", c.stderr, err)
	}
	return data[:size], scanIt, nil
}

func (c *catFile) close() {
	_ = c.in.Close()
	_ = c.cmd.Wait()
}
