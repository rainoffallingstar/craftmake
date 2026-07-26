package otter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRunV1BuildsCompilerContext(t *testing.T) {
	configurationPath := writeRunSnapshot(t, runSnapshotYAML("rna-pdx", "craftmake", "slurm", "true", true))
	context, err := Load(configurationPath)
	if err != nil {
		t.Fatal(err)
	}
	if context.Workflow.WorkflowName != "BeaverRNASEQPDX" || context.Workflow.Mode != "RNASEQ" || !context.Workflow.PDXMode {
		t.Fatalf("unexpected workflow context: %#v", context.Workflow)
	}
	if len(context.Samples) != 1 || context.Samples[0].Read1 != "/project/data/S01_R1.fastq.gz" || context.Samples[0].Adapter1 != "AUTO" {
		t.Fatalf("unexpected sample context: %#v", context.Samples)
	}
	if len(context.Species) != 2 || context.Species[0].Name != "hg38" || context.Species[1].Name != "mm39" {
		t.Fatalf("unexpected species context: %#v", context.Species)
	}
	output := context.Raw["output"].(map[string]any)
	if output["raw_dir"] != "/project/data" || output["trim_dir"] != "/project/runs/example/work/trim" || output["analysis_dir"] != "/project/runs/example/results" {
		t.Fatalf("unexpected derived output paths: %#v", output)
	}
	directories := context.Raw["directories"].(map[string]any)
	if directories["sid_log"] != "/project/runs/example/logs" || directories["methylation_call"] != "/project/runs/example/work/expression" {
		t.Fatalf("unexpected derived directories: %#v", directories)
	}
	reference := context.Raw["reference"].(map[string]any)
	if reference["graft_fasta"] != "/refs/hg38.fa" || reference["host_fasta"] != "/refs/mm39.fa" {
		t.Fatalf("unexpected PDX references: %#v", reference)
	}
	rnaseq := reference["rnaseq"].(map[string]any)
	if rnaseq["graft_gtf"] != "/refs/hg38.gtf" || rnaseq["primary_reference"] != "/refs/hg38-star" {
		t.Fatalf("unexpected RNA references: %#v", rnaseq)
	}
}

func TestLoadRunV1MapsCatalogScenarios(t *testing.T) {
	testCases := []struct {
		scenario string
		workflow string
	}{
		{scenario: "rrbs", workflow: "BeaverBS"},
		{scenario: "wgbs", workflow: "BeaverBS"},
		{scenario: "rnaseq", workflow: "BeaverRNA"},
		{scenario: "bs-pdx", workflow: "BeaverPDX"},
		{scenario: "rna-pdx", workflow: "BeaverRNASEQPDX"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.scenario, func(t *testing.T) {
			pdx := strings.Contains(testCase.scenario, "pdx")
			context, err := Load(writeRunSnapshot(t, runSnapshotYAML(testCase.scenario, "craftmake", "local", "true", pdx)))
			if err != nil {
				t.Fatal(err)
			}
			if context.Workflow.WorkflowName != testCase.workflow {
				t.Fatalf("scenario %q resolved to %q", testCase.scenario, context.Workflow.WorkflowName)
			}
		})
	}
}

func TestLoadRunV1RejectsInvalidContracts(t *testing.T) {
	base := runSnapshotYAML("rrbs", "craftmake", "local", "true", false)
	testCases := []struct {
		name          string
		configuration string
		want          string
	}{
		{name: "schema", configuration: strings.Replace(base, "otter.run/v1", "otter.project/v1", 1), want: "schema_version"},
		{name: "run id", configuration: strings.Replace(base, "id: run-20260726T000000Z-abcdef", "id: ''", 1), want: "run.id"},
		{name: "mutable", configuration: strings.Replace(base, "immutable: true", "immutable: false", 1), want: "immutable"},
		{name: "executor", configuration: strings.Replace(base, "value: craftmake", "value: snakemake", 1), want: "executor"},
		{name: "auto backend", configuration: strings.Replace(base, "value: local", "value: auto", 1), want: "backend"},
		{name: "samples", configuration: strings.Replace(base, "samples:\n  - id: S01\n    r1: /project/data/S01_R1.fastq.gz\n    r2: /project/data/S01_R2.fastq.gz\n    adapter_r1: AUTO\n    adapter_r2: AUTO", "samples: []", 1), want: "samples"},
		{name: "unknown field", configuration: base + "unexpected: true\n", want: "field unexpected not found"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := Load(writeRunSnapshot(t, testCase.configuration))
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("expected error containing %q, got %v", testCase.want, err)
			}
		})
	}
}

func writeRunSnapshot(t *testing.T, configuration string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "run.yaml")
	if err := os.WriteFile(path, []byte(configuration), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func runSnapshotYAML(scenario, executor, backend, immutable string, includeHost bool) string {
	hostReference := ""
	if includeHost {
		hostReference = `
    - role: host
      id: mm39
      release: GRCm39
      registry_root: /refs
      manifest_digest: sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
      fasta:
        type: fasta
        path: /refs/mm39.fa
        sha256: sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
      annotations:
        - type: gtf
          path: /refs/mm39.gtf
          sha256: sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd
      indexes:
        - type: star
          path: /refs/mm39-star
          sha256: sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee
`
	}
	primaryRole := "primary"
	if includeHost {
		primaryRole = "graft"
	}
	return fmt.Sprintf(`schema_version: otter.run/v1
run:
  id: run-20260726T000000Z-abcdef
  created_at: "2026-07-26T00:00:00Z"
  immutable: %s
project:
  id: project-a
  root: /project
workflow:
  scenario: %s
  toolchain: modern
  asset_root: /project/workflows
execution:
  executor:
    value: %s
    source: project
  backend:
    value: %s
    source: project
    evidence: {}
  site:
    value: local
    source: project
  resources: {}
samples:
  - id: S01
    r1: /project/data/S01_R1.fastq.gz
    r2: /project/data/S01_R2.fastq.gz
    adapter_r1: AUTO
    adapter_r2: AUTO
references:
  project_selection: {}
  effective_selection: {}
  override: false
  override_source: none
  resolved:
    - role: %s
      id: hg38
      release: GRCh38
      registry_root: /refs
      manifest_digest: sha256:1111111111111111111111111111111111111111111111111111111111111111
      fasta:
        type: fasta
        path: /refs/hg38.fa
        sha256: sha256:2222222222222222222222222222222222222222222222222222222222222222
      annotations:
        - type: gtf
          path: /refs/hg38.gtf
          sha256: sha256:3333333333333333333333333333333333333333333333333333333333333333
      indexes:
        - type: bismark
          path: /refs/hg38-bismark
          sha256: sha256:4444444444444444444444444444444444444444444444444444444444444444
        - type: star
          path: /refs/hg38-star
          sha256: sha256:5555555555555555555555555555555555555555555555555555555555555555
%s
paths:
  run_root: /project/runs/example
  work: /project/runs/example/work
  results: /project/runs/example/results
  logs: /project/runs/example/logs
  state: /project/runs/example/state
  metrics: /project/runs/example/metrics
digests:
  project: sha256:6666666666666666666666666666666666666666666666666666666666666666
  samples: sha256:7777777777777777777777777777777777777777777777777777777777777777
  workflow_assets: sha256:8888888888888888888888888888888888888888888888888888888888888888
observability: {}
parity: {}
`, immutable, scenario, executor, backend, primaryRole, hostReference)
}
