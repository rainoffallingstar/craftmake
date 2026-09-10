# TDD：Colab 混合加速器 (CPU/GPU) 多实例调度

## 问题陈述

当前 Colab 后端采用**单实例模型**：`BeginRun` 分配一台固定加速器类型的机器，整个 Run 的所有 Submission/Task 都在同一台机器上顺序执行。这导致：

1. **浪费 GPU 配额**：数据预处理、QC 等纯 CPU 任务也占用了 GPU 实例的算力配额和有限并发（Google 免费账户仅 1 个 GPU 实例）。
2. **无法混合任务类型**：一个工作流若包含 CPU 预处理 → GPU 训练 → CPU 后处理的三阶段 DAG，只能全程使用 GPU 实例。
3. **无法并行 CPU + GPU**：两个无依赖关系的 Job——一个跑 CPU、一个跑 GPU——无法同时在各自实例上执行。

## 设计目标

支持在**同一个 Action 工作流**中，按 Job 粒度声明不同的加速器类型（`cpu` / `gpu` / `tpu`），调度器根据 DAG 依赖自动：

1. **按需分配实例**：CPU Job → 分配 CPU 实例；GPU Job → 分配 GPU 实例。
2. **复用同类型实例**：同一加速器类型的连续 Job 共享同一个实例（避免反复 assign/unassign）。
3. **支持跨类型并行**：无依赖关系的 CPU Job 和 GPU Job 可以同时在不同实例上运行。
4. **实例生命周期自动管理**：所有实例在 `EndRun` 时保证释放（零泄漏）。

---

## 架构方案：RuntimePool + Job-Level Accelerator

### 核心概念

```text
┌────────────────────────────────────────────────────────────┐
│                    Action Workflow YAML                      │
│  ┌──────────┐  ┌──────────────┐  ┌────────────────────┐    │
│  │ preprocess│  │    train     │  │   postprocess      │    │
│  │ accel: cpu│→ │ accel: gpu   │→ │   accel: cpu       │    │
│  └──────────┘  └──────────────┘  └────────────────────┘    │
└────────────────────────────────────────────────────────────┘
                          ↓ 调度器编排
┌─────────────────────────────────────────────────┐
│               RuntimePool (新增组件)              │
│  ┌──────────────────┐  ┌──────────────────────┐  │
│  │ CPU Slot          │  │ GPU Slot              │  │
│  │ Runtime: ...      │  │ Runtime: ...          │  │
│  │ Idle/Active/None  │  │ Idle/Active/None      │  │
│  └──────────────────┘  └──────────────────────┘  │
│  ┌──────────────────┐                            │
│  │ TPU Slot (future) │                            │
│  │ None              │                            │
│  └──────────────────┘                            │
└─────────────────────────────────────────────────┘
```

### 层级设计

| 层 | 变更 | 说明 |
|---|---|---|
| **YAML Schema** | `JobSpec.Accelerator` 新增字段 | 声明 `accelerator: cpu` / `gpu` / `tpu`，默认空（继承 Action 级别或 backend 默认） |
| **ActionSpec** | `ColabSpec.DefaultAccelerator` | Action 级别的默认加速器类型 |
| **compiler.Task** | `Accelerator string` 新增字段 | 编译后每个 Task 携带加速器标记 |
| **protocol.ResourceRequest** | `Accelerator string` 新增字段 | 随 TaskManifest 下发到后端 |
| **RuntimePool (新增)** | 替代单一 `b.runtime` | 管理多个 Colab 实例的分配/复用/释放池 |
| **Colab Backend** | `RunSubmission` 改造 | 按 Task 的 Accelerator 字段从 RuntimePool 获取对应实例 |
| **Scheduler** | 无核心变更 | DAG 依赖、admission 逻辑保持不变；资源标签通过 `Partition` 或 `Accelerator` 透传 |

---

## YAML Schema 示例

```yaml
schema_version: craftmake.action/v1
name: ml_pipeline
backend: colab
colab:
  session: gpu
  default_accelerator: cpu          # 未声明 accelerator 的 Job 默认跑 CPU
  drive_root: /content/drive/MyDrive/ml_pipeline
jobs:
  preprocess:
    accelerator: cpu                 # 显式声明：CPU 实例
    steps:
      - run: |
          python3 preprocess.py --input data/raw --output data/processed

  train:
    accelerator: gpu                 # 显式声明：GPU 实例
    needs: [preprocess]
    steps:
      - run: |
          python3 train.py --data data/processed --epochs 50 --output models/

  evaluate:
    accelerator: gpu                 # 继续复用 GPU 实例
    needs: [train]
    steps:
      - run: |
          python3 evaluate.py --model models/best.pt --output results/

  report:
    accelerator: cpu                 # 回到 CPU 实例
    needs: [evaluate]
    steps:
      - run: |
          python3 generate_report.py --results results/ --output report/
```

### 并行 CPU + GPU 示例

