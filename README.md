# craftmake

`craftmake` is a standalone native workflow runner for `xdxtools` pipelines. It compiles versioned workflow YAML into a task DAG, executes tasks locally or through Slurm, records run state in SQLite, and provides task-level caching, recovery, logs, and reports.

The first release targets these workflow families:

- BeaverBS
- BeaverPDX
- BeaverRNA
- BeaverRNASEQPDX

`craftmake` does not create `xdxtools` projects, scan FASTQ files, generate domain configuration, interpret Snakemake files, or replace the existing `xdxtools` command. It reads an existing `xdxtools` configuration and executes one workflow phase at a time.

## Current Validation Status

The core runner is approximately 97% complete and the first-release scope is approximately 96% complete. Core CLI, protocol, Local execution, caching, recovery, cancellation, Slurm submission slots, pending timeout, submit retry, accounting refresh, release archives, and workflow catalog routing are implemented and covered by the repository test suite.

Workflow acceptance currently stands at:

| Workflow family | YAML, compiler, and Local fixtures | Real tools and Slurm | Remaining acceptance |
|---|---|---|---|
| BeaverBS | Complete for all five phases | Complete for a synthetic cross-phase hg38-window project; step1 also has a small real-FASTQ run | Public human RRBS small-sample replay from `SRP547218`; no full-size run in the current gate |
| BeaverPDX | Complete for all five phases | Complete for a synthetic dual-species five-phase chain with real tools and Slurm, including selective invalidation and interrupted-Controller recovery | Public prostate PDX WGBS small-sample replay from `GSE227086`; no 30× full-run download in the current gate |
| BeaverRNA | Complete for all three phases | Not yet accepted end to end | Two-run mouse RNA-seq small sample from `SRP175361`: STAR, Qualimap, HTSeq, matrix, QCTB, cache, and recovery; statistical splicing is not run for one group |
| BeaverRNASEQPDX | Complete for all five phases | Not yet accepted end to end | Two-run PDAC PDX RNA-seq small sample from `GSE278757`, subject to SRA access preflight: dual-species STAR/Xenofilter, HTSeq, matrix, QCTB, cache, and recovery |

Current workflow validation is intentionally limited to deterministic small samples derived from public real sequencing runs. The registered sources are `SRP547218` for BeaverBS, `GSE227086 / PRJNA943199` for BeaverPDX, `SRP175361 / PRJNA513077` for BeaverRNA, and `GSE278757 / PRJNA1168601` for BeaverRNASEQPDX. The default contract is two runs per workflow and the first 1,000,000 paired reads per run, with accession, metadata, counts, and checksums retained. These runs validate tool and file contracts, orchestration, caching, and recovery; they do not establish production throughput, biological coverage, differential results, or statistical power. Production-scale and unified multi-omics acceptance are deferred until all four small-sample toolchains are complete.

The BeaverBS synthetic acceptance used real FastQC, Trim Galore, Bismark, samtools, Qualimap, Picard, MultiQC, `paireads`, Methrix 0.1.0, Bismark report/summary, and QCTB on Paracloud Slurm. It validates orchestration and real tool contracts, but it is not a substitute for public real-data or production-scale acceptance.

The Paracloud production-tool baseline is accepted on both the login node and fresh `amd_512` allocations. The recorded baseline includes Bismark 0.25.1, STAR 2.7.11b, samtools 1.15.1, Qualimap 2.3, Picard 3.4.0, HTSeq 2.0.3, MultiQC 1.19, Trim Galore 0.6.10, `paireads` and Xenofilter `daily-20260717`, QCTB 0.1.0, FQC 0.3.4, and Methrix 0.1.0. Real Picard and Methrix minimum-input jobs completed with `COMPLETED/0`.

The BeaverPDX synthetic dual-species chain now passes all five phases with real tools on Paracloud Slurm. Its human/mouse fixture produced non-empty Xenofilter graft BAMs, methylation outputs, a 5,239-CpG Methrix reference, valid HDF5/XLSX/HTML reports, and a QCTB summary for both samples. Final phase cache counts are `7`, `12`, `13`, `2`, and `13`, with zero new Slurm submissions; all recorded jobs settled at `COMPLETED/0` and left an empty queue. Gate 1 also passed isolated selective invalidation: after a 13-task cached baseline, changing only the QCTB XLSX metadata produced `cached: 11, succeeded: 2` and submitted only QCTB plus its checker. An independently isolated Controller-interruption run recovered the already-submitted QCTB job without resubmission, then created a continuation with `cached: 12, succeeded: 1` for only the pending checker; a final replay was `cached: 13`. This is synthetic orchestration evidence, not production-scale biological acceptance.

