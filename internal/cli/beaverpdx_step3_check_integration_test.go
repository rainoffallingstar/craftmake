package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBeaverPDXStep3CheckRunsLocallyAndUsesCache(t *testing.T) {
	repositoryRoot := resolveRepositoryRoot(t)
	temporaryDirectory := t.TempDir()
	binaryPath := filepath.Join(temporaryDirectory, "craftmake")
	buildCraftmakeBinary(t, repositoryRoot, binaryPath)

	toolDirectory := filepath.Join(temporaryDirectory, "bin")
	projectDirectory := filepath.Join(temporaryDirectory, "project")
	writeFakeBeaverBSStep3CheckTools(t, toolDirectory)
	writeBeaverPDXStep3CheckProjectFixture(t, projectDirectory)

	workflowPath := filepath.Join(repositoryRoot, "workflows", "BeaverPDX", "step3-check.yaml")
	statePath := filepath.Join(projectDirectory, "workflow", ".craftmake", "state.sqlite")
	commandEnvironment := append(
		os.Environ(),
		"PATH="+toolDirectory+string(os.PathListSeparator)+os.Getenv("PATH"),
		"METHX="+filepath.Join(toolDirectory, "methx-override"),
	)
	firstRunOutput := runCraftmake(t, binaryPath, commandEnvironment,
		"run",
		"--workflow", workflowPath,
		"--legacy-config", "--config", filepath.Join(projectDirectory, "config.yaml"),
		"--project-dir", projectDirectory,
		"--backend", "local",
		"--max-parallel", "6",
		"--max-cores", "20",
		"--max-memory", "64G",
	)
	firstRunID := outputValue(t, firstRunOutput, "run_id")
	firstStatus := runCraftmake(t, binaryPath, commandEnvironment, "status", "--state", statePath, "--run", firstRunID)
	if !strings.Contains(firstStatus, "status: succeeded") || !strings.Contains(firstStatus, "succeeded: 13") {
		t.Fatalf("unexpected first BeaverPDX step3-check status:\n%s", firstStatus)
	}

	expectedOutputs := []string{
		"workflow/mCall/methrixh5/reference_cpgs.ron",
		"workflow/mCall/methrixh5/methrix_data.h5",
		"workflow/mCall/methrixh5/CpG_coverage.xlsx",
		"workflow/bsmap/human/bismark_summary_report.html",
		"workflow/QC/summary/qc_summary.xlsx",
		"workflow/log/step3_success.txt",
	}
	for _, sampleID := range []string{"sample-a", "sample-b"} {
		expectedOutputs = append(expectedOutputs,
			filepath.Join("workflow", "log", "step3-check", sampleID+".ready"),
			filepath.Join("workflow", "bsmap", "human", sampleID+".html"),
		)
		for _, speciesName := range []string{"human", "mouse"} {
			expectedOutputs = append(expectedOutputs,
				filepath.Join("workflow", "log", "step3-check", sampleID+"_"+speciesName+"_qc.ready"),
			)
		}
	}
	for _, expectedOutput := range expectedOutputs {
		if _, err := os.Stat(filepath.Join(projectDirectory, expectedOutput)); err != nil {
			t.Fatalf("expected BeaverPDX step3-check output %q: %v", expectedOutput, err)
		}
	}

	secondRunOutput := runCraftmake(t, binaryPath, commandEnvironment,
		"run",
		"--workflow", workflowPath,
		"--legacy-config", "--config", filepath.Join(projectDirectory, "config.yaml"),
		"--project-dir", projectDirectory,
		"--backend", "local",
		"--max-parallel", "6",
		"--max-cores", "20",
		"--max-memory", "64G",
	)
	secondRunID := outputValue(t, secondRunOutput, "run_id")
	secondStatus := runCraftmake(t, binaryPath, commandEnvironment, "status", "--state", statePath, "--run", secondRunID)
	if !strings.Contains(secondStatus, "status: succeeded") || !strings.Contains(secondStatus, "cached: 13") {
		t.Fatalf("unexpected cached BeaverPDX step3-check status:\n%s", secondStatus)
	}
}

func writeBeaverPDXStep3CheckProjectFixture(t *testing.T, projectDirectory string) {
	t.Helper()
	inputPaths := []string{
		"references/human.fasta",
		"references/mouse.fasta",
		"references/bismark-human",
		"references/bismark-mouse",
	}
	for _, sampleID := range []string{"sample-a", "sample-b"} {
		inputPaths = append(inputPaths,
			filepath.Join("workflow", "mCall", sampleID+"_nsort.bismark.cov.gz"),
			filepath.Join("workflow", "mCall", sampleID+"_nsort_splitting_report.txt"),
			filepath.Join("workflow", "mCall", sampleID+"_nsort.M-bias.txt"),
			filepath.Join("workflow", "bsmap", sampleID+"_nsort.bam"),
			filepath.Join("workflow", "bsmap", "Filtered_bams", sampleID+"_fixed_human_Filtered.bam"),
			filepath.Join("workflow", "trim", sampleID+"_R1.fastq.gz_trimming_report.txt"),
			filepath.Join("workflow", "trim", sampleID+"_R2.fastq.gz_trimming_report.txt"),
			filepath.Join("workflow", "fastqc_raw", sampleID+"_R1_fastqcx", "fastqc_data.txt"),
			filepath.Join("workflow", "fastqc_raw", sampleID+"_R2_fastqcx", "fastqc_data.txt"),
			filepath.Join("workflow", "fastqc_clean", sampleID+"_val_1_fastqcx", "fastqc_data.txt"),
			filepath.Join("workflow", "fastqc_clean", sampleID+"_val_2_fastqcx", "fastqc_data.txt"),
			filepath.Join("workflow", "bsmap", "human", sampleID+"_val_1_bismark_bt2_pe.bam"),
			filepath.Join("workflow", "bsmap", "human", sampleID+"_val_1_bismark_bt2_PE_report.txt"),
			filepath.Join("workflow", "bsmap", "human", sampleID+"_val_1_bismark_bt2_pe.nucleotide_stats.txt"),
		)
		for _, speciesName := range []string{"human", "mouse"} {
			inputPaths = append(inputPaths,
				filepath.Join("workflow", "bsmap", sampleID+"_"+speciesName+".bam"),
				filepath.Join("workflow", "QC", "qualimap", sampleID+"_"+speciesName, "qualimapReport.html"),
			)
		}
	}
	for _, relativePath := range inputPaths {
		path := filepath.Join(projectDirectory, relativePath)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("fixture\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	configuration := `SIDs: [sample-a, sample-b]
mode: RRBS
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
  methylation_call: workflow/mCall
  qualimap: workflow/QC/qualimap
  qc:
    main: workflow/QC
    before: workflow/fastqc_raw
    after: workflow/fastqc_clean
  qc_summary: workflow/QC/summary
  sid_log: workflow/log
workflow:
  mode: RRBS
  jobid: beaverpdx-step3-check-integration
  userid: integration
  species:
    name: [human, mouse]
    primary: human
    secondary: mouse
    graft: human
    host: mouse
metadata:
  sample_ids: [sample-a, sample-b]
reference:
  files:
    fasta: [references/human.fasta, references/mouse.fasta]
  indices:
    genome: [references/bismark-human, references/bismark-mouse]
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
