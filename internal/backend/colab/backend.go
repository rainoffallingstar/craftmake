package colab

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/fallingstar10/craftmake/internal/backend"
)

type RuntimeRequest struct {
	RunID  string
	Region string
}
type Runtime struct{ ID string }

type ControlPlane interface {
	AcquireRuntime(context.Context, RuntimeRequest) (Runtime, error)
	ReleaseRuntime(context.Context, Runtime) error
}
type NotebookExecutor interface {
	ExecuteNotebook(context.Context, Runtime, []byte) (string, error)
}

type Config struct {
	RemoteRoot  string
	ScratchRoot string
	DriveRoot   string
}

type Backend struct {
	Config   Config
	Control  ControlPlane
	Executor NotebookExecutor
	mutex    sync.Mutex
	runtime  Runtime
	active   bool
}

func (b *Backend) Name() string { return "colab" }

func (b *Backend) BeginRun(ctx context.Context, run backend.RunContext) error {
	if b.Control == nil || b.Executor == nil {
		return fmt.Errorf("colab control plane and notebook executor are required")
	}
	b.mutex.Lock()
	defer b.mutex.Unlock()
	if b.active {
		return fmt.Errorf("colab runtime is already active")
	}
	runtime, err := b.Control.AcquireRuntime(ctx, RuntimeRequest{RunID: run.RunID})
	if err != nil {
		return fmt.Errorf("acquire Colab runtime: %w", err)
	}
	b.runtime, b.active = runtime, true
	return nil
}

func (b *Backend) EndRun(ctx context.Context, outcome backend.RunOutcome) error {
	b.mutex.Lock()
	if !b.active {
		b.mutex.Unlock()
		return nil
	}
	runtime := b.runtime
	b.active = false
	b.runtime = Runtime{}
	b.mutex.Unlock()
	if err := b.Control.ReleaseRuntime(ctx, runtime); err != nil {
		return fmt.Errorf("release Colab runtime after %s: %w", outcome.Status, err)
	}
	return nil
}

func (b *Backend) RunSubmission(ctx context.Context, submissionID string, request backend.SubmissionRequest) (*backend.SubmissionResult, error) {
	b.mutex.Lock()
	runtime, active := b.runtime, b.active
	b.mutex.Unlock()
	if !active {
		return nil, fmt.Errorf("colab runtime is not active")
	}
	result := &backend.SubmissionResult{BackendID: "colab:" + submissionID, Tasks: map[string]backend.TaskOutcome{}}
	for _, manifest := range request.Manifests {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		mapping := RemoteTaskMapping{WorkDirectory: filepath.Join(b.remoteRoot(), "work"), TempDirectory: filepath.Join(b.scratchRoot(), "tmp"), RuntimeDirectory: filepath.Join(b.remoteRoot(), "runtime", manifest.TaskID), ResultPath: filepath.Join(b.remoteRoot(), "runtime", manifest.TaskID, "result.json")}
		notebook, err := BuildNotebook(manifest, mapping)
		if err != nil {
			result.Tasks[manifest.TaskID] = backend.TaskOutcome{Err: err}
			continue
		}
		payload, err := notebook.JSON()
		if err != nil {
			result.Tasks[manifest.TaskID] = backend.TaskOutcome{Err: err}
			continue
		}
		if request.OnStarted != nil {
			if err := request.OnStarted("colab:"+manifest.TaskID, map[string]any{"runtime_id": runtime.ID}); err != nil {
				return result, err
			}
		}
		output, err := b.Executor.ExecuteNotebook(ctx, runtime, payload)
		if err != nil {
			result.Tasks[manifest.TaskID] = backend.TaskOutcome{Err: err}
			continue
		}
		taskResult, err := DecodeTaskResult(output)
		if err != nil {
			result.Tasks[manifest.TaskID] = backend.TaskOutcome{Err: err}
			continue
		}
		result.Tasks[manifest.TaskID] = backend.TaskOutcome{Result: &backend.Result{TaskResult: taskResult, BackendID: "colab:" + manifest.TaskID}}
	}
	return result, nil
}

func (b *Backend) CancelSubmission(context.Context, string, map[string]any) error { return nil }
func (b *Backend) Cancel(context.Context) error                                   { return nil }
func (b *Backend) remoteRoot() string {
	if b.Config.RemoteRoot != "" {
		return b.Config.RemoteRoot
	}
	return "/content/craftmake"
}
func (b *Backend) scratchRoot() string {
	if b.Config.ScratchRoot != "" {
		return b.Config.ScratchRoot
	}
	return "/content"
}

var _ backend.Backend = (*Backend)(nil)
var _ backend.RunLifecycle = (*Backend)(nil)
