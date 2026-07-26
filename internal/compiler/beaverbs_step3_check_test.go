package compiler_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/fallingstar10/craftmake/internal/adapters/otter"
	"github.com/fallingstar10/craftmake/internal/compiler"
	"github.com/fallingstar10/craftmake/internal/spec"
)

func TestCompileBeaverBSStep3CheckFixture(t *testing.T) {
	repositoryRoot := repositoryRootForTest(t)
	workflow, err := spec.Load(filepath.Join(repositoryRoot, "workflows", "BeaverBS", "step3-check.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	context, err := otter.LoadLegacy(filepath.Join(repositoryRoot, "testdata", "configs", "beaverbs-step3-check.yaml"))
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

	firstArtifactTask := plan.TaskByID["BeaverBS/step3-check/sample_artifacts/sample=sample-a"]
	if firstArtifactTask == nil || len(firstArtifactTask.Inputs) != 10 {
		t.Fatalf("unexpected sample artifact validation task: %#v", firstArtifactTask)
	}
	if firstArtifactTask.Inputs["coverage"][0] != filepath.Join("workflow", "mCall", "sample-a_nsort.bismark.cov.gz") {
		t.Fatalf("unexpected sample coverage input: %#v", firstArtifactTask.Inputs["coverage"])
	}
	if firstArtifactTask.Inputs["fastqc_before_r1"][0] != filepath.Join("workflow", "fastqc_raw", "sample-a_R1_fastqcx", "fastqc_data.txt") ||
		firstArtifactTask.Inputs["fastqc_after_r2"][0] != filepath.Join("workflow", "fastqc_clean", "sample-a_val_2_fastqcx", "fastqc_data.txt") {
		t.Fatalf("unexpected Fastqcx artifact inputs: %#v", firstArtifactTask.Inputs)
	}

	prepareReferenceTask := plan.TaskByID["BeaverBS/step3-check/prepare_methrix_reference"]
	if prepareReferenceTask == nil {
		t.Fatal("missing Methrix reference preparation task")
	}
	expectedReference := filepath.Join(repositoryRoot, "testdata", "configs", "references", "human.fasta")
	if prepareReferenceTask.Inputs["genome"][0] != expectedReference {
		t.Fatalf("unexpected canonical graft reference: %#v", prepareReferenceTask.Inputs["genome"])
	}
	if !strings.Contains(prepareReferenceTask.Steps[0].Command, "--contigs") || !strings.Contains(prepareReferenceTask.Steps[0].Command, `contig_arguments+=(--contigs "$contig")`) || !strings.Contains(prepareReferenceTask.Steps[0].Command, "methx_extract_command") {
		t.Fatalf("Methrix reference preparation lacks repeated nonstandard contig flags: %s", prepareReferenceTask.Steps[0].Command)
	}
	if !strings.Contains(prepareReferenceTask.Steps[0].Command, "METHX:-methx") {
		t.Fatalf("Methrix reference preparation should support an explicit executable override: %s", prepareReferenceTask.Steps[0].Command)
	}

	methrixTask := plan.TaskByID["BeaverBS/step3-check/create_methrix_object"]
	if methrixTask == nil || len(methrixTask.Inputs["sample_artifacts"]) != 2 || len(methrixTask.Dependencies) != 3 {
		t.Fatalf("Methrix task should depend on both sample artifacts and reference preparation: %#v", methrixTask)
	}
	if methrixTask.Outputs["methrix_data"] != filepath.Join("workflow", "mCall", "methrixh5", "methrix_data.h5") {
		t.Fatalf("unexpected Methrix HDF5 output %q", methrixTask.Outputs["methrix_data"])
	}
	if strings.Contains(methrixTask.Steps[0].Command, "--annotation-dir") {
		t.Fatalf("Methrix process command uses unsupported --annotation-dir: %s", methrixTask.Steps[0].Command)
	}
	if !strings.Contains(methrixTask.Steps[0].Command, "METHX:-methx") {
		t.Fatalf("Methrix process should support an explicit executable override: %s", methrixTask.Steps[0].Command)
	}

	bismarkSummaryTask := plan.TaskByID["BeaverBS/step3-check/bismark_summary"]
	if bismarkSummaryTask == nil || len(bismarkSummaryTask.Inputs["sample_reports"]) != 2 || len(bismarkSummaryTask.Dependencies) != 2 {
		t.Fatalf("Bismark summary should aggregate both sample reports: %#v", bismarkSummaryTask)
	}

	checkerTask := plan.TaskByID["BeaverBS/step3-check/step3_checker"]
	if checkerTask == nil {
		t.Fatal("missing step3 checker task")
	}
	if len(checkerTask.Inputs["sample_artifacts"]) != 2 || len(checkerTask.Dependencies) != 5 {
		t.Fatalf("unexpected checker aggregation: inputs=%#v dependencies=%#v", checkerTask.Inputs, checkerTask.Dependencies)
	}
	if checkerTask.Outputs["success_marker"] != filepath.Join("workflow", "log", "step3_success.txt") {
		t.Fatalf("unexpected step3 success marker %q", checkerTask.Outputs["success_marker"])
	}
}
