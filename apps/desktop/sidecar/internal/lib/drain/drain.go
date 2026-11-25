// Package drain is the shared shutdown primitive: wait for in-flight work, but no longer
// than the caller's deadline.
package drain

import "context"

// WithContext blocks until wait returns (nil) or ctx is done. wait must return on its own
// — typically a WaitGroup.Wait over already-signalled handlers — since ctx bounds only
// how long the CALLER waits, not wait itself.
func WithContext(ctx context.Context, wait func()) error {
	done := make(chan struct{})
	go func() {
		wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
