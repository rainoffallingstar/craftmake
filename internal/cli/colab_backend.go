package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/fallingstar10/craftmake/internal/backend"
	colabpkg "github.com/fallingstar10/craftmake/internal/backend/colab"
	"github.com/fallingstar10/craftmake/internal/backend/local"
	"github.com/fallingstar10/craftmake/internal/backend/slurm"
)

// backendForNameWithColab resolves a backend by name for persistable run
// operations (for example cancel), building a Colab backend from the supplied
// session configuration when the persisted backend is "colab".
func backendForNameWithColab(ctx context.Context, backendName string, config colabBackendConfig) (backend.Backend, error) {
	switch backendName {
	case "local":
		return local.New(), nil
	case "slurm":
		return slurm.New(), nil
	case "colab":
		return buildColabBackend(ctx, config)
	default:
		return nil, fmt.Errorf("unsupported backend %q", backendName)
	}
}

// colabBackendConfig carries the flags needed to build a Colab backend from a
// configured session.
type colabBackendConfig struct {
	SessionID        string
	AuthConfig       string
	ProjectDirectory string
}

// buildColabBackend loads the named session authentication and builds a
// reference-informed Colab backend through the shared factory. It is
// offline-testable: only configuration and adapter wiring happen here; live
// control-plane and Drive calls are deferred until a run begins.
func buildColabBackend(ctx context.Context, config colabBackendConfig) (backend.Backend, error) {
	if config.SessionID == "" {
		return nil, fmt.Errorf("colab backend requires --colab-session")
	}
	if config.AuthConfig == "" {
		return nil, fmt.Errorf("colab backend requires --colab-auth-config")
	}
	path, err := expandUserPath(config.AuthConfig)
	if err != nil {
		return nil, fmt.Errorf("resolve colab auth config: %w", err)
	}
	auth, err := colabpkg.LoadSessionAuth(path, config.SessionID)
	if err != nil {
		return nil, err
	}
	client := colabpkg.NewColabServerClient(os.Getenv("CRAFTMAKE_COLAB_DOMAIN"), os.Getenv("CRAFTMAKE_COLAB_GAPI_DOMAIN"), nil)
	if tokenEnv := auth.ColabRefreshTokenEnv; tokenEnv != "" {
		manager := &colabpkg.TokenManager{}
		manager.SetRefreshToken(os.Getenv(tokenEnv))
		client.GetAccessToken = func() (string, error) { return manager.AccessToken(context.Background()) }
	}
	factory := colabpkg.NewFactory(colabpkg.FactoryDependencies{
		Server:         client,
		MountPreflight: colabpkg.NoopMountPreflight{},
	})
	return factory(ctx, backend.FactoryConfig{
		ProjectDirectory: config.ProjectDirectory,
		AuthConfigPath:   path,
		SessionID:        config.SessionID,
	})
}
