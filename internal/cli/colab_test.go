package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestColabAuthCLIConfiguresAndMountsNamedSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	configure := newColabAuthConfigureCommand()
	configure.SetArgs([]string{"--config", path, "--session", "gpu", "--drive-root", "/content/drive/MyDrive/craftmake", "--colab-credential-file", "/tmp/colab.json", "--drive-credential-file", "/tmp/drive.json"})
	var output bytes.Buffer
	configure.SetOut(&output)
	if err := configure.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	mount := newColabDriveMountCommand()
	mount.SetArgs([]string{"--config", path, "--session", "gpu"})
	mount.SetOut(&output)
	if err := mount.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "\"status\": \"ready-for-backend-mount\"") {
		t.Fatalf("unexpected output: %s", output.String())
	}
	doctor := newColabDoctorCommand()
	doctor.SetArgs([]string{"--config", path, "--session", "gpu"})
	doctor.SetOut(&output)
	if err := doctor.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "\"ready\": true") {
		t.Fatalf("unexpected doctor output: %s", output.String())
	}
}
