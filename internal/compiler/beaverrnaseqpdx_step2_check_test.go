package compiler_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/fallingstar10/craftmake/internal/adapters/otter"
	"github.com/fallingstar10/craftmake/internal/compiler"
	"github.com/fallingstar10/craftmake/internal/spec"
)

func TestCompileBeaverRNASEQPDXStep2CheckFixture(t *testing.T) {
	repositoryRoot := repositoryRootForTest(t)
	workflow, err := spec.Load(filepath.Join(repositoryRoot, "workflows", "BeaverRNASEQPDX", "step2-check.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	context, err := otter.LoadLegacy(filepath.Join(repositoryRoot, "testdata", "configs", "beaverrnaseqpdx-step2-check.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := compiler.Compile(workflow, context)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 12 || len(plan.Submissions) != 12 {
		t.Fatalf("expected twelve tasks and submissions, got tasks=%d submissions=%d", len(plan.Tasks), len(plan.Submissions))
	}

	artifactTaskID := "BeaverRNASEQPDX/step2-check/sample_artifacts/sample=sample-a/species=mouse"
	artifactTask := plan.TaskByID[artifactTaskID]
	if artifactTask == nil || len(artifactTask.Inputs) != 4 {
		t.Fatalf("unexpected RNA-seq PDX artifact task: %#v", artifactTask)
	}

	patchTaskID := "BeaverRNASEQPDX/step2-check/patch_bam/sample=sample-a/species=mouse"
	patchTask := plan.TaskByID[patchTaskID]
	if patchTask == nil || !containsTaskID(patchTask.Dependencies, artifactTaskID) {
		t.Fatalf("mouse patch task should depend on matching artifact validation: %#v", patchTask)
	}
	if patchTask.Inputs["reference"][0] != filepath.Join(repositoryRoot, "testdata", "configs", "references", "mouse.fasta") {
		t.Fatalf("unexpected mouse patch reference: %#v", patchTask.Inputs["reference"])
	}
	if !strings.Contains(patchTask.Steps[0].Command, "IS_BISULFITE_SEQUENCE=false") {
		t.Fatalf("RNA-seq PDX patch should disable bisulfite mode: %q", patchTask.Steps[0].Command)
	}
	if !strings.Contains(patchTask.Steps[0].Command, `export JAVA_HOME="${CONDA_PREFIX}/lib/jvm"`) {
		t.Fatalf("RNA-seq PDX patch task should bind Picard to the active environment Java runtime: %q", patchTask.Steps[0].Command)
	}

	xenofilxTask := plan.TaskByID["BeaverRNASEQPDX/step2-check/xenofilx"]
	if xenofilxTask == nil || len(xenofilxTask.Inputs["patched_bams"]) != 4 || len(xenofilxTask.Dependencies) != 4 {
		t.Fatalf("unexpected RNA-seq Xenofilx aggregation: %#v", xenofilxTask)
	}
	xenofilxCommand := xenofilxTask.Steps[0].Command
	if !strings.Contains(xenofilxCommand, "for graft_bam") || !strings.Contains(xenofilxCommand, `xenofilx_command+=(--graft "$graft_bam")`) || !strings.Contains(xenofilxCommand, "--mm-threshold 4") || !strings.Contains(xenofilxCommand, "--graft-ref '") || !strings.Contains(xenofilxCommand, "--host-ref '") || !strings.Contains(xenofilxCommand, "xenofilx_exit_code") || !strings.Contains(xenofilxCommand, "temporary_filtered_directory") {
		t.Fatalf("unexpected RNA-seq Xenofilx command: %q", xenofilxCommand)
	}
	if strings.Contains(xenofilxCommand, "--bisulfite") {
		t.Fatalf("RNA-seq Xenofilx command must not enable bisulfite mode: %q", xenofilxCommand)
	}

	filteredTask := plan.TaskByID["BeaverRNASEQPDX/step2-check/filtered_artifacts/sample=sample-a"]
	if filteredTask == nil || !containsTaskID(filteredTask.Dependencies, xenofilxTask.ID) {
		t.Fatalf("filtered BAM validation should depend on RNA-seq Xenofilx: %#v", filteredTask)
	}
	if filteredTask.Inputs["filtered_bam"][0] != filepath.Join("workflow", "bsmap", "Filtered_bams", "sample-a_fixed_human_Filtered.bam") {
		t.Fatalf("unexpected RNA-seq filtered graft BAM path: %#v", filteredTask.Inputs["filtered_bam"])
	}

	checkerTask := plan.TaskByID["BeaverRNASEQPDX/step2-check/step2_checker"]
	if checkerTask == nil || len(checkerTask.Dependencies) != 11 {
		t.Fatalf("unexpected RNA-seq PDX step2 checker aggregation: %#v", checkerTask)
	}
	if checkerTask.Outputs["success_marker"] != filepath.Join("workflow", "log", "step2_success.txt") {
		t.Fatalf("unexpected checker marker %q", checkerTask.Outputs["success_marker"])
	}
}
