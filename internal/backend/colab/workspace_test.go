package colab

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFileWorkspaceSyncerCopiesProjectAndExcludesState(t *testing.T) {
	source, target := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, ".craftmake", "state"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "main.txt"), []byte("source"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".craftmake", "state", "state.sqlite"), []byte("private"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (FileWorkspaceSyncer{}).Sync(context.Background(), WorkspaceSyncRequest{LocalRoot: source, RemoteRoot: filepath.Join(target, "drive"), Direction: "in", Excludes: []string{".craftmake/state"}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(target, "drive", "main.txt"))
	if err != nil || string(data) != "source" {
		t.Fatalf("copied file: %q %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(target, "drive", ".craftmake", "state", "state.sqlite")); !os.IsNotExist(err) {
		t.Fatalf("state should be excluded, err=%v", err)
	}
}
