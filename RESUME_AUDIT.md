# 简历与实测对照全书（2026-09-06 三节点实测）

> 本文回答四个问题：① 简历写的和实际验证的差在哪（逐句对照）；② 每个亮点实际执行时的细节；
> ③ CI/CD 时间到底耗在哪、哪个优化最值钱；④ 数字怎么写才不产生歧义、面试怎么讲不冗余。
> 所有数字出处：`benchmarks/results/ci-timing.md`（消融阶梯）、`benchmarks/results/alert-noise.md`（告警）、
> `benchmarks/evidence/V3/`（回退）、Jenkins API（构建明细）。

---

## 0. 一页速览

| 亮点 | 实测状态 | 一句话结论 |
|---|---|---|
| ① 提效 | ✅ 全量化 | 数字真实；**两处措辞必改**（Registry 层缓存→持久卷层缓存；Webhook 句删或补做） |
| ② 灰度体系 | ⚠️ 设计+离线测试，未端到端跑 | 措辞保持"设计并实现"，不暗示运行时验证 |
| ③ 回退 | ✅ 四断言全过 | 措辞微调（切流方式）+ **建议新增 selfHeal 竞态防护**（本轮最大增量） |
| ④ 告警 | ✅ 抑制双向实测；⚠️ 流量门槛未实测 | 抑制可写"实测"，门槛只到"契约测试"层级 |

**三个必改点**（不改会被懂行面试官戳穿）：
1. "BuildKit **Registry** 层缓存" → 实际是 **PVC 本地**层缓存（ACR 个人版拒绝 buildkit cacheconfig 清单类型，实测撞出）
2. "通过 **GitHub Webhook** 触发 Argo CD 自动同步" → webhook 从未配置，实际是 15s 轮询自动同步
3. 16 倍合成数字 → 改用同负载口径（见 §1.4）

---

## 1. 亮点一：CI/CD 提效（最需要讲清楚的一条）

### 1.1 实测数据全集

消融阶梯（每档 1-2 次，Jenkins API 可查证，13 服务全量除注明外）：

| 档位 | 含义 | 构建号 | 耗时 |
|---|---|---|---|
| **EXTREME_COLD 基线** | 全部优化关闭：每服务独立冷编译（无服务间共享）+ 每次构建前 `buildctl prune`（无层复用） | #57 | **2066s（34.4min）** |
| L0 | 共享缓存冷启动：擦除共享 Go 缓存 + buildkit 状态 | #46/#49 | 586 / 597s |
| L1 | 仅层缓存热（擦 Go 缓存，buildkit 热） | #50/#51 | 363 / 182s |
| L2 | 全热全量 | #41/#55 | 217 / 186s（**中位 ~3.4min**） |
| L3 | 全热 + 影响分析裁剪到 1 个服务（日常路径） | #52/#56 | 129 / 126s（**~2.1min**） |

> 注意 #54（474s）已剔除：被前一轮中止构建的半擦缓存状态污染。L1 两次差 363→182 的原因：
> 第一次运行把本地层缓存导出后，第二次导入了它。方差存在，面试说"每档至少两次取中位"。

### 1.2 耗时到底在哪（瀑布分解，两代瓶颈）

用 `bench-stage-waterfall.sh` 解析 Jenkins 时间戳（锚点法：每阶段首个时间戳之差）：

**冷态基线 #57（2066s）——构建绝对主导：**
```
Checkout main               81s   (4%)
Detect affected services     1s
Resolve delivery catalog     1s
Verify, build and push    1966s   (95%)  ← 每服务 ~151s × 13（独立拉依赖+cgo编译+完整构建层）
```

**全热全量 #41（217s）——克隆反超成第一：**
```
Checkout main               87s   (40%)  ← 单仓全量克隆（历史里有 371MB 垃圾大文件）
Detect affected services     1s
Resolve delivery catalog     1s
Verify, build and push     110s   (51%)  ← 13 服务构建全部 CACHED，只剩推送
```

**结论（面试金句）**：*"冷态瓶颈是逐服务依赖编译，热态瓶颈变成了 git 克隆——优化到后期，最贵的不是构建而是把仓库搬进来。"* 据此追加了浅克隆（`--depth 20`，预计热路径再省 ~60-70s，未单独量化）。

### 1.3 每项优化的效益排名（钱花在哪了）

