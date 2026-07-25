package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBeaverRNASEQPDXStep2CheckRunsLocallyAndUsesCache(t *testing.T) {
	repositoryRoot := resolveRepositoryRoot(t)
	temporaryDirectory := t.TempDir()
	binaryPath := filepath.Join(temporaryDirectory, "craftmake")
	buildCraftmakeBinary(t, repositoryRoot, binaryPath)

	toolDirectory := filepath.Join(temporaryDirectory, "bin")
	projectDirectory := filepath.Join(temporaryDirectory, "project")
	writeFakeBeaverRNASEQPDXStep2CheckTools(t, toolDirectory)
	writeBeaverRNASEQPDXStep2CheckProjectFixture(t, projectDirectory)

	workflowPath := filepath.Join(repositoryRoot, "workflows", "BeaverRNASEQPDX", "step2-check.yaml")
	statePath := filepath.Join(projectDirectory, "workflow", ".craftmake", "state.sqlite")
	commandEnvironment := append(
		os.Environ(),
		"PATH="+toolDirectory+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SAMTOOLS="+filepath.Join(toolDirectory, "samtools"),
	)
	firstRunOutput := runCraftmake(t, binaryPath, commandEnvironment,
		"run",
		"--workflow", workflowPath,
		"--config", filepath.Join(projectDirectory, "config.yaml"),
		"--project-dir", projectDirectory,
		"--backend", "local",
		"--max-parallel", "8",
		"--max-cores", "16",
		"--max-memory", "64G",
	)
	firstRunID := outputValue(t, firstRunOutput, "run_id")
	firstStatus := runCraftmake(t, binaryPath, commandEnvironment, "status", "--state", statePath, "--run", firstRunID)
	if !strings.Contains(firstStatus, "status: succeeded") || !strings.Contains(firstStatus, "succeeded: 12") {
		t.Fatalf("unexpected first BeaverRNASEQPDX step2-check status:\n%s", firstStatus)
	}

	expectedOutputs := []string{
		"workflow/bsmap/Filtered_bams/filtered_success.txt",
		"workflow/log/step2_success.txt",
	}
	for _, sampleID := range []string{"sample-a", "sample-b"} {
		expectedOutputs = append(expectedOutputs,
			filepath.Join("workflow", "bsmap", "Filtered_bams", sampleID+"_fixed_human_Filtered.bam"),
			filepath.Join("workflow", "log", "step2-check", sampleID+"_filtered.ready"),
		)
		for _, speciesName := range []string{"human", "mouse"} {
			expectedOutputs = append(expectedOutputs,
				filepath.Join("workflow", "log", "step2-check", sampleID+"_"+speciesName+".ready"),
				filepath.Join("workflow", "bsmap", sampleID+"_fixed_"+speciesName+".bam"),
				filepath.Join("workflow", "bsmap", sampleID+"_"+speciesName+"_pdx_patch_success"),
			)
		}
	}
	for _, expectedOutput := range expectedOutputs {
		if _, err := os.Stat(filepath.Join(projectDirectory, expectedOutput)); err != nil {
			t.Fatalf("expected BeaverRNASEQPDX step2-check output %q: %v", expectedOutput, err)
		}
	}

	secondRunOutput := runCraftmake(t, binaryPath, commandEnvironment,
		"run",
		"--workflow", workflowPath,
		"--config", filepath.Join(projectDirectory, "config.yaml"),
		"--project-dir", projectDirectory,
		"--backend", "local",
		"--max-parallel", "8",
		"--max-cores", "16",
		"--max-memory", "64G",
	)
	secondRunID := outputValue(t, secondRunOutput, "run_id")
	secondStatus := runCraftmake(t, binaryPath, commandEnvironment, "status", "--state", statePath, "--run", secondRunID)
	if !strings.Contains(secondStatus, "status: succeeded") || !strings.Contains(secondStatus, "cached: 12") {
		t.Fatalf("unexpected cached BeaverRNASEQPDX step2-check status:\n%s", secondStatus)
	}
}

