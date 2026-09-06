# 面试复习讲义：把项目讲成"一条流水线的故事"

> 使用方式：这不是问答集（问答见 INTERVIEW_QA.md），是**叙述版讲义**——按"一次提交的旅程"为主线，
> 每经过一个组件就停下来讲清它的**原理、我们为什么这样设计、实测时发生了什么**。
> 复习方法：通读两遍 → 合上文档对着架构图把主线讲一遍 → 每个深潜小节能脱稿讲 90 秒。
> 所有数字与事件均有据可查（benchmarks/、evidence/、TROUBLESHOOTING_STORY.md）。

---

## 第〇章 30 秒全局图（开场白，先背熟）

```
GitHub app-test（源码+GitOps 单仓）
        │ 手动/API 触发（BEFORE/AFTER SHA）
        ▼
Jenkins（k8s 内，动态 agent Pod：go/buildkitd/yq/git/rollouts/curl 六容器）
  ① 浅克隆源码+GitOps 仓库
  ② 影响分析：git diff + go list -deps 闭包 → 变更服务集（13 → 1）
  ③ 服务目录解析：服务 → 策略/镜像名/values 文件
  ④ 逐服务：go test（共享 GOCACHE/GOMODCACHE PVC）
  ⑤ BuildKit 构建（RUN --mount cache + 本地缓存 PVC）→ 按 digest 推 ACR
  ⑥ yq 改 GitOps values（digest/deploy_id/策略参数）→ release PR → 合并
        ▼
Argo CD（15s 对账，auto-sync + selfHeal）
  helm template（repo-server 渲染）→ 与集群实时状态 diff → apply
        ▼
K8s（Argo Rollouts：canary/蓝绿/滚动 按 catalog 逐服务）
  Pod 带发布身份标签 deploy_id/git_sha/digest
        ▼
Prometheus（录制规则关联发布身份）→ Alertmanager（抑制/路由）
PostgreSQL（发布记录：releasing/stable/failed，回退的 L2 数据源）
```

集群：3 台阿里云（node3 4c16g = k3s server + 平台组件；node1 2c4g = light，跑 PG/平台服务/业务；
node2 2c4g = worker，跑业务/监控），Tailscale 组网，flannel 绑 tailscale0。

---

## 第一章 一次正常发布的完整旅程

### 1.1 触发与检出：为什么是"参数化手动触发"

流水线入参：`SOURCE_REPO / TARGET_ENV / BEFORE_SHA / AFTER_SHA / BUILDKIT_CACHE_TAG / SKIP_RELEASE / EXTREME_COLD / ROLLBACK_PAUSE_SYNC`。
设计初衷是 GitHub webhook 把 before/after SHA 打进来；原型环境没有公网入口，退化为 API 触发——
**这解释了简历里"Webhook"措辞为什么要改**。BEFORE/AFTER 都存在且是祖先关系时走增量影响分析，
否则保守全量（`--all` fallback）——**fail-safe 默认值**是这套设计的底色：分析工具失灵时宁可多构建。

克隆走浅克隆（`--depth 20`）。这不是拍脑袋：瀑布分析显示全热态下克隆占 40%（87s，单仓历史里有
371MB 垃圾大文件），而影响分析只需要近 20 个提交的 diff 窗口；SHA 落在窗口外会自动 fallback 全量。

### 1.2 影响分析：闭包怎么算，边界在哪

`ecampus-impact` 工具做两件事：`git diff BEFORE..AFTER` 拿变更文件集；`go list -deps` 把变更的
package 映射到"依赖它的服务集合"（依赖闭包）。改了 `internal/theme/handler.go` → 只有 theme；
改了共享的 `internal/app/bootstrap` → 闭包扩到所有依赖它的服务。

**完备性（被追问"凭什么不漏"的标准答案）**：判定标准是 **Go 工具链自己算出的闭包成员资格**——
`go list -deps` 的 import 解析与实际构建是同一套逻辑，所以对 Go 代码在构造上完备；所有无法证明无关的
情形（文件删除/重命名/映射不到 Go 包/不在任何服务闭包里的包/go.mod/go.sum/Dockerfile/configs/CI 脚本）
一律**回退全量**——**错误方向只会多构建，不会漏构建**。闭包用的是变更后（HEAD）的依赖图：若某次提交
既改共享包又增删了某服务的 import，该服务自己的源文件也在 diff 里、会经自己的包被选中，不会漏。