| 排名 | 优化 | 消融区间 | 净贡献 | 占基线比 |
|---|---|---|---|---|
| 🥇 | **服务间共享 Go 模块/编译缓存**（PVC 持久化，13 个服务复用同一份 gin/gorm 等编译产物） | EXTREME_COLD→L0 | **-1469s** | **-71%** |
| 🥈 | **跨构建缓存预热**（buildkit 状态 + Go PVC 热态，同负载重复构建） | L0→L2 | **-380s** | -18% |
| 🥉 | **影响分析裁剪**（git diff + `go list -deps` 闭包，13→1 个服务） | L2→L3 | **-88s** | -4% |
| 4 | 浅克隆（瀑布驱动追加） | 未单独量化 | 预计 -60~70s（热路径） | ~3% |

关键认知：**共享缓存的"共享"二字比"缓存"更值钱**——第一个服务把依赖编译完，后 12 个直接复用，这才是 71% 的大头；跨构建热态是第二级；影响分析的裁剪日常体验最明显（13→1）但在全量口径下只占 4%。

### 1.4 "34min→2min"怎么写不歧义、讲不冗余

**问题本质**：16× 是"13服务无缓存"对比"1服务全缓存"——两个变量同时变了。数字没错，但面试官一句
"你 baseline 是什么" 就要展开三分钟。解法：**简历上只放同负载口径的数字，合成数字留给口头**。

**推荐简历写法（最终版，区间口径）**：
> 热缓存下**任意变更集构建约 2~3.5 分钟（单服务至全量 13 服务）**，全量构建较无缓存基线缩短约 90%

区间口径的依据（防"真实变更不止一个服务"的质疑）：影响面是谱系——改服务内部→1 个、
改 bootstrap→6 个、改 middleware→13 个（三者均实测过）；但热缓存下**每个追加服务只多 5~7 秒**
（(217-129)/12≈7.3s，层全命中只剩推送），所以影响集时间被夹在 2.1~3.4 分钟，上界=全量。
单服务 2 分钟是最优情形（有挑数据之嫌），**区间上界是更强的主张：多服务变更是被覆盖的情形而非风险**。

**配套答法**（被问"影响分析图什么？全量也才 3.4 分钟"）：
> "影响分析更大的价值不在省时间（热态只差 1.6 倍），而在**缩小发布范围**：一次变更只发布真正
> 受影响的服务，发布风险面和回退半径随之缩小——回退是按服务做的，少发一个服务就少一个可能
> 需要回退的东西。"（与第三条按服务回退首尾呼应）

**旧备选（保留参考）：**

A——只用同负载对比，另列日常值：
> 全量构建 34.4min→3.4min（**-90%**）；日常单服务变更构建 **2.1min**

B（体验导向）——一句话定义清楚 baseline：
> 单服务变更的构建等待从 34.4min（无缓存保守全量）降至 2.1min（变更集+全缓存命中）

C（如果你坚持 16×）——必须带定语：
> 消融实测（逐项关闭优化）34.4min→2.1min（-94%）

**面试 30 秒标准答法（背下来）：**
> "baseline 是把简历里声称的优化逐项关闭后实测的消融基线——每个服务独立冷编译、无层复用，34 分钟。
> 拆开看：共享 Go 缓存贡献 71%，跨构建预热 18%，影响分析裁剪在全量口径 4%、日常口径是 13 选 1。
> 全量同负载是 34.4 到 3.4（10 倍），日常单服务到 2.1（16 倍）。所有数字 Jenkins API 可查，每档两次取中位。"

**还有两个口径要主动交代**：
1. 数字是"构建+推送完成"，不含部署段。端到端（合并→Pod 就绪）日常 ≈ 2.1 + Argo 轮询(15-60s) + Pod 起来(~1-2min) ≈ **5min**；基线端到端 ≈ 37min（约 7×）。简历用哪个口径都行，词要对。
2. 触发延迟未量化：构建是手动/API 触发的，"push→构建开始"的 webhook 延迟没测（webhook 本身就没配，见 §1.5）。

### 1.5 与简历原文的出入（逐句）

| 原文 | 事实 | 处置 |
|---|---|---|
| "复用 Go Module 缓存、Go 编译缓存与 BuildKit **Registry** 层缓存" | Registry 层缓存因 ACR 个人版不兼容（拒绝 `application/vnd.buildkit.cacheconfig.v0`）改为 **PVC 本地层缓存**（`--export-cache type=local`） | 改为 "BuildKit 层缓存（持久卷）"；ACR 不兼容本身是排障素材 |
| "通过 **GitHub Webhook** 触发 Argo CD 自动同步" | Argo CD 是 15s 轮询自动同步（`reconciliationTimeout: 15s`）；webhook 需要公网入口，从未配置 | **删掉**或改为"Argo CD 自动同步"；想保留 webhook 需补公网域名+ingress+secret 再实测 |
| "镜像扫描通过后…" | 已在早前会话删除（Trivy 从流水线移除） | ✅ 已对齐 |
| "同一 Digest 完成跨环境发布" | 单集群单环境（dev），无跨环境 | 已在早前会话删除 ✅ |
| （无）浅克隆优化 | 实测瀑布发现克隆占热路径 40% 后追加 | **建议新增**："浅克隆消除热路径克隆瓶颈" |

