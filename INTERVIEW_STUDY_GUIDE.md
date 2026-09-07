# 面试复习讲义（详版）：项目全链路 walkthrough 与组件原理

> 本讲义以"一次提交的旅程"为主线：先讲清系统由哪些组件构成、各自为什么存在，再逐步骤展开完整流程；每经过一个组件，说明其工作原理、本项目的设计取舍与实测过程中的实际情况。数字与事件的出处：`benchmarks/results/ci-timing.md`（消融阶梯）、`benchmarks/results/alert-noise.md`（告警）、`benchmarks/evidence/V3/`（回退证据）、Jenkins API（构建明细）。
> 建议的复习路径：通读两遍 → 对照 0.1 流程图独立复述主线 → 对第一章（Jenkins）与第七章每个专题准备 90 秒的脱稿讲解。

---

## 第〇章 全局架构与组件分工

### 0.1 一次代码变更到上线的完整流程

整个系统分两段，以 **GitOps 仓库为交接界面**：前半段（CI 段）由 Jenkins 驱动，产出是"改好 values 的 GitOps 提交"；后半段（CD 段）由 Argo CD 驱动，输入是 GitOps 仓库，产出是集群里运行的新版本。

```
【CI 段 · Jenkins 驱动 ─────────────────────────────────────────────】

 开发者 push 代码            Jenkins 流水线被触发（传入 BEFORE/AFTER SHA）
      │                              │
      │                     ┌────────┴────────────────────────────────────┐
      │                     │ ① 检出：浅克隆源码仓 + GitOps 仓             │
      │                     │ ② 影响分析：git diff + go list -deps 闭包    │
      │                     │    → 产出 impact.json（受影响服务集）        │
      │                     │ ③ 目录解析：服务→策略/镜像名/values/参数     │
      │                     │    → 产出 delivery-catalog.json            │
      │                     │ ④ 逐服务：go test（共享 Go 缓存 PVC）        │
      │                     │ ⑤ 逐服务：BuildKit 构建（缓存卷）            │
      │                     │    → 按 digest 推送 ACR，写 digest 文件     │
      │                     │ ⑥ 向 PG 写 releasing 状态记录               │
      │                     │ ⑦ yq 改写 values（digest/deploy_id/参数）   │
      │                     │    → 提交 release/<svc>/<sha> 分支 → PR     │
      │                     │ ⑧ PR 合并                                   │
      │                     └────────┬────────────────────────────────────┘
      │                              │
      ▼                              ▼
   app-test 单仓（源码 + GitOps values 同仓）━━━━━ 交接界面 ━━━━━ Git 状态更新
                                                                     │
【CD 段 · Argo CD 驱动 ─────────────────────────────────────────────】│
                                                                     ▼
                                     Argo CD 对账循环（15s 周期）
                                       repo-server：helm template 渲染 + schema 校验
                                       controller：Git 期望态 vs 集群实际态 diff → apply
                                                                     │
                                                                     ▼
                                     Argo Rollouts / Deployment 滚动更新
                                       新 Pod 携带发布身份标签（deploy_id/git_sha/digest）
                                                                     │
                                     Jenkins：wait-for-release 四层验证
                                       （Argo synced → 运行 digest → Rollout 健康 → /health）
                                                                     │
                                     全部通过 → PG 写 stable 记录（回退 L2 的数据源）
                                                                     │
                                     Prometheus（录制规则关联发布身份）→ Alertmanager（抑制/路由）
```

阅读要点：Jenkins 并非"构建完就退场"——它贯穿 CI 段并执行 CD 段的验证与失败回退；Argo CD 只负责"Git → 集群"这一段同步。两者职责边界见 0.4。

### 0.2 组件职责一览

