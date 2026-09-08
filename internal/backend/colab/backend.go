package colab

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fallingstar10/craftmake/internal/backend"
	"github.com/fallingstar10/craftmake/pkg/protocol"
)

type RuntimeRequest struct {
	RunID  string
	Region string
}
type Runtime struct {
	ID         string
	ProxyURL   string
	ProxyToken string
}

type ControlPlane interface {
	AcquireRuntime(context.Context, RuntimeRequest) (Runtime, error)
	ReleaseRuntime(context.Context, Runtime) error
}
type NotebookExecutor interface {
	ExecuteNotebook(context.Context, Runtime, []byte) (string, error)
}

type DriveMountRequest struct {
	RunID          string
	SessionID      string
	AuthConfigPath string
	MountPath      string
	DriveRoot      string
}
type DriveMountPreflight interface {
	CheckMount(context.Context, DriveMountRequest) error
}
type LogMaterializer interface {
	Materialize(context.Context, string, string) error
}

type Config struct {
	RemoteRoot     string
	ScratchRoot    string
	DriveRoot      string
	MountPath      string
	AuthConfigPath string
	SessionID      string
	LocalRoot      string
	SyncExcludes   []string
}

type Backend struct {
	Config         Config
	Control        ControlPlane
	Executor       NotebookExecutor
	MountPreflight DriveMountPreflight
	Materializer   LogMaterializer
	Workspace      WorkspaceSyncer
	ResultReader   RemoteResultReader
	Redactor       *Redactor
	mutex          sync.Mutex
	runtime        Runtime
	active         bool
}

func (b *Backend) Name() string { return "colab" }

func (b *Backend) BeginRun(ctx context.Context, run backend.RunContext) error {
	if b.Control == nil || b.Executor == nil {
		return fmt.Errorf("colab control plane and notebook executor are required")
	}
	config := b.Config
	if config.LocalRoot == "" {
		config.LocalRoot = run.ProjectDirectory
	}
	if config.AuthConfigPath != "" {
		auth, err := LoadSessionAuth(config.AuthConfigPath, config.SessionID)
		if err != nil {
			return fmt.Errorf("load Colab session auth: %w", err)
		}
		if config.DriveRoot == "" {
			config.DriveRoot = auth.DriveRoot
		}
		if config.MountPath == "" {
			config.MountPath = auth.MountPath
		}
	}
	b.mutex.Lock()
	if b.active {
		b.mutex.Unlock()
		return fmt.Errorf("colab runtime is already active")
	}
	runtime, err := b.Control.AcquireRuntime(ctx, RuntimeRequest{RunID: run.RunID})
	if err != nil {
		b.mutex.Unlock()
		return fmt.Errorf("acquire Colab runtime: %w", err)
	}
	b.runtime, b.active = runtime, true
	b.mutex.Unlock()
	rollback := func(beginErr error) error {
		b.mutex.Lock()
		b.active = false
		b.runtime = Runtime{}
		b.mutex.Unlock()
		if releaseErr := b.Control.ReleaseRuntime(ctx, runtime); releaseErr != nil {
			return fmt.Errorf("%w; release Colab runtime after failed begin: %v", beginErr, releaseErr)
		}
		return beginErr
	}
	if config.DriveRoot != "" {
		if b.MountPreflight == nil {
			return rollback(fmt.Errorf("drive mount preflight is required when drive root is configured"))
		}
		mountPath := config.MountPath
		if mountPath == "" {
			mountPath = "/content/drive"
		}
		if err := b.MountPreflight.CheckMount(ctx, DriveMountRequest{RunID: run.RunID, SessionID: config.SessionID, AuthConfigPath: config.AuthConfigPath, MountPath: mountPath, DriveRoot: config.DriveRoot}); err != nil {
			return rollback(&MountNotAuthorizedError{SessionID: config.SessionID, AuthConfigPath: config.AuthConfigPath, MountPath: mountPath, DriveRoot: config.DriveRoot, Err: err})
		}
	}
	if b.Workspace != nil && config.LocalRoot != "" {
		target := config.DriveRoot
		if target == "" {
			target = b.remoteRoot()
		}
		excludes := config.SyncExcludes
		if len(excludes) == 0 {
			excludes = []string{".craftmake/state", ".git"}
		}
		if err := b.Workspace.SyncIn(ctx, WorkspaceSyncRequest{RunID: run.RunID, LocalRoot: config.LocalRoot, RemoteRoot: target, Direction: "in", Excludes: excludes}); err != nil {
			return rollback(fmt.Errorf("sync workspace to Colab: %w", err))
		}
	}
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
	var syncErr error
	config := b.Config
	if config.AuthConfigPath != "" {
		if auth, err := LoadSessionAuth(config.AuthConfigPath, config.SessionID); err != nil {
			syncErr = fmt.Errorf("load Colab session auth for sync-out: %w", err)
		} else {
			if config.DriveRoot == "" {
				config.DriveRoot = auth.DriveRoot
			}
			if config.MountPath == "" {
				config.MountPath = auth.MountPath
			}
		}
	}
	if syncErr == nil && b.Workspace != nil && outcome.ProjectDirectory != "" {
		target := config.DriveRoot
		if target == "" {
			target = b.remoteRoot()
		}
		excludes := config.SyncExcludes
		if len(excludes) == 0 {
			excludes = []string{".craftmake/state", ".git"}
		}
		syncErr = b.Workspace.SyncOut(ctx, WorkspaceSyncRequest{RunID: outcome.RunID, LocalRoot: target, RemoteRoot: outcome.ProjectDirectory, Direction: "out", Excludes: excludes})
	}
	releaseErr := b.Control.ReleaseRuntime(ctx, runtime)
	if syncErr != nil && releaseErr != nil {
		return fmt.Errorf("sync workspace from Colab: %v; release Colab runtime after %s: %w", syncErr, outcome.Status, releaseErr)
	}
	if syncErr != nil {
		return fmt.Errorf("sync workspace from Colab: %w", syncErr)
	}
	if releaseErr != nil {
		return fmt.Errorf("release Colab runtime after %s: %w", outcome.Status, releaseErr)
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
		notebook, err := BuildNotebookRedacted(manifest, mapping, b.Redactor)
		if err != nil {
			result.Tasks[manifest.TaskID] = backend.TaskOutcome{Err: RedactError(b.Redactor, err)}
			continue
		}
		payload, err := notebook.JSON()
		if err != nil {
			result.Tasks[manifest.TaskID] = backend.TaskOutcome{Err: RedactError(b.Redactor, err)}
			continue
		}
		if request.OnStarted != nil {
			if err := request.OnStarted("colab:"+manifest.TaskID, map[string]any{"runtime_id": runtime.ID}); err != nil {
				return result, err
			}
		}
		output, err := b.Executor.ExecuteNotebook(ctx, runtime, payload)
		if err != nil {
			result.Tasks[manifest.TaskID] = backend.TaskOutcome{Err: RedactError(b.Redactor, err)}
			continue
		}
		taskResult, err := DecodeTaskResult(output)
		if err != nil {
			result.Tasks[manifest.TaskID] = backend.TaskOutcome{Err: RedactError(b.Redactor, fmt.Errorf("%w; raw kernel output: %q", err, output))}
			continue
		}
		if err := b.materializeTaskLogs(ctx, taskResult, manifest, mapping); err != nil {
			taskResult.ObservabilityErrors = append(taskResult.ObservabilityErrors, err.Error())
		}
		result.Tasks[manifest.TaskID] = backend.TaskOutcome{Result: &backend.Result{TaskResult: taskResult, BackendID: "colab:" + manifest.TaskID}}
	}
	return result, nil
}

