package main

import (
	"context"
	"errors"
	"fmt"
)

// A successful merge-tree writes exactly a SHA-1 tree OID and a newline. Bound
// capture even when a conflict reports many PR-controlled paths and messages.
type queueMergeOutput struct {
	data     []byte
	overflow bool
}

func (out *queueMergeOutput) Write(data []byte) (int, error) {
	n := len(data)
	remaining := 41 - len(out.data)
	if len(data) > remaining {
		out.overflow = true
		data = data[:remaining]
	}
	out.data = append(out.data, data...)
	return n, nil
}

// This checks immutable Git objects in the existing bare DCO repository. It
// never checks out PR files, invokes their hooks, or consults GitHub's delayed
// mergeable field. The metadata cache contains no reusable conflict verdict.
func checkQueueMerge(ctx context.Context, repoName string, p pull, repo string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !repoPattern.MatchString(repoName) || !shaPattern.MatchString(p.Base.SHA) || !shaPattern.MatchString(p.Head.SHA) {
		return errors.New("invalid immutable queue conflict range")
	}
	args := append(queueDCOMetadataArgs(repoName, repo), "merge-tree", "--write-tree", p.Base.SHA, p.Head.SHA)
	cmd := gateCommand(ctx, "git", args...)
	cmd.Env = queueDCOEnvironment()
	var output queueMergeOutput
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("immutable queue conflict proof failed: %w", err)
	}
	if output.overflow || len(output.data) != 41 || output.data[40] != '\n' || !shaPattern.Match(output.data[:40]) {
		return errors.New("immutable queue conflict proof returned an invalid tree identity")
	}
	// The successful merge's output must identify an actual local tree. Disable
	// hydration for this readback: merge-tree has just written the object.
	args = append(queueDCOMetadataArgs(repoName, repo), "-c", "remote.origin.promisor=false", "cat-file", "-t", string(output.data[:40]))
	readback := gateCommand(ctx, "git", args...)
	readback.Env = queueDCOEnvironment()
	var kind queueMergeOutput
	readback.Stdout = &kind
	if err := readback.Run(); err != nil || kind.overflow || string(kind.data) != "tree\n" {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("immutable queue conflict proof could not authenticate its tree")
	}
	return ctx.Err()
}
