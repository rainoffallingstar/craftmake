package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBeaverRNAStep2CheckRunsLocallyAndUsesCache(t *testing.T) {
	repositoryRoot := resolveRepositoryRoot(t)
	temporaryDirectory := t.TempDir()
	binaryPath := filepath.Join(temporaryDirectory, "craftmake")
	buildCraftmakeBinary(t, repositoryRoot, binaryPath)

	toolDirectory := filepath.Join(temporaryDirectory, "bin")
	projectDirectory := filepath.Join(temporaryDirectory, "project")
	writeFakeBeaverRNAStep2CheckTools(t, toolDirectory)
	writeBeaverRNAStep2CheckProjectFixture(t, projectDirectory)

	workflowPath := filepath.Join(repositoryRoot, "workflows", "BeaverRNA", "step2-check.yaml")
	statePath := filepath.Join(projectDirectory, "workflow", ".craftmake", "state.sqlite")
	commandEnvironment := append(os.Environ(), "PATH="+toolDirectory+string(os.PathListSeparator)+os.Getenv("PATH"))
	firstRunOutput := runCraftmake(t, binaryPath, commandEnvironment,
		"run",
		"--workflow", workflowPath,
		"--config", filepath.Join(projectDirectory, "config.yaml"),
		"--project-dir", projectDirectory,
		"--backend", "local",
		"--max-parallel", "4",
		"--max-cores", "40",
		"--max-memory", "64G",
	)
	firstRunID := outputValue(t, firstRunOutput, "run_id")
	firstStatus := runCraftmake(t, binaryPath, commandEnvironment, "status", "--state", statePath, "--run", firstRunID)
	if !strings.Contains(firstStatus, "status: succeeded") || !strings.Contains(firstStatus, "succeeded: 6") {
		t.Fatalf("unexpected first BeaverRNA step2-check status:\n%s", firstStatus)
	}

	for _, expectedOutput := range []string{
		"workflow/log/step2-check/sample-a_human.ready",
		"workflow/log/step2-check/sample-b_human.ready",
		"workflow/expression/matrix/matrix_count.txt",
		"workflow/expression/matrix/matrix_norm.txt",
		"workflow/bsmap/RNASplicing/RNASplicing_success.txt",
		"workflow/QC/summary/qc_summary.xlsx",
		"workflow/log/step2_success.txt",
	} {
		if _, err := os.Stat(filepath.Join(projectDirectory, expectedOutput)); err != nil {
			t.Fatalf("expected BeaverRNA step2-check output %q: %v", expectedOutput, err)
		}
	}
	splicingMarker, err := os.ReadFile(filepath.Join(projectDirectory, "workflow", "bsmap", "RNASplicing", "RNASplicing_success.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(splicingMarker)) != "RNASplicing_DONE" {
		t.Fatalf("unexpected RNA splicing marker %q", splicingMarker)
	}

	secondRunOutput := runCraftmake(t, binaryPath, commandEnvironment,
		"run",
		"--workflow", workflowPath,
		"--config", filepath.Join(projectDirectory, "config.yaml"),
		"--project-dir", projectDirectory,
		"--backend", "local",
		"--max-parallel", "4",
		"--max-cores", "40",
		"--max-memory", "64G",
	)
	secondRunID := outputValue(t, secondRunOutput, "run_id")
	secondStatus := runCraftmake(t, binaryPath, commandEnvironment, "status", "--state", statePath, "--run", secondRunID)
	if !strings.Contains(secondStatus, "status: succeeded") || !strings.Contains(secondStatus, "cached: 6") {
		t.Fatalf("unexpected cached BeaverRNA step2-check status:\n%s", secondStatus)
	}
}

func writeFakeBeaverRNAStep2CheckTools(t *testing.T, toolDirectory string) {
	t.Helper()
	writeFakeBeaverBSStep3CheckTools(t, toolDirectory)
	writeExecutable(t, filepath.Join(toolDirectory, "seq2mat"), `#!/usr/bin/env bash
set -euo pipefail
output_directory=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --htseq_dir|--postfix) shift 2 ;;
    --output_dir) output_directory="$2"; shift 2 ;;
    *) shift ;;
  esac
done
mkdir -p "$output_directory"
printf 'count matrix\n' > "$output_directory/matrix_count.txt"
printf 'normalized matrix\n' > "$output_directory/matrix_norm.txt"
`)
	writeExecutable(t, filepath.Join(toolDirectory, "matsrun"), `#!/usr/bin/env bash
set -euo pipefail
if [ "$1" != "run" ]; then exit 2; fi
shift
while [ "$#" -gt 0 ]; do
  case "$1" in
    --root|--threads|--pdata|--seqlengthQC|--gtf|--pdxmode) shift 2 ;;
    *) shift ;;
  esac
done
`)
}

func writeBeaverRNAStep2CheckProjectFixture(t *testing.T, projectDirectory string) {
	t.Helper()
	inputPaths := []string{
		"config/config.yaml",
		"config/pdata.xlsx",
		"references/human.gtf",
	}
	for _, sampleID := range []string{"sample-a", "sample-b"} {
		inputPaths = append(inputPaths,
			filepath.Join("workflow", "expression", sampleID+"_human.txt"),
			filepath.Join("workflow", "bsmap", sampleID+"_human.bam"),
			filepath.Join("workflow", "QC", "qualimap", sampleID+"_human", "qualimapReport.html"),
			filepath.Join("workflow", "trim", sampleID+"_R1.fastq.gz_trimming_report.txt"),
			filepath.Join("workflow", "trim", sampleID+"_R2.fastq.gz_trimming_report.txt"),
			filepath.Join("workflow", "fastqc_raw", sampleID+"_R1_fastqcx", "fastqc_data.txt"),
			filepath.Join("workflow", "fastqc_raw", sampleID+"_R2_fastqcx", "fastqc_data.txt"),
			filepath.Join("workflow", "fastqc_clean", sampleID+"_val_1_fastqcx", "fastqc_data.txt"),
			filepath.Join("workflow", "fastqc_clean", sampleID+"_val_2_fastqcx", "fastqc_data.txt"),
		)
	}
	for _, relativePath := range inputPaths {
		fixturePath := filepath.Join(projectDirectory, relativePath)
		if err := os.MkdirAll(filepath.Dir(fixturePath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fixturePath, []byte("fixture\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	configuration := `SIDs: [sample-a, sample-b]
mode: RNASEQ
species: human
output:
  trim_dir: workflow/trim
  workflow_dir: workflow
  analysis_dir: analysis
  log_dir: workflow/log
directories:
  work: workflow
  selfconfig: config
  bsmap:
    main: workflow/bsmap
  methylation_call: workflow/expression
  beta_matrix: workflow/expression/matrix
  qualimap: workflow/QC/qualimap
  qc:
    main: workflow/QC
    before: workflow/fastqc_raw
    after: workflow/fastqc_clean
  qc_summary: workflow/QC/summary
  sid_log: workflow/log
workflow:
  mode: RNASEQ
  jobid: beaverrna-step2-check-integration
  userid: integration
  species:
    name: [human]
    primary: human
    graft: human
metadata:
  sample_ids: [sample-a, sample-b]
  pdx_pipeline: "false"
  group_levels: 2
reference:
  files:
    fasta: [references/human.fasta]
  indices:
    genome: [references/star-human]
  rnaseq:
    gtf: [references/human.gtf]
    ref: [references/star-human]
`
	if err := os.WriteFile(filepath.Join(projectDirectory, "config.yaml"), []byte(configuration), 0o644); err != nil {
		t.Fatal(err)
	}
}
