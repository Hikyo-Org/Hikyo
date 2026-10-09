//go:build !darwin && !windows

package cli

import (
	"context"
	"errors"
)

func prepareSecureClipboard() (func(context.Context, string) error, error) {
	return nil, errors.New("protected native clipboard requires macOS or Windows")
}