| 组件 | 是什么 | 在本系统中的职责 | 不负责什么 |
|---|---|---|---|
| **Jenkins** | CI 流水线编排引擎 | 驱动从检出→影响分析→构建→推送→GitOps PR→发布验证→失败回退的完整编排；管理凭据；记录全过程日志 | 不直接改集群工作负载（回退经 RBAC 限定的 ServiceAccount 执行） |
| **Argo CD** | GitOps 持续交付控制器 | 监听 GitOps 仓库，将 Git 期望态同步到集群（渲染、diff、apply、健康评估） | 不构建镜像、不跑测试、不创建 PR |
| **Argo Rollouts** | 渐进式发布控制器 | 提供 Canary/蓝绿策略、AnalysisRun 指标门禁（本项经设计与离线验证） | 常规镜像构建、Git 同步 |
| **BuildKit** | 镜像构建引擎（buildkitd 守护进程 + buildctl 客户端） | 在 CI Pod 内构建 OCI 镜像，管理内容寻址的层缓存 | 推送以外的任何流程逻辑 |
| **ACR** | 阿里云容器镜像仓库 | 存放服务镜像（按 digest 寻址）与缓存制品 | — |
| **Prometheus** | 指标系统 | 抓取/录制（降基数）/告警规则评估；为发布门禁与告警治理提供数据 | 告警的路由与抑制 |
| **Alertmanager** | 告警路由与抑制 | 分组、按四元组精确抑制、路由到 receiver | 告警规则的评估 |
| **PostgreSQL** | 关系数据库 | 发布状态机单一事实来源（releasing/stable/failed），回退 L2 数据源 | 业务数据（业务库是独立的 MySQL/Redis） |
| **k3s 集群** | 轻量 Kubernetes 发行版 | 承载以上全部组件与 13 个业务服务 | — |

### 0.3 集群拓扑

三台阿里云 ECS：node3（4c16g）为 k3s server，承载平台组件与 CI 构建 Pod（`platform-role=control`）；node1（2c4g）承担 PostgreSQL、平台服务与部分业务（`platform-role=light`）；node2（2c4g）承担业务与监控（`platform-role=worker`）。节点间经 Tailscale 组网（无公网互通），flannel 显式绑定 tailscale0 接口。命名空间划分：`app`（13 个业务服务）、`delivery`（Jenkins、PostgreSQL、CI 缓存 PVC）、`monitoring`（Prometheus、Alertmanager）、`argocd`（Argo CD、Rollouts 控制器）。

### 0.4 两条关键职责边界（面试最常被混淆的地方）

**Jenkins 与 Argo CD 的分工**：Argo CD 只回答一个问题——"Git 里的期望态和集群的实际态是否一致，不一致就把 Git 应用上去"。其余一切（检出示例代码、跑影响分析、执行测试、构建镜像、推送仓库、修改 values、创建 PR、验证部署结果、失败时执行回退）都由 Jenkins 编排。**交接界面是 GitOps 仓库**：Jenkins 写入（经 PR），Argo CD 读取并应用。

**digest 与 tag 的分工**：推送产物为不可变的 `sha256:…`；tag（`git-<短sha>`）只是可变指针。流水线下游的一切——GitOps values 记录的内容、集群验证比对的对象、回退解析的目标——全部使用 digest。不可变性是回退正确性的前提（tag 可被覆盖，digest 永远指向同一份内容）。

---

## 第一章 Jenkins：CI 编排中枢

### 1.1 Jenkins 是什么，为什么这套系统需要它

Jenkins 是开源的 CI/CD 自动化服务器，核心能力是把"一系列步骤"编排为流水线（pipeline）：按定义的顺序执行各阶段、管理凭据、调度执行环境、记录每一步日志、处理失败分支。

本系统需要它的原因可以从反面理解：**系统里没有其他组件能承担编排职责**。Argo CD 只做 Git→集群的同步；BuildKit 只构建镜像；ACR 只存镜像。而一次发布需要有人按正确顺序完成十余个步骤（检出、分析、测试、构建、推送、改写 values、开 PR、等合并、验证部署、失败时回退），步骤之间还传递中间产物（SHA、服务清单、digest 文件）。这个"总指挥"就是 Jenkins。

一句话定位：**Jenkins 驱动"代码 → 镜像 → GitOps PR"的 CI 段并编排全流程（含 CD 段的验证与回退）；Argo CD 驱动"Git → 集群"的 CD 段。**

### 1.2 架构：controller 与动态 agent

- **controller**：Web UI、任务（Job）定义、调度、凭据管理。本项目部署于 `delivery` 命名空间，使用预烘焙插件的自定义镜像（集群网络无法访问 Jenkins 插件更新中心，插件离线固化）。
- **agent（执行器）**：实际执行 shell 的地方。传统方式是常驻虚拟机；本项目采用 **Kubernetes 插件的动态 agent**——每次构建开始时向集群申请创建一个专用 Pod，构建结束后销毁。收益：构建是脉冲型负载，按需创建避免为峰值常驻买单；每次构建环境干净、互不污染。
- **通信**：agent Pod 内的 jnlp 容器与 controller 保持连接，接收指令并回传日志。

