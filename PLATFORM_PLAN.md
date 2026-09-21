# DevOps 运维平台整体规划

> 2026-09 起，仓库从「k3s 集群 + GitOps 渐进式交付系统」转向「应用运维平台」。
> 本文是整体蓝图：分层架构、逐项取舍、子系统设计、分阶段路线图。
> 现状基线：灰度发布（Argo Rollouts Canary/BlueGreen/Analysis）与集群回退
> （rollback-release、补偿 PR、selfHeal 竞态防护）已全部移除，保留最小 CICD
> 流水线（构建 → GitOps PR → 部署等待验证 → 发布记录）。
>
> **项目定位（2026-09 修订）**：本项目最终产出是一段可讲的项目经历（目标
> 岗位：腾讯运营开发工程师，DevOps/AIOps 平台方向），按真实生产标准建设、
> 跑完测试并沉淀数据后退订资源。因此每个组件必须满足三件套：需求背景叙事、
> 数据流终点是平台可见功能、可复现的测试/压测数据。装了没用的组件不写上简历。

---

## 1. 现状与这次清理的结果

保留下来的基础流水线（单一发布通道，无灰度、无自动回退）：

```
git push → Jenkins（影响分析 → 测试 → BuildKit 构建推送 digest）
        → GitOps PR（yq 写 values）→ auto-merge
        → Argo CD sync → Deployment 滚动更新
        → wait-for-release.sh 验证（Argo CD revision / 运行 digest /
          Deployment 就绪 / Prometheus SLI / /health 探活）
        → PostgreSQL release 记录（releasing → stable / failed）
        → 失败时 ReleaseFailed 告警；恢复 = git revert + 重新发布
```

删除范围：Rollout/AnalysisTemplate chart 模板、四档发布 profile、蓝绿人工审批
stage、rollback-release.sh 及其 fixtures、补偿 PR / selfHeal 暂停编排、
argo-rollouts 控制器（Application + values）、AnalysisRun/RolloutDegraded 告警、
rollout RBAC 权限、release 记录中的 rollout_strategy 字段。

## 2. 总体架构：IaaS → PaaS → SaaS

```
┌─────────────────────────── SaaS：面向业务的运维门户 ───────────────────────────┐
│  React + TypeScript 门户                                                        │
│  应用总览/拓扑 │ 发布与流水线 │ 监控大盘(Grafana嵌入) │ 告警中心 │ 巡检报告/周报 │
│  日志检索(CH) │ 慢接口/链路页(自研,CH) │ 工单/审批 │ 服务自助接入 │ 权限管理      │
└───────────────────────────────────┬─────────────────────────────────────────────┘
                                    │ REST/WS（Gin API）
┌─────────────────────────── PaaS：平台核心（Go/Gin）───────────────────────────┐
│  ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌──────────┐ ┌─────────┐ ┌────────────┐  │
│  │服务目录  │ │CICD 编排 │ │监控聚合  │ │告警降噪   │ │巡检引擎  │ │报表/周报    │  │
│  │(CMDB-lite)│ │(触发/状态)│ │(Prom API)│ │(AI 盯盘) │ │(定时任务)│ │(定时生成)  │  │
│  └─────────┘ └─────────┘ └─────────┘ └──────────┘ └─────────┘ └────────────┘  │
│  PostgreSQL（元数据/记录）  Redis（缓存/会话/轻量队列）  MinIO（制品/报告/快照） │
└───────────────────────────────────┬─────────────────────────────────────────────┘
                                    │ 驱动 / 采集
┌─────────────────────────── IaaS：基础设施（k3s 三节点集群）────────────────────┐
│  Jenkins（构建引擎）  Argo CD（GitOps 引擎）  镜像仓库                          │
│  Prometheus + Alertmanager + Grafana                                           │
│  可观测数据管道：Fluent Bit / OTel → Kafka → ClickHouse（日志/事件/指标存储）    │
│  链路：OTel SDK → collector → Jaeger（起步）/ ClickHouse（自研分析）            │
│  MinIO（对象存储）  PostgreSQL  阿里云 OpenAPI（ECS/ACR/OSS，资源纳管与归档）   │
│  13 个 ecampus 业务服务（Deployment + Ingress）                                 │
└─────────────────────────────────────────────────────────────────────────────────┘
```

关键原则：**平台是编排与门面，引擎留在底层**。Jenkins 继续做构建、Argo CD 继续
做部署，平台不重写它们，而是调用它们的 API 并统一呈现。这与企业内部平台的
普遍做法一致（见 §9）。

