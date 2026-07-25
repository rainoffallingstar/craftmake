package compiler_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/fallingstar10/craftmake/internal/adapters/xdxtools"
	"github.com/fallingstar10/craftmake/internal/compiler"
	"github.com/fallingstar10/craftmake/internal/spec"
)

func TestCompileBeaverRNASEQPDXStep2CheckFixture(t *testing.T) {
	repositoryRoot := repositoryRootForTest(t)
	workflow, err := spec.Load(filepath.Join(repositoryRoot, "workflows", "BeaverRNASEQPDX", "step2-check.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	context, err := xdxtools.Load(filepath.Join(repositoryRoot, "testdata", "configs", "beaverrnaseqpdx-step2-check.yaml"))
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

	xenofilterTask := plan.TaskByID["BeaverRNASEQPDX/step2-check/xenofilter"]
	if xenofilterTask == nil || len(xenofilterTask.Inputs["patched_bams"]) != 4 || len(xenofilterTask.Dependencies) != 4 {
		t.Fatalf("unexpected RNA-seq Xenofilter aggregation: %#v", xenofilterTask)
	}
	xenofilterCommand := xenofilterTask.Steps[0].Command
	if !strings.Contains(xenofilterCommand, "for graft_bam") || !strings.Contains(xenofilterCommand, `xenofilter_command+=(--graft "$graft_bam")`) || !strings.Contains(xenofilterCommand, "--mm-threshold 4") || !strings.Contains(xenofilterCommand, "--graft-ref '") || !strings.Contains(xenofilterCommand, "--host-ref '") || !strings.Contains(xenofilterCommand, "xenofilter_exit_code") || !strings.Contains(xenofilterCommand, "temporary_filtered_directory") {
		t.Fatalf("unexpected RNA-seq Xenofilter command: %q", xenofilterCommand)
	}
	if strings.Contains(xenofilterCommand, "--bisulfite") {
		t.Fatalf("RNA-seq Xenofilter command must not enable bisulfite mode: %q", xenofilterCommand)
	}

	filteredTask := plan.TaskByID["BeaverRNASEQPDX/step2-check/filtered_artifacts/sample=sample-a"]
	if filteredTask == nil || !containsTaskID(filteredTask.Dependencies, xenofilterTask.ID) {
		t.Fatalf("filtered BAM validation should depend on RNA-seq Xenofilter: %#v", filteredTask)
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
