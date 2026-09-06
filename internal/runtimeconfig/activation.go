package runtimeconfig

import "context"

// RuntimeInstaller prepares the application's consumers without making a
// candidate active. The app owns this interface; only its configuration worker
// invokes it, outside request and datastore transaction lifetimes.
type RuntimeInstaller interface {
	Prepare(context.Context, *Bundle) (PreparedActivation, error)
}

// PreparedActivation owns candidate resources until Activate transfers them
// to the application. Close disposes an unused candidate and is idempotent;
// after successful activation it must not close the active application.
// Failed activation must leave the prior application available for recovery.
type PreparedActivation interface {
	Activate(context.Context) error
	Close() error
}
