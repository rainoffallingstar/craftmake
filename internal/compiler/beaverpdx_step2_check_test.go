package compiler_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/fallingstar10/craftmake/internal/adapters/otter"
	"github.com/fallingstar10/craftmake/internal/compiler"
	"github.com/fallingstar10/craftmake/internal/spec"
)

func TestCompileBeaverPDXStep2CheckFixture(t *testing.T) {
	repositoryRoot := repositoryRootForTest(t)
	workflow, err := spec.Load(filepath.Join(repositoryRoot, "workflows", "BeaverPDX", "step2-check.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	context, err := otter.LoadLegacy(filepath.Join(repositoryRoot, "fixtures", "BeaverPDX", "step2-check.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := compiler.Compile(workflow, context)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 10 || len(plan.Submissions) != 10 {
		t.Fatalf("expected ten tasks and submissions, got tasks=%d submissions=%d", len(plan.Tasks), len(plan.Submissions))
	}

	artifactTaskID := "BeaverPDX/step2-check/sample_artifacts/sample=sample-a/species=mouse"
	artifactTask := plan.TaskByID[artifactTaskID]
	if artifactTask == nil || len(artifactTask.Inputs) != 7 {
		t.Fatalf("unexpected PDX sample artifact task: %#v", artifactTask)
	}
	for _, requiredFragment := range []string{
		"otter.sample-artifacts-validation/v1",
		"gc_metrics",
		"gc_chart",
		"gc_summary",
		"sha256sum",
		"regular non-symlink file",
	} {
		if !strings.Contains(artifactTask.Steps[0].Command, requiredFragment) {
			t.Fatalf("PDX sample artifact validation manifest command does not contain %q:\n%s", requiredFragment, artifactTask.Steps[0].Command)
		}
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

	xenofilxTask := plan.TaskByID["BeaverPDX/step2-check/xenofilx"]
	if xenofilxTask == nil || len(xenofilxTask.Inputs["patched_bams"]) != 4 || len(xenofilxTask.Dependencies) != 4 {
		t.Fatalf("unexpected Xenofilx aggregation: %#v", xenofilxTask)
	}
	if !strings.Contains(xenofilxTask.Steps[0].Command, "for graft_bam") || !strings.Contains(xenofilxTask.Steps[0].Command, `xenofilx_command+=(--graft "$graft_bam")`) || !strings.Contains(xenofilxTask.Steps[0].Command, "--graft-ref '") || !strings.Contains(xenofilxTask.Steps[0].Command, "--host-ref '") || !strings.Contains(xenofilxTask.Steps[0].Command, "--bisulfite") || !strings.Contains(xenofilxTask.Steps[0].Command, "xenofilx_exit_code") || !strings.Contains(xenofilxTask.Steps[0].Command, "temporary_filtered_directory") {
		t.Fatalf("unexpected Xenofilx command: %q", xenofilxTask.Steps[0].Command)
	}

	if xenofilxTask.Outputs["validation_manifest"] != filepath.Join("workflow", "bsmap", "Filtered_bams", "filtered-bam-validation.json") {
		t.Fatalf("unexpected filtered BAM validation manifest: %#v", xenofilxTask.Outputs)
	}
	for _, requiredFragment := range []string{
		"otter.filtered-bam-validation/v1",
		"--mm-threshold 6",
		"--bisulfite",
		"sha256sum",
		"regular non-symlink files",
	} {
		if !strings.Contains(xenofilxTask.Steps[0].Command, requiredFragment) {
			t.Fatalf("filtered BAM validation manifest command does not contain %q:\n%s", requiredFragment, xenofilxTask.Steps[0].Command)
		}
	}
	if plan.TaskByID["BeaverPDX/step2-check/filtered_artifacts/sample=sample-a"] != nil {
		t.Fatal("filtered BAM validation must be represented by Xenofilx's content-bearing aggregate manifest")
	}

	multiQCTask := plan.TaskByID["BeaverPDX/step2-check/multiqc"]
	if multiQCTask == nil || len(multiQCTask.Inputs["sample_artifacts"]) != 4 || len(multiQCTask.Dependencies) != 4 {
		t.Fatalf("unexpected PDX MultiQC aggregation: %#v", multiQCTask)
	}
	if plan.TaskByID["BeaverPDX/step2-check/step2_checker"] != nil {
		t.Fatal("step2-check must terminate in content-bearing validation manifests and MultiQC, not a marker-only checker")
	}
}