实测矩阵（真实工具，golang-ci 容器内验证）：

| 变更 | 实测结果 | 验证点 |
|---|---|---|
| `internal/theme/handler.go`（单服务）| 只选 theme（test+build）| 裁剪精准 |
| `internal/middleware/metrics.go`（全员共享）| **13 服务全选** | 闭包正确扩散 |
| `internal/app/bootstrap/http.go`（部分共享）| 扩散到 **6 个真实导入方**（不是无脑全量）| 闭包=真实依赖 |
| `build/Dockerfile.go-service` | 全量 13（保守回退触发）| 非 Go 输入不漏 |
| `go.mod` | 全量 13 | 依赖变更全员重建 |

加上端到端实测（真实流水线 console `test services: theme`，构建 #52/#56），单服务与扩散两个方向都有据可查。

### 1.3 构建与缓存：10 倍提升的机制拆开讲

**两级缓存，先分清楚谁缓存谁**：

1. **宿主侧（Jenkins go 容器）**：`GOCACHE/GOMODCACHE` 指到 `jenkins-agent-cache` PVC。
   服务测试/编译在这里跑，**13 个服务共享同一份缓存**——第一个服务编译完 gin/gorm，
   后 12 个直接命中。消融实测这是最大头：**-71%（2066s→597s）**。
2. **镜像构建侧（BuildKit）**：Dockerfile 里 `RUN --mount=type=cache,id=ecampus-go-mod`——
   编译进镜像的过程也挂缓存卷（buildkitd 管理的 cache mounts）；构建结束把可复用层
   `--export-cache type=local` 导出到 `buildkit-cache` PVC，下次 `--import-cache` 导入。
   **为什么不是 Registry 缓存**：ACR 个人版拒绝 buildkit 的 cacheconfig 媒体类型，实测撞墙后改的本地。
   消融贡献 **-18%（597s→217s，含跨构建 Go 缓存预热）**。

**一次构建内部发生了什么**（能讲清这个=真懂 BuildKit）：buildkitd 收到 frontend 请求 → 解析 Dockerfile
构建 DAG → 每层按内容寻址（content-addressed）缓存 → 某层输入没变（go.mod 没动、源码没动）直接
CACHED 复用 → 输出镜像 manifest 按 digest 推送（tag 只是指针）。我们全热态 13 服务构建只剩 110s，
因为绝大多数层全 CACHED，只剩推送。

**digest 而非 tag**：推送产出 `sha256:…`，tag（git-短sha）只是别名。回退、验证、GitOps values
全部围绕 digest——**不可变性是回退正确性的地基**（tag 可以被覆盖，digest 永远指向同一份内容）。

### 1.4 GitOps PR：为什么不直接 push main

构建成功后：克隆 GitOps 仓库 → `yq` 把 values 里的 `image.digest/tag/release.deployId/gitSha/策略参数`
全部改写 → 提交到 `release/<service>/<sha>` 分支 → 开 PR → auto-merge。
**为什么绕 PR**：① 审计痕迹（谁在何时把什么放进了环境）；② 人工审批的挂载点（蓝绿服务
`manual_promotion` 在这一步等人）。
**真正在跑的清单校验在哪**：不在 PR（原仓库设计的 kubeconform/conftest PR 门禁在单仓合并时未迁移，
**简历不写**），而在 **Argo 渲染层**——go-service chart 内置 `values.schema.json`，helm template 时
强制校验。这不是理论：实测 `previewReplicaCount: 0` 越界曾在 sync 时被 schema 挡下，修复后才放行。
面试若被问"PR 上有什么检查"，答："审批与审计走 PR；清单合法性由 chart 的 JSON Schema 在 Argo
渲染层强制，实测挡下过越界值。"

