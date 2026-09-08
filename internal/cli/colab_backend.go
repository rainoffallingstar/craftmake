package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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

// resolveColabRefreshToken returns the Colab refresh token from the session's
// credential file (written by `colab auth login`) or, as a fallback, from the
// configured environment variable. The second return value reports whether a
// token was found.
func resolveColabRefreshToken(auth colabpkg.SessionAuth) (string, bool) {
	// 1. Explicit credential file from session auth
	if auth.ColabCredentialFile != "" {
		if path, err := expandUserPath(auth.ColabCredentialFile); err == nil {
			if data, err := os.ReadFile(path); err == nil {
				if token := strings.TrimSpace(string(data)); token != "" {
					return token, true
				}
			}
		}
	}
	// 2. Default local credential file location (~/.config/craftmake/credentials/<sessionID>.json)
	if auth.SessionID != "" {
		credPath := filepath.Join("~", ".config", "craftmake", "credentials", auth.SessionID+".json")
		if defaultCredPath, err := expandUserPath(credPath); err == nil {
			if data, readErr := os.ReadFile(defaultCredPath); readErr == nil {
				if token := strings.TrimSpace(string(data)); token != "" {
					return token, true
				}
			}
		}
	}
	// 3. Environment variable specified in session auth
	if auth.ColabRefreshTokenEnv != "" {
		if token := os.Getenv(auth.ColabRefreshTokenEnv); strings.TrimSpace(token) != "" {
			return strings.TrimSpace(token), true
		}
	}
	// 4. Global environment variable fallback
	if token := os.Getenv("CRAFTMAKE_COLAB_REFRESH_TOKEN"); strings.TrimSpace(token) != "" {
		return strings.TrimSpace(token), true
	}
	return "", false
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
	client.AppName = "craftmake"
	client.ExtensionVersion = "0.1.0"
	if refreshToken, ok := resolveColabRefreshToken(auth); ok {
		clientID := os.Getenv("CRAFTMAKE_COLAB_CLIENT_ID")
		if clientID == "" {
			clientID = defaultColabClientID
		}
		clientSecret := os.Getenv("CRAFTMAKE_COLAB_CLIENT_SECRET")
		if clientSecret == "" {
			clientSecret = defaultColabClientSecret
		}
		manager := &colabpkg.TokenManager{Config: colabpkg.TokenConfig{ClientID: clientID, ClientSecret: clientSecret, TokenURL: os.Getenv("CRAFTMAKE_COLAB_TOKEN_URL")}}
		manager.SetRefreshToken(refreshToken)
		client.GetAccessToken = func() (string, error) { return manager.AccessToken(context.Background()) }
	}
	executor := &colabpkg.JupyterWebSocketExecutor{
		SessionID:   config.SessionID,
		ColabClient: client,
		AuthConsentHandler: func(ctx context.Context, authType, redirectURI string) error {
			fmt.Printf("\n[Colab] Google Drive authorization required for this runtime.\nOpen this URL in your browser to grant Drive access to Colab:\n%s\n\nWaiting for authorization (press Enter once authorized in browser)...\n", redirectURI)
			_ = openBrowser(redirectURI)

			enterCh := make(chan struct{}, 1)
			go func() {
				var buf [1]byte
				_, _ = os.Stdin.Read(buf[:])
				enterCh <- struct{}{}
			}()

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-enterCh:
				return nil
			case <-time.After(3 * time.Minute):
				return fmt.Errorf("authorization timed out after 3 minutes")
			}
		},
	}
	factory := colabpkg.NewFactory(colabpkg.FactoryDependencies{
		Server:         client,
		Executor:       executor,
		MountPreflight: colabpkg.NoopMountPreflight{},
	})
	return factory(ctx, backend.FactoryConfig{
		ProjectDirectory: config.ProjectDirectory,
		AuthConfigPath:   path,
		SessionID:        config.SessionID,
	})
}
