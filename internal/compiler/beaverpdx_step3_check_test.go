package compiler_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/fallingstar10/craftmake/internal/adapters/xdxtools"
	"github.com/fallingstar10/craftmake/internal/compiler"
	"github.com/fallingstar10/craftmake/internal/spec"
)

func TestCompileBeaverPDXStep3CheckFixture(t *testing.T) {
	repositoryRoot := repositoryRootForTest(t)
	workflow, err := spec.Load(filepath.Join(repositoryRoot, "workflows", "BeaverPDX", "step3-check.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	context, err := xdxtools.Load(filepath.Join(repositoryRoot, "fixtures", "BeaverPDX", "step3-check.yaml"))
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

	artifactTask := plan.TaskByID["BeaverPDX/step3-check/sample_artifacts/sample=sample-a"]
	if artifactTask == nil || len(artifactTask.Inputs) != 9 {
		t.Fatalf("unexpected PDX step3 sample artifact task: %#v", artifactTask)
	}
	if artifactTask.Inputs["filtered_bam"][0] != filepath.Join("workflow", "bsmap", "Filtered_bams", "sample-a_fixed_human_Filtered.bam") {
		t.Fatalf("unexpected filtered graft BAM input: %#v", artifactTask.Inputs["filtered_bam"])
	}

	speciesTask := plan.TaskByID["BeaverPDX/step3-check/species_qc_artifacts/sample=sample-b/species=mouse"]
	if speciesTask == nil || len(speciesTask.Inputs) != 2 {
		t.Fatalf("unexpected species QC task: %#v", speciesTask)
	}
	if speciesTask.Inputs["sorted_bam"][0] != filepath.Join("workflow", "bsmap", "sample-b_mouse.bam") {
		t.Fatalf("unexpected mouse mapping BAM input: %#v", speciesTask.Inputs["sorted_bam"])
	}

	referenceTask := plan.TaskByID["BeaverPDX/step3-check/prepare_methrix_reference"]
	if referenceTask == nil || referenceTask.Inputs["genome"][0] != filepath.Join(repositoryRoot, "fixtures", "BeaverPDX", "references", "human.fasta") {
		t.Fatalf("unexpected PDX graft reference task: %#v", referenceTask)
	}
	if !strings.Contains(referenceTask.Steps[0].Command, "--contigs") || !strings.Contains(referenceTask.Steps[0].Command, `contig_arguments+=(--contigs "$contig")`) || !strings.Contains(referenceTask.Steps[0].Command, "methrix_extract_command") {
		t.Fatalf("PDX Methrix reference preparation lacks repeated nonstandard contig flags: %s", referenceTask.Steps[0].Command)
	}
	if !strings.Contains(referenceTask.Steps[0].Command, "METHRIX_CLI:-methrix-cli") {
		t.Fatalf("PDX Methrix reference preparation should support an explicit executable override: %s", referenceTask.Steps[0].Command)
	}

	methrixTask := plan.TaskByID["BeaverPDX/step3-check/create_methrix_object"]
	if methrixTask == nil || len(methrixTask.Inputs["sample_artifacts"]) != 2 || len(methrixTask.Dependencies) != 3 {
		t.Fatalf("unexpected PDX Methrix aggregation: %#v", methrixTask)
	}
	if methrixTask.Outputs["methrix_data"] != filepath.Join("workflow", "mCall", "methrixh5", "methrix_data.h5") {
		t.Fatalf("unexpected PDX Methrix HDF5 output %q", methrixTask.Outputs["methrix_data"])
	}
	if strings.Contains(methrixTask.Steps[0].Command, "--annotation-dir") {
		t.Fatalf("PDX Methrix process command uses unsupported --annotation-dir: %s", methrixTask.Steps[0].Command)
	}
	if !strings.Contains(methrixTask.Steps[0].Command, "METHRIX_CLI:-methrix-cli") {
		t.Fatalf("PDX Methrix process should support an explicit executable override: %s", methrixTask.Steps[0].Command)
	}

	bismarkReportTask := plan.TaskByID["BeaverPDX/step3-check/bismark_report/sample=sample-a"]
	if bismarkReportTask == nil || !strings.Contains(bismarkReportTask.Steps[0].Command, "bismark2report") {
		t.Fatalf("unexpected PDX Bismark report task: %#v", bismarkReportTask)
	}
	bismarkSummaryTask := plan.TaskByID["BeaverPDX/step3-check/bismark_summary"]
	if bismarkSummaryTask == nil || len(bismarkSummaryTask.Inputs["sample_reports"]) != 2 || len(bismarkSummaryTask.Dependencies) != 2 {
		t.Fatalf("unexpected PDX Bismark summary aggregation: %#v", bismarkSummaryTask)
	}

	qcSummaryTask := plan.TaskByID["BeaverPDX/step3-check/qc_summary"]
	if qcSummaryTask == nil || len(qcSummaryTask.Inputs["species_qc_artifacts"]) != 4 || len(qcSummaryTask.Dependencies) != 7 {
		t.Fatalf("unexpected PDX QC summary aggregation: %#v", qcSummaryTask)
	}
	if !strings.Contains(qcSummaryTask.Steps[0].Command, `species_configuration["name"] = graft_species`) || !strings.Contains(qcSummaryTask.Steps[0].Command, "yaml.safe_load") {
		t.Fatalf("PDX QC summary should derive a QCTB-compatible species config: %s", qcSummaryTask.Steps[0].Command)
	}
	checkerTask := plan.TaskByID["BeaverPDX/step3-check/step3_checker"]
	if checkerTask == nil || len(checkerTask.Dependencies) != 5 {
		t.Fatalf("unexpected PDX step3 checker aggregation: %#v", checkerTask)
	}
	if checkerTask.Outputs["success_marker"] != filepath.Join("workflow", "log", "step3_success.txt") {
		t.Fatalf("unexpected PDX step3 success marker %q", checkerTask.Outputs["success_marker"])
	}
}