func (b *Backend) materializeTaskLogs(ctx context.Context, taskResult *protocol.TaskResult, manifest *protocol.TaskManifest, mapping RemoteTaskMapping) error {
	if b.Materializer == nil {
		return nil
	}
	var failures []string
	for _, step := range taskResult.Steps {
		if step.Index < 0 || step.Index >= len(manifest.Steps) {
			continue
		}
		manifestStep := manifest.Steps[step.Index]
		remoteStdout := step.StdoutPath
		if remoteStdout == "" {
			remoteStdout = filepath.Join(mapping.RuntimeDirectory, fmt.Sprintf("step-%d.stdout", step.Index))
		}
		remoteStderr := step.StderrPath
		if remoteStderr == "" {
			remoteStderr = filepath.Join(mapping.RuntimeDirectory, fmt.Sprintf("step-%d.stderr", step.Index))
		}
		localStdout := manifestStep.StdoutPath
		localStderr := manifestStep.StderrPath
		if localStdout == "" {
			localStdout = filepath.Join(manifest.RuntimeDirectory, fmt.Sprintf("step-%d.stdout", step.Index))
		}
		if localStderr == "" {
			localStderr = filepath.Join(manifest.RuntimeDirectory, fmt.Sprintf("step-%d.stderr", step.Index))
		}
		if err := b.Materializer.Materialize(ctx, remoteStdout, localStdout); err != nil {
			failures = append(failures, fmt.Sprintf("stdout step %d: %v", step.Index, err))
		}
		if err := b.Materializer.Materialize(ctx, remoteStderr, localStderr); err != nil {
			failures = append(failures, fmt.Sprintf("stderr step %d: %v", step.Index, err))
		}
		step.StdoutPath, step.StderrPath = localStdout, localStderr
	}
	if len(failures) > 0 {
		return fmt.Errorf("materialize Colab logs: %s", strings.Join(failures, "; "))
	}
	return nil
}

func (b *Backend) remoteRoot() string {
	if b.Config.DriveRoot != "" {
		return b.Config.DriveRoot
	}
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
var _ backend.Recoverable = (*Backend)(nil)