## 3. 集群资源规划

结论：**1×4c16g（control）+ 2×4c8g（agent），共 12c32g**。若维持
1×4c16g + 2×2c4g（8c24g）则只能跑精简栈（见下表 node2 顶到上限）；「总共只开
两台 4c8g」（8c16g）不推荐：总内存反而更少，构建高峰挤压业务 Pod，且失去
三节点调度域。

稳态内存预算（目标全栈）：

| 节点 | 配置 | 主要负载 | 稳态估算 |
|---|---|---|---|
| node1 | 4c16g，control + 构建 | k3s/etcd ~1.2G、Argo CD ~0.7G、Jenkins ~1G、PostgreSQL ~0.75G、platform-server+web ~0.4G、Redis ~0.2G、MinIO ~0.6G、Kafka（KRaft 单节点）~1G、ClickHouse ~2.5G | ~8.5G；**构建时再 +3~9G（go 容器 + buildkitd burst），压测窗口与构建错峰** |
| node2 | 4c8g，agent | Prometheus+Alertmanager+Grafana+KSM ~1.8G、约一半业务服务 ~0.9G、Fluent Bit+otel-collector ~0.45G | ~3.2–4.5G（**2c4g 下这格爆掉，是升级主因**） |
| node3 | 4c8g，agent | Jaeger all-in-one ~0.5G（span 进 CH 自研时为 0）、另一半业务服务 ~0.8G、k6 压测器与 HPA burst 余量 | ~1.5–3G |

要点：
- 瓶颈是**小节点单点内存**而非总 CPU：4G 节点放不下 ClickHouse（~2.5G）、
  Kafka（~1G）这类大块头，调度碎片化；8G 节点大 Pod 随便落，也为压测期间的
  HPA 扩容、Grafana 快照渲染、AI 判断并发留余量。
- 磁盘：node1 独立数据盘 80–120G（buildkit 缓存 PVC 12G + ClickHouse/Kafka/
  MinIO 各 20–40G）；node2/3 各 40–60G；观测数据 30 天留存约 20–40G。
- 降级顺序（若中途要省资源）：Kafka 与 ClickHouse 挪 node1 与构建错峰 →
  Jaeger 不部署（span 直接进 CH）→ 砍 MinIO 用本地 PV。
- Flink（可选加分项，见 §6.5）不常驻：压测窗口临时拉起 session cluster
  （~1.5–2G），跑完数据即回收。

## 4. 技术选型

| 层 | 选型 | 说明 |
|---|---|---|
| 前端 | React 18 + TypeScript + Vite + Ant Design | 主选 React；不主动引入 Vue，除非某个独立子工具（如某可视化大屏）有现成 Vue 生态组件 |
| 后端 | Go + Gin + GORM | platform/server 已有雏形；已有 releasestore 的 pgx 原生 SQL 可与新模块共存，重构时顺手迁移 |
| 元数据库 | PostgreSQL | release 记录已在使用 |
| 缓存 | Redis | 会话、API 结果缓存、巡检任务锁、平台内部轻量队列（Stream） |
| 对象存储 | MinIO | 构建产物、周报/巡检报告、Grafana 面板快照、备份归档 |
| 事件总线 | Kafka（KRaft 单节点，Bitnami/Strimzi chart） | 可观测数据管道的传输层；多消费者解耦（日志入库、告警聚合、AI 分析） |
| 分析存储 | ClickHouse（单副本） | 日志/事件/指标长期统一存储，替代 ES；支撑慢查询分析、周报聚合、AI 根因检索 |
| 任务调度 | Go cron（robfig/cron）+ DB 任务表 | 平台内定时任务（周报、巡检） |
| 数据采集 | Fluent Bit（日志）+ OTel SDK/collector（链路/指标）+ Python 聚合脚本（周报数据源） | Python 只写聚合分析脚本，由平台调度执行 |

## 5. 逐项取舍（对应初始 12 条想法 + 2026-09 修订）