### 1.3 构建 agent Pod 的内部结构

每次构建创建的 Pod 包含 7 个容器与 2 个 PVC：

| 容器 | 镜像 | 职责 |
|---|---|---|
| jnlp | Jenkins 内置 | 与 controller 通信 |
| go | 自烘焙 golang-ci（含 git/gcc/musl-dev/make） | 影响分析、服务测试、platform-server 编译、发布记录写入 |
| buildkitd | moby/buildkit rootless | 镜像构建守护进程（接收 buildctl 指令） |
| git | alpine/git | 仓库克隆、分支推送 |
| yq | mikefarah/yq（root 运行） | 改写 GitOps values |
| rollouts | 自烘焙 alpine-tools（含 jq/wget 等） | kubectl/argo-rollouts CLI、wait-for-release 与回退脚本 |
| curl | curlimages/curl | 调用 GitHub API 创建/查询 PR |

**为什么是多容器而不是一个大镜像**：每个工具使用官方（或自烘焙）镜像，职责单一、互不污染；共享同一个 workspace 卷（emptyDir）传递中间产物。实测教训：跨容器文件权限需显式处理（uid 不一致导致过 `dubious ownership` 与写权限错误，最终以统一 uid / 目录放开写权限解决）。

**两个 PVC（缓存的物理载体）**：
- `jenkins-agent-cache`：GOCACHE/GOMODCACHE（服务间共享的 Go 编译产物，消融实测贡献 -71%）+ 工具二进制（kubectl/rollouts CLI，避免每次下载）；
- `buildkit-cache`：buildkitd 自身状态（内容寻址存储）+ 导出的本地层缓存（`--export-cache type=local`）。

**RBAC**：Pod 使用 `ecampus-release` ServiceAccount，权限限定为：app 命名空间 Rollout 的读写与 abort/undo、Deployment 的读与 patch（回退精确钉 digest）、Pod/Service 只读、argocd 命名空间 Application 读 + patch（仅 selfHeal 开关）。CI 无法越权改其他资源。

### 1.4 流水线阶段逐段解读

Jenkinsfile（声明式）定义十个阶段，每阶段的输入、动作与产出如下：

| # | 阶段 | 输入 | 动作 | 产出（下游消费者） |
|---|---|---|---|---|
| 1 | Checkout main | 触发参数 | 浅克隆源码仓（`--depth 20`）与 GitOps 仓，带 token 认证与重试 | `COMMIT_SHA`（后续所有阶段的版本锚点） |
| 2 | Detect affected services | BEFORE/AFTER SHA | `go run ./cmd/ecampus-impact`：git diff + 依赖闭包 | `impact.json`：test_matrix 与 build_matrix（受影响服务及其入口路径） |
| 3 | Resolve delivery catalog | impact.json 的服务集 | `go run ./cmd/server catalog`：查服务目录 | `delivery-catalog.json`：每服务的镜像名、values 文件路径、发布 profile、分析参数 |
| 4 | Verify agent generated code | impact.json 标记 proto 变更时 | 重新生成 proto 并比对无漂移 | —（防生成代码与源不同步） |
| 5 | Verify, build and push | 上述两份 JSON | 逐服务（串行）：go test → 缓存探测 → buildctl 构建 → 推 ACR | `.ci/digests/<svc>.digest`（不可变镜像标识）；向 PG 写 releasing |
| 6 | Prepare tools | — | 首次下载 kubectl/rollouts CLI（PVC 缓存复用）；编译 platform-server | `/cache/jenkins-tools/*` |
| 7 | Publish GitOps PRs | digest 文件 + catalog | 逐服务：克隆 GitOps → yq 改 values → 推 `release/<svc>/<sha>` 分支 → 创建 PR → 等待合并 | 合并后的 main revision（configRevision） |
| 8 | Wait for rollout and health | configRevision + digest | 逐服务执行 wait-for-release 四层验证（见 2.6） | 验证通过 → PG 写 stable；失败 → 进入回退分支 |
| 9 | Approve blue-green promotion | 蓝绿服务清单 | `input` 人工审批（timeout 30min） | 审批通过/超时中止 |
| 10 | Promote blue-green | 审批结果 | 执行切流并做切流后分析 | — |

