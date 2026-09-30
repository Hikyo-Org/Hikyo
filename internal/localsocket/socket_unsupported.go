//go:build !linux && !darwin

// Package localsocket provides same-user Unix-domain HTTP transport.
package localsocket

import (
	"context"
	"errors"
	"net"
)

var errUnsupported = errors.New("same-user local CLI sockets are supported only on Linux and macOS; use pinned HTTPS")

func ValidatePath(string) error                             { return errUnsupported }
func Listen(string) (net.Listener, error)                   { return nil, errUnsupported }
func DialContext(context.Context, string) (net.Conn, error) { return nil, errUnsupported }
