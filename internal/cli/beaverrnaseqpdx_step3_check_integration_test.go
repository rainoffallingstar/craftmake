package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBeaverRNASEQPDXStep3CheckRunsLocallyAndUsesCache(t *testing.T) {
	repositoryRoot := resolveRepositoryRoot(t)
	temporaryDirectory := t.TempDir()
	binaryPath := filepath.Join(temporaryDirectory, "craftmake")
	buildCraftmakeBinary(t, repositoryRoot, binaryPath)

	toolDirectory := filepath.Join(temporaryDirectory, "bin")
	projectDirectory := filepath.Join(temporaryDirectory, "project")
	writeFakeBeaverRNASEQPDXStep3CheckTools(t, toolDirectory)
	writeBeaverRNASEQPDXStep3CheckProjectFixture(t, projectDirectory)

	workflowPath := filepath.Join(repositoryRoot, "workflows", "BeaverRNASEQPDX", "step3-check.yaml")
	statePath := filepath.Join(projectDirectory, "workflow", ".craftmake", "state.sqlite")
	commandEnvironment := append(os.Environ(), "PATH="+toolDirectory+string(os.PathListSeparator)+os.Getenv("PATH"))
	firstRunOutput := runCraftmake(t, binaryPath, commandEnvironment,
		"run",
		"--workflow", workflowPath,
		"--config", filepath.Join(projectDirectory, "config.yaml"),
		"--project-dir", projectDirectory,
		"--backend", "local",
		"--max-parallel", "6",
		"--max-cores", "20",
		"--max-memory", "64G",
	)
	firstRunID := outputValue(t, firstRunOutput, "run_id")
	firstStatus := runCraftmake(t, binaryPath, commandEnvironment, "status", "--state", statePath, "--run", firstRunID)
	if !strings.Contains(firstStatus, "status: succeeded") || !strings.Contains(firstStatus, "succeeded: 9") {
		t.Fatalf("unexpected first BeaverRNASEQPDX step3-check status:\n%s", firstStatus)
	}

	expectedOutputs := []string{
		"workflow/expression/matrix/matrix_count.txt",
		"workflow/expression/matrix/matrix_norm.txt",
		"workflow/QC/summary/qc_summary.xlsx",
		"workflow/log/step3_success.txt",
	}
	for _, sampleID := range []string{"sample-a", "sample-b"} {
		expectedOutputs = append(expectedOutputs, filepath.Join("workflow", "log", "step3-check", sampleID+".ready"))
		for _, speciesName := range []string{"human", "mouse"} {
			expectedOutputs = append(expectedOutputs, filepath.Join("workflow", "log", "step3-check", sampleID+"_"+speciesName+"_qc.ready"))
		}
	}
	for _, expectedOutput := range expectedOutputs {
		if _, err := os.Stat(filepath.Join(projectDirectory, expectedOutput)); err != nil {
			t.Fatalf("expected BeaverRNASEQPDX step3-check output %q: %v", expectedOutput, err)
		}
	}

	secondRunOutput := runCraftmake(t, binaryPath, commandEnvironment,
		"run",
		"--workflow", workflowPath,
		"--config", filepath.Join(projectDirectory, "config.yaml"),
		"--project-dir", projectDirectory,
		"--backend", "local",
		"--max-parallel", "6",
		"--max-cores", "20",
		"--max-memory", "64G",
	)
	secondRunID := outputValue(t, secondRunOutput, "run_id")
	secondStatus := runCraftmake(t, binaryPath, commandEnvironment, "status", "--state", statePath, "--run", secondRunID)
	if !strings.Contains(secondStatus, "status: succeeded") || !strings.Contains(secondStatus, "cached: 9") {
		t.Fatalf("unexpected cached BeaverRNASEQPDX step3-check status:\n%s", secondStatus)
	}
}

func writeFakeBeaverRNASEQPDXStep3CheckTools(t *testing.T, toolDirectory string) {
	t.Helper()
	writeFakeBeaverBSStep3CheckTools(t, toolDirectory)
	writeExecutable(t, filepath.Join(toolDirectory, "htseq2matrix"), `#!/usr/bin/env bash
set -euo pipefail
input_directory=""
output_directory=""
postfix=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --htseq_dir) input_directory="$2"; shift 2 ;;
    --output_dir) output_directory="$2"; shift 2 ;;
    --postfix) postfix="$2"; shift 2 ;;
    *) shift ;;
  esac
done
if [ "$postfix" != "_human.txt" ] || [ ! -f "$input_directory/sample-a$postfix" ] || [ ! -f "$input_directory/sample-b$postfix" ]; then exit 3; fi
mkdir -p "$output_directory"
printf 'count matrix\n' > "$output_directory/matrix_count.txt"
printf 'normalized matrix\n' > "$output_directory/matrix_norm.txt"
`)
}

func writeBeaverRNASEQPDXStep3CheckProjectFixture(t *testing.T, projectDirectory string) {
	t.Helper()
	inputPaths := []string{
		"workflow/bsmap/RNASplicing/RNASplicing_success.txt",
		"references/human.fasta",
		"references/mouse.fasta",
		"references/human.gtf",
		"references/mouse.gtf",
		"references/star-human/Genome",
		"references/star-mouse/Genome",
	}
	for _, sampleID := range []string{"sample-a", "sample-b"} {
		inputPaths = append(inputPaths,
			filepath.Join("workflow", "expression", sampleID+"_human.txt"),
			filepath.Join("workflow", "bsmap", "Filtered_bams", sampleID+"_fixed_human_Filtered.bam"),
			filepath.Join("workflow", "trim", sampleID+"_R1.fastq.gz_trimming_report.txt"),
			filepath.Join("workflow", "trim", sampleID+"_R2.fastq.gz_trimming_report.txt"),
			filepath.Join("workflow", "fastqc_raw", sampleID+"_R1_fqc", "fastqc_data.txt"),
			filepath.Join("workflow", "fastqc_raw", sampleID+"_R2_fqc", "fastqc_data.txt"),
			filepath.Join("workflow", "fastqc_clean", sampleID+"_val_1_fqc", "fastqc_data.txt"),
			filepath.Join("workflow", "fastqc_clean", sampleID+"_val_2_fqc", "fastqc_data.txt"),
		)
		for _, speciesName := range []string{"human", "mouse"} {
			inputPaths = append(inputPaths,
				filepath.Join("workflow", "bsmap", sampleID+"_"+speciesName+".bam"),
				filepath.Join("workflow", "QC", "qualimap", sampleID+"_"+speciesName, "qualimapReport.html"),
			)
		}
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
species1: human
species2: mouse
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
  jobid: beaverrnaseqpdx-step3-check-integration
  userid: integration
  species:
    name: [human, mouse]
    primary: human
    secondary: mouse
    graft: human
    host: mouse
metadata:
  sample_ids: [sample-a, sample-b]
  pdx_pipeline: "true"
  group_levels: 2
reference:
  files:
    fasta: [references/human.fasta, references/mouse.fasta]
  indices:
    genome: [references/star-human, references/star-mouse]
  rnaseq:
    gtf: [references/human.gtf, references/mouse.gtf]
    ref: [references/star-human, references/star-mouse]
`
	for _, configPath := range []string{
		filepath.Join(projectDirectory, "config.yaml"),
		filepath.Join(projectDirectory, "config", "config.yaml"),
	} {
		if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(configPath, []byte(configuration), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