| # | 想法 | 决定 | 理由 |
|---|---|---|---|
| 1 | 删灰度+回退，保留基础流水线 | ✅ 已完成 | 本仓库已执行，见 §1 |
| 2 | React+TS / Gin+Go 全栈 | ✅ 采纳 | React 为主线，Vue 仅按需 |
| 3 | CICD 与平台的交互 | ✅ 集成而非重写 | 见 §6，平台第一优先级 |
| 4 | Prometheus+Python 可视化、Grafana 面板、周报推送 | ✅ 采纳，形态调整 | Grafana 负责人看的大盘，平台负责「聚合→报告→推送」的自动化 |
| 5 | Alertmanager + AI 多模态盯盘降噪，飞书机器人 | ✅ 采纳，分三道闸 | 见 §7.2；AI 只降噪不拦截 |
| 6 | ELK/EFK + OTel/SkyWalking 解决慢接口 | ✅ 采纳，两处修订 | 存储：Fluent Bit→Kafka→CH 替代 EFK（对齐大数据条线且省 ~4.5G）；链路：OTel 埋点必要，**SkyWalking 非必需**——Jaeger 起步、演进到 span 进 CH 自研慢接口页，见 §7.3 |
| 7 | 自动化巡检 | ✅ 平台内置巡检引擎 | 见 §7.4 |
| 8 | 大数据组件 | ✅ **修订为引入**（原判定"不引入"基于真实上线成本；项目定位改为简历导向+对齐腾讯运营开发岗 JD 后改判） | 引入的是轻量可观测数据管道（Kafka+ClickHouse，可选 Flink），不是 Hadoop 系；需求叙事与数据量估算见 §7.5；纪律：每组件必有三件套（叙事/数据流到平台功能/压测数据） |
| 9 | 消息队列 / 云存储 | Kafka 随管道引入（P4），MinIO 一期就上 | Kafka 的消费者：日志入库、告警聚合、AI 分析；平台内部轻量异步仍走 Redis Stream，不上第二个 MQ |
| 10 | 捋清 IaaS/PaaS/SaaS 分层 | ✅ 见 §2 | 平台本体在 PaaS 层 |
| 11 | 计划与取舍 | 本文即是 | — |
| 12 | 企业云平台交互模式调研 | ✅ 见 §10 | Backstage / 蓝鲸 / KubeSphere / Argo 等 |

## 6. CICD 与平台怎么交互

底层引擎保留，平台必须拿到「触发 + 状态 + 历史」三样，否则发布出问题时
还是登 Jenkins 看蓝白屏，平台价值大打折扣。分两步走：

**Phase 1（低成本，纯集成）**
- 触发：平台后端调 Jenkins REST API（buildWithParameters），带
  BEFORE_SHA/AFTER_SHA/SERVICE 参数；Web 端「发布」按钮即触发。
- 状态回传：Jenkins 已有的 `release-record` CLI 把 releasing/stable/failed
  写进 PostgreSQL——平台直接读；再加 pipeline 结束时的 webhook 推送实时
  stage 进度。
- 呈现：每次发布的 stages、digest、GitOps PR 链接、验证结果；Argo CD /
  Jenkins UI 深跳。**发布历史页提供一键 revert PR**（生成 revert 分支按钮，
  纯 Git 操作不是集群操作），作为删除自动回退后的轻量补偿。

**Phase 2（可选演进）**
- 平台内建流水线编排（stage DAG 用平台 API 描述），Jenkins 退化为纯构建
  执行器；或换 Argo Workflows/Tekton。
- 发布审批工单进平台，替代 GitHub auto-merge 通道。

不要做：在平台里重新实现构建/部署逻辑。引擎与编排分离是行业共识。

## 7. 子系统设计要点

### 7.1 监控与周报
- 数据面不变：Prometheus 采集（ecampus SLI 规则、kube-state-metrics、
  node-exporter、容器资源指标）。
- Grafana：面向人的交互式大盘，平台 iframe 嵌入 + 反向代理统一鉴权。
- 周报（平台 cron，周一 09:00）：Go 调度 → Python 聚合脚本（Prometheus
  range API + PG 发布记录 + ClickHouse 告警/日志统计）→ HTML 报告（可用性、
  错误率、P95、发布成功率、资源水位 TOP、告警 TOP、巡检摘要、AI 复盘段）
  → 存 MinIO 归档 + 飞书机器人/邮件推送指定部门。
- 降级：任一数据源失败标记数据缺口，不整体失败。