**实测踩过的三个坑都在这环节**：分支名带斜杠导致 curl 输出路径不存在（mkdir -p 修复）；
alpine/git 镜像里没有 curl（API 调用挪到 curl 容器）；PR 合并判断 `state=='merged'` 永假
（GitHub 合并后 state 是 `closed`+`merged:true`）。第三个坑让好发布也超时 900 秒——
**"从未端到端跑过"的代码，这类 bug 一定在等你**。

### 1.5 ⭐ 深潜：Argo CD 到底怎么同步（上次被问哑的那题，90 秒版）

Argo CD 三个组件各司其职：

- **api-server**：UI/API/webhook 入口。
- **repo-server**：**清单渲染工厂**。拿到（仓库，revision，路径，values）后执行
  `helm template`（我们的 go-service chart + values 文件），输出最终 K8s 清单。按 revision 缓存——
  同一 commit 渲染一次。 Helm 在这里发生，所以 schema 校验（previewReplicaCount 那个坑）也在这里报错。
- **application-controller**：**对账循环的大脑**。持续 watch 两边：
  - 目标态：问 repo-server 要"Git 里应该长什么样"
  - 实际态：通过 K8s watch 缓存拿"集群里现在长什么样"
  - **diff**（做归一化：剥掉 API Server 补的默认值、系统字段，否则永远不等）→ 得出 Synced/OutOfSync
  - 健康评估（内置 Lua 规则：Deployment available==desired 才 Healthy）
  - 若开 auto-sync 且 OutOfSync → 执行 apply

**触发时机**：webhook（即时）或 `timeout.reconciliation`（默认 180s，**我们设了 15s**——
没有公网 webhook 的补偿，同步检测上界 15 秒）。

**三个容易混的开关（面试高频）**：
- `automated`（auto-sync）：**Git 变了**就自动 apply。Git 没变它不动你。
- `selfHeal`：**集群被人手动改了**（kubectl 改了 live），也拉回 Git 期望态。我们的回退竞态就出在这——
  回退先改集群、Git 还是坏的，selfHeal 会把坏版本"治"回来。所以回退窗口要先 pause。
- `prune`：Git 里删了资源，集群里也删。我们设 false（防误删，代价是孤儿资源要手动清）。

**一次真实同步的时间线**（演练实测）：PR 合并 → 最多 15s controller 刷新 → repo-server 渲染 →
diff 出 Deployment 镜像不一致 → apply → 新 RS 创建 → Pod 拉镜像就绪。合并到 Pod Running 实测 2-3 分钟。

### 1.6 部署形态与发布验证

13 个服务按 catalog 分四种 profile：critical-canary（comment/topic/user）、standard-canary
（academic/file）、controlled-bluegreen（7 个低流量）、fast-rolling（theme，Deployment）。
**实测端到端只验证了 theme（rolling）**——这是简历第二条只敢写"设计+契约测试"的原因。

发布后 `wait-for-release.sh` 做四层验证：Argo synced 到指定 revision → 运行镜像 digest 与预期一致 →
Rollout 健康 → stable Service `/health` 200。全过才把 `stable` 写进 PostgreSQL——
**PG 里的 stable 记录是回退 L2 的数据源，它只能来自一次被验证的好发布**（我们在首日踩过
"库里全是 releasing 没有 stable"导致回退降级到 L3 拿错目标的坑）。

---

## 第二章 发布失败的旅程（亮点三主线）

按时间顺序讲（build #70 实测，全程留证）：

**1. 失败注入**：`cmd/ecampus-theme/main.go` 加 `func init(){ panic(...) }`——编译通过、启动即崩、
只影响 theme。这是"可验证的坏版本"：健康检查必然失败，但失败方式可控。

**2. 失败检出**：新 Pod CrashLoop → wait-for-release 的健康验证超时 → 流水线进入回退分支。

**3. 回退目标的三级解析**（为什么三级：每级依赖上一级可能缺失的状态）：
- L1 `kubectl argo rollouts status` 的 stableRS——**Rollout 类型才有**；theme 是 Deployment，无此状态
- L2 PostgreSQL 最近一次 `stable` 记录——**前提是存在**（见 1.6 的来源说明）
- L3 Git 历史里上一条 values 变更——**最弱的一级**：如果上一次发布也是坏的（两次坏发布相邻），
  它会拿到"上一个坏版本"。实测踩中，这正是 L2 存在的意义

