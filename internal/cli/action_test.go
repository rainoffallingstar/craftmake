package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadActionPlanCarriesAccelerator(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".craftmake")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "mixed.yaml")
	content := []byte("schema_version: craftmake.action/v1\nname: mixed\nbackend: colab\ncolab:\n  session: gpu\n  default_accelerator: cpu\njobs:\n  pre:\n    steps:\n      - run: echo pre\n  train:\n    accelerator: gpu\n    needs: [pre]\n    steps:\n      - run: echo train\n  post:\n    needs: [train]\n    steps:\n      - run: echo post\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	plan, _, err := loadActionPlan(root, "mixed", nil)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]string{}
	for _, task := range plan.Tasks {
		byID[task.JobID] = task.Accelerator
	}
	if byID["pre"] != "cpu" || byID["train"] != "gpu" || byID["post"] != "cpu" {
		t.Fatalf("unexpected accelerators: %#v", byID)
	}
	if len(plan.Order) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(plan.Order))
	}
}

func TestActionPlanPrintsAccelerator(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".craftmake")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "accel.yaml")
	content := []byte("schema_version: craftmake.action/v1\nname: accel\nbackend: colab\ncolab:\n  session: gpu\n  default_accelerator: cpu\njobs:\n  train:\n    accelerator: gpu\n    steps:\n      - run: echo train\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadActionPlan(root, "accel", nil); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	cmd := newActionPlanCommand()
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"accel", "--dir", root})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "accel=gpu") {
		t.Fatalf("plan output missing accel=gpu: %q", buf.String())
	}
}

func TestLoadActionPlanCompilesSelfContainedAction(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".craftmake")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "build.yaml")
	content := []byte("schema_version: craftmake.action/v1\nname: build\ninputs:\n  target:\n    default: hg38\njobs:\n  compile:\n    outputs:\n      result: results/${{ inputs.target }}.txt\n    steps:\n      - run: echo ok\n")
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
