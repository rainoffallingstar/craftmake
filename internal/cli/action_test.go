package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadActionPlanCompilesSelfContainedAction(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".craftmake")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "build.yaml")
	content := []byte(`schema_version: craftmake.action/v1
name: build
inputs:
  target:
    default: hg38
jobs:
  compile:
    outputs:
      result: results/${{ inputs.target }}.txt
    steps:
      - run: echo ok
`)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	plan, options, err := loadActionPlan(root, "build", nil)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Workflow != "build" || plan.Phase != "main" || len(plan.Tasks) != 1 {
		t.Fatalf("unexpected plan: %#v", plan)
	}
	if options.resolvedBackend != "local" || string(options.configKind) != "craftmake.action/v1" {
		t.Fatalf("unexpected options: %#v", options)
	}
}
