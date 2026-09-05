package updater

import (
	"context"
	"log/slog"
)

// Run retires the privileged helper before reading config, opening a socket,
// changing a journal, or executing a queued job. Stop any old helper process
// before installing this binary; already running old code is not retroactively
// fenced by a replacement on disk.
func Run(context.Context, []string, *slog.Logger) error {
	return ErrRemoteApplyDisabled
}
