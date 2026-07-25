package compiler_test

import (
	"path/filepath"
	"testing"

	"github.com/fallingstar10/craftmake/internal/adapters/xdxtools"
	"github.com/fallingstar10/craftmake/internal/compiler"
	"github.com/fallingstar10/craftmake/internal/spec"
)

func TestCompileBeaverPDXStep1Fixture(t *testing.T) {
	repositoryRoot := repositoryRootForTest(t)
	workflow, err := spec.Load(filepath.Join(repositoryRoot, "workflows", "BeaverPDX", "step1.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	context, err := xdxtools.Load(filepath.Join(repositoryRoot, "testdata", "configs", "beaverpdx-step1.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if context.Workflow.WorkflowName != "BeaverPDX" || !context.Workflow.PDXMode {
		t.Fatalf("unexpected PDX context: %#v", context.Workflow)
	}
	if len(context.Species) != 2 || context.Species[0].Name != "human" || context.Species[1].Name != "mouse" {
		t.Fatalf("unexpected PDX species contexts: %#v", context.Species)
	}
	plan, err := compiler.Compile(workflow, context)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 7 || len(plan.Submissions) != 7 {
		t.Fatalf("expected seven tasks and submissions, got tasks=%d submissions=%d", len(plan.Tasks), len(plan.Submissions))
	}

	fastqcTask := plan.TaskByID["BeaverPDX/step1/fastqc_before/sample=sample-a"]
	if fastqcTask == nil {
		t.Fatal("missing sample-only PDX FastQC task")
	}
	if len(fastqcTask.Dimensions) != 1 || fastqcTask.Dimensions["sample"] != "sample-a" {
		t.Fatalf("PDX step1 should expand by sample only: %#v", fastqcTask.Dimensions)
	}
	if fastqcTask.Inputs["read1"][0] != filepath.Join("data", "sample-a_R1.fastq.gz") {
		t.Fatalf("unexpected PDX sample input: %#v", fastqcTask.Inputs["read1"])
	}
	checkerTask := plan.TaskByID["BeaverPDX/step1/step1_checker"]
	if checkerTask == nil || len(checkerTask.Dependencies) != 6 {
		t.Fatalf("PDX checker should depend on six sample tasks: %#v", checkerTask)
	}
	if checkerTask.Outputs["success_marker"] != filepath.Join("workflow", "log", "step1_success.txt") {
		t.Fatalf("unexpected PDX checker marker %q", checkerTask.Outputs["success_marker"])
	}
}