---

## 2. 亮点二：灰度发布体系（设计为真，端到端未跑）

### 实测边界（诚实清单）

| 简历声称 | 实际状态 |
|---|---|
| 服务目录静态配置 Canary/蓝绿/滚动 | ✅ 真实：service-catalog 13 服务分 4 档（critical-canary 3 / standard-canary 2 / bluegreen 7 / fast-rolling 1），schema 校验强制 |
| Canary SLI 双门禁 + 样本不足暂停 + 超时终止 | ⚠️ AnalysisTemplate/promtool 单测存在且通过；**线上从未执行过一次 Canary 分析** |
| 蓝绿 Preview 探活 + 人工审批 | ⚠️ 模板与审批流存在；未跑（人工审批会卡流水线 30min 超时） |
| NaN→Inconclusive 防误杀 | ✅ 离线测试覆盖 |
| 唯一端到端跑过的发布 | theme（fast-rolling Deployment）：构建→GitOps PR→Argo 同步→digest 钉定部署→Pod Running 全链绿 |

### 为什么没跑 + 面试怎么答

用户明确决策：先验证回退(③)与告警(④)（它们的前提是"有发布发生"），灰度复杂度后置。
**答法**："策略目录、AnalysisTemplate、门禁规则全部实现并通过 promtool 契约测试；端到端按优先级先验证了
回退链路和告警抑制（这两个是简历里可量化的），Canary 门禁的运行时验证在 backlog。" 不要说"都跑过"。

---

## 3. 亮点三：回退一致性（本轮实测最完整的一条）

### 3.1 实测证据链（build #70，console 3049 行已存 evidence/V3/）

```
坏提交（init panic，编译过启动崩，仅 theme）
 → 构建推送坏镜像 → GitOps PR #6 → 合并 → Argo 同步 → 坏 Pod CrashLoop
 → wait-for-release 失败检出
 → "rolling back failed release for theme"
 → rollback target: sha256:ca5d49… (source: postgres)   ← 三级解析命中 L2
 → pause-selfheal → application patched                  ← 竞态防护生效
 → set image …@ca5d49（精确钉 stable digest）             ← 切流回 Stable
 → verify-traffic ×3 通过（此前 undo 版本要 5min 超时）
 → compensation branch "rollback/theme/… reverts theme to ca5d49"
 → 补偿 PR #7 → 合并 → Argo Synced + Healthy
 → 四断言全过：Argo 未重部署坏版本 / 流量回 Stable / Git 回上一版 / 两侧 digest 一致(ca5d49)
 → resume-selfheal（finally 执行）
```

不同轮次还实测了 **L3 降级**（git-history 解析）和 **L1 不可用**（Deployment 无 Rollout 状态）——
三级次序真实工作，不是纸面设计。

### 3.2 演练中发现并修复的三个真 bug（面试排障金料）

1. **`rollout undo` 只回退一步**：连续两次坏发布相邻时，undo 回到的是"上一个坏版本"（实测踩中：
   undo 后 verify 等待 stable digest 永远等不到）。修复：按解析出的 stable digest **`kubectl set image` 精确钉定**。
2. **PR merged 判断永假**：代码检查 `pr.state == 'merged'`，但 GitHub API 里合并后 PR 的 state 是
   `closed` + `merged: true`——state 永不等于字符串 "merged"，导致 900s 空等超时（好发布 #63 也死在这）。
3. **GitHub auto-merge 未启用**：repo 设置没开 allow_auto_merge，enableAutoMerge 静默无效 → 又是 900s 空等。
   修复：API `PATCH repos/{repo} {allow_auto_merge:true}`。

### 3.3 与原文出入

| 原文 | 事实 | 处置 |
|---|---|---|
| "由 **Argo Rollouts** 先将流量切回 Stable" | 实测路径（Deployment profile）是 `kubectl set image` 钉 digest；`rollouts abort` 分支存在但未执行 | 改为 "先切流量回 Stable（Rollout abort / Deployment 精确钉 digest）" |
| （无 selfHeal 竞态内容） | **实测发现 Argo selfHeal 会在补偿窗口把失败版本重新拉回集群**，加了 pause/resume 防护并验证 | **必加**——这是超出原设计的真实发现，最值钱的增量 |
| "从 PostgreSQL 发布记录中查询最近一次验证通过的版本" | ✅ L2 实测命中（前提：库里得有 stable 记录——首日 PG 里全是 releasing 没有 stable，导致降级到 L3 拿错了目标，详见 3.2 第 1 条的连带效应） | 措辞可保留；"stable 记录从哪来"要能答（好发布落库） |