The detailed status, evidence, and next validation gates are maintained in [`doc/implementation-plan.md`](doc/implementation-plan.md).

## Known Production Constraints

- `craftmake` coordinates declared CPU and memory budgets but does not enforce cgroup or OS hard limits on child processes.
- Production Slurm acceptance must check both the `craftmake` terminal state and `sacct`; a job can briefly remain `COMPLETING` after its task result and accounting record are terminal.
- BeaverBS and BeaverPDX Methrix steps accept `METHRIX_CLI=/absolute/path/to/methrix`, defaulting to `methrix-cli`. On Paracloud, the global `~/.cargo/bin/methrix-cli` remains unusable because `libhdf5_serial.so.103` is absent; the accepted baseline sets `METHRIX_CLI=$HOME/methrix-cli/target/release/methrix`. That binary was verified in a fresh allocation with `LD_LIBRARY_PATH` unset and resolves HDF5 through its recorded `rust_build` RUNPATH. A clean-host release still needs a portable Methrix package or managed environment.
- Managed workflow steps discard inherited host Java variables. The bundled Picard steps then bind `JAVA_HOME` to `${CONDA_PREFIX}/lib/jvm` when that runtime exists; this avoids mixing a base Java installation with the active environment libraries. Custom workflows invoking Picard should preserve the same rule.
- QCTB is treated as an external managed tool and is not modified by this repository. For PDX workflows, the QC task writes a temporary compatibility copy of the project YAML in which `workflow.species.name` is the graft-species string expected by QCTB; the original Craftmake configuration remains unchanged.
- Methrix 0.1.0 produces `methrix_data.h5` and `CpG_coverage.xlsx`, does not accept `process --annotation-dir`, and filters nonstandard FASTA contigs by default. The bundled BeaverBS and BeaverPDX workflows implement those contracts and retry CpG extraction with FASTA header-derived `--contigs` when the default reference is empty.
- The current workflow gate uses only deterministic public real-data small samples. Full-run production throughput, unified multi-omics, and biological/statistical acceptance are explicitly deferred; small-sample success must not be reported as any of those outcomes.
- A first release still requires clean-Linux installation, old-database migration, and medium-load Slurm control-plane acceptance. Production-sized workflow acceptance remains a later, separately scoped gate.

## Requirements

For all commands:

- Go 1.26 or a prebuilt `craftmake` binary
- A workflow YAML file and an `xdxtools` configuration YAML file
- Input files, reference files, and output directories referenced by the configuration

For Local execution:

- POSIX shell utilities used by the workflow steps
- The workflow's configured tools, such as `fqc`, `trim_galore`, `samtools`, or `STAR`
- GNU `time` is optional; without it, the run continues with reduced resource metrics
- `enva` or conda is optional and only required by workflows that select those environments

For Slurm execution:

- `sbatch`, `srun`, `squeue`, `sacct`, and `scancel` on `PATH`
- A usable Slurm account and partition
- The workflow's configured tools available on compute nodes
- Slurm accounting configured well enough for delayed metric collection

Check the selected execution environment before running a workflow:

```bash
craftmake doctor --backend local
craftmake doctor --backend slurm
```

## Build And Install

Build from source:

```bash
make build
./build/craftmake --version
```

Install the binary and the bundled workflow catalog:

```bash
sudo make install
```

The default installation locations are:

- Binary: `/usr/local/bin/craftmake`
- Workflow catalog: `/usr/local/share/craftmake/workflows`

Use a different prefix without modifying the source tree:

```bash
make install PREFIX="$HOME/.local"
```

The executable searches for workflows in the installed catalog, `./workflows`, and the directory specified by `CRAFTMAKE_WORKFLOW_CATALOG`. Use `--catalog` when an explicit catalog root is required.

## Select A Workflow

A workflow can be selected explicitly:

```bash
craftmake validate \
  --workflow workflows/BeaverBS/step1.yaml \
  --config /path/to/config.yaml
```

Or it can be routed automatically from the `xdxtools` configuration and phase:

```bash
craftmake validate \
  --config /path/to/config.yaml \
  --phase step1 \
  --catalog /path/to/workflows
```

The same workflow and configuration flags are accepted by `plan`, `run`, and `validate`:

