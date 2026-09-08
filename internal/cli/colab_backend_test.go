package cli

import (
	"context"
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
