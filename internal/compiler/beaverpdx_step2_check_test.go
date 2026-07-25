package compiler_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/fallingstar10/craftmake/internal/adapters/xdxtools"
	"github.com/fallingstar10/craftmake/internal/compiler"
	"github.com/fallingstar10/craftmake/internal/spec"
)

func TestCompileBeaverPDXStep2CheckFixture(t *testing.T) {
	repositoryRoot := repositoryRootForTest(t)
	workflow, err := spec.Load(filepath.Join(repositoryRoot, "workflows", "BeaverPDX", "step2-check.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	context, err := xdxtools.Load(filepath.Join(repositoryRoot, "fixtures", "BeaverPDX", "step2-check.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := compiler.Compile(workflow, context)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 13 || len(plan.Submissions) != 13 {
		t.Fatalf("expected thirteen tasks and submissions, got tasks=%d submissions=%d", len(plan.Tasks), len(plan.Submissions))
	}

	artifactTaskID := "BeaverPDX/step2-check/sample_artifacts/sample=sample-a/species=mouse"
	artifactTask := plan.TaskByID[artifactTaskID]
	if artifactTask == nil || len(artifactTask.Inputs) != 7 {
		t.Fatalf("unexpected PDX sample artifact task: %#v", artifactTask)
	}

	patchTaskID := "BeaverPDX/step2-check/patch_bam/sample=sample-a/species=mouse"
	patchTask := plan.TaskByID[patchTaskID]
	if patchTask == nil || !containsTaskID(patchTask.Dependencies, artifactTaskID) {
		t.Fatalf("mouse patch task should depend on matching artifact validation: %#v", patchTask)
	}
	if patchTask.Inputs["reference"][0] != filepath.Join(repositoryRoot, "fixtures", "BeaverPDX", "references", "mouse.fasta") {
		t.Fatalf("unexpected mouse patch reference: %#v", patchTask.Inputs["reference"])
	}
	if patchTask.Outputs["fixed_bam"] != filepath.Join("workflow", "bsmap", "sample-a_fixed_mouse.bam") {
		t.Fatalf("unexpected fixed BAM output %q", patchTask.Outputs["fixed_bam"])
	}
	if !strings.Contains(patchTask.Steps[0].Command, "IS_BISULFITE_SEQUENCE=true") {
		t.Fatalf("PDX methylation patch should enable bisulfite mode: %q", patchTask.Steps[0].Command)
	}
	if !strings.Contains(patchTask.Steps[0].Command, `export JAVA_HOME="${CONDA_PREFIX}/lib/jvm"`) {
		t.Fatalf("PDX patch task should bind Picard to the active environment Java runtime: %q", patchTask.Steps[0].Command)
	}

	xenofilterTask := plan.TaskByID["BeaverPDX/step2-check/xenofilter"]
	if xenofilterTask == nil || len(xenofilterTask.Inputs["patched_bams"]) != 4 || len(xenofilterTask.Dependencies) != 4 {
		t.Fatalf("unexpected Xenofilter aggregation: %#v", xenofilterTask)
	}
	if !strings.Contains(xenofilterTask.Steps[0].Command, "for graft_bam") || !strings.Contains(xenofilterTask.Steps[0].Command, `xenofilter_command+=(--graft "$graft_bam")`) || !strings.Contains(xenofilterTask.Steps[0].Command, "--graft-ref '") || !strings.Contains(xenofilterTask.Steps[0].Command, "--host-ref '") || !strings.Contains(xenofilterTask.Steps[0].Command, "--bisulfite") || !strings.Contains(xenofilterTask.Steps[0].Command, "xenofilter_exit_code") || !strings.Contains(xenofilterTask.Steps[0].Command, "temporary_filtered_directory") {
		t.Fatalf("unexpected Xenofilter command: %q", xenofilterTask.Steps[0].Command)
	}

	filteredTask := plan.TaskByID["BeaverPDX/step2-check/filtered_artifacts/sample=sample-a"]
	if filteredTask == nil || !containsTaskID(filteredTask.Dependencies, xenofilterTask.ID) {
		t.Fatalf("filtered BAM validation should depend on Xenofilter: %#v", filteredTask)
	}
	if filteredTask.Inputs["filtered_bam"][0] != filepath.Join("workflow", "bsmap", "Filtered_bams", "sample-a_fixed_human_Filtered.bam") {
		t.Fatalf("unexpected filtered graft BAM path: %#v", filteredTask.Inputs["filtered_bam"])
	}

	multiQCTask := plan.TaskByID["BeaverPDX/step2-check/multiqc"]
	if multiQCTask == nil || len(multiQCTask.Inputs["sample_artifacts"]) != 4 || len(multiQCTask.Dependencies) != 4 {
		t.Fatalf("unexpected PDX MultiQC aggregation: %#v", multiQCTask)
	}
	checkerTask := plan.TaskByID["BeaverPDX/step2-check/step2_checker"]
	if checkerTask == nil || len(checkerTask.Dependencies) != 12 {
		t.Fatalf("unexpected PDX step2 checker aggregation: %#v", checkerTask)
	}
	if checkerTask.Outputs["success_marker"] != filepath.Join("workflow", "log", "step2_success.txt") {
		t.Fatalf("unexpected checker marker %q", checkerTask.Outputs["success_marker"])
	}
}