**4. 竞态防护**：先 patch Application `selfHeal=false`（1.5 讲的原理在这里兑现——不关的话，
补偿 PR 合并前 Argo 会把坏版本重新 apply 回来）。finally 里一定恢复。

**5. 切流回 Stable**：按解析出的 digest `kubectl set image` 精确钉定。
**为什么不是 `rollout undo`**：undo 只回退一个 revision——上一个 RS 可能也是坏的（实测：
undo 后 verify-traffic 等 stable digest 等了 5 分钟超时）。精确钉 digest 治愈了这个不确定性，
verify 3 次通过（约 25 秒）。

**6. 补偿 PR**：把 values 的 digest 改回 stable → `rollback/<service>/<sha>` 分支 → PR → 合并 →
Argo 同步 → **Git 与集群收敛到同一个 digest**。最后 resume-selfheal。

**7. 四断言**：Argo 未重部署坏版本 / 流量回 stable digest / Git 回上一版 / 两侧 digest 一致。
实测全过，`Synced + Healthy`。

**讲这个故事的思考层**（面试官爱追问"为什么这么设计"）：
- 为什么先切流再改 Git？**用户影响最小化**：集群先恢复服务（分钟级），Git 一致性是稍后的收敛（最终一致）。
  代价是存在一个"集群≠Git"的窗口——所以需要 4 的防护。这是典型的**故意选择的最终一致性**。
- 为什么回退也走 PR 而不直接 push？同一个答案：审计+门禁+人工兜底点。

---

## 第三章 告警系统：从"标签爆炸"讲到"该静的静、该响的响"（亮点四）

**先讲问题是什么**：发布期告警治理的两难——发布必然产生 Pod 重启/未就绪等"噪声"，无差别通知会淹没人；
无差别抑制会吞掉真故障（发布恰好搞挂服务的场景）。

**我们的解法是三层结构，每层解决一个问题**：

**第一层：基数纪律（Prometheus 侧）**。如果 deploy_id（每次发布都变）进 SLI 序列，每次发布产生一批
新时间序列然后变孤儿。所以：SLI 录制规则只按 `namespace/service/environment/revision` 聚合；
发布身份（deploy_id/git_sha/digest）由独立的 `delivery_platform:pod_release_info` 录制规则承载，
**告警表达式触发时才 `group_left` 关联**——新发布不产生新 SLI 序列。
（实现细节里有个实测坑：kube-state-metrics 的注解白名单键名有 AllowList/Allowlist 两种拼法，
子 chart 版本决定用哪个——join 断链排查了半小时，写成了双拼写兼容。）

**第二层：噪声窗作为"上下文信号"**。规则：该 deploy_id 下存在创建于 15 分钟内的 Pod →
`ReleaseDeployNoiseWindow` firing。它路由到**没有任何配置的 receiver**——自己永不通知，
存在的唯一意义是当抑制源。这是关键设计：**"正在发布"是一个事实，不是一个需要通知的故障**。

**第三层：Alertmanager 精确抑制**。抑制规则四元组 `equal: [namespace, service, environment, deploy_id]`
全等才生效——A 服务的发布窗口不会抑制 B 服务的告警；target 白名单**只有** ReleasePodRestarting。
CrashLoop/StuckTerminating/NotReady/ReplicaShortage 出现在抑制目标里会被 CI 契约测试直接 fail——
**"永不抑制清单"用测试锁死，而不是靠自觉**。

**Alertmanager 机制补充**（面试可能延伸）：抑制(inhibit)是标签匹配的运行时逻辑，静默(silence)是
人工按时间的操作；分组(group_by)决定通知聚合粒度。我们的抑制语义只在"同一次发布"内生效，
正是用标签精确性替代时间窗口模糊性。

**实测结果**（5 轮发布/回退演练窗口，Prometheus range 聚合）：
- ReleasePodRestarting firing 122 分钟，**全程 suppressed**（inhibitedBy 指向同 deploy_id 噪声窗）
- ReleasePodCrashLooping firing 44 分钟，**零抑制**（active 贯穿始终）
- 峰值并发 4 条 firing → 通知面只剩持续故障 1 类