阶段间的衔接完全依靠 workspace 中的中间产物文件（下一节），这是多容器协作的纽带。

### 1.5 流水线的中间产物（把流程讲具体的关键）

**impact.json（影响分析输出）**——核心字段：
```json
{
  "changed_files": ["internal/theme/handler.go"],
  "test_matrix":  { "include": [ { "service": "theme", "entrypoint": "./cmd/ecampus-theme", ... } ] },
  "build_matrix": { "include": [ { "service": "theme", "image": "theme", ... } ] },
  "fallback_reason": null   // 非 null 时表示触发了保守全量及其原因
}
```

**delivery-catalog.json（目录解析输出）**——把"服务名"翻译成发布所需的全部元数据：
```json
{ "service": "theme", "image": "<ACR地址>/pulseops/ecampus-theme",
  "values_file": "k3s/helm-values/workloads/ecampus-theme.yaml",
  "effective_profile": "fast-rolling", "namespace": "app",
  "analysis": { "minSamples": 1000, "maxErrorRate": 0.02, "maxP95Ratio": 1.5, ... } }
```

**yq 实际写入 values 的字段**（发布身份与策略参数一体化传递）：
`image.digest`（不可变标识）、`image.tag`、`release.deployId / releaseBatch / gitSha / gitopsRevision / environment / buildNumber`、`rollout.*`（策略与全部分析参数）。

**下游消费关系**：digest 文件被阶段 7（写入 values）、阶段 8（集群运行比对）、回退流程（目标解析）使用；deploy_id 经 chart 渲染进入 Pod 标签，最终被 Prometheus 录制规则与 Alertmanager 抑制规则消费——**一个标识从 CI 贯穿到告警**。

### 1.6 凭据与触发方式

凭据两类：`git-https`（GitHub token，用于源码克隆认证、GitOps 分支推送、PR 创建与合并的 API 调用）与 `acr-https`（ACR 凭据，用于缓存存在性探测）；镜像推送凭据以 dockerconfig Secret 挂载给 buildkitd。token 的注入路径：K8s Secret → Jenkins 挂载 → JCasC 读取为凭据（token 不落 git 与 values）。

触发方式：设计上由 GitHub webhook 携带 BEFORE/AFTER SHA 触发；原型环境无公网入口，退化为 Jenkins API 触发并显式传参。`SKIP_RELEASE` 参数使流水线在构建推送后即止（基准测试用）；`EXTREME_COLD` 参数关闭全部缓存复用（消融基线用）；`ROLLBACK_PAUSE_SYNC` 控制回退窗口的 selfHeal 暂停（默认开启）。

---

## 第二章 一次正常发布的完整旅程

### 2.1 全流程时间线（实测口径）

以日常单服务变更（热缓存）为例的端到端分解（数据来自消融实测构建 #52/#56 与发布演练）：

| 阶段 | 耗时（量级） | 说明 |
|---|---|---|
| 检出两仓（浅克隆） | ~20-30s | 热路径主要开销之一（瀑布显示曾占 40%，浅克隆优化后显著下降） |
| 影响分析 + 目录解析 | ~2s | 依赖闭包计算极快 |
| 单服务测试 | ~10-30s | 缓存命中态 |
| 单服务构建+推送 | ~30-60s | 层缓存命中，剩余为推送 |
| 工具准备 | ~1s（缓存复用） | 首次约 1-2 分钟 |
| **CI 段合计** | **~2.1 分钟**（实测 126/129s） | |
| GitOps PR 创建→合并 | 数秒~数分钟 | auto-merge 开启后为秒级；演练期含人工合并 |
| Argo CD 检出变更（轮询） | ≤15s | reconciliationTimeout |
| 渲染+diff+apply | 秒级 | |
| 新 Pod 调度/拉镜像/就绪 | ~1-2 分钟 | 镜像已在 ACR、节点可能需拉取 |
| wait-for-release 验证 | 秒~分钟 | 四层验证 |
| **端到端合计** | **约 5 分钟** | 基线端到端约 37 分钟（34.4min 构建 + 部署段） |

### 2.2 检出与影响分析

