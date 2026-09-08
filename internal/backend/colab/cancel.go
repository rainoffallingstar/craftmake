package colab

import (
	"context"
	"errors"
	"time"

	"github.com/fallingstar10/craftmake/internal/backend"
)

// KernelGracePeriod is how long CancelSubmission waits for an in-flight
// notebook execution to settle after issuing an interrupt.
const KernelGracePeriod = 5 * time.Second

// NotebookInterruptor is an optional capability that lets the backend request
// an in-flight notebook execution stop. The concrete executor implements it;
// when absent, CancelSubmission falls back to the graceful-wait path only.
type NotebookInterruptor interface {
	Interrupt(context.Context, Runtime) error
}

// errRemoteGone is wrapped by cancel paths when the runtime is already gone;
// callers treat it as an idempotent success so a 404 never overwrites the
// local cancelled state.
var errRemoteGone = errors.New("remote runtime is already gone")

// isRemoteGone reports whether err indicates the runtime no longer exists
// (for example a 404 from the Colab control plane during unassign).
func isRemoteGone(err error) bool {
	var remoteError *RemoteError
	if errors.As(err, &remoteError) && remoteError.StatusCode == 404 {
		return true
	}
	return errors.Is(err, errRemoteGone)
}

// CancelSubmission interrupts the currently executing kernel and waits a
// bounded grace period for it to settle. It is idempotent: if no runtime is
// active there is nothing to cancel.
func (b *Backend) CancelSubmission(ctx context.Context, submissionID string, metadata map[string]any) error {
	b.mutex.Lock()
	runtime, active := b.runtime, b.active
	b.mutex.Unlock()
	if !active {
		return nil
	}
	interruptor, ok := b.Executor.(NotebookInterruptor)
	if !ok {
		return nil // no interrupt capability; nothing to signal
	}
	cancelCtx, cancel := context.WithTimeout(context.Background(), KernelGracePeriod)
	defer cancel()
	return interruptor.Interrupt(cancelCtx, runtime)
}

// Cancel stops the run: it releases the assigned runtime, treating a missing
// remote (404) as an idempotent success.
func (b *Backend) Cancel(ctx context.Context) error {
	b.mutex.Lock()
	if !b.active {
		b.mutex.Unlock()
		return nil
	}
	runtime := b.runtime
	b.active = false
	b.runtime = Runtime{}
	b.mutex.Unlock()
	if b.Control == nil {
		return nil
	}
	if err := b.Control.ReleaseRuntime(ctx, runtime); err != nil {
		if isRemoteGone(err) {
			return nil // already gone; keep local cancelled state
		}
		return err
	}
	return nil
}

var _ backend.Backend = (*Backend)(nil)
