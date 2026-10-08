// Prepare compiler export data before the lint suite's test timeout starts.
// Its repository loads cover tagged and non-host platforms, whose dependencies
// are not populated by the ordinary host build or vet steps on a cold runner.
package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/Hikyo-Org/hikyo/internal/lint"
)

func main() {
	if err := prepare(); err != nil {
		fmt.Fprintf(os.Stderr, "prepare lint cache: %v\n", err)
		os.Exit(1)
	}
}

func prepare() error {
	dir, err := os.MkdirTemp("", "hikyo-lint-cache-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	for _, ctx := range lint.Contexts {
		overlay, err := ctx.WriteOverlay(dir)
		if err != nil {
			return fmt.Errorf("%s overlay: %w", ctx.Name, err)
		}
		args := []string{"list", "-export"}
		if !ctx.ProductionOnly {
			args = append(args, "-test")
		}
		args = append(args, ctx.BuildFlags...)
		if overlay != "" {
			args = append(args, "-overlay="+overlay)
		}
		args = append(args, lint.Module+"/...")
		fmt.Printf("prepare lint cache: %s\n", ctx.Name)
		cmd := exec.Command("go", args...)
		cmd.Env = ctx.Env()
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s: %w", ctx.Name, err)
		}
	}
	return nil
}
