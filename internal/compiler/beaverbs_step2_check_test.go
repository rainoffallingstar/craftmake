package compiler_test

import (
	"path/filepath"
	"testing"

	"github.com/fallingstar10/craftmake/internal/adapters/xdxtools"
	"github.com/fallingstar10/craftmake/internal/compiler"
	"github.com/fallingstar10/craftmake/internal/spec"
)

func TestCompileBeaverBSStep2CheckFixture(t *testing.T) {
	repositoryRoot := repositoryRootForTest(t)
	workflow, err := spec.Load(filepath.Join(repositoryRoot, "workflows", "BeaverBS", "step2-check.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	context, err := xdxtools.Load(filepath.Join(repositoryRoot, "testdata", "configs", "beaverbs-step2.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := compiler.Compile(workflow, context)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 4 || len(plan.Submissions) != 4 {
		t.Fatalf("expected four tasks and submissions, got tasks=%d submissions=%d", len(plan.Tasks), len(plan.Submissions))
	}

	artifactTask := plan.TaskByID["BeaverBS/step2-check/sample_artifacts/sample=sample-a/species=human"]
	if artifactTask == nil || len(artifactTask.Inputs) != 7 {
		t.Fatalf("unexpected sample artifact task: %#v", artifactTask)
	}
	multiQCTask := plan.TaskByID["BeaverBS/step2-check/multiqc"]
	if multiQCTask == nil || len(multiQCTask.Inputs["sample_artifacts"]) != 2 || len(multiQCTask.Dependencies) != 2 {
		t.Fatalf("unexpected MultiQC aggregation: %#v", multiQCTask)
	}
	checkerTask := plan.TaskByID["BeaverBS/step2-check/step2_checker"]
	if checkerTask == nil || len(checkerTask.Inputs["sample_artifacts"]) != 2 || len(checkerTask.Dependencies) != 3 {
		t.Fatalf("unexpected step2 checker aggregation: %#v", checkerTask)
	}
	if checkerTask.Outputs["success_marker"] != filepath.Join("workflow", "log", "step2_success.txt") {
		t.Fatalf("unexpected checker marker %q", checkerTask.Outputs["success_marker"])
	}
}
