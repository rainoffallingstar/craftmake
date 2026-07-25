package compiler_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/fallingstar10/craftmake/internal/adapters/xdxtools"
	"github.com/fallingstar10/craftmake/internal/compiler"
	"github.com/fallingstar10/craftmake/internal/spec"
)

func TestCompileBeaverBSStep3Fixture(t *testing.T) {
	repositoryRoot := repositoryRootForTest(t)
	workflow, err := spec.Load(filepath.Join(repositoryRoot, "workflows", "BeaverBS", "step3.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	context, err := xdxtools.Load(filepath.Join(repositoryRoot, "testdata", "configs", "beaverbs-step3.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := compiler.Compile(workflow, context)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 2 || len(plan.Submissions) != 2 {
		t.Fatalf("expected two tasks and submissions, got tasks=%d submissions=%d", len(plan.Tasks), len(plan.Submissions))
	}

	taskID := "BeaverBS/step3/extract_methylation/sample=sample-a/species=human"
	task := plan.TaskByID[taskID]
	if task == nil {
		t.Fatalf("missing methylation extraction task %q", taskID)
	}
	if task.Inputs["bam"][0] != filepath.Join("workflow", "bsmap", "sample-a_human.bam") {
		t.Fatalf("unexpected step3 BAM input: %#v", task.Inputs["bam"])
	}
	if task.Outputs["name_sorted_bam"] != filepath.Join("workflow", "bsmap", "sample-a_nsort.bam") {
		t.Fatalf("unexpected name-sorted BAM output %q", task.Outputs["name_sorted_bam"])
	}
	if task.Outputs["coverage"] != filepath.Join("workflow", "mCall", "sample-a_nsort.bismark.cov.gz") {
		t.Fatalf("unexpected methylation coverage output %q", task.Outputs["coverage"])
	}
	if len(task.Steps) != 3 {
		t.Fatalf("expected sort, pair filtering and extraction steps, got %#v", task.Steps)
	}
	if task.Steps[0].Environment != "xdxtools-core" || task.Steps[1].Environment != "" || task.Steps[2].Environment != "xdxtools-core" {
		t.Fatalf("unexpected step environment boundaries: %#v", task.Steps)
	}
	if !strings.Contains(task.Steps[0].Command, "samtools sort") || !strings.Contains(task.Steps[1].Command, "paireads") || !strings.Contains(task.Steps[2].Command, "bismark_methylation_extractor") {
		t.Fatalf("unexpected step3 commands: %#v", task.Steps)
	}
}