**未验证边界（主动交代）**：流量门槛（≥50 req/5m）与 user_impact 告警只到离线测试层级——
从没起过真实流量（hey 没跑）。被问就答："抑制逻辑实测、门槛逻辑契约测试，流量验证在计划内。"

---

## 第四章 性能：数字背后的机制（亮点一）

**用叙事讲消融**（比背表格有说服力）：

"基线状态下，13 个服务各自为战：每个都独立下载全部 Go 依赖、独立做 cgo 编译、BuildKit 无任何
可复用层——平均每个服务 151 秒，总共 34.4 分钟。第一刀是**共享**：把 GOMODCACHE/GOCACHE 放到
持久卷，13 个服务复用同一份编译产物——第一个服务编译完 gin/gorm，后面全部命中，这一刀 -71%。
第二刀是**跨构建记忆**：BuildKit 的 cache mounts + 本地缓存导出导入，昨天编译过的层今天直接
CACHED——全量构建进入 3.4 分钟。第三刀是**别构建不需要的东西**：影响分析把 13 裁到 1，
日常单服务变更 2.1 分钟。修到后期瓶颈换了位置——全热态下 40% 的时间花在 git 克隆，于是有了
浅克隆这一刀。"

**两代瓶颈**（瀑布实测）：
- 冷态：构建 95%（编译依赖是全部）
- 热态：克隆 40% / 构建推送 51%（构建层全 CACHED，只剩推送）

**被追问数字口径时的三句话**：① baseline 是消融基线（逐项关闭优化实测），不是历史存量；
② 同负载口径 34.4→3.4（10×），日常路径 34.4→2.1（16×，含裁剪效应）；③ 数字出自 Jenkins API，
每档至少两次取中位，evidence 在仓库里。

---

## 第五章 基础设施：三台机器上发生过什么

**k3s 而非 kubeadm**：3 节点单控制面，k3s 单二进制内置 containerd/etcd(SQLite)/CoreDNS/flannel，
运维面最小。节点间 Tailscale 组网（无公网互通），flannel 显式绑 `tailscale0`——**三件套**
（`--flannel-iface/--node-ip/--tls-san`）必须一起钉死，少一个就是 NotReady 或证书不匹配。

**这个环境教我的网络课**（每条都有实战出处）：
- 阿里云安全组会拦自己的系统服务（100.100.2.x 内网 DNS）——症状像 Tailscale 劫持 CGNAT 段，
  `ip route get` 排除路由后才锁定安全组
- 镜像加速是三条独立通路：containerd 的 registries.yaml（管 kubelet 拉镜像）、ctr CLI（默认不走
  mirror）、构建容器内的网络（谁也帮不了，靠预烘焙镜像消灭运行时 apk）
- DNS 是长期幽灵：解析器选了 AAAA 但 Pod 无 IPv6 出口（GODEBUG=netdns=go 治）、
  CoreDNS 单上游瞬时故障（多上游+自愈治）、故障窗撞上重试窗（重试间隔 20s 治）

**ACR 个人版的三副面孔**：v2 API 走 Bearer token 舞蹈（basic 只在换 token 时用）；
仓库名不允许斜杠（缓存仓库拍平成 `buildkit-cache-<svc>`）；不认 buildkit cacheconfig 清单
（registry 层缓存改 PVC 本地）。

**资源教训**：并行度×单实例内存才是真实预算（13 路并行 cgo 编译两层 OOM：go 容器、buildkitd）；
重负载守护进程的探针超时要按"最忙时刻"定（buildkitd 被 1s 超时的 liveness 误杀过）；
local-path 的 PVC 是 WaitForFirstConsumer——没有 Pod 挂载它就 Pending，不是故障。

---

## 第六章 技术栈深挖清单（每个都是 90 秒讲稿，不是背答案）