检出采用浅克隆（`--depth 20`），依据是瀑布分析：全热态下克隆曾占总耗时 40%（87s，仓库历史中存在 371MB 无效大文件）；影响分析只需近 20 个提交的 diff 窗口，SHA 落在窗口外自动回退全量克隆，正确性不受影响。

影响分析的算法与完备性：

1. `git diff --name-status --find-renames base head` 获取变更文件集；
2. 对每个服务以 `go list -deps <服务入口>` 计算运行时闭包，再以 `-deps -test` 计算更宽的测试闭包（含测试专用 import）；
3. 变更文件映射到 Go 包后判定成员资格：运行时闭包内 → 测试+构建；仅测试闭包 → 只测试；`_test.go` → 只测试。

**完备性论证**：判定标准是 Go 工具链自身计算的闭包成员资格（与实际构建同一套 import 解析），对 Go 代码构造上完备；所有无法证明无关的情形（删除/重命名/映射不到包/go.mod/Dockerfile/configs/CI 文件）一律回退全量——错误方向只会多构建，不会漏构建；依赖图按变更后（HEAD）状态计算，"改共享包+增删 import"的组合不会遗漏。

实测验证矩阵（真实工具，golang-ci 容器内执行）：

| 变更场景 | 实测结果 | 验证点 |
|---|---|---|
| `internal/theme/handler.go`（单服务） | 仅选中 theme | 裁剪精确 |
| `internal/middleware/metrics.go`（全员共享） | 13 个服务全部选中 | 闭包正确扩散 |
| `internal/app/bootstrap/http.go`（部分共享） | 扩散至 6 个真实导入方 | 闭包反映真实依赖 |
| `build/Dockerfile.go-service` | 全量 13（保守回退） | 非 Go 输入不遗漏 |
| `go.mod` | 全量 13 | 依赖变更全员重建 |

### 2.3 构建与缓存：两级缓存的责任划分与量化收益

**第一级：宿主侧缓存（go 容器）**。`GOCACHE/GOMODCACHE` 指向 `jenkins-agent-cache` PVC，13 个服务共享同一份编译产物——第一个服务编译完 gin/gorm 后其余直接命中。消融贡献 **-71%（2066s→597s）**。

**第二级：镜像构建侧缓存（BuildKit）**。Dockerfile 中 `RUN --mount=type=cache` 使镜像内编译挂载 buildkitd 管理的缓存卷；构建后 `--export-cache type=local` 导出可复用层至 `buildkit-cache` PVC，下次 `--import-cache` 导入。未用 Registry 缓存的原因：ACR 个人版拒绝 buildkit 的 cacheconfig 媒体类型，实测失败后改为本地持久卷。合计贡献 **-18%**。

**BuildKit 内部执行流程**（深挖话题）：buildkitd 接收 frontend 请求 → 解析 Dockerfile 为 DAG → 每层按内容寻址缓存，输入未变的层直接 CACHED → 输出 manifest 按 digest 推送、tag 为可变指针。全热态 13 服务构建+推送仅 110s，因绝大多数层命中缓存。

### 2.4 GitOps PR：流程与设计依据

流程：克隆 GitOps 仓 → yq 改写 values（字段清单见 1.5）→ 提交 `release/<svc>/<sha>` 分支 → 创建 PR → auto-merge → 获取合并后 revision。

采用 PR 而非直接 push 的依据：① 审计痕迹；② 人工审批挂载点（蓝绿 `manual_promotion`）。当前生效的清单校验位于 Argo 渲染层（chart 内置 `values.schema.json`，实测 `previewReplicaCount` 越界曾被阻断）。

该环节实测修复的三个缺陷：分支名含斜杠导致输出路径不存在（mkdir -p）；alpine/git 无 curl（调用迁移至 curl 容器）；PR 合并判断 `state=='merged'` 永假（GitHub 合并后 state 为 `closed`+`merged:true`，曾致正常发布空等 900 秒）。

### 2.5 专题：Argo CD 的同步机制（高频考点）

三个组件：**api-server**（UI/API/webhook 入口）；**repo-server**（清单渲染：输入仓库/revision/路径/values，执行 helm template 输出最终清单，按 revision 缓存；schema 校验发生在此层）；**application-controller**（对账循环核心）。