- `--workflow` or `-w`: explicit workflow YAML
- `--config` or `-c`: `xdxtools` configuration YAML
- `--phase`: phase used for catalog routing
- `--catalog`: workflow catalog root
- `--project-dir`: project root used to resolve relative paths
- `--state-dir`: directory containing the SQLite state database

## Validate And Plan

Validate configuration, workflow syntax, references, resources, inputs, and the compiled task count:

```bash
craftmake validate \
  --workflow workflows/BeaverBS/step1.yaml \
  --config /path/to/config.yaml
```

Inspect the compiled DAG before execution:

```bash
craftmake plan \
  --workflow workflows/BeaverBS/step1.yaml \
  --config /path/to/config.yaml
```

Machine-readable plans are available with `--format json`:

```bash
craftmake plan \
  --workflow workflows/BeaverBS/step1.yaml \
  --config /path/to/config.yaml \
  --format json > plan.json
```

Use `--dry-run` with `run` to compile and print the plan without creating a run:

```bash
craftmake run \
  --workflow workflows/BeaverBS/step1.yaml \
  --config /path/to/config.yaml \
  --dry-run
```

## Run Locally

A Local run uses `workflow/.craftmake/state.sqlite` below the project directory by default:

```bash
craftmake run \
  --workflow workflows/BeaverBS/step1.yaml \
  --config /path/to/config.yaml \
  --backend local \
  --workers 4 \
  --max-cores 16 \
  --max-memory 64G
```

`--workers` is the maximum number of active physical submissions. Local submissions additionally share the `--max-cores` and `--max-memory` admission budgets. A submission that is larger than either explicit budget fails immediately with a `submission.unschedulable` Controller event instead of remaining pending indefinitely. These budgets coordinate craftmake tasks; they are not OS or cgroup hard limits on child processes.

The command prints the run identifier, SQLite state path, and Controller JSONL path:

```text
run_id: ...
state: .../workflow/.craftmake/state.sqlite
controller_log: .../workflow/.craftmake/runs/.../controller.jsonl
```

Use `--project-dir` and `--state-dir` when the configuration directory and state directory should be separated:

```bash
craftmake run \
  --workflow /path/to/workflow.yaml \
  --config /path/to/config.yaml \
  --project-dir /path/to/project \
  --state-dir /path/to/project/workflow/.craftmake
```

Successful task fingerprints are reused automatically. Use `--force` to bypass the cache for a run:

```bash
craftmake run \
  --workflow /path/to/workflow.yaml \
  --config /path/to/config.yaml \
  --force
```

## Run On Slurm

First verify the controller host has the required Slurm commands:

```bash
craftmake doctor --backend slurm
```

Then select the Slurm backend and, when needed, a partition:

```bash
craftmake run \
  --workflow /path/to/workflow.yaml \
  --config /path/to/config.yaml \
  --backend slurm \
  --partition amd_512 \
  --workers 8
```

`--workers` limits the number of Slurm allocations that craftmake has submitted or is waiting on. A successful `sbatch` that remains `PENDING` still owns one slot; when it reaches a terminal state, the slot is released and the next ready submission is filled automatically. Each allocation can independently run multiple `srun` workers according to the compiled batch worker plan.

Use the optional submission controls when a cluster enforces per-user limits or has a long queue:

```bash
craftmake run \
  --workflow /path/to/workflow.yaml \
  --config /path/to/config.yaml \
  --backend slurm \
  --workers 8 \
  --slurm-submit-attempts 20 \
  --slurm-submit-backoff 30s \
  --slurm-submit-max-backoff 5m \
  --slurm-pending-timeout 2h
```

A transient `sbatch` rejection caused by submit limits or temporary controller unavailability is retried with exponential backoff and does not consume an active allocation slot. Permanent errors such as an invalid partition, account, QoS, or impossible resource request fail immediately. `submission.submit_retry_scheduled`, `submission.pending`, `submission.pending_timeout`, and `submission.unschedulable` events record these distinctions.

A Slurm run should be considered accepted only after checking both the `craftmake` run state and Slurm accounting:

```bash
craftmake status --state /path/to/state.sqlite --run latest --verbose
craftmake report --state /path/to/state.sqlite --run latest
sacct -X -j <job-id> --format=JobID,State,ExitCode,Elapsed
```

## Resume And Recovery

`resume` first reconciles a still-running source run, then creates a new run that reuses recoverable state and fingerprints:

```bash
craftmake resume \
  --state /path/to/project/workflow/.craftmake/state.sqlite \
  --run <source-run-id> \
  --partition amd_512
```