func writeFakeBeaverRNASEQPDXStep2CheckTools(t *testing.T, toolDirectory string) {
	t.Helper()
	if err := os.MkdirAll(toolDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(toolDirectory, "enva"), `#!/usr/bin/env bash
set -euo pipefail
if [ "${1:-}" = "--quiet" ]; then shift; fi
if [ "${1:-}" != "run" ]; then exit 2; fi
shift 2
if [ "${1:-}" = "--" ]; then shift; fi
exec "$@"
`)
	writeExecutable(t, filepath.Join(toolDirectory, "picard"), `#!/usr/bin/env bash
set -euo pipefail
if [ "${1:-}" != "SetNmMdAndUqTags" ]; then exit 2; fi
shift
input_path=""
output_path=""
reference_path=""
bisulfite_mode=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    I=*) input_path="${1#I=}"; shift ;;
    O=*) output_path="${1#O=}"; shift ;;
    R=*) reference_path="${1#R=}"; shift ;;
    IS_BISULFITE_SEQUENCE=*) bisulfite_mode="${1#IS_BISULFITE_SEQUENCE=}"; shift ;;
    *) shift ;;
  esac
done
if [ ! -f "$input_path" ] || [ ! -f "$reference_path" ] || [ "$bisulfite_mode" != "false" ]; then exit 3; fi
mkdir -p "$(dirname "$output_path")"
printf 'RNA fixed from %s with %s\n' "$input_path" "$reference_path" > "$output_path"
`)
	writeExecutable(t, filepath.Join(toolDirectory, "xenofilter"), `#!/usr/bin/env bash
set -euo pipefail
if [ "${1:-}" != "run" ]; then exit 2; fi
shift
graft_bams=()
host_bams=()
output_directory=""
graft_reference=""
host_reference=""
mm_threshold=""
bisulfite_enabled="false"
while [ "$#" -gt 0 ]; do
  case "$1" in
    --graft)
      shift
      while [ "$#" -gt 0 ] && [[ "$1" != --* ]]; do graft_bams+=("$1"); shift; done
      ;;
    --host)
      shift
      while [ "$#" -gt 0 ] && [[ "$1" != --* ]]; do host_bams+=("$1"); shift; done
      ;;
    --output) output_directory="$2"; shift 2 ;;
    --graft-ref) graft_reference="$2"; shift 2 ;;
    --host-ref) host_reference="$2"; shift 2 ;;
    --mm-threshold) mm_threshold="$2"; shift 2 ;;
    --unmapped-penalty|--threads) shift 2 ;;
    --recalculate-nm) shift ;;
    --bisulfite) bisulfite_enabled="true"; shift ;;
    *) shift ;;
  esac
done
if [ "${#graft_bams[@]}" -ne "${#host_bams[@]}" ] || [ "${#graft_bams[@]}" -eq 0 ]; then exit 3; fi
if [ ! -f "$graft_reference" ] || [ ! -f "$host_reference" ] || [ "$mm_threshold" != "4" ] || [ "$bisulfite_enabled" != "false" ]; then exit 4; fi
mkdir -p "$output_directory"
for graft_bam in "${graft_bams[@]}"; do
  output_name="$(basename "$graft_bam" .bam)_Filtered.bam"
  printf 'RNA filtered from %s\n' "$graft_bam" > "$output_directory/$output_name"
  printf 'RNA index for %s\n' "$output_name" > "$output_directory/$output_name.bai"
done
`)
	writeExecutable(t, filepath.Join(toolDirectory, "samtools"), `#!/usr/bin/env bash
set -euo pipefail
case "${1:-}" in
  quickcheck)
    shift
    while [ "$#" -gt 0 ]; do
      case "$1" in
        -*) shift ;;
        *) test -s "$1"; shift ;;
      esac
    done
    ;;
  view)
    printf '1\n'
    ;;
  *)
    exit 2
    ;;
esac
`)
}

func writeBeaverRNASEQPDXStep2CheckProjectFixture(t *testing.T, projectDirectory string) {
	t.Helper()
	inputPaths := []string{
		"references/human.fasta",
		"references/mouse.fasta",
		"references/human.gtf",
		"references/mouse.gtf",
		"references/star-human/Genome",
		"references/star-mouse/Genome",
	}
	for _, sampleID := range []string{"sample-a", "sample-b"} {
		for _, speciesName := range []string{"human", "mouse"} {
			inputPaths = append(inputPaths,
				filepath.Join("workflow", "bsmap", sampleID+"_"+speciesName+".bam"),
				filepath.Join("workflow", "bsmap", sampleID+"_"+speciesName+".bam.bai"),
				filepath.Join("workflow", "QC", "qualimap", sampleID+"_"+speciesName, "qualimapReport.html"),
				filepath.Join("workflow", "QC", "qualimap", sampleID+"_"+speciesName, "report.pdf"),
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
  raw_dir: data
  trim_dir: workflow/trim
  workflow_dir: workflow
  analysis_dir: analysis
  log_dir: workflow/log
directories:
  work: workflow
  bsmap:
    main: workflow/bsmap
  qualimap: workflow/QC/qualimap
  qc:
    main: workflow/QC
  sid_log: workflow/log
workflow:
  mode: RNASEQ
  jobid: beaverrnaseqpdx-step2-check-integration
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
reference:
  files:
    fasta: [references/human.fasta, references/mouse.fasta]
  indices:
    genome: [references/star-human, references/star-mouse]
  rnaseq:
    gtf: [references/human.gtf, references/mouse.gtf]
    ref: [references/star-human, references/star-mouse]
`
	if err := os.WriteFile(filepath.Join(projectDirectory, "config.yaml"), []byte(configuration), 0o644); err != nil {
		t.Fatal(err)
	}
}
