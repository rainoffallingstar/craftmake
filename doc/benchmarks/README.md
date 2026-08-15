# PDX Scheduler Benchmark: Craftmake and Snakemake Compatibility Executor

## Scope

This directory retains the checked source data, generated summary statistics, and figure for the current repeated PDX scheduler comparison. The experiment measures **controller reconciliation time** for the `step2-check` phase: the elapsed interval from the final worker-job completion to the controller's recorded terminal completion.

The source report is the immutable Paracloud aggregate `gate6-r41-pdx-scheduling-repeat-aggregate-r2`, collected with release `gate6-20260812T104500Z-pdx-host-fasta-r41`. It covers six balanced executor pairs: three `bs-pdx` pairs and three `rna-pdx` pairs. Each pair fixes the scenario, phase, prerequisite content checksums, release, workflow, and Slurm resource envelope; both executors produced matching filtered BAM/BAI checksums and mapped-read counts.

The paired runs are real workflow benchmarks, not topology-identical executor microbenchmarks. Craftmake submitted three worker jobs per PDX cell (two validation tasks and Xenofilx), whereas the Snakemake compatibility projection submitted one worker job. Therefore, task count, queue interaction, and worker makespan describe the observed execution topology. They must not be interpreted as an isolated implementation-speed ranking.

## Results

![Median controller reconciliation time with observed range and paired repeats](pdx-step2-check-controller-reconciliation.svg)

The bars show the median of three paired repeats. Whiskers show the observed minimum and maximum, not a confidence interval. Gray lines join matching repeat numbers. The figure reports an exact two-sided Wilcoxon signed-rank p-value; with only three pairs, the smallest attainable two-sided exact p-value is `0.250`, so these data are descriptive and do not establish a conventional statistical-significance claim.

| Scenario | Craftmake median (min–max), s | Snakemake median (min–max), s | Median paired difference, s (Snakemake − Craftmake) | Exact two-sided Wilcoxon p |
|---|---:|---:|---:|---:|
| BS-PDX | 4.213 (2.385–5.057) | 19.819 (14.008–164.668) | 15.606 | 0.250 |
| RNA-PDX | 2.651 (1.552–4.308) | 9.478 (8.458–29.556) | 5.807 | 0.250 |

Within this specific release, compatibility projection, PDX `step2-check` phase, and Paracloud Slurm environment, the observed Craftmake controller-reconciliation intervals were lower and narrower than the corresponding Snakemake intervals. The experiment does not attribute that behavior to a particular internal mechanism, does not compare topology-identical DAGs, and does not establish general throughput or production-scale performance. Queue delay remains a separate scheduler/site metric and is not used for this conclusion.

## Next gates

The current PDX comparison closes a bounded scheduler-evidence step only. It does not close representative or production-scale validation.

1. **Requalify production inputs.** All five registered production candidates are currently `missing_reacquire_for_production`; publish a new create-only `otter.sra-acquisition/v1` record before treating any re-downloaded FASTQ as production evidence.
2. **Complete real-data canaries.** For the four active non-WGBS scenarios, run fresh immutable Craftmake and explicit Snakemake compatibility canaries with artifact verification, semantic review, accounting settlement, and a relevant recovery/invalidation exercise.
3. **Expand repeated evidence.** Preserve separate controller, queue, worker, and reconciliation metrics. Use topology-identical microbenchmarks before attributing any difference to executor implementation.
4. **Run the representative matrix.** Only accepted canary scenarios may enter the planned 20-cell matrix with at least three runs per cell.
5. **Run scale and release gates.** After representative acceptance, measure production throughput/scheduler pressure and complete clean-Linux installation, archive, migration, and post-install Slurm-smoke validation.

Do not infer a general performance ranking, a production-throughput result, or a Snakemake retirement decision from this benchmark.

## Provenance and reproduction

| Artifact | Purpose | SHA-256 |
|---|---|---|
| `gate6-r41-pdx-scheduling-repeat-aggregate-r2.source.json` | Verbatim checked copy of the immutable aggregate source report | `c80a81a26635c91c850e940b5157d88a727d44dbed2e31e67537ceb1838278f0` |
| `pdx-step2-check-controller-reconciliation-summary.csv` | Derived statistics used by this document and figure | `b49a34f39c9a591ce1863912c64f840e90c0a2ceca249d213b3a13e74ff20fc8` |
| `pdx-step2-check-controller-reconciliation.svg` | Version-controlled publication figure | `4c5972e7bab5fdcea28fb55d572fd3df958d3ec5c3403af9a1ab24d586ea98c2` |
| `../../scripts/generate_pdx_scheduler_benchmark.py` | Fail-closed chart/statistics generator | Generated artifact source |

Regenerate the derived CSV and SVG from the repository root:

```bash
python3 craftmake/scripts/generate_pdx_scheduler_benchmark.py \
  --source craftmake/doc/benchmarks/gate6-r41-pdx-scheduling-repeat-aggregate-r2.source.json \
  --summary-csv craftmake/doc/benchmarks/pdx-step2-check-controller-reconciliation-summary.csv \
  --figure-svg craftmake/doc/benchmarks/pdx-step2-check-controller-reconciliation.svg
```

The generator verifies the source SHA-256, expected r41 release, the two PDX scenarios, and exactly three complete Craftmake/Snakemake repeat pairs per scenario before it writes output. It requires Python 3 with `matplotlib` and `scipy`.

For Craftmake's migration boundary and remaining validation gates, see the local [implementation plan](../implementation-plan.md).