**对账循环**：controller 持续对比目标态（repo-server 渲染结果）与实际态（K8s watch 缓存），执行带归一化的 diff（剥离 API Server 补全的默认值与系统字段）→ Synced/OutOfSync → 健康评估（内置 Lua 规则）→ 若 auto-sync 且 OutOfSync 则 apply。

**触发时机**：webhook（即时）或 `timeout.reconciliation` 轮询（默认 180s，本项目 15s）。

| 开关 | 语义 | 本项目配置 |
|---|---|---|
| `automated` | Git 变更后自动 apply；Git 未变时不干预集群 | 开启 |
| `selfHeal` | 集群与 Git 漂移时拉回期望态 | 开启（回退窗口内暂停） |
| `prune` | Git 删除的资源同步删除 | 关闭 |

实测同步时间线：PR 合并 → ≤15s 刷新 → 渲染 → diff → apply → 新 RS → Pod 就绪，合计 2~3 分钟。

### 2.6 部署形态与发布验证

13 个服务分四档 profile：critical-canary（comment/topic/user）、standard-canary（academic/file）、controlled-bluegreen（7 个低流量）、fast-rolling（theme，普通 Deployment）。端到端实测覆盖 theme；Canary/蓝绿经离线验证（详见第七章 7.1）。

**wait-for-release 四层验证**（全部通过才写 stable 记录）：
1. Argo CD Application 同步至指定 revision（确认 Git 状态已应用）；
2. 集群实际运行的镜像 digest 与本次构建产出一致（防"同步了但跑的不是这个镜像"）；
3. Rollout/Deployment 健康（副本就绪）；
4. stable Service `/health` 返回 200（业务进程真实可用）。

stable 记录是回退 L2 的数据源，只能来源于通过验证的正常发布——首日曾因库中无 stable 导致回退降级 L3 并解析到错误目标（见 3.2）。

---

## 第三章 发布失败与回退流程（亮点三主线）

时间线来自 build #70 实测（console 3049 行存于 `benchmarks/evidence/V3/`）：

**1. 失败注入**：`cmd/ecampus-theme/main.go` 加入 `func init(){ panic(...) }`——编译通过、启动即崩、仅影响 theme。

**2. 失败检出**：新 Pod CrashLoop → 阶段 8 健康验证超时 → 进入回退分支。

**3. 回退目标三级解析**（每级依赖的状态都可能缺失）：
- L1 Rollout status 的 stableRS——仅 Rollout 类型存在；theme 为 Deployment，不可用；
- L2 PostgreSQL 最近一次 stable——前提是存在（来源见 2.6）；
- L3 Git 历史上一条 values 变更——最弱：两次坏发布相邻时会解析到上一个坏版本（实测触发过，L2 存在的意义）。

**4. 竞态防护**：回退前置 `selfHeal=false`。窗口期内 Git 仍持有失败版本、集群已恢复 stable，selfHeal 会在数秒内把失败版本重新 apply——回退在补偿 PR 落地前就被撤销。恢复置于 finally，任何路径必执行。**与补偿 PR 的分工：暂停是过程护栏（为 PR 争取时间），PR 是终态修复（消除分歧本身）**；只暂停不 PR，恢复后失败版本立刻回来；只 PR 不暂停，PR 未合并前回退已被覆盖。

**5. 精确切流**：按解析 digest 执行 `kubectl set image` 钉定。不用 `rollout undo` 的原因：undo 仅回退一个 revision，上一 RS 也可能是坏版本（实测 undo 后 verify 等待超时 5 分钟；精确钉定后 3 次通过、约 25 秒）。

**6. 补偿 PR**：values digest 改回 stable → `rollback/<svc>/<sha>` 分支 → PR → 合并 → Argo 同步 → Git 与集群收敛至同一 digest。

**7. 四项断言**（实测全过）：Argo 未重部署失败版本；流量回 stable digest；Git 回退至上一版；两侧 digest 一致。最终 Synced + Healthy。

**设计思考**：先切流再改 Git 是主动选择的最终一致性（集群分钟级恢复服务，Git 随后收敛）；回退走 PR 与正向发布同理（审计+审批+兜底）。

---

## 第四章 告警治理：机制与实测效果（亮点四）

**问题定义**：发布必然伴生 Pod 重启等噪声，无差别通知淹没有效信号；无差别抑制则可能在"发布引发真实故障"时吞掉关键告警。

三层结构，每层解决一个问题：