### 7.2 告警降噪 + AI 盯盘（飞书）
```
Alertmanager（第一道：grouping / inhibition / 静默——已有 deploy-noise 抑制）
   ↓ webhook（平台 /api/alerts/webhook）
告警网关（Go，第二道）：
   1. 富化：deploy_id→发布记录、近 1h 同服务告警、最近变更（CH 检索）
   2. 规则闸：同源聚合、抖动检测（N 分钟内 M 次起落才算真）
   3. AI 盯盘（第三道，只处理规则闸拿不准的抖动）：
      - 输入：告警上下文 + 指标近 30-60min 时序 + Grafana Render API 面板 PNG
      - 输出：{degrade: bool, confidence, reason}
      - degrade=true 或 confidence 低 → 照发；否则降级为日报摘要
   4. 发送：飞书机器人（签名校验），卡片含发布关联/趋势判断/处理建议
```
红线：**AI 只能降噪，不能拦截**。高危（CrashLoop、磁盘>90%、节点 NotReady）
与 user_impact 类直达飞书。AI 判决落库（ClickHouse），周报复盘误杀/漏报率。

AI 形态（当前只做盯盘判决，根因 agent 暂缓）：
- **盯盘判决 = 单次多模态结构化调用**，不做 agent 循环。理由：告警在关键
  路径上要秒级返回；判决需可重复评估（历史告警回归出误杀/漏报混淆矩阵）；
  高频入口要控成本。输入同时给图（Grafana 面板 PNG，趋势形态）和数
  （采样点+斜率/p95 变化量，防看图幻觉）；JSON schema 输出；LLM 失败或
  输出不合规时 fail-open 照发；同组告警 N 小时内复用判决。
- **告警日志摘要（并入盯盘证据包，P5）**：告警后在海量日志里人工找错误
  很低效，提效分三层——检索（代码：CH 按 service+时间窗+level+trace_id
  过滤，依赖 P4 结构化日志）→ 聚类（代码：按错误 fingerprint 归并，
  500 条 → 3~4 个模式，各带次数/首现/样本）→ 阅读理解（AI：一次调用
  输出人话摘要与建议，随飞书卡片下发）。反模式：把原始海量日志直接丢给
  LLM 找错误（成本爆炸+幻觉）；分工原则是检索和聚交给数据库，AI 只做
  「把模式变成结论」。错误模式摘要同时进入判决证据包（发布窗口内出现的
  错误是强劣化证据）；CH 不可用时降级为纯指标判决。量化对比：人工翻日志
  10-30 分钟 → 看卡片 30 秒。
- **证据包的边界与质量保障**：
  - 上下文有界是构造保证：喂给 AI 的是聚合结果而非原始日志——模式查询
    LIMIT 5 × 样本截断 200 字符（堆栈只取顶帧）；指标先降采样
    （max_over_time[1m]，60min 固定 60 点/条，与抓取间隔无关）；整包
    预算 ~6-8K token，超限按优先级整段丢弃。上下文大小由 LIMIT 与截断
    决定，与日志量解耦；调用频次由规则闸 + 判决缓存（同组 N 小时）封顶。
  - 检索可信度四道机制：①关联键走同一条身份链（日志 service 字段与
    指标标签同源于部署注入的 SERVICE_NAME/Pod 标签，构造上正确，不靠
    名字猜）；②窗口从告警语义推导（startsAt - lookback，lookback ≥
    for 时长 + 余量；模式 first_seen 贴窗口起点时明示"错误早于窗口"）；
    ③空结果陷阱：先查窗口内日志总量 sanity——总量 0 = 采集坏了（查
    Kafka lag/CH 摄入速率），证据标记不可用退回纯指标判决；ERROR 为 0
    时放宽到 WARN+ 复查（"没有错误"≠"没有数据"）；④防污染：每个模式
    查 7 天基线，标注"长期存在，非本次新增"（新增 vs 复发信号）。
  - 三道兜底：AI 输出 evidence_quality(sufficient/partial/missing)，
    证据不足不得据此降噪（接 fail-open 红线）；飞书卡片带溯源行与
    "查看原始日志"深链（同过滤条件预填），AI 不是看日志的唯一路径；
    窗口推导与过滤拼装是纯代码，fixture 单测回归，与 LLM 评估分层。
- **根因分析 agent：暂缓不做**（backlog）。如果将来做，形态是有界小
  agent：开放式 drill-down（哪个端点慢 → span 卡在哪 → CH 日志模式 →
  发布 diff 对比），function calling 循环，工具白名单={查指标/查日志(CH)/
  查trace/查发布记录}，步数上限 6-8，异步旁路，只读不写。日志摘要功能
  是它的单调用子集，provider 底座完全复用。
- **共享底座**：observability 模块提供 PromClient/CHClient/TraceClient/
  DeployStore 查询 provider；ai 模块封装 LLM 客户端。盯盘的证据组装只
  用到其中一部分，其余 provider 随 P4/P6 自然就位——将来若恢复根因
  agent，无需补底层。

