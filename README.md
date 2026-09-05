# craftmake

**A native workflow executor for compiling versioned YAML into reproducible task runs.**

Craftmake builds a task DAG, executes it locally or through SLURM, persists state in SQLite, and exposes plan, run, resume, cancel, logs, reports, caching, and artifact-aware recovery. It is the execution layer being integrated into `otter`.

## What it provides

- Versioned workflow YAML and deterministic catalog routing.
- Local execution with CPU, memory, and parallelism admission budgets.
- Native SLURM submission with task-level resource envelopes and accounting refresh.
- SQLite run/task/submission state, cache decisions, resume, cancellation, and reports.
- Immutable `otter.run/v1` input/reference boundary and create-only artifact publication.
- Structured run IDs, JSON output, failure classification, and reproducible evidence.

## Where it fits

```text
otter project/config → immutable run.yaml → craftmake → enva/operators → results manifest
```

Craftmake can also run a strict `craftmake.standalone/v1` DAG. It does not create Otter projects or replace domain-specific configuration and sample validation.

## Current boundary

Craftmake is the native executor under integration. Existing Otter production workflows still have an explicit Snakemake compatibility path; Craftmake is not an embedded Snakemake interpreter and is not an automatic fallback.

The accepted Gate 6 evidence is bounded. It covers executor parity and selected recovery/publication behavior for RRBS, RNA-seq, BS-PDX, and RNA-PDX. Fresh seven-input matrix work, representative repeats, production-scale qualification, and WGBS qualification remain deferred and must not be presented as completed.

## Install and build

```bash
git clone https://github.com/rainoffallingstar/craftmake.git
cd craftmake
make build
./build/craftmake --help
```

Install to a prefix:

```bash
sudo make install PREFIX="/usr/local"
```

## Minimal commands

```bash
craftmake doctor --backend local
craftmake doctor --backend slurm

craftmake plan \
  --config /analysis/runs/run-20260905T010203Z-abcdef/run.yaml \
  --phase step1 \
  --catalog workflows/

craftmake run \
  --config /analysis/runs/run-20260905T010203Z-abcdef/run.yaml \
  --phase step1 \
  --backend local \
  --workers 4 \
  --max-cores 16 \
  --max-memory 64G
```

Resume and inspect a run:

```bash
craftmake resume --state /analysis/runs/<run-id>/state/state.sqlite --run <run-id>
craftmake report --state-dir /analysis/runs/<run-id>/state
craftmake cancel --state-dir /analysis/runs/<run-id>/state
```

The exact workflow phase, backend, resource envelope, and reference identity for an `otter.run/v1` snapshot are resolved before execution. Do not override them ad hoc at runtime.

## Workflow assets

Current catalog families include:

- `BeaverBS` — RRBS/WGBS-compatible bisulfite phases;
- `BeaverRNA` — RNA-seq phases;
- `BeaverPDX` — bisulfite PDX phases;
- `BeaverRNASEQPDX` — RNA-PDX phases;
- `SRAArchiveDecode` — validated archive decode and paired-FASTQ publication.

See [benchmark evidence](doc/benchmarks/README.md) for the reproducible PDX controller-reconciliation dataset and generator.

## Operational limits

- Admission budgets coordinate scheduling; they do not create OS cgroups for child processes.
- SLURM jobs may remain in `COMPLETING`; inspect Craftmake state and `sacct` together.
- A valid manifest is necessary but is not by itself scientific parity.
- Real production-scale throughput is not established by the current bounded evidence.

## Development

```bash
go test ./...
go vet ./...
make benchmark-pdx-scheduler
```

The repository uses Go 1.26.x as declared in `go.mod`. Each source revision is independent from the parent `otter` checkout; update the parent gitlink only when intentionally integrating a new revision.

## License

MIT
