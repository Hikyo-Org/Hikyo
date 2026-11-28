package shell

import "context"

// gate admits one holder at a time and lets a waiter give up when its context
// ends, which a sync.Mutex can't (a reauthentication ceremony holds it for as
// long as the person takes in the browser).
type gate chan struct{}

func newGate() gate { return make(gate, 1) }

// acquire waits for the gate and returns the function that releases it.
func (g gate) acquire(ctx context.Context) (release func(), err error) {
	select {
	case g <- struct{}{}:
		return func() { <-g }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