### 7.3 日志与链路（解决慢接口）
- 日志管道：**Fluent Bit（采集）→ Kafka（传输/削峰/多消费者）→ ClickHouse
  （存储）**；平台日志检索页直查 CH（SQL 模板化），Grafana CH 插件画大盘。
  替代 EFK 的理由：一套列存存储同时服务日志检索、事件分析、指标长期存储
  与 AI 检索；省下 ES+Kibana ~4.5G 内存；压缩与聚合分析强于 ES。
- 链路（必要性结论：OTel 埋点必要，SkyWalking 非必需）：
  - 必做且便宜：OTel SDK 埋点（otelgin/otelgrpc，trace_id 注入日志与
    响应头）+ otel-collector。标准协议，换后端不改业务代码。
  - 后端三选一：
    A. SkyWalking 全家桶（+~2G，UI/拓扑现成，国内运维平台认知度高）
    B. Jaeger all-in-one（+~0.5G，半天落地，UI 现成）——推荐起步
    C. span 直接进 ClickHouse，平台自研慢接口页/调用拓扑（零新增组件，
      span 的 service 调用关系一条 SQL 可聚合）——推荐演进终点，
      最贴合「拿数据做平台功能」的中台开发定位
  - 路径：P6 先 B 打通数据与排障闭环，时间允许演进到 C（span 与日志/事件
    统一进 CH）。A 仅当需要强调 SkyWalking 使用经验时才值得 2G 常驻。
  - 定位说明：慢接口 80% 的场景用已有 Prometheus histogram（按路由 P95）+
    结构化日志耗时字段 + pprof 就能定位；tracing 的不可替代点是跨服务链路
    定位与根因推理链证据源，因此不在关键路径上（P6）。
- 慢接口排查闭环：TopN 慢接口 → span 耗时拆解（DB/RPC/外部调用）→
  trace_id 关联 CH 日志 → 关联发布记录。告警 → trace → 日志 → 变更四连。

### 7.4 自动化巡检
平台内置巡检引擎（Go）：**声明式巡检项 + 并发执行 + 结果入库 + 趋势对比**。
- 巡检项 YAML 声明（名称/类型/目标/阈值/修复动作），worker 池并发（默认 10）。
- 内置检查器：中间件连通（TCP 拨测 + 认证 ping + 连接池水位）、Nginx
  `nginx -t` 干跑 + 配置 hash 漂移 + 证书到期、备份存在性/大小/时间戳 +
  每月抽样恢复演练、磁盘水位、镜像 tag 漂移、副本不足、死信堆积。
- 提效：并发调度；增量巡检（CMDB 变更事件驱动）；连接复用；失败重试与
  超时隔离；结果幂等入库，报告 diff 上次只报变化。
- 输出：报告（Web + MinIO 归档 + 飞书异常项推送）；自动修复仅白名单动作。

### 7.5 大数据管道（JD 对齐核心，2026-09 新增）
对齐 JD「海量大数据相关采集、传输、存储、应用等相关平台建设」：

```
采集              传输            存储               应用
Fluent Bit   →   Kafka      →   ClickHouse     →   平台日志检索 / 慢查询分析
OTel SDK         (削峰、          (日志+事件+        告警聚合与降噪上下文
                 多消费者解耦)     指标长期存储、      周报聚合统计
                                  列存高压缩)        AI 根因分析 retrieval
```

需求叙事（面试用，基于 ~1w 活跃用户的小程序）：
- 日请求 50–100 万，高峰 100–200 QPS；日志日增 1–4G 原始（CH 列存压缩后
  200–800M/日）；指标 ~3–5w active series；发布/告警/变更事件日千级。
- 痛点：ES 检索成本高、跨日志-指标-变更的聚合分析弱，而 AI 根因分析需要
  统一检索底座 → 引入 Kafka 解耦采集与消费、ClickHouse 统一存储。
- 措辞纪律：说「**日均百万级事件的可观测数据管道**」，不吹"海量/PB"；
  数据量需用 k6/locust 造流量真实压出来（benchmarks/ 目录沉淀对比数据）。

组件纪律（三件套缺一不上简历）：①需求背景叙事 ②数据流终点是平台可见功能
③压测/对比数据（Kafka lag 与吞吐、CH 写入 rows/s、CH vs ES 查询延迟与
压缩比、30 天留存成本对比）。

