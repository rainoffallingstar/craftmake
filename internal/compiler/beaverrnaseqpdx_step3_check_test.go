package compiler_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/fallingstar10/craftmake/internal/adapters/otter"
	"github.com/fallingstar10/craftmake/internal/compiler"
	"github.com/fallingstar10/craftmake/internal/spec"
)

func TestCompileBeaverRNASEQPDXStep3CheckFixture(t *testing.T) {
	repositoryRoot := repositoryRootForTest(t)
	workflow, err := spec.Load(filepath.Join(repositoryRoot, "workflows", "BeaverRNASEQPDX", "step3-check.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	context, err := otter.LoadLegacy(filepath.Join(repositoryRoot, "testdata", "configs", "beaverrnaseqpdx-step3-check.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := compiler.Compile(workflow, context)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 9 || len(plan.Submissions) != 9 {
		t.Fatalf("expected nine tasks and submissions, got tasks=%d submissions=%d", len(plan.Tasks), len(plan.Submissions))
	}

	sampleTask := plan.TaskByID["BeaverRNASEQPDX/step3-check/sample_artifacts/sample=sample-a"]
	if sampleTask == nil || len(sampleTask.Inputs) != 8 {
		t.Fatalf("unexpected RNA-seq PDX sample artifact task: %#v", sampleTask)
	}
	if sampleTask.Inputs["counts"][0] != filepath.Join("workflow", "expression", "sample-a_human.txt") {
		t.Fatalf("unexpected graft count input: %#v", sampleTask.Inputs["counts"])
	}
	if sampleTask.Inputs["filtered_bam"][0] != filepath.Join("workflow", "bsmap", "Filtered_bams", "sample-a_fixed_human_Filtered.bam") {
		t.Fatalf("unexpected filtered graft BAM input: %#v", sampleTask.Inputs["filtered_bam"])
	}
	if sampleTask.Inputs["fastqc_before_r1"][0] != filepath.Join("workflow", "fastqc_raw", "sample-a_R1_fastqcx", "fastqc_data.txt") ||
		sampleTask.Inputs["fastqc_after_r2"][0] != filepath.Join("workflow", "fastqc_clean", "sample-a_val_2_fastqcx", "fastqc_data.txt") {
		t.Fatalf("unexpected RNA-seq PDX Fastqcx artifact inputs: %#v", sampleTask.Inputs)
	}

	speciesTask := plan.TaskByID["BeaverRNASEQPDX/step3-check/species_qc_artifacts/sample=sample-b/species=mouse"]
	if speciesTask == nil || len(speciesTask.Inputs) != 2 {
		t.Fatalf("unexpected species QC task: %#v", speciesTask)
	}
	if speciesTask.Inputs["sorted_bam"][0] != filepath.Join("workflow", "bsmap", "sample-b_mouse.bam") {
		t.Fatalf("unexpected mouse mapping BAM input: %#v", speciesTask.Inputs["sorted_bam"])
	}

	matrixTask := plan.TaskByID["BeaverRNASEQPDX/step3-check/construct_expression_matrix"]
	if matrixTask == nil || len(matrixTask.Inputs["sample_artifacts"]) != 2 || len(matrixTask.Dependencies) != 2 {
		t.Fatalf("unexpected expression matrix aggregation: %#v", matrixTask)
	}
	if !strings.Contains(matrixTask.Steps[0].Command, "--postfix '_human.txt'") {
		t.Fatalf("unexpected graft matrix command: %q", matrixTask.Steps[0].Command)
	}

	qcSummaryTask := plan.TaskByID["BeaverRNASEQPDX/step3-check/qc_summary"]
	if qcSummaryTask == nil || len(qcSummaryTask.Inputs["species_qc_artifacts"]) != 4 || len(qcSummaryTask.Dependencies) != 6 {
		t.Fatalf("unexpected RNA-seq PDX QC summary aggregation: %#v", qcSummaryTask)
	}
	if !strings.Contains(qcSummaryTask.Steps[0].Command, "qctb") || !strings.Contains(qcSummaryTask.Steps[0].Command, "--rnaseq") {
		t.Fatalf("unexpected RNA-seq PDX QC command: %q", qcSummaryTask.Steps[0].Command)
	}
	if !strings.Contains(qcSummaryTask.Steps[0].Command, `species_configuration["name"] = graft_species`) || !strings.Contains(qcSummaryTask.Steps[0].Command, "yaml.safe_load") {
		t.Fatalf("RNA-seq PDX QC command does not create a task-local QCTB compatibility config: %q", qcSummaryTask.Steps[0].Command)
	}

	checkerTask := plan.TaskByID["BeaverRNASEQPDX/step3-check/step3_checker"]
	if checkerTask == nil || len(checkerTask.Dependencies) != 8 {
		t.Fatalf("unexpected RNA-seq PDX final checker aggregation: %#v", checkerTask)
	}
	if checkerTask.Inputs["splicing"][0] != filepath.Join("workflow", "bsmap", "RNASplicing", "RNASplicing_success.txt") {
		t.Fatalf("unexpected splicing marker input: %#v", checkerTask.Inputs["splicing"])
	}
	if checkerTask.Outputs["success_marker"] != filepath.Join("workflow", "log", "step3_success.txt") {
		t.Fatalf("unexpected RNA-seq PDX step3 marker %q", checkerTask.Outputs["success_marker"])
	}
}