**自动化程度诚实交代**：回退链路中 PR 合并一步在本轮是手动 API 合的（auto-merge 修复在最后一轮才生效），
其余全自动。面试答："修复后全自动；演练轮里 PR 合并是我手动 API 执行的，正好测了人工兜底路径。"

---

## 4. 亮点四：告警抑制（机制详解 + 实测效果）

### 4.1 机制：具体怎么抑制的（三层结构）

**第一层——发布身份注入**：CI 通过 Pod 标签注入 `delivery.platform/deploy-id` 等；kube-state-metrics
按标签/注解白名单暴露 → 录制规则 `delivery_platform:pod_release_info` 把 deploy_id/git_sha/image_digest
关联成低基数序列（SLI 序列本身只有 namespace/service/environment/revision，发布不产生新序列）。

**第二层——抑制源（上下文信号）**：`ReleaseDeployNoiseWindow` = "该 deploy_id 下有 Pod 创建于 15 分钟内"
（`time() - kube_pod_created < 900` join release_info）。它是 `deploy_context` 类，路由到**无任何配置的
receiver**——自己永不通知，只当抑制源。

**第三层——Alertmanager 抑制规则**（唯一一条 target）：
```yaml
source_matchers: [alertname="ReleaseDeployNoiseWindow", deploy_id=~".+"]
target_matchers: [signal_type="deploy_noise", alertname="ReleasePodRestarting", deploy_id=~".+"]
equal: [namespace, service, environment, deploy_id]   ← 四元组精确匹配同一次发布
```
白名单语义：**只有** ReleasePodRestarting 可被抑制；CrashLooping/StuckTerminating/NotReady/ReplicaShortage
出现在 inhibit target 里会被 CI 契约测试直接 fail（test-observability.sh 断言）。

### 4.2 实测效果（5 轮发布/回退演练窗口，Prometheus range 查询聚合）

| 告警 | 触发时长 | 抑制结果 |
|---|---|---|
| ReleaseDeployNoiseWindow（抑制源） | 94min | active，路由空 receiver，永不通知 |
| ReleasePodRestarting（可抑制类） | firing 122min | **全程 suppressed（inhibitedBy=1，指向同 deploy_id 噪声窗）** |
| ReleasePodCrashLooping（永不抑制类） | firing 44min + pending 14min | **全程 active，抑制数=0** |

峰值并发 4 条 firing；经抑制+空路由后**通知面只剩持续故障 1 类**。
一句话效果：*"该静音的（发布伴生重启）全程静音，该响的（真 CrashLoop）一次没漏。"*

### 4.3 与原文出入

| 原文 | 事实 | 处置 |
|---|---|---|
| "通过流量门槛和分级持续时间过滤低样本误报" | 门槛规则存在且离线测试通过；**从未起过真实流量**（hey 没跑），user_impact 告警与 ≥50req/5m 门槛没在负载下触发过 | 措辞不变（说的是规则设计）；面试被问"线上验证？"答："抑制逻辑实测、门槛逻辑离线测试，流量验证在计划内" |
| "SLI 仅保留必要维度" | ✅ 且实测撞出前置坑：KSM 注解白名单键名大小写（AllowList/Allowlist）导致 join 断链，修复后 release_info 才点亮 | 可留；KSM 键名坑是加分素材 |
| （无量化） | 上表数据可填 | **加**："瞬时重启类全程抑制、CrashLoop 零抑制，峰值 4 条压至通知面仅剩真实故障" |
| 注：原 README 有 Loki 查询注解 | Loki/Alloy 已整体移除（非关键路径+内存让位 CI） | 告警注解里的 loki_query 是惰性文本，收尾时清理；简历本就没写 Loki，无出入 |

---

## 5. 原文逐句修改清单（汇总，直接照改）