```yaml
jobs:
  data_qc:                           # CPU 实例
    accelerator: cpu
    steps:
      - run: fastqc --threads 4 data/*.fastq.gz

  gpu_precompute:                    # GPU 实例（与 data_qc 并行执行）
    accelerator: gpu
    steps:
      - run: python3 precompute_embeddings.py

  final_analysis:                    # CPU 实例（等两者都完成）
    accelerator: cpu
    needs: [data_qc, gpu_precompute]
    steps:
      - run: python3 integrate.py
```

---

## TDD 切片清单

### Phase 1: 数据模型扩展（纯离线，无网络调用）

| # | 测试名 | 验证内容 | 涉及文件 |
|---|---|---|---|
| 1.1 | `TestJobSpecAcceleratorField` | `spec.JobSpec` 可反序列化 `accelerator: gpu` 字段 | `internal/spec/model.go` |
| 1.2 | `TestJobSpecAcceleratorDefault` | 未声明 `accelerator` 的 Job 字段为空字符串 | `internal/spec/model.go` |
| 1.3 | `TestJobSpecAcceleratorValidation` | 非法值（如 `accelerator: quantum`）编译时拒绝 | `internal/spec/model.go` |
| 1.4 | `TestResourceRequestAccelerator` | `protocol.ResourceRequest` 序列化/反序列化携带 `accelerator` | `pkg/protocol/types.go` |
| 1.5 | `TestTaskCarriesAccelerator` | 编译后的 `compiler.Task` 带 `Accelerator` 字段 | `internal/compiler/model.go`, `compiler.go` |
| 1.6 | `TestActionColabDefaultAccelerator` | `ColabSpec.DefaultAccelerator` 可解析并回填未声明的 Job | `internal/adapters/action/loader.go` |

### Phase 2: RuntimePool 核心（纯单元测试，Mock ControlPlane）

| # | 测试名 | 验证内容 | 涉及文件 |
|---|---|---|---|
| 2.1 | `TestRuntimePoolAcquireCPU` | 请求 CPU 加速器 → Pool 调用 `ControlPlane.AcquireRuntime` 创建 CPU 实例 | `internal/backend/colab/runtime_pool.go` (新) |
| 2.2 | `TestRuntimePoolAcquireGPU` | 请求 GPU 加速器 → Pool 用 `Accelerator: "GPU"` 参数请求实例 | 同上 |
| 2.3 | `TestRuntimePoolReuseSameType` | 连续两次请求 CPU → 第二次复用已有实例（不重新 Assign） | 同上 |
| 2.4 | `TestRuntimePoolDifferentTypes` | 先请求 CPU 再请求 GPU → 两个不同实例共存 | 同上 |
| 2.5 | `TestRuntimePoolConcurrentAcquire` | 并发请求同一类型 → 串行等待同一实例（不重复分配） | 同上 |
| 2.6 | `TestRuntimePoolReleaseAll` | `ReleaseAll` 释放池中所有实例，释放后再 Acquire 需重新分配 | 同上 |
| 2.7 | `TestRuntimePoolReleaseAllIdempotent` | 多次 `ReleaseAll` 不 panic、不重复调用 Unassign | 同上 |
| 2.8 | `TestRuntimePoolAcquireAfterContextCancel` | 上下文取消时 Acquire 立即返回错误 | 同上 |
| 2.9 | `TestRuntimePoolFailedAcquireDoesNotPoison` | 第一次 Assign 失败后，第二次仍可重试（不缓存失败实例） | 同上 |

### Phase 3: Backend 集成（Mock Executor + Pool）

| # | 测试名 | 验证内容 | 涉及文件 |
|---|---|---|---|
| 3.1 | `TestRunSubmissionRoutesByAccelerator` | 两个 Manifest（CPU+GPU）分别在不同 Runtime 上执行 | `internal/backend/colab/backend.go` |
| 3.2 | `TestRunSubmissionDefaultAccelerator` | Manifest 无 Accelerator → 使用 `Config.DefaultAccelerator` | 同上 |
| 3.3 | `TestRunSubmissionSameAcceleratorReuses` | 两个 GPU Manifest 顺序执行 → 共用同一个 GPU Runtime | 同上 |
| 3.4 | `TestEndRunReleasesAllRuntimes` | EndRun 后 Pool 中所有实例被释放 | 同上 |
| 3.5 | `TestRunSubmissionPartialFailure` | GPU Manifest 失败不影响后续 CPU Manifest 执行 | 同上 |
| 3.6 | `TestBeginRunCreatesPool` | BeginRun 初始化空 RuntimePool（替代单一 runtime 字段） | 同上 |

### Phase 4: 端到端 Action 集成