Flink 取舍：不常驻。若上，给它真实任务（实时错误聚类/滑动窗口异常检测喂
AI 盯盘），压测窗口临时拉起；面试更稳的答法是「评估过 Flink，当前窗口
聚合用 ClickHouse 物化视图更划算」——这是做过权衡的表达。Hadoop/Spark 系
不引入：数据量级撑不起叙事，反而暴露没做过容量判断。

### 7.6 消息队列与云存储
- Kafka（P4 随数据管道引入）：消费者有日志入库、告警聚合、AI 分析三个，
  解耦成立；平台内部轻量异步（巡检调度、通知重试）仍走 Redis Stream，
  不引入第二个 MQ。
- MinIO（P1 就部署）：构建产物、周报/巡检报告、Grafana 快照（AI 多模态图源）、
  数据库备份落地、CH 冷数据。S3 API 标准。

### 7.7 压测场景清单（k6 = 流量发生器，一切数据故事的地基）

| 场景 | 做法 | 产出 |
|---|---|---|
| 单服务容量基线 | ramping-vus 阶梯加压至错误率拐点，逐服务 | 单副本 QPS 上限、P95/P99、拐点 |
| 日形态稳态负载 | 按 1w DAU 形态建模（读写比 ~7:3、早晚高峰系数）持续 24–48h | 日均请求量、日志/指标/事件真实日增量（管道容量叙事的数据源） |
| 慢接口定位 | 稳态负载背景 + trace 采样 | TopN 慢接口、span 耗时拆解 |
| 滚动发布不断流 | 持续压测中触发发布 | maxUnavailable=0 验证：发布窗口错误率/断流时长 |
| HPA 响应 | 突发加压 | 扩容触发→新副本就绪耗时 |
| 抖动 vs 劣化 | 周期性加减压 + 故障注入（延迟/错误） | AI 盯盘判决素材：误杀/漏报混淆矩阵 |

k6 单实例几百 MB，跑 node3 或本机；脚本与结果入 benchmarks/，与现有 CI
维度 bench 脚本（构建/流水线耗时）互补，k6 负责应用/HTTP 维度。

### 7.8 阿里云 OpenAPI 集成（alibabacloud-go SDK v2）

三个挂到平台真实功能上的集成点 + 一个可选项：

| 集成点 | OpenAPI | 挂到平台哪里 |
|---|---|---|
| ECS 实例纳管 | DescribeInstances（只读）：列表/规格/到期/状态 | 「资源管理」页 + 巡检项「实例到期/状态异常」——CMDB 从集群内对象扩展到云资源 |
| ACR 镜像仓库 | 仓库列表、tag 查询、清理规则 | 「制品管理」页 + 巡检项「无用 tag 清理」（个人版有配额）；与流水线 digest 体系衔接 |
| OSS 备份归档 | 对象读写 SDK | 巡检「数据备份」检查器把 PG 备份/周报/CH 快照归档推 OSS——备份不能与集群同生共死 |
| 可选：BSS 账单 | 费用查询 | 周报加「本周云资源成本」一栏 |

安全约束：RAM 子账号 + 最小权限（ECS 只读、ACR 指定命名空间、OSS 指定
bucket），AK 走 K8s Secret 注入不进代码；条件允许用 ECS 绑定 RAM Role 免 AK。

### 7.9 单集群 vs 多集群：网络边界与演进预留

平台与业务集群之间只有两条通道，所有网络问题都归到这两条上：

```
控制面（平台 → 业务）：下发指令（发布、巡检执行、配置）
数据面（业务 → 平台）：上报遥测（日志、指标、trace、事件）
```

- **数据面：agent 出站上报，不搞平台进业务集群拉。** 业务侧 Fluent Bit /
  otel-collector / Prometheus remote_write 单向出站连平台 Kafka/collector，
  业务集群零入站端口，安全组只配出站白名单；跨 VPC/跨云不暴露内部服务。
  反模式是平台直连业务集群的 Prometheus/ES 拉数据（入口暴露 + 认证负担）。
- **控制面两种模式**：a) 平台持业务集群 kubeconfig/SA token 调 apiserver
  （业务侧只暴露 443 + 最小 RBAC），适合少量集群；b) 业务集群常驻 agent
  出站长连接等指令（蓝鲸 GSE/Argo 模式），跨云最友好，需自研 agent。
- **各层配置**：NetworkPolicy 管集群内东西向（default-deny + 放行
  monitoring→app 抓取、platform→app 探活），跨集群管不到，靠 VPC 对等连接/
  CEN + 安全组；每个跨集群入口要 LB+域名+TLS（agent 上报模式下业务侧零
  入口）；跨云痛点是流量费与时延，解法是边缘缓冲、异步汇聚（每边本地
  Kafka，中心消费）。