**Argo Rollouts 的 Canary 机制**（设计过、离线测试过、未线上跑——讲原理要标这个前提）：
Rollout CRD 替代 Deployment，同时维护 stableRS 和 canaryRS；`steps` 里 `setWeight` 控制流量比例、
`pause` 停等；每步挂 AnalysisTemplate——Prometheus provider 查 SLI，比对绝对阈值与相对 Stable 退化
双门禁；AnalysisRun 的四种终态里 **Inconclusive 是关键**：查询为空时注入 NaN（`or on() vector(0)/vector(0)`），
NaN 与任何阈值比较都是 false → 判 Inconclusive 暂停等人，而不是当 0 误判失败自动回滚——
**"测不到"和"测出来是坏的"必须区分**。abort 保留 stable 缩掉 canary；undo 回上一个 RS
（和 Deployment undo 一样只有一步，这是我们回退改用 set image 的同款理由）。

**蓝绿与金丝雀的选择逻辑**：金丝雀依赖流量做统计验证——低流量服务样本不足，门槛形同虚设；
所以低流量走蓝绿：Preview Service 先探活候选版本（进程级健康），人工审批后切 Active，
切完再跑 post-promotion 分析。**承认边界**：低流量服务的业务级正确性其实仍无验证手段，
蓝绿是"风险后置+人工裁决"不是"验证"。

**Prometheus 的三层计算**：抓取（scrape，各 exporter）→ 录制规则（recording，预聚合降基数，
我们的 release_info 就在这层）→ 告警规则（evaluation，`for` 宽限期过滤瞬时抖动）。
`ALERTS` 序列本身可查（我们拿它做 range 聚合统计抑制率）。

**OCI 镜像模型**：manifest（清单）↔ digest（内容的 sha256，不可变）↔ tag（可变指针）。
multi-arch 镜像是一个 index 清单指向多个平台 manifest——我们在 Mac 上推过 arm64 给 amd64 节点，
`exec format error` 教会了我 buildx `--platform` 和 `imagetools` 的正确用法。

**Jenkins 的四个反直觉行为**（都是实测踩的）：沙箱白名单（非白名单 Groovy 调用被拒，需管理员批准或
关沙箱）；withEnv 赋空串等于删除变量（set -u 下爆炸）；parameters 的 defaultValue 只在首次注册生效
（之后改 Jenkinsfile 不更新任务里存的值）；CPS 引擎不能序列化任意对象（JsonSlurperClassic 要包
@NonCPS）。

**PostgreSQL 在这套系统里的角色**：不是业务库，是**发布状态机的单一致源**（releasing/stable/failed）。
它的 stable 记录只能由"验证通过的好发布"写入——这个不变量被破坏时（首日没有 stable），
回退链条的质量就退化。**状态机的每一级信任都要有明确的写入者**。

---

## 第七章 三层讲法（把全文压缩成三档）

**30 秒版**（"介绍一下这个项目"）：
"单仓 13 个 Go 微服务的 GitOps 交付平台，跑在三节点 k3s 上。核心三件事：变更影响分析加多级缓存
把日常构建从半小时级压到 2 分钟；回退做成三级目标解析加 GitOps 补偿 PR，故障演练验证 Git 和集群
最终一致；发布期告警做了精确抑制，瞬时噪声全静默、CrashLoop 零漏报。所有数字消融实测，证据在仓库。"

**2 分钟版**：30 秒版 + 第一章主线（触发→影响分析→缓存构建→PR→Argo 同步→验证落库）+
一句性能拆解（共享缓存 71%/预热 18%/裁剪日常 13→1）。

**5 分钟版**：再加第二章回退时间线（含 selfHeal 竞态发现）和第三章告警三层结构（含"永不抑制清单"）。
**永远留一个钩子**给面试官追问——selfHeal 竞态和"两代瓶颈"是最好的两个，深挖空间大且全有实据。

---

## 附：与其它文档的分工

| 文档 | 用途 |
|---|---|
| 本文 | 叙述主线 + 组件原理 + 思考层，复习主读 |
| RESUME_AUDIT.md | 简历措辞与实测的对照、数字口径、必改点 |
| TROUBLESHOOTING_STORY.md | 排障案例 9+ 个（故障怎么讲） |
| INTERVIEW_QA.md | 问答题库（背诵用） |
| benchmarks/ | 全部数字证据 |
