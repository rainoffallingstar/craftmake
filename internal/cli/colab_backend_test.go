package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	colabpkg "github.com/fallingstar10/craftmake/internal/backend/colab"
)

func TestBuildColabBackendLoadsSessionConfig(t *testing.T) {
	authPath := filepath.Join(t.TempDir(), "auth.json")
	if err := colabpkg.UpsertSessionAuth(authPath, colabpkg.SessionAuth{SessionID: "gpu", DriveRoot: "/content/drive/MyDrive/project", MountPath: "/content/drive", ColabCredentialFile: "/tmp/colab.json", DriveCredentialFile: "/tmp/drive.json"}); err != nil {
		t.Fatal(err)
	}
	b, err := buildColabBackend(context.Background(), colabBackendConfig{SessionID: "gpu", AuthConfig: authPath, ProjectDirectory: "/local/project"})
	if err != nil {
		t.Fatal(err)
	}
	colabBackend, ok := b.(*colabpkg.Backend)
	if !ok {
		t.Fatalf("expected *colab.Backend, got %T", b)
	}
	if colabBackend.Config.SessionID != "gpu" || colabBackend.Config.DriveRoot != "/content/drive/MyDrive/project" {
		t.Fatalf("session config not loaded: %#v", colabBackend.Config)
	}
}

func TestBuildColabBackendRejectsMissingSession(t *testing.T) {
	if _, err := buildColabBackend(context.Background(), colabBackendConfig{AuthConfig: "/tmp/none"}); err == nil {
		t.Fatal("expected error when session is missing")
	}
}

func TestResolveColabRefreshTokenFromCredentialFile(t *testing.T) {
	credFile := filepath.Join(t.TempDir(), "gpu.json")
	if err := os.WriteFile(credFile, []byte("refresh-from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	auth := colabpkg.SessionAuth{ColabCredentialFile: credFile}
	token, ok := resolveColabRefreshToken(auth)
	if !ok || token != "refresh-from-file" {
		t.Fatalf("token = %q ok=%v", token, ok)
	}
}

func TestResolveColabRefreshTokenFallsBackToEnv(t *testing.T) {
	_ = os.Setenv("CRAFTMAKE_TEST_REFRESH", "refresh-from-env")
	defer os.Unsetenv("CRAFTMAKE_TEST_REFRESH")
	auth := colabpkg.SessionAuth{ColabRefreshTokenEnv: "CRAFTMAKE_TEST_REFRESH"}
	token, ok := resolveColabRefreshToken(auth)
	if !ok || token != "refresh-from-env" {
		t.Fatalf("token = %q ok=%v", token, ok)
	}
}

func TestResolveColabRefreshTokenMissing(t *testing.T) {
	if _, ok := resolveColabRefreshToken(colabpkg.SessionAuth{}); ok {
		t.Fatal("expected no token")
	}
}
