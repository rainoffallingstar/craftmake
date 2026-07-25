package compiler_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/fallingstar10/craftmake/internal/adapters/xdxtools"
	"github.com/fallingstar10/craftmake/internal/compiler"
	"github.com/fallingstar10/craftmake/internal/spec"
)

func TestCompileBeaverRNAStep2CheckFixture(t *testing.T) {
	repositoryRoot := repositoryRootForTest(t)
	workflow, err := spec.Load(filepath.Join(repositoryRoot, "workflows", "BeaverRNA", "step2-check.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	context, err := xdxtools.Load(filepath.Join(repositoryRoot, "testdata", "configs", "beaverrna-step2-check.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := compiler.Compile(workflow, context)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 6 || len(plan.Submissions) != 6 {
		t.Fatalf("expected six tasks and submissions, got tasks=%d submissions=%d", len(plan.Tasks), len(plan.Submissions))
	}

	artifactTask := plan.TaskByID["BeaverRNA/step2-check/sample_artifacts/sample=sample-a/species=human"]
	if artifactTask == nil || len(artifactTask.Inputs) != 9 {
		t.Fatalf("unexpected BeaverRNA sample artifact task: %#v", artifactTask)
	}
	if artifactTask.Inputs["counts"][0] != filepath.Join("workflow", "expression", "sample-a_human.txt") {
		t.Fatalf("unexpected RNA-seq count input: %#v", artifactTask.Inputs["counts"])
	}

	matrixTask := plan.TaskByID["BeaverRNA/step2-check/construct_expression_matrix"]
	if matrixTask == nil || len(matrixTask.Inputs["sample_artifacts"]) != 2 || len(matrixTask.Dependencies) != 2 {
		t.Fatalf("unexpected expression matrix aggregation: %#v", matrixTask)
	}
	if !strings.Contains(matrixTask.Steps[0].Command, "--postfix '_human.txt'") {
		t.Fatalf("unexpected htseq2matrix command: %s", matrixTask.Steps[0].Command)
	}

	splicingTask := plan.TaskByID["BeaverRNA/step2-check/rnaseq_splicing"]
	if splicingTask == nil || len(splicingTask.Dependencies) != 2 {
		t.Fatalf("unexpected RNA splicing aggregation: %#v", splicingTask)
	}
	if !strings.Contains(splicingTask.Steps[0].Command, "gomats run") || !strings.Contains(splicingTask.Steps[0].Command, "--pdxmode 0") {
		t.Fatalf("unexpected RNA splicing command: %s", splicingTask.Steps[0].Command)
	}

	qcSummaryTask := plan.TaskByID["BeaverRNA/step2-check/qc_summary"]
	if qcSummaryTask == nil || len(qcSummaryTask.Dependencies) != 2 {
		t.Fatalf("unexpected RNA QC summary aggregation: %#v", qcSummaryTask)
	}
	checkerTask := plan.TaskByID["BeaverRNA/step2-check/step2_checker"]
	if checkerTask == nil || len(checkerTask.Dependencies) != 5 {
		t.Fatalf("unexpected BeaverRNA step2 checker aggregation: %#v", checkerTask)
	}
	if checkerTask.Outputs["success_marker"] != filepath.Join("workflow", "log", "step2_success.txt") {
		t.Fatalf("unexpected BeaverRNA step2 success marker %q", checkerTask.Outputs["success_marker"])
	}
}