- **本项目决策**：单集群起步（平台用 in-cluster SA 调 k8s API，即控制面 a
  模式的退化形态），但平台与业务的耦合面收敛为三个接口——① k8s API
  ② agent 上报通道 ③ 对象存储——多集群化时只需把 ① 换成多份 kubeconfig，
  ②③ 不动。

## 8. 路线图

| 阶段 | 内容 | 验收标准 |
|---|---|---|
| **P0 已完成** | CICD 瘦身 | 契约测试绿；13 服务全 Deployment；零 rollouts/rollback 残留 |
| **P1 平台骨架（2-3 周）** | React+TS 前端骨架（登录/布局/路由）；Gin 后端模块化；PG 迁移框架；服务目录 CRUD（CMDB-lite）；MinIO/Redis 部署；**集群升配 2×4c8g** | 登录后看到 13 服务目录与拓扑；目录变更走 API 落库 |
| **P2 CICD 集成（2 周）** | Jenkins 触发 API；stage 进度 webhook；发布历史页（读 release 记录）；一键 revert PR；Argo CD 状态展示 | 平台上完成一次发布全流程；历史可查 |
| **P3 可观测与告警（2-3 周）** | Grafana 嵌入与反代；告警网关（Alertmanager webhook 落库+飞书直发）；周报 v1（PG/Prom 数据源） | 飞书收到首份自动周报；告警平台可查可认领 |
| **P4 数据管道（2 周）** | Kafka + ClickHouse 部署；Fluent Bit 采集接入；平台日志页 v1；告警/事件入 CH；管道压测与 CH/ES 对比数据 | 日志从平台可检索；benchmarks 沉淀吞吐/压缩/查询对比 |
| **P5 AI 降噪（2 周）** | 时序富化 + Grafana 快照 + **错误日志模式摘要（CH 检索+fingerprint 聚类）**；LLM 单次调用出判决+日志摘要；判决落库与周报复盘（根因 agent 暂缓，见 §7.2） | 告警噪音量可量化下降；零高危漏发；卡片自带错误摘要 |
| **P6 链路与巡检（3 周）** | OTel 全服务埋点 + Jaeger 打通；（可选演进）span 进 CH 自研慢接口页；巡检引擎 + 首批检查器 + 巡检报告 | 慢接口从平台一路点到 span 与日志；巡检报告自动出 |
| **P7 打磨（持续）** | RBAC 细化、审计日志、审批工单、多环境、**服务自助 onboarding**（注册→生成 chart/Jenkins job/Argo App/监控模板）、**云资源管理页（ECS/ACR OpenAPI）与 OSS 备份归档** | 新服务 10 分钟接入；云资源/成本在平台可见；按团队反馈排 |

节奏：P1-P2 是「平台被用起来」的生死线；P4-P5 是简历差异化（大数据管道 +
AIOps）；P7 的自助接入对齐 JD「安全自助接入」。

> **各阶段开工级计划**（任务清单 / 验收标准 / 交付结果，checklist 粒度）
> 见 [docs/plans/](./docs/plans/README.md)；P0 完成记录亦在其中。

## 9. 仓库结构演进

```
devops-platform/
├── platform/
│   ├── server/          # Go/Gin 后端（已有，扩展）
│   └── web/             # React/TS 前端（新建）
├── k3s/                 # 基础设施：ansible/集群/gitops/helm-values（保持，
│                       #   helm-values/platform/ 增加 kafka/click-house 值文件）
├── monitoring/          # 告警规则、Grafana dashboard JSON、Python 聚合脚本
├── inspection/          # 巡检项声明（YAML）与自定义检查器
├── benchmarks/          # 压测与对比数据（已有习惯，继续沉淀）
└── docs/                # 平台文档（本文移入）
```

镜像与部署：platform/web（nginx 静态）+ platform/server 走同一条基础流水线
发布，吃自己的狗粮。**退订前导出清单**：Grafana dashboard JSON、CH/Prom
关键查询结果、benchmarks 数据与图表、平台页面截图、压测原始日志。

## 10. 企业内部云平台的通用交互模式（调研结论）

对比 Backstage（Spotify 开源 IDP 标准）、腾讯蓝鲸、KubeSphere/Rancher、
Argo 系（CD/Workflows）、嘉为蓝鲸一体化方案，共性模式：

