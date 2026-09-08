package colab

import (
	"context"
	"testing"

	backendpkg "github.com/fallingstar10/craftmake/internal/backend"
)

// fakeInterruptibleExecutor records interrupts and reports the interrupt
// capability used by CancelSubmission.
type fakeInterruptibleExecutor struct {
	interrupted bool
}

func (f *fakeInterruptibleExecutor) ExecuteNotebook(context.Context, Runtime, []byte) (string, error) {
	return "", nil
}
func (f *fakeInterruptibleExecutor) Interrupt(_ context.Context, _ Runtime) error {
	f.interrupted = true
	return nil
}

// fakeControlPlaneWithError lets a test inject errors into ReleaseRuntime.
type fakeControlPlaneWithError struct {
	releaseErr error
	released   int
}

func (f *fakeControlPlaneWithError) AcquireRuntime(context.Context, RuntimeRequest) (Runtime, error) {
	return Runtime{ID: "runtime-1"}, nil
}
func (f *fakeControlPlaneWithError) ReleaseRuntime(context.Context, Runtime) error {
	f.released++
	return f.releaseErr
}

func TestCancelSubmissionInterruptsActiveRun(t *testing.T) {
	executor := &fakeInterruptibleExecutor{}
	b := &Backend{Control: &fakeControlPlane{}, Executor: executor}
	if err := b.BeginRun(context.Background(), backendpkg.RunContext{RunID: "run-1", ProjectDirectory: "/local/project"}); err != nil {
		t.Fatal(err)
	}
	if err := b.CancelSubmission(context.Background(), "sub-1", nil); err != nil {
		t.Fatal(err)
	}
	if !executor.interrupted {
		t.Fatal("expected executor interrupt")
	}
	if err := b.CancelSubmission(context.Background(), "sub-1", nil); err != nil {
		t.Fatal(err)
	} // idempotent while active
}

func TestCancelSubmissionWhenInactiveIsNoop(t *testing.T) {
	b := &Backend{Control: &fakeControlPlane{}, Executor: &fakeNotebookExecutor{}}
	if err := b.CancelSubmission(context.Background(), "sub-1", nil); err != nil {
		t.Fatal(err)
	}
}

func TestCancelReleasesRuntimeAndIsIdempotent(t *testing.T) {
	control := &fakeControlPlane{}
	b := &Backend{Control: control, Executor: fakeNotebookExecutor{}}
	if err := b.BeginRun(context.Background(), backendpkg.RunContext{RunID: "run-1", ProjectDirectory: "/local/project"}); err != nil {
		t.Fatal(err)
	}
	if err := b.Cancel(context.Background()); err != nil {
		t.Fatal(err)
	}
	if control.released != 1 {
		t.Fatalf("expected one release, got %d", control.released)
	}
	if err := b.Cancel(context.Background()); err != nil {
		t.Fatal(err)
	} // second cancel is a no-op
}

func TestCancelTreatsRemoteGoneAsIdempotent(t *testing.T) {
	control := &fakeControlPlaneWithError{releaseErr: &RemoteError{Kind: ErrorTransferFailed, Operation: "unassign", StatusCode: 404, Err: nil}}
	b := &Backend{Control: control, Executor: fakeNotebookExecutor{}}
	if err := b.BeginRun(context.Background(), backendpkg.RunContext{RunID: "run-1", ProjectDirectory: "/local/project"}); err != nil {
		t.Fatal(err)
	}
	if err := b.Cancel(context.Background()); err != nil {
		t.Fatal(err)
	}
	if control.released != 1 {
		t.Fatalf("expected one release, got %d", control.released)
	}
}