**第一层：基数纪律**。deploy_id 每次发布均变化，若进入 SLI 标签则每次发布产生孤儿序列。方案：SLI 录制规则仅按 `namespace/service/environment/revision` 聚合；发布身份由独立的 `delivery_platform:pod_release_info` 录制规则承载，告警表达式触发时才 `group_left` 关联。（实测问题：KSM 注解白名单键名 AllowList/Allowlist 两种拼写曾致关联断链，以双拼写兼容解决。）

**第二层：噪声窗作为上下文信号**。"该 deploy_id 下存在创建于 15 分钟内的 Pod" → `ReleaseDeployNoiseWindow` 触发，路由至无通知配置的 receiver——自身永不通知，仅作抑制源。设计含义："正在发布"是事实状态，不是需要通知的故障。

**第三层：Alertmanager 精确抑制**。`equal: [namespace, service, environment, deploy_id]` 四元组全等才生效；抑制目标白名单仅 ReleasePodRestarting；CrashLoop/StuckTerminating/NotReady/ReplicaShortage 出现在抑制目标中则 CI 测试直接失败——"永不抑制清单"由测试锁定。

**机制辨析**：抑制（inhibit）是标签匹配的运行时逻辑；静默（silence）是人工按时间的操作；分组（group_by）决定聚合粒度。

**实测结果**（5 轮演练窗口）：

| 告警 | 触发时长 | 抑制结果 |
|---|---|---|
| ReleaseDeployNoiseWindow | 94 分钟 | active，空路由不通知 |
| ReleasePodRestarting | firing 122 分钟 | 全程 suppressed（同 deploy_id 噪声窗） |
| ReleasePodCrashLooping | firing 44 分钟 | 全程 active，抑制数 0 |

峰值并发 4 条 → 通知面仅剩持续故障一类。**未验证边界**：流量门槛仅至离线测试层级（如实说明）。

---

## 第五章 性能：消融数据与机制解释（亮点一）

**消融阶梯**：

| 档位 | 含义 | 实测 |
|---|---|---|
| EXTREME_COLD 基线 | 全部优化关闭 | 2066s（34.4min） |
| L0 | 共享缓存冷启动 | 586/597s |
| L1 | 仅层缓存热 | 363/182s |
| L2 | 全热全量 | 186/217s（~3.4min） |
| L3 | 全热+裁剪至 1 服务 | 126/129s（~2.1min） |

**两代瓶颈**（瀑布实测）：冷态为逐服务依赖编译（构建段占 95%）；热态转为仓库克隆（曾占 40%，浅克隆后消除）→ 优化后期的主要成本不在构建而在源码获取。

**口径要点**：基线为消融基线（逐项关闭优化实测），非历史存量对比；同负载口径 34.4→3.4min（约 10 倍）；热缓存下任意变更集（1~13 服务）构建落在 2.1~3.4 分钟区间（每追加服务约 +5~7s，层缓存命中仅剩推送），上界即全量。

**影响分析价值的完整表述**：热稳态时间差异为 1.6 倍，其更大价值在于缩小发布范围——风险面与回退半径随之缩小，与第三条（按服务回退）呼应。

---

## 第六章 基础设施：选型依据与实测问题

**k3s 而非 kubeadm**：3 节点单控制面下，k3s 单二进制内置 containerd/etcd(SQLite)/CoreDNS/flannel，运维面最小。Tailscale 组网下 `--flannel-iface/--node-ip/--tls-san` 三参数必须同时显式指定。

**网络层实测问题**：① 安全组拦截阿里云系统服务（内网 DNS 100.100.2.x），症状与 Tailscale 劫持 CGNAT 相似，`ip route get` 排除路由后定位；② 镜像加速三条独立通路（containerd registries.yaml / ctr CLI / 构建容器内网络），预烘焙镜像消除运行时依赖；③ DNS 三种形态：AAAA 优先但无 IPv6 出口（`GODEBUG=netdns=go`）、CoreDNS 单上游瞬时故障（多上游 failover）、故障窗与重试窗重叠（重试间隔 20s）。

**ACR 个人版三类限制**：Bearer token 流程；仓库名不允许斜杠；不接受 buildkit cacheconfig 清单类型。