For a running source run, recovery checks terminal task results, running backend submissions, task manifests, and backend-specific state before the new run starts. Recovery does not silently treat a missing or incompatible result as success.

The recovery summary and Controller log path are printed to standard output. Recovery and resumed execution can be inspected independently through their `run_id` values.

## Inspect State, Logs, And Reports

Show the latest run:

```bash
craftmake status \
  --state /path/to/project/workflow/.craftmake/state.sqlite
```

Show every task's cache decision and reason:

```bash
craftmake status \
  --state /path/to/project/workflow/.craftmake/state.sqlite \
  --run latest \
  --verbose
```

List the Controller JSONL file and task attempt directories:

```bash
craftmake logs \
  --state /path/to/project/workflow/.craftmake/state.sqlite \
  --run latest
```

Export task metrics and artifact information as CSV:

```bash
craftmake report \
  --state /path/to/project/workflow/.craftmake/state.sqlite \
  --run latest
```

Retry delayed or unavailable Slurm accounting metrics before exporting:

```bash
craftmake report \
  --state /path/to/project/workflow/.craftmake/state.sqlite \
  --run latest \
  --refresh-metrics
```

The Controller log is JSON Lines. It contains control-plane events such as run lifecycle, cache decisions, submissions, attempts, retries, cancellation, and recovery. Task stdout and stderr remain in their task attempt directories and are not copied into the Controller log.

## Cancel A Run

Cancel the latest running run:

```bash
craftmake cancel \
  --state /path/to/project/workflow/.craftmake/state.sqlite \
  --run latest
```

Cancellation persists the run and task state, requests cancellation from active backend submissions, and records cancellation events. Backend cancellation may take time to settle; inspect both `craftmake status` and Slurm accounting afterward.

## State Layout

With the default state directory, a project contains:

```text
workflow/.craftmake/
  state.sqlite
  runs/<run-id>/
    controller.jsonl
    tasks/<task-id>/attempt-001/
      manifest.json
      result.json
      stdout.log
      stderr.log
      ...
```

The SQLite database is the source of truth for run, task, submission, attempt, artifact, and metric state. The Controller JSONL file is the append-only diagnostic stream for one run.

## Exit Codes

The CLI uses stable exit-code categories so automation can distinguish invalid input, state failures, backend failures, task failures, and cancellation. The command's stderr contains the human-readable error; successful commands return zero.

Do not parse normal status text as the primary automation contract. For automation, use `validate`, `plan --format json`, the SQLite database, CSV reports, and the printed `run_id`/`state`/`controller_log` paths.

## Release Archives

Build Linux amd64 and arm64 archives with checksums:

```bash
make release VERSION=0.1.0 COMMIT="unknown"
cd dist
sha256sum -c checksums.txt
```

Each archive contains:

```text
bin/craftmake
share/craftmake/workflows/
```

The release workflow publishes archives when a `v*` tag is pushed. Before publishing, run the release gate and verify an archive in a clean directory:

```bash
make check
make release VERSION=0.1.0
mkdir -p /tmp/craftmake-release-check
tar -xzf dist/craftmake_0.1.0_linux_amd64.tar.gz -C /tmp/craftmake-release-check
/tmp/craftmake-release-check/craftmake_0.1.0_linux_amd64/bin/craftmake doctor --backend local
```

## Development Checks

Run the repository checks locally:

```bash
make check
go test -race ./internal/controllerlog ./internal/store ./internal/scheduler ./internal/cli -count=1
```

The full test suite includes compiler, store, runtime, Local backend, Slurm backend, CLI, workflow fixture, cancellation, recovery, release packaging, and migration compatibility coverage.

## Scope And Deferred Work

The first release intentionally does not include:

- `xdxtools` sub-process integration
- Snakemake interpreter or native/Snakemake parity testing
- Kubernetes, SSH, or container backends
- A web UI, Prometheus, or OpenTelemetry exporter
- Cross-phase global DAG execution
- General-purpose Python or user-expression evaluation

Production validation is tracked as gated acceptance rather than a single pass/fail claim. BeaverBS has completed a real-tool synthetic cross-phase Slurm acceptance, while full-size BeaverBS, all BeaverPDX, BeaverRNA, and BeaverRNASEQPDX production acceptance remain open. Local fixtures validate orchestration and declared output contracts but do not replace real-tool, real-reference, Slurm accounting, cache, recovery, and artifact-integrity evidence. See [`doc/implementation-plan.md`](doc/implementation-plan.md#15-后续验收路线图) for the required sequence and evidence.
