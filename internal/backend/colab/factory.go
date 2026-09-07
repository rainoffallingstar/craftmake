package colab

import (
	"context"
	"fmt"

	backendpkg "github.com/fallingstar10/craftmake/internal/backend"
)

type FactoryDependencies struct {
	Control        ControlPlane
	Executor       NotebookExecutor
	MountPreflight DriveMountPreflight
	Materializer   LogMaterializer
	Workspace      WorkspaceSyncer
}

func NewFactory(dependencies FactoryDependencies) backendpkg.Factory {
	return func(ctx context.Context, options backendpkg.FactoryConfig) (backendpkg.Backend, error) {
		config := Config{LocalRoot: options.ProjectDirectory, AuthConfigPath: options.AuthConfigPath, SessionID: options.SessionID}
		if config.AuthConfigPath != "" {
			if config.SessionID == "" {
				return nil, fmt.Errorf("Colab session id is required when auth config is set")
			}
			authConfig, err := ConfigForSession(config.AuthConfigPath, config.SessionID)
			if err != nil {
				return nil, err
			}
			config.DriveRoot, config.MountPath = authConfig.DriveRoot, authConfig.MountPath
		}
		if dependencies.Control == nil || dependencies.Executor == nil {
			return nil, fmt.Errorf("Colab factory requires control plane and notebook executor")
		}
		_ = ctx
		return &Backend{Config: config, Control: dependencies.Control, Executor: dependencies.Executor, MountPreflight: dependencies.MountPreflight, Materializer: dependencies.Materializer, Workspace: dependencies.Workspace}, nil
	}
}
