package action

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fallingstar10/craftmake/internal/spec"
)

func TestLoadNormalizesSelfContainedAction(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, ".craftmake", "build.yaml")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("schema_version: craftmake.action/v1\nname: build\nbackend: local\ninputs:\n  target:\n    default: hg38\nenv:\n  TAG: ${{ inputs.target }}\njobs:\n  compile:\n    needs: []\n    env:\n      MODE: release\n    outputs:\n      result: results/${{ inputs.target }}.txt\n    steps:\n      - id: make\n        run: echo $TAG > $CRAFTMAKE_OUTPUT\n        env:\n          CFLAGS: -O3\n")
	if err := os.WriteFile(source, content, 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(source, nil, root, filepath.Join(root, ".craftmake", "state"))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Workflow.Name != "build" || loaded.Workflow.On.Otter.Workflow != "build" || loaded.Workflow.On.Otter.Phase != "main" {
		t.Fatalf("unexpected workflow identity: %#v", loaded.Workflow)
	}
	job := loaded.Workflow.Jobs["compile"]
	if job.Scope != "global" || job.Outputs["result"] != "results/hg38.txt" {
		t.Fatalf("unexpected normalized job: %#v", job)
	}
	if job.Steps[0].Run != "echo $TAG > $CRAFTMAKE_OUTPUT" || job.Env["MODE"] != "release" {
		t.Fatalf("unexpected step/job values: %#v", job)
	}
	if loaded.Context.Raw["inputs"].(map[string]any)["target"] != "hg38" {
		t.Fatalf("missing resolved input: %#v", loaded.Context.Raw)
	}
}

func TestLoadRejectsMissingRequiredInput(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "action.yaml")
	content := []byte("schema_version: craftmake.action/v1\nname: build\ninputs:\n  token:\n    required: true\njobs:\n  job:\n    steps:\n      - run: echo ok\n")
	if err := os.WriteFile(source, content, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(source, nil, root, filepath.Join(root, "state"))
	if err == nil || !strings.Contains(err.Error(), "input token is required") {
		t.Fatalf("expected required input error, got %v", err)
	}
}

func TestDiscoverOnlyFlatYAMLFiles(t *testing.T) {
	root := t.TempDir()
	craftmake := filepath.Join(root, ".craftmake")
	if err := os.MkdirAll(filepath.Join(craftmake, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.yaml", "b.yml", "nested/c.yaml", "ignore.txt"} {
		path := filepath.Join(craftmake, name)
		if err := os.WriteFile(path, []byte("schema_version: craftmake.action/v1\nname: "+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || filepath.Base(entries[0].Path) != "a.yaml" || filepath.Base(entries[1].Path) != "b.yml" {
		t.Fatalf("unexpected discovery: %#v", entries)
	}
}

var _ = spec.CurrentVersion