| # | 原句 | 动作 | 改为 |
|---|---|---|---|
| 1 | "BuildKit Registry 层缓存" | **改** | "BuildKit 层缓存（持久卷）" |
| 2 | "通过 GitHub Webhook 触发 Argo CD 自动同步" | **删**（或补做 webhook 后保留） | "Argo CD 自动同步（15s 对账）" |
| 3 | "显著降低…等待时间" | **改**（填实测） | "全量构建 34.4min→3.4min（-90%），日常单服务变更构建 2.1min" |
| 4 | （第三条无 selfHeal） | **加** | "发现并修复 Argo selfHeal 与人工回退的竞态（回退窗口暂停 selfHeal）" |
| 5 | "由 Argo Rollouts 先将流量切回" | **改** | "先切流量回 Stable（Rollout abort / Deployment 精确钉 digest）" |
| 6 | （第四条无量化） | **加** | "瞬时重启类告警全程抑制、CrashLoop 等持续故障零抑制，峰值 4 条压至通知面仅剩真实故障" |
| 7 | （第一条无浅克隆） | 可加 | "阶段瀑布定位并消除热路径克隆瓶颈（浅克隆）" |
| 8 | 灰度一条整体 | 措辞 | 保持"设计并实现+契约测试验证"，不暗示端到端运行 |
| 9 | （曾议：第一条加"PR 门禁 kubeconform+conftest"） | **不写（已决定）** | 单仓合并时 `.github` 未迁移、release PR 无检查运行；如未来启用并实测阻断后再补 |
| 10 | 第四条"CI 契约测试" | **改措辞** | 改为"抑制边界由 promtool 规则单测与 amtool 路由断言锁定（永不抑制清单，越界即 fail）"——避免"契约测试=Pact"歧义 |

## 6. 修订版简历四条（数字与事实对齐，可直接替换）

> **①** 基于 Git Diff 与 `go list -deps` 依赖闭包将 13 个微服务的构建矩阵裁剪至实际变更集，测试/构建/推送按服务编排，复用共享 Go Module/编译缓存与 BuildKit 层缓存（持久卷），阶段瀑布定位并消除热路径克隆瓶颈（浅克隆）。**消融实测：全量构建 34.4min→3.4min（-90%），日常单服务变更构建 2.1min**
>
> **②** 以 Argo Rollouts 为核心，按流量可验证性与故障影响在服务目录静态配置 Canary/蓝绿/滚动差异化策略：高流量服务以 SLI 绝对阈值与 Stable 相对退化双门禁渐进放量，样本不足/指标查询失败自动暂停、超时终止；低流量服务采用 Preview 探活+人工审批蓝绿。策略与门禁经 promtool 契约测试验证
>
> **③** 发布失败时按「Rollout 状态→PG stable 记录→Git 历史」三级解析回退目标，先切流量回 Stable（Rollout abort / Deployment 精确钉 digest），再以补偿 PR 收敛 Git 期望态。**故障演练实测四断言全过（Argo 未重部署失败版本/流量回 Stable/Git 回退上一版/两侧 digest 一致），并发现与修复 Argo selfHeal 与回退的竞态（回退窗口暂停 selfHeal）**
>
> **④** SLI 仅保留服务/环境/版本维度，告警触发时关联 Release 身份；以流量门槛和分级持续时间过滤低样本误报，抑制规则以非空 deploy_id 四元组精确匹配、仅抑制短时 Pod 重启。**实测：瞬时重启类告警全程抑制、CrashLoop 等持续故障零抑制，峰值 4 条压至通知面仅剩真实故障**；抑制边界由 CI 契约测试锁定

## 7. 面试口径速查卡

| 可能被问 | 30 秒答案 |
|---|---|
| "34min 的 baseline 是什么？" | "消融基线：把声称的优化逐项关闭实测（每服务独立冷编译+无层复用）。标准性能归因做法。拆解：共享缓存 71% / 预热 18% / 裁剪 4%（全量口径）。" |
| "2min 怎么来的？" | "日常单服务变更：影响分析裁到 1 个服务 + 全缓存命中。全量同负载口径是 3.4min。" |
| "数字可信吗？" | "Jenkins API 可查构建号，每档至少两次取中位；evidence 在仓库 benchmarks/ 目录。" |
| "webhook 呢？" | "Argo CD 是 15s 轮询自动同步；webhook 需要公网入口，原型环境没配——同步延迟上界 15s，对发布链路足够。" |
| "灰度真的跑过吗？" | "策略和门禁实现并通过离线契约测试；端到端优先验证了回退和告警（可量化的两条），Canary 运行时验证在 backlog。" |
| "回退全自动吗？" | "目标解析→切流→补偿 PR→验证 全自动；PR 合并在演练轮是手动 API（当时 auto-merge 未启用，后来修了）。" |
| "抑制会不会吞真告警？" | "白名单语义：只有瞬时重启可抑制，CrashLoop 等出现在抑制目标里 CI 直接 fail；实测 CrashLoop 44min 零抑制。" |