**资源层结论**：真实内存预算=并行度×单实例内存（两级 OOM 实测）；重负载守护进程探针超时按最繁忙时刻设定（buildkitd 曾被 1s 超时 liveness 误杀）；local-path PVC 为 WaitForFirstConsumer，无 Pod 挂载时 Pending 属正常。

---

## 第七章 技术栈专题（每题 90 秒讲解）

### 7.1 Argo Rollouts 的 Canary 机制
（前提：设计+离线验证，未运行时验证。）Rollout CRD 替代 Deployment，维护 stableRS 与 canaryRS；steps 中 setWeight 控流量、pause 停等；每步挂 AnalysisTemplate——Prometheus provider 查 SLI，绝对阈值+相对 Stable 退化双门禁。AnalysisRun 四终态中 **Inconclusive 是关键**：查询为空注入 NaN（`or on() vector(0)/vector(0)`），NaN 与任何阈值比较均为 false → success/failure 条件同时不满足 → 判 Inconclusive 暂停等人，而非把"无法测量"误判为失败自动回滚。abort 保留 stable 缩 canary；undo 仅回退一步（与 Deployment 同理，回退改精确钉定的同源原因）。

### 7.2 蓝绿与金丝雀的选择逻辑
金丝雀依赖流量做统计验证，低流量样本不足则门禁失效。低流量走蓝绿：Preview Service 探活候选版本（进程级），人工审批切 Active，切后 post-promotion 分析。边界：低流量服务的业务级正确性仍缺自动化验证，蓝绿本质是风险后置+人工裁决。

### 7.3 Prometheus 三层计算
抓取（exporter）→ 录制规则（预聚合降基数，release_info 在此层）→ 告警规则（for 宽限期滤瞬时抖动）。ALERTS 序列可查询（本项目以其做 range 聚合统计抑制）。

### 7.4 OCI 镜像模型
manifest/digest/tag 三者关系；多架构镜像为 index 指向多平台 manifest。实测教训：arm64 构建推 amd64 节点致 `exec format error`，正确做法为 buildx `--platform` 或 `imagetools` 跨仓复制。

### 7.5 Jenkins 的四个非直觉行为
（均为实测）沙箱白名单（非白名单 Groovy 调用被拒）；`withEnv` 赋空串等价删除变量（`set -u` 报错）；parameters 的 defaultValue 仅首次注册生效；CPS 无法序列化任意对象（JsonSlurperClassic 需 @NonCPS）。

### 7.6 PostgreSQL 的角色
非业务库，是发布状态机单一事实来源（releasing/stable/failed）。stable 只能由通过验证的正常发布写入——不变量被破坏时回退链路质量退化（首日实测）。状态机每级信任必须有明确写入者。

---

## 第八章 分层陈述（30 秒 / 2 分钟 / 5 分钟）

**30 秒版**：
"单仓 13 个 Go 微服务的 GitOps 交付平台，部署在三节点 k3s 集群。Jenkins 编排从影响分析到构建推送的 CI 流水线并以 GitOps PR 交接给 Argo CD 同步部署；核心三件事：影响分析配合多级缓存把日常构建压到 2 分钟；回退三级解析加补偿 PR，演练验证 Git 与集群最终一致；发布期告警精确抑制，瞬时噪声全静默、CrashLoop 零漏报。所有数字消融实测。"

**2 分钟版**：30 秒版 + 第一章 Jenkins 职责一句话 + 第二章主线（触发→影响分析→缓存构建→PR→Argo 同步→四层验证落库）+ 性能分解（共享缓存 -71%、预热 -18%、裁剪日常 2.1 分钟）。

**5 分钟版**：再加第三章回退时间线（selfHeal 竞态与暂停/补偿 PR 分工）与第四章告警三层结构（永不抑制清单）。

可主动引导追问的两个方向：selfHeal 竞态（Argo CD 原理+回退设计+实测三层）与两代瓶颈迁移（性能分析方法论）。两者均有完整实测证据。

---

## 附：文档分工

| 文档 | 用途 |
|---|---|
| 本文 | 流程主线、组件原理（含 Jenkins 专章）、设计思考（复习主读） |
| RESUME_AUDIT.md | 简历措辞与实测对照、数字口径、必改项 |
| TROUBLESHOOTING_STORY.md | 排障案例（故障类问题话术） |
| INTERVIEW_QA.md | 问答题库（背诵用） |
| benchmarks/ | 全部数字证据 |