1. **统一门户 + SSO 是入口共识**：所有能力从一个门户进，权限统一收口。
2. **CMDB/服务目录是底座**：蓝鲸把配置平台（CMDB）与管控平台放在最底层，
   上层 SaaS（作业/监控/故障自愈）全部围绕它交互；Backstage 的软件目录
   同理。→ 我们的 service-catalog 就是这个角色（P1 立起来）。
3. **PaaS 提供开发框架 + API 网关 + 调度引擎，SaaS 场景应用长在上面**
   （蓝鲸模式）：→ 后端模块边界 catalog / delivery / observability /
   alerting / inspection 各自成模块，模块间只走 API。
4. **引擎与编排分离**：没人自研构建引擎；Jenkins/GitLab CI 做引擎，平台做
   编排与可视化。
5. **事件驱动联动**：发布事件 → 告警抑制窗口 → 自愈动作（蓝鲸「标准运维」
   核心思路）。→ deploy_id 贯穿 CI→Pod 标签→Prometheus→Alertmanager 抑制，
   接进平台事件总线（Kafka）即可。
6. **交互形态**：列表/详情/拓扑/时间线四个高频形态；拓扑图与时间线
   （发布/告警/变更流水）是差异化体验重点。

参考：[aenix IDP 六种架构模式](https://aenix.io)、
[Roadie: Backstage 架构分析](https://roadie.io)、
[腾讯蓝鲸开源项目](https://github.com/Tencent/bk-job)、
[蓝鲸 PaaS 架构（嘉为蓝鲸）](https://www.canway.net)、
[AIOps 告警降噪实践](https://clickhouse.com/resources/engineering/what-is-aiops)、
[OneUptime 告警关联实现指南](https://oneuptime.com/blog/post/2026-01-30-alert-correlation/view)。

## 11. 与腾讯运营开发岗 JD 的映射

| JD 条目 | 平台对应 | 面试讲法 |
|---|---|---|
| 1. 需求→研发→测试→上线全流程研效 + AI 提效 | CICD 编排集成 + AI 周报/AI 盯盘 | 平台触发发布、看进度、一键 revert；周报 AI 聚合生成 |
| 2. IaaS/PaaS/SaaS 资源稳定运营 + AI 智能监控可观测、调度操作 | 三段分层 + AI 盯盘降噪（根因 agent 暂缓在规划里）+ 巡检引擎 | deploy_id 贯穿 CI→Pod→Prometheus→告警抑制；巡检即调度操作 |
| 3. 海量大数据采集、传输、存储、应用平台建设 | Fluent Bit→Kafka→ClickHouse 管道 | 四个词逐一对上，配吞吐/压缩/查询对比数据（§7.5） |
| 4. 安全可控、个性自助、高效便捷的接入流程与运营服务 | 服务自助 onboarding（P7）+ RBAC/审计 | 新服务 10 分钟接入的量化故事 |
| 加分：云路由/消息队列/数据库/云存储实操 | ingress-nginx + 平台反代网关 / Kafka / PG+CH / MinIO | 全中 |
| 加分：前后端全栈 | React+TS 前端 + Go/Gin 后端 | 全中 |
| 加分：分布式架构、高并发调优、大数据实战 | 管道 + k6 压测数据 + 容量规划（§3 即是证据） | 有数字的容量判断本身就是加分项 |
| 加分：开源社区贡献 | 不在本项目射程内 | 另行规划（如给 SkyWalking/CH 生态提 PR） |

## 12. 风险与开放问题

- **资源上限**：12c32g 下全栈稳态 ~14G、构建峰值 node1 ~15G，压测窗口与
  构建必须错峰；Flink 只临时拉起。降级顺序见 §3。
- **AI 降噪的成本与延迟**：只对规则闸拿不准的告警调 LLM（含图 2-5s/次），
  24h 去重；判决全落库供复盘。
- **造流量的真实性**：数据量叙事依赖压测流量，用 k6 场景模拟 1w DAU 的
  请求形态（读写比、高峰系数），原始数据进 benchmarks/。
- **迁移一致性**：catalog 从 YAML 演进为 DB 为主时保留导入导出；部署侧
  catalog 同步策略定为「DB 为源，CI 阶段导出校验」。
- **回退能力空窗**：自动回退已删，P2 的一键 revert PR 是轻量补偿。
- **trace 采样率**：全量采样只在压测窗口开，常态 1–10%，防 collector/CH 被打爆。