| # | 测试名 | 验证内容 | 涉及文件 |
|---|---|---|---|
| 4.1 | `TestActionPlanShowsAccelerator` | `action plan` 输出中显示每个 Task 的加速器标签 | `internal/cli/action.go` |
| 4.2 | `TestActionRunMixedAcceleratorOffline` | 离线 Mock：混合 Action YAML → 编译 → 正确路由到不同 Runtime | `internal/cli/action_test.go` |
| 4.3 | `TestColabSpecDefaultAcceleratorRendering` | `default_accelerator: cpu` → 未声明的 Job 获得 `cpu` | `internal/adapters/action/action_test.go` |

### Phase 5: 实机验证（可选，需 Colab 凭据）

| # | 测试名 | 验证内容 | 涉及文件 |
|---|---|---|---|
| 5.1 | `TestLiveCPUThenGPU` | 真实分配 CPU 实例 → 执行 → 释放 → 分配 GPU 实例 → 执行 → 释放 | 实机探针测试 |
| 5.2 | `TestLiveParallelCPUGPU` | 并发分配 CPU + GPU → 并行执行 → 全部释放 | 同上 |

---

## RuntimePool 接口设计

```go
// runtime_pool.go (new file)
package colab

import (
    "context"
    "fmt"
    "sync"
)

// RuntimePool manages a set of Colab runtimes keyed by accelerator type.
// It lazily acquires instances on first request and reuses them for subsequent
// requests of the same type. All instances are released together on ReleaseAll.
type RuntimePool struct {
    control   ControlPlane
    runID     string
    notebookHash string

    mu        sync.Mutex
    runtimes  map[string]*poolEntry   // key: accelerator ("cpu", "gpu", "tpu")
    released  bool
}

type poolEntry struct {
    runtime  Runtime
    ready    bool
    mu       sync.Mutex           // serializes concurrent Acquire for same type
}

// Acquire returns a Runtime for the given accelerator type, reusing an existing
// one or lazily allocating a new one. It is safe for concurrent use.
func (p *RuntimePool) Acquire(ctx context.Context, accelerator string) (Runtime, error)

// ReleaseAll releases all acquired runtimes. It is idempotent and safe to call
// multiple times. After ReleaseAll, Acquire returns an error.
func (p *RuntimePool) ReleaseAll(ctx context.Context) error

// ActiveCount returns the number of currently held runtimes.
func (p *RuntimePool) ActiveCount() int
```

## Backend 改造要点

```go
// backend.go — BeginRun 改造
type Backend struct {
    // ...existing fields...
    // runtime    Runtime              // 移除：单实例字段
    // active     bool                  // 移除
    pool       *RuntimePool            // 新增：多实例池
    defaultAccelerator string           // 新增：默认加速器类型
}

func (b *Backend) BeginRun(ctx context.Context, run backend.RunContext) error {
    // ...existing config loading...
    b.pool = NewRuntimePool(b.Control, run.RunID, notebookHash)
    b.defaultAccelerator = config.DefaultAccelerator
    if b.defaultAccelerator == "" {
        b.defaultAccelerator = "cpu"
    }
    // 注意：不再在 BeginRun 中 Assign！延迟到 RunSubmission 按需分配。
    return nil
}

// RunSubmission — 按 Task Accelerator 获取实例
func (b *Backend) RunSubmission(ctx context.Context, submissionID string, request backend.SubmissionRequest) (*backend.SubmissionResult, error) {
    for _, manifest := range request.Manifests {
        accel := manifest.Resources.Accelerator
        if accel == "" {
            accel = b.defaultAccelerator
        }
        runtime, err := b.pool.Acquire(ctx, accel)
        // ...execute on this specific runtime...
    }
}

// EndRun — 释放所有实例
func (b *Backend) EndRun(ctx context.Context, outcome backend.RunOutcome) error {
    return b.pool.ReleaseAll(ctx)
}
```

## ResourceRequest 扩展

```go
// pkg/protocol/types.go
type ResourceRequest struct {
    Cores       int    `json:"cores"`
    MemoryByte  int64  `json:"memory_bytes"`
    Partition   string `json:"partition,omitempty"`
    Time        string `json:"time,omitempty"`
    Accelerator string `json:"accelerator,omitempty"`   // 新增: "cpu", "gpu", "tpu"
}
```

## Google Colab 免费层约束与应对

| 约束 | 应对策略 |
|---|---|
| 免费账户仅 1 个 GPU 实例 | RuntimePool 按类型串行：同时只有 1 个 GPU 实例，但可同时有 1 个 CPU 实例 |
| Colab Pro 支持多 GPU | RuntimePool 可扩展为按类型设置并发上限 |
| 实例空闲回收 | RuntimePool 可选心跳保活（`sendKeepAlive`） |
| HTTP 412 配额冲突 | 复用现有 412 → ListAssignments → 自动清理逻辑 |

---

## 实施顺序（推荐）

```text
Phase 1 → Phase 2 → Phase 3 → Phase 4 → Phase 5
(模型)    (Pool)    (集成)    (Action)   (实机)
```

每个 Phase 独立可测试、独立可提交，最小化每次变更的风险半径。
