# DevOps 交付项目 — 面试问答口径

> 本文档基于仓库真实代码整理,每条回答都对应可指的文件/行号。
> 标注规范:
> - ✅ **代码里有**:可以直接讲,被追问能指代码
> - ⚠️ **代码里有但没跑通/没接进 CI**:讲的时候要主动限定边界
> - 🚫 **代码里没有**:简历和口述都不要提,被问到要诚实降级
>
> 口径总原则:**这是个原型(PoC),不是线上系统。** 一上来就主动说清"我在一个单仓多模块 Go 后端上设计并验证了一套 GitOps 渐进式交付原型",不要让对方误以为是生产系统。原型的好处是你可以讲设计权衡,坏处是你扛不住"线上 QPS 多少、故障复盘"这类问题——后者要诚实。

---

## 目录

- [0. 开场:30 秒电梯陈述](#0-开场30-秒电梯陈述)
- [1. 项目整体与背景](#1-项目整体与背景)
- [2. 变更影响分析与增量构建(CI 侧)](#2-变更影响分析与增量构建ci-侧)
- [3. 渐进式发布体系(Argo Rollouts)](#3-渐进式发布体系argo-rollouts)
- [4. 发布策略对比(蓝绿/金丝雀/滚动)](#4-发布策略对比蓝绿金丝雀滚动)
- [5. 回退与 GitOps 一致性](#5-回退与-gitops-一致性)
- [6. 告警治理(Prometheus + Alertmanager)](#6-告警治理prometheus--alertmanager)
- [7. 日志体系(Loki + Alloy)](#7-日志体系loki--alloy)
- [8. 可观测性整体链路](#8-可观测性整体链路)
- [9. 项目真实性与"没上线"应对](#9-项目真实性与没上线应对)
- [10. 通用运维八股(本项目相关)](#10-通用运维八股本项目相关)
- [附录 A:数字与参数速记表](#附录-a数字与参数速记表)
- [附录 B:被问到这些就老实承认](#附录-b被问到这些就老实承认)

---

## 0. 开场:30 秒电梯陈述

> 我做的是一个面向**单仓多模块 Go 后端**的 **GitOps 渐进式交付原型**,核心解决四个问题:
> 1. **全量构建慢** —— 用 git diff + Go 依赖闭包识别受影响服务,只构建变更部分,叠加 Go 编译缓存和 BuildKit registry 层缓存;
> 2. **低流量灰度难裁决** —— 按服务流量大小分级配置 Canary/BlueGreen/Rolling 三种策略,高流量服务用 SLI 绝对阈值 + Stable 相对退化双门禁,低流量服务用 BlueGreen + 人工审批,避免样本不足误判;
> 3. **回切后 Git 和集群版本不一致** —— 回退时先切流量回 Stable,再用 yq 把失败 digest 改回去提 GitOps PR,保证 Git 始终是唯一事实源;
> 4. **发布窗口告警噪声** —— 用 Prometheus + Alertmanager,通过 deploy_id 精确匹配同一次发布,只抑制瞬时 Pod 重启告警,持续故障(CrashLoop、副本不足)永远透传。
>
> 这套东西**跑了离线契约测试(promtool/amtool/yq 断言),但没有跑在真实生产集群**,所以我能讲清楚设计权衡和实现细节,但不会有"线上故障复盘"那种故事。

**为什么这样讲:** 主动框定边界 = 抢占主动权。面试官知道是原型后,追问方向会转向"你为什么这么设计",而不是"线上出问题怎么办"——前者你准备充分,后者你没素材。

---

## 1. 项目整体与背景

### Q1.1 介绍一下这个项目,为什么做?

**答:**
背景是一个单仓多模块的 Go 后端(ecampus,13 个微服务),原来的发布方式是**全量构建 + 手动 kubectl apply**,有三个痛点:
1. 改一个服务要构建全部 13 个镜像,慢;
2. 灰度靠人盯监控,低流量服务根本看不出趋势,容易误判;
3. 回滚靠 `kubectl rollout undo`,Git 仓库还是新版本,集群和代码版本对不上。

我做的就是围绕这三个痛点搭一套 CI + GitOps + 渐进式发布的原型。

### Q1.2 为什么选 GitOps 而不是传统 push 模式? ✅

**答:**
GitOps 是 pull 模式,集群里的 Argo CD 主动拉 Git,而不是 CI 直接 kubectl apply。我选它有三个理由:
1. **Git 是唯一事实源**:集群状态 drift 了,selfHeal 会自动收敛回 Git;
2. **审计天然有**:每次发布就是一个 PR,谁发的、发了什么、什么时候,git log 全有;
3. **权限收敛**:CI 不需要集群的写权限,只往 Git 推,集群凭证只在 Argo CD 手里,攻击面小。

**代码里对应的配置**(`k3s/gitops/applications/workloads/ecampus-*.yaml`):每个 Application 都开了 `syncPolicy.automated.selfHeal: true`,Jenkins 不直接 kubectl,而是提 PR 改 values 文件。

### Q1.3 整体架构画一下?

**口述版(准备成图):**
```
开发 push → GitHub Webhook → Jenkins
                │
                ├─ git diff + go list -deps → 受影响服务
                ├─ 测试 + BuildKit 构建(三层缓存) + Trivy 扫描
                ├─ yq 改 GitOps 仓库 values 的 image.digest
                └─ 提 PR → 自动合并 → Argo CD selfHeal sync
                                          │
                              ┌───────────┴───────────┐
                          Argo Rollouts          Prometheus/Alertmanager
                          (Canary/BlueGreen)     (AnalysisTemplate 门禁)
                                          │
                                     PostgreSQL(platform-server 记发布状态)
```

---

## 2. 变更影响分析与增量构建(CI 侧)

> 这是项目描述第一段,也是最容易量化、最不容易被穿的部分。重点讲透。

### Q2.1 怎么识别哪些服务受影响? ✅

**答:**
分两步:

**第一步:git diff 拿变更文件。** CI 拿到 GitHub push 事件的 `before` 和 `after` 两个 SHA,先做三个校验——SHA 都存在、`before` 是 `after` 的祖先(`git merge-base --is-ancestor`,保证是快进推送)。三个校验任何一个不通过就**保守回退到全量构建**(`--all`),宁可慢不能漏。代码在 `Jenkinsfile` 第 764-795 行。

**第二步:Go 依赖闭包。** 对每个候选服务跑 `go list -deps`,算出它的依赖闭包。任何变更文件落进某个服务的依赖闭包,这个服务就进 `build_matrix`;如果只触及测试或 mock 文件,只进 `test_matrix`(跑测试但不构建)。

输出一个 `impact.json`,里面分 `test_matrix` 和 `build_matrix` 两个矩阵,Jenkins 后续按矩阵并行跑。

### Q2.2 为什么用 `go list -deps` 而不是看目录? ⚠️

**答:**
看目录只能抓"直接改了这个服务"的情况,抓不到**跨服务依赖**。比如我改了 `internal/auth` 这个公共包,所有 import 它的服务都受影响,但目录层面看不出来。`go list -deps ./cmd/<service>` 能递归算出这个服务二进制最终链接进来的所有包,把变更文件和这些包做交集,就能抓到间接依赖。

**诚实边界:** 这套影响分析工具(`ecampus-impact`)的源码在**应用仓库**,不在这个 GitOps 仓库里。我这个仓库里消费的是它的输出 `impact.json`。如果让我现场写,逻辑就是 `git diff --name-only` + `go list -deps` 求交集。

### Q2.3 三层缓存具体怎么做的? ✅

**答:** 三层各管一段,代码在 `Jenkinsfile`:

**① Go Module 缓存(`GOMODCACHE`)**
- 挂在共享 PVC `jenkins-agent-cache` 的 `/cache/go-mod`
- 缓存下载好的 module zip,避免重复 `go mod download`

**② Go 编译缓存(`GOCACHE`)**
- 挂在同一个 PVC 的 `/cache/go-build`
- 缓存编译中间产物,没改的包不重新编译
- 两个 cache 都跨 build、跨 Pod 复用(同一个 PVC)

**③ BuildKit Registry 层缓存**
- 这个最关键。构建镜像用 BuildKit 而不是 `docker build`
- `--import-cache type=registry,ref=<cache-image>` 和 `--export-cache type=registry,ref=<cache-image>,mode=max`
- 缓存媒介是**镜像仓库**(腾讯云 TCR),不是本地 PVC
- `mode=max` 表示导出**所有中间层**,不止最终层 —— 这样 Dockerfile 里前面的 `go mod download` 步骤哪怕 Go 代码变了,只要 `go.mod` 没变,那层就命中缓存
- 每个服务独立 cache ref(`buildkit-cache/<service>:main-amd64`),不互相挤占

### Q2.4 这三层缓存各自的命中场景?

**答:**
| 缓存层 | 命中条件 | 省掉的开销 |
|---|---|---|
| GOMODCACHE | `go.mod` 没变 | 重复下载依赖 |
| GOCACHE | 源文件没变 | 重复编译未改动的包 |
| BuildKit registry 层 | Dockerfile 某层 inputs 没变 | 重复执行该层(go mod download、apt install 等) |

关键点:**GOCACHE 和 BuildKit 是互补的**。Go 编译缓存管"包级别"复用,BuildKit 管"Dockerfile 层级别"复用。一个改了 2 个包的提交,GOCACHE 让其他几十个包不重编,BuildKit 让 `go mod download` 那层不重跑。

### Q2.5 为什么用 BuildKit 而不是 docker build? ✅

**答:**
1. **支持 registry 远程缓存**:`docker build` 的缓存是本地的,BuildKit 能把缓存推到 registry,跨机器复用;
2. **`mode=max` 导出中间层**:`docker build` 默认只缓存最终层(后来 buildx 也支持了,但 BuildKit 一直原生支持);
3. **并发执行无关 stage**:Dockerfile 多 stage 时 BuildKit 能并行;
4. **不依赖 Docker daemon**:用 `buildctl` 直连 `buildkitd`,适合在 K8s pod 里跑(Jenkins agent 不需要 DinD)。

### Q2.6 镜像扫描怎么做的? ✅

**答:** 用 **Trivy**,在 BuildKit 构建推送之后立即跑(`Jenkinsfile` 第 144-157 行)。
```sh
trivy image --exit-code 1 --severity CRITICAL --no-progress "$IMAGE@$digest"
```
- **只看 CRITICAL**,HIGH 及以下不拦 —— 原型阶段不想被低危拖死流水线;
- **用 digest 扫不用 tag**,保证扫的就是刚推的那个镜像层;
- `--exit-code 1` 让扫描发现 CRITICAL 直接 fail 整条流水线,不会带漏洞进 GitOps。

### Q2.7 同一 Digest 跨环境发布怎么保证? ✅

**答:**
核心是 **digest 驱动,不用 tag**。

1. BuildKit 构建完从 metadata 抽 `containerimage.digest`,强校验格式 `^sha256:[0-9a-f]{64}$`(`Jenkinsfile` 第 137-142 行);
2. 用 `yq` 把 **digest** 写进 values,不是 tag:`.image.digest = <digest>`;
3. chart 的镜像渲染逻辑(`_helpers.tpl`):有 digest 就渲染成 `<repo>@sha256:...`,没有才 fallback 到 tag;
4. Argo CD sync 之后,`wait-for-release.sh` 轮询 pod 的 `containers[0].image` 字段是不是包含 `@<expected-digest>`,这是运行时一致性校验。

**为什么用 digest 不用 tag:** tag 是可变的,同一个 `:dev` tag 上午下午可能指向不同镜像,跨环境复用不可靠。digest 是内容寻址,同一 digest 永远是同一份镜像,天生保证"dev 验证过的就是 prod 部署的"。

### Q2.8 你说"显著降低合并到 Canary Pod 就绪的等待时间",降了多少? ⚠️

**答(诚实版):**
这是**离线对比测**的数,不是生产数据。对比方式是关掉三层缓存跑一次、打开跑一次,看 `git push` 到 Canary Pod Ready 的端到端时间。主要省在三块:Go module 下载(冷启几十秒→几秒)、Go 编译(全量编译→增量)、Dockerfile 前置层(go mod download 那层命中后基本零开销)。

**如果被追问具体数字:** 给相对值(冷热对比的倍数),不给绝对生产数字。说"原型环境测的,不能代表生产规模"。

---

## 3. 渐进式发布体系(Argo Rollouts)

> 这是项目第二条,也是最有内容、最能体现设计思考的部分。

### Q3.1 你怎么决定一个服务用 Canary 还是 BlueGreen? ✅

**答:**
我在**服务目录**(`configs/service-catalog.yaml`)里给每个服务静态配一个 `rolloutProfile`,分四种:

| Profile | 策略 | 适用场景 | 特点 |
|---|---|---|---|
| **critical-canary** | Canary | 高流量核心服务 | 步长小(1%→5%→20%→50%→100%),阈值严(errorRate 1%),带 Stable 相对退化门禁 |
| **standard-canary** | Canary | 中等流量 | 步长大(20%→50%→100%),阈值松(errorRate 2%),Inconclusive 立即终止 |
| **controlled-bluegreen** | BlueGreen | 低流量服务 | Preview 探活 + 人工审批,避免低样本误判 |
| **fast-rolling** | Rolling | 低风险配置变更 | 纯 Deployment 滚动,无 Analysis |

**选型逻辑是按"流量可验证性"和"故障影响范围":**
- **高流量服务**有足够样本让 SLI 统计显著,适合 Canary 渐进验证;
- **低流量服务**样本太少,SLI 波动大,Canary 会误判(要么一直 Inconclusive,要么样本不够瞎通过)。所以走 BlueGreen + 人工审批,用 Preview 环境探活兜底。

### Q3.2 为什么低流量服务不用 Canary?这是核心设计点 ✅

**答:**
Canary 的门禁依赖 SLI 统计(错误率、P95)。统计学上,样本量越小,方差越大。低流量服务在 5% Canary 阶段可能 5 分钟才几十个请求,这时算错误率:
- 哪怕新版本没问题,随机波动一下错误率就超阈值,被误判失败 → 瞎回滚;
- 或者新版本有问题,但样本太少,错误率没超阈值 → 漏过。

两个方向都会出错。所以我让低流量服务走 BlueGreen:先起 Preview 副本,用**主动探活**(curl 健康检查和关键接口)而不是被动统计来验证,验证过了**人工审批**才切流。

**代码对应**(`rollout.yaml` + `analysis-template.yaml`):BlueGreen 的 `prePromotionAnalysis` 用的是 **Job provider**(主动 curl),不是 Prometheus provider(被动统计);`autoPromotionEnabled: false` 强制人工。

### Q3.3 Canary 的双门禁是什么? ✅

**答:**
高流量服务(Critical Canary)有两个门禁,缺一不可:

**① SLI 绝对阈值门禁**
- Canary 自己的错误率不能超过 `maxErrorRate`(critical 是 1%)
- Canary 自己的 P95 不能超过 `maxP95Seconds`
- 至少 `minSamples` 个样本(critical 是 3000),样本不足不判 fail,判 Inconclusive

**② Stable 相对退化门禁**
- 不光看 Canary 自己,还要看 **Stable 是不是本来就健康**
- 如果 Stable 自己错误率就高(比如发版前线上就有问题),那 Canary 比 Stable 差一点也不该判 fail——因为基线本身坏了,比较无意义
- 实现:Stable 错误率超阈值时,metric 返回 **NaN**,触发 Inconclusive 而不是 Failed,让发布暂停等人决策

**这个设计的精髓:** 不做"绝对正确性判断",做"相对退化判断",并且基线不可信时**宁可暂停也不误杀**。

### Q3.4 样本不足、查询失败、Stable 异常时怎么处理? ✅

**答:** 全部走 **Inconclusive**,不是 Failed。
- **Inconclusive**:发布**暂停**,等人看,不自动回退
- **Failed**:发布**失败**,自动回退

实现是 AnalysisTemplate 里每个 metric 末尾加 `or on() (vector(0) / vector(0))`。`0/0` 在 Prometheus 里是 NaN,NaN 既不满足 success 条件也不满足 failure 条件,所以 Rollouts 把它当 Inconclusive。

配合 `inconclusiveTimeout`(critical 是 15m):暂停超过 15 分钟自动终止发布。

**为什么这样设计:** 误回滚(把好版本回退掉)和漏回滚(把坏版本放过去)都糟糕,但**测量工具不可信时,最安全的动作是不动**。让 Rollouts 暂停等人,而不是在数据不足时强行做决定。

### Q3.5 BlueGreen 的资源预检是什么? ✅

**答:**
BlueGreen 切流前新旧副本并存,如果集群资源不够,新副本起不来,发布会卡住。所以 `previewReplicaCount: 2` 限制 Preview 副本数,避免直接按目标副本数起导致资源耗尽。

切流时机也有控制:`scaleDownDelaySeconds: 900`(15 分钟),切流后旧版本不立即删,留 15 分钟观察,有问题还能切回去。

### Q3.6 Canary 步长怎么定的? ✅

**答:**
Critical Canary 的 steps(`charts/go-service/values.yaml`):
```yaml
- setWeight: 1;   pause: 5m    # 先放 1%,看 5 分钟
- setWeight: 5;   pause: 5m    # 没事放到 5%
- setWeight: 20;  pause: 5m
- setWeight: 50;  pause: 5m
- setWeight: 100; pause: 0s    # 全量
```
逻辑:**前密后疏**。1% 阶段是最高风险的(第一次接触真实流量),停 5 分钟看能不能扛住;5%→20%→50% 逐步加;到 50% 没事基本就稳了,直接上 100%。

Standard Canary 松一档:20%→50%→100%,适合中等风险变更。

### Q3.7 Canary 流量怎么切分的?用的什么路由? ✅

**答:** 用 **Nginx Ingress 的 Canary 注解**做流量路由(Argo Rollouts 的 `nginx` traffic router)。
Rollout CR 里配置:
```yaml
strategy:
  canary:
    trafficRouting:
      provider: nginx
```
Rollouts 控制器会自动改 Nginx Ingress 的 canary 权重注解(`nginx.ingress.kubernetes.io/canary-weight`),实现按百分比切流。

**为什么不用 Istio/Service Mesh:** 原型阶段不想引入 mesh 的复杂度,Nginx Ingress 已经够做流量切分,而且集群里本来就要装 ingress-nginx。

---

## 4. 发布策略对比(蓝绿/金丝雀/滚动)

> 这是面试最高频八股,务必准备成肌肉记忆。结合项目讲。

### Q4.1 蓝绿、金丝雀、滚动发布的区别?

**答:**
| 维度 | 蓝绿 | 金丝雀 | 滚动 |
|---|---|---|---|
| **环境** | 两套完整环境并存 | 新旧 Pod 共存 | 逐批替换 Pod |
| **流量** | 一键 100% 切换 | 按比例渐进(5%→50%→100%) | 跟着 Pod 替换走 |
| **回滚** | 秒级(切回去) | 收回流量,较慢 | 重新拉旧镜像 |
| **资源** | 双倍 | 增量(看比例) | 最省 |
| **验证** | 切流前完整测试 | 真实流量小批量验证 | 边滚边看 |
| **风险** | 切流瞬间集中 | 分散,可控 | 中等 |

**一句话:** 蓝绿求"快回滚",金丝雀求"准验证",滚动求"省资源"。

### Q4.2 什么时候选哪个?(结合你的项目)

**答:** 我项目里就是按这个逻辑分的:
- **高流量核心服务 → Canary**:有样本做统计验证,渐进放量风险最小;
- **低流量服务 → BlueGreen**:样本不够做 Canary 统计,用蓝绿+人工兜底;
- **低风险纯配置变更 → Rolling**:没必要搞 Canary/BlueGreen 的复杂度,直接滚。

**通用选型原则:**
- 改动大、需要完整验证、资源充裕、回滚要求快 → 蓝绿
- 改动有风险、需要真实流量验证、有完善监控 → 金丝雀
- 改动低风险、想省资源 → 滚动

### Q4.3 蓝绿切换瞬间有长连接怎么办?

**答:** 配合**优雅停机**。旧实例收到 SIGTERM 后:
1. 先从负载均衡摘除( readiness 标 not ready),不再收新请求;
2. 等已有连接处理完(drain),给一个 grace period;
3. 再真正退出。

K8s 层面:`preStop` hook + `terminationGracePeriodSeconds`。`preStop` 里 sleep 一会让 ingress 摘除生效,grace period 给足时间 drain。

### Q4.4 金丝雀怎么判断"可以继续放量"?

**答:** 看 SLI:
- **错误率**低于阈值
- **P95 延迟**低于阈值
- **样本量**足够(不然统计无意义)
- 可选:关键业务指标(如支付成功率)

我项目里用 Argo Rollouts 的 AnalysisTemplate 自动化这个判断,每个 pause 点跑 analysis,过了自动放下一个 setWeight,不过自动 abort。

### Q4.5 数据库 schema 变更和发布怎么配合?

**答:** 这是蓝绿/金丝雀都绕不开的难题,因为新旧版本可能同时访问同一个 DB。原则是 schema 变更要**向前兼容**:
- 不删列、不改列类型(破坏性变更会让旧版本挂);
- 加列给默认值;
- 破坏性变更走 **Expand-Contract**:先加新列(Expand)→ 发版让代码双写 → 再删旧列(Contract),分多次发布。

**诚实边界:** ⚠️ 我项目里没涉及 schema 变更流程,这块是理论准备。我的 PostgreSQL 只存发布记录(service_releases 表),不是业务库。

---

## 5. 回退与 GitOps 一致性

> 项目第三条核心。这个的设计能体现"考虑过 failure scenario"。

### Q5.1 灰度失败时怎么回退? ✅

**答:** 分两步,**先切流量,再修 Git**,顺序很重要。

**第一步:切流量回 Stable**
- Canary 失败:`kubectl-argo-rollouts abort <rollout>`,立刻把流量切回 Stable RS;
- BlueGreen post-promotion 失败:`rollouts undo`,回到上一个 RS;
- fast-rolling(Deployment):`kubectl rollout undo`。

**第二步:修 GitOps 仓库**
切完流量,集群已经回到 Stable 了,但 **Git 仓库里 values 还写着失败的 digest**。这时 CI 用 yq 把失败服务的 `image.digest` 改回 Stable 的 digest,提交补偿 PR 到 GitOps 仓库。

**为什么要修 Git:** 不修的话,下次 Argo CD selfHeal 一触发,又把失败的版本同步回来了——因为 Git 是事实源,集群得跟着 Git 走。所以必须让 Git 也回到 Stable。

### Q5.2 回退到哪个版本?怎么确定 Stable 版本? ✅

**答:** 三层兜底,按优先级降级(`rollback-release.sh` 的 `resolve-target` 子命令):

**L1:Argo Rollouts Status**
- `kubectl-argo-rollouts get rollout -o json`,取 `.status.stableRS`
- 再 `kubectl get rs <stableRS>`,从 RS 的 `containers[0].image` 抠出 digest
- BlueGreen 还会校验 stableRS 不是当前 active 的(否则不算真"稳定")

**L2:PostgreSQL 发布记录**
- 如果 Rollouts 状态缺失(比如控制器重启了),查 `service_releases` 表
- 取最近一条 `release_status='stable'` 的记录
- 用 `platform-server release-record --stable-digest` 子命令查

**L3:GitOps git 历史**
- 如果 DB 也没有(服务从没成功发布过),遍历该服务 values 文件的 git 历史
- `git log --follow --format=%H -- <values> | head -50`,从新到老找第一个有合法 digest 的提交
- 可选跑 registry head 命令验证 digest 在镜像仓库还存在

三层任一层拿到有效 digest 就短路,全失败返回 exit code 3。

### Q5.3 为什么不直接用 Argo Rollouts 自己的回退?

**答:** 用,但不够。Rollouts 的 `abort` 只切流量,**不改 Git**。如果我只 `rollouts abort`:
1. 流量是回到 Stable 了,但 GitOps 仓库 values 还是失败 digest;
2. Argo CD 的 selfHeal 一触发,又把失败版本同步回来;
3. 或者下次有人改这个服务,基于失败 digest 发,等于埋雷。

所以必须在 Rollouts 切完流量后,**CI 再补一个 GitOps 补偿 PR**,让 Git 和集群一致。这是"GitOps 闭环回退"和"纯集群回退"的关键区别。

### Q5.4 回退完怎么验证真的回退成功了? ✅

**答:** `verify-traffic` 子命令轮询当前 RS 的 `containers[0].image`,看包不包含目标 `@<digest>`。轮询最多 30 次、每次 sleep 10 秒(共 5 分钟),超时还没回到目标 digest 就报错。

为什么轮询:Rollouts abort 是异步的,流量切回需要时间(Pod 终止、Endpoints 更新),不能 assume 立即生效。

### Q5.5 `service_releases` 表怎么设计的? ✅

**答:** 核心是**部分唯一索引**保证"每个 service+env 只有一条 stable":
```sql
create unique index service_releases_stable_unique
  on service_releases (service, environment)
  where release_status = 'stable';
```
这样 `on conflict ... do update` 时,stable 记录会被 upsert(每个服务环境永远只有一条 stable),其他状态(releasing/failed/compensating)是 append 历史行,留审计。

状态机:`releasing`(开始)→ `stable`(成功)/ `failed`(回退)/ `compensating`(提补偿 PR)。

---

## 6. 告警治理(Prometheus + Alertmanager)

> 项目第四条。这是设计感最强、也最容易体现"真的想过告警噪声问题"的部分。

### Q6.1 告警治理解决什么问题?

**答:** 发布期间告警噪声特别大,因为发版本身就会触发一堆瞬时现象:Pod 重启、短暂 5xx、副本扩缩。如果不治理,发一次版告警刷屏,真故障被淹没。我要做的是:**只压制发布带来的瞬时噪声,永远不压制真故障。**

### Q6.2 你的告警怎么分类的? ✅

**答:** 四类,用 `signal_type` 标签区分(`helm-values/platform/prometheus.yaml`):

| signal_type | 含义 | 例子 | 是否通知 |
|---|---|---|---|
| **deploy_context** | 发布上下文信号 | NoiseWindow(发布窗口打开) | **永不通知**,只做抑制源 |
| **deploy_noise** | 发布瞬时噪声 | PodRestarting | 通知,但**可被抑制** |
| **release_gate** | 发布门禁失败 | AnalysisFailed、AnalysisInconclusive | 通知,不抑制 |
| **user_impact** | 用户影响 | 错误率高、P95 高 | 通知,不抑制 |
| **infra** | 基础设施 | LokiPVCUsageHigh | 通知,不抑制 |

**关键设计:** 只有 `deploy_noise` 类的**瞬时**告警(PodRestarting)能被抑制;CrashLooping、StuckTerminating、ReplicaShortage、PodNotReady 这些**持续**故障**永远不被抑制**。

### Q6.3 抑制规则(inhibition)怎么写的? ✅

**答:** 两条 inhibit 规则,核心是 **deploy_id 精确匹配同一次发布**:

**规则一:发布窗口抑制瞬时重启**
```yaml
source_matchers: [alertname="ReleaseDeployNoiseWindow", deploy_id=~".+"]
target_matchers: [signal_type="deploy_noise", alertname="ReleasePodRestarting", deploy_id=~".+"]
equal: [namespace, service, environment, deploy_id]
```
含义:当某个 deploy_id 的发布窗口打开时,抑制**同一个 deploy_id** 的瞬时 Pod 重启告警。

**规则二:user_impact 抑制 deploy_noise(优先级保护)**
```yaml
source_matchers: [signal_type="user_impact", alert_scope="revision", ...]
target_matchers: [alertname="ReleasePodRestarting"]
equal: [namespace, service, environment, deploy_id]
```
含义:如果已经告了 user_impact(真影响用户了),那就别再发 PodRestarting 这种噪声了。

**为什么用 `equal: deploy_id`:** 发布期间集群里可能同时在跑多个版本(stable + canary),不用 deploy_id 锁定就会误抑制别的发布的告警。deploy_id 是本次发布的唯一标识(格式 `<releaseBatch>-<service>-1`)。

### Q6.4 为什么 CrashLooping 不能被抑制?

**答:** CrashLooping 是 Pod **持续**崩溃重启(`increase(restarts[10m]) >= 3`),这是**真故障**,不是发布瞬态。如果发布窗口把它抑制了,发版把服务搞挂了却没告警,就是事故。

区分原则:
- **PodRestarting**(瞬态):重启了一次,`for` 没有 grace,可能只是发版正常重启;
- **CrashLooping**(持续):10 分钟重启 3 次以上,`for: 2m` grace,是病;

抑制只针对前者,后者永远透传。

**代码兜底:** `test-observability.sh` 第 82 行有硬断言:`ReleasePodCrashLooping|StuckTerminating|ReplicaShortage|PodNotReady must never appear in inhibit targets`。这是契约测试,防止以后有人改规则误扩大抑制范围。

### Q6.5 deploy_id 怎么贯穿告警和日志的? ✅

**答:** 这是闭环的关键,整条链路:

1. **CI 生成 deploy_id**:`Jenkinsfile` 里 `DEPLOY_ID = releaseBatch + '-' + service + '-1'`;
2. **注入 Pod**:go-service chart 用 downward API 把 deploy_id 注入容器环境变量(`_helpers.tpl`),同时写到 Pod 的 label 和 annotation(`delivery.platform/deploy-id`);
3. **Prometheus 采集**:kube-state-metrics 抓 Pod label/annotation,Recording rule 把它重命名成 `deploy_id` metric label(6 层 label_replace);
4. **告警携带**:告警 expr 用 `* on(...) group_left(deploy_id) <release_info>`,把 deploy_id join 进告警 label;
5. **日志关联**:Alloy 从应用 JSON 日志解析 deploy_id,写进 Loki structured metadata;
6. **告警卡片跳转**:告警 annotation 里预埋 `loki_query: '{namespace="...",service="..."} | json | deploy_id="..."'`,点开告警复制这条查询到 Loki,直接定位本次发布的日志。

### Q6.6 告警卡片跳转具体是什么?⚠️

**答(诚实版,这是面试重点):**
我**没有 Grafana,没有自建 dashboard**。所谓"告警卡片"指的是 **Alertmanager 原生 UI 里每条告警的展示**。我在每条告警的 annotation 里预埋了几个字段:
- `loki_query`:用告警的 label 渲染好的 LogQL,复制到 Loki 查日志;
- `prometheus_query`:渲染好的 PromQL,复制回 Prometheus 看指标曲线;
- `git_sha` / `image_digest`:这次发布对应的代码版本和镜像。

面试官点开 Alertmanager 一条告警,能看到这些 annotation,复制 loki_query 跳到 Loki 就能查到这次发布版本的日志。链路是通的,但**没有做更花哨的可视化 dashboard**。

### Q6.7 低流量过滤怎么做的? ✅

**答:** 每条 user_impact 告警的 expr 都带样本量门禁:
```
(ecampus:http_errors_total:rate5m / ecampus:http_requests_total:rate5m) > 0.05
and on(...) ecampus:http_requests_total:rate5m > <min_samples>
```
样本不足时,`and` 条件不成立,告警不 fire。这防止"5 分钟 3 个请求错了 1 个 = 33% 错误率"这种低样本误报。

minSamples 在服务目录里按服务配,critical 是 3000,dev 环境降级到 50(因为 dev 流量小)。

### Q6.8 SLI 为什么只保留必要维度? ✅

**答:** SLI(错误率、P95)的 label 只留 `service, environment, version/revision` 这些**低基数**维度。高基数字段(`deploy_id`、`image_digest`、`git_sha`)不进 SLI 的常规 label,而是在**告警触发时**才通过 join 关联进来。

原因:Prometheus 标签是时间序列的维度,每个 label 值组合都是一条独立 series。如果把 deploy_id 当 label,每次发布都产生新 series,series 数爆炸,内存和查询都扛不住。

所以设计是:**低基数维度建指标,高基数维度触发时 join**。这是 Prometheus 的标准最佳实践。

### Q6.9 契约测试怎么防止抑制范围被误改? ✅

**答:** `test-observability.sh` 是一套硬性断言,用 yq/docker amtool 检查:
- inhibit 的 target 只能是 `ReleasePodRestarting`,不能出现 CrashLooping 等持续故障;
- inhibit 的 equal 必须包含 `deploy_id`;
- 各 alert 的 `for` 时长:NoiseWindow 无 grace、ReplicaShortage/NotReady=5m、CrashLooping/StuckTerminating=2m;
- amtool 路由测试:给定 label,验证它路由到哪个 receiver。

⚠️ **诚实边界:** 这套契约测试**代码完整,但没接进 GitHub Actions**(因为 CI runner 没装 docker)。我在本地跑通过。如果面试官问"CI 里跑了没",老实说"写了契约测试,本地验证,还没接进流水线,是待办"。

---

## 7. 日志体系(Loki + Alloy)

> 你最担心的"日志圆不回来"的部分。其实代码里齐全,看下面。

### Q7.1 日志方案是什么?为什么选 Loki 不选 ELK? ✅

**答:** Loki + Alloy(Grafana 的 collector)。

**为什么不用 ELK:**
1. **资源**:ES 做全文索引,吃内存吃磁盘。我这个原型资源有限,Loki 轻量得多;
2. **架构匹配**:Loki 的标签体系和 Prometheus 完全一致(`namespace/service/environment`),指标和日志用同一套标签,关联查询天然顺畅;
3. **成本**:Loki 只对标签建索引,日志正文压缩成 chunk 存,存储成本低一个数量级。

### Q7.2 为什么用 Alloy 不用 Promtail? ✅

**答:** Alloy 是 Grafana 官方新一代 collector,要替代 Promtail。我选它的理由:
1. **官方演进方向**:Promtail 进入维护模式,新功能在 Alloy;
2. **一个实例覆盖全集群**:Alloy 用 `loki.source.kubernetes` 通过 K8s API tail 容器日志,Deployment 单副本就够,不需要像 Promtail 那样开 DaemonSet;
3. **支持 structured metadata**:Loki v13 的新特性,Alloy 原生支持。

**如果被追问 Promtail vs Alloy 细节:** Promtail 是 DaemonSet 模式(每个节点一个),Alloy 可以 Deployment 模式(集群级)。DaemonSet 更可靠(节点级隔离),Deployment 更省资源。原型阶段我选省资源的。

### Q7.3 deploy_id 怎么进 Loki 的? ✅

**答:** 关键是 **structured metadata**,不进索引。

Alloy 的处理流水线(`helm-values/platform/alloy.yaml` 的 `loki.process.release_meta`):
1. `stage.json`:从应用 JSON 日志里解析 `deploy_id`、`git_sha`、`image_digest` 等字段;
2. `stage.structured_metadata`:把这些字段写进 Loki 的 structured metadata(Loki v13 schema)。

**为什么用 structured metadata 不用 label:** 这就是低基数问题。如果把 deploy_id 当 Loki label,每次发布产生新 label 值,series 数爆炸,Loki 的索引会膨胀到不可用。structured metadata 允许把字段和日志关联,**但不建索引**,查询时做运行时过滤。

**只有低基数维度**(`namespace/service/environment`)才进 Loki label 当索引。

### Q7.4 Loki 怎么避免高基数问题?

**答:** 三个手段:
1. **label 只放低基数字段**:`namespace`、`service`、`environment` 这种值域有限的;
2. **高基数走 structured metadata**:`deploy_id`、`trace_id`、`image_digest` 这些,不进索引;
3. **查询时过滤**:LogQL 用 `| json | deploy_id="xxx"` 在查询时过滤,不在索引阶段展开。

这是 Loki 官方推荐做法。面试常问"高基数怎么处理",这就是标准答案。

### Q7.5 应用日志格式有什么规范? ⚠️

**答:** 我定义的规范是应用输出 **JSON 日志**,每条日志里包含 `deploy_id`、`git_sha`、`image_digest`、`level`、`msg` 等字段。deploy_id 通过 downward API 注入容器环境变量,应用从 env 读出来打到日志里。

**诚实边界:** 应用源码在另一个仓库(ecampus-go),**应用是否真的按这个规范输出了 deploy_id,我无法在这个 GitOps 仓库里验证**。我定义的是规范和采集侧的处理逻辑,应用侧的落地在应用仓库。

### Q7.6 Loki 怎么部署的?HA 吗? ⚠️

**答(诚实版):** **不是 HA**。Monolithic 单副本,filesystem 存储,7 天保留。values 文件里注释自己写了"Experimental logging backend for the dev cluster"。

原型阶段求跑通,没做 HA。生产要做 HA 的话得上 Loki 的 Simple Scalable 或 Microservices 模式,加 S3 做对象存储,这是后续的事。

---

## 8. 可观测性整体链路

### Q8.1 指标、日志、告警怎么串起来的?

**答:** 用 **deploy_id 贯穿三套系统:

```
CI 生成 deploy_id
   ↓ downward API
Pod label/annotation
   ├─→ kube-state-metrics → Prometheus(deploy_id 进 metric label via recording rule)
   │                              ↓
   │                         告警 expr join deploy_id
   │                              ↓
   │                         Alertmanager 告警携带 deploy_id + loki_query annotation
   │
   └─→ 应用输出 JSON 日志(含 deploy_id)
          ↓ Alloy stage.json + structured_metadata
        Loki(按 deploy_id 查日志)
```

**结果:** 从一条告警能定位到"哪次发布、哪个镜像、哪个代码 commit",再到 Loki 查这次发布的日志。闭环。

### Q8.2 为什么用 recording rule 做关联,不直接在告警里 join?

**答:** 两个理由:
1. **性能**:告警 expr 里做 `group_left` join 很贵,每次告警评估都算。Recording rule 预算好关联结果存成新指标,告警直接查这个指标,便宜;
2. **复用**:关联逻辑(record)写一次,告警和 dashboard 都能用。

我项目里有 4 条核心 recording rule(`delivery-platform-release-recording` 组),把 Pod label 上的 release 信息重命名成干净的 metric label,供 14 条告警 join。

### Q8.3 SLI 是怎么定义的? ✅

**答:** 核心 SLI 两个:**错误率**和**P95 延迟**,都按 `(service, environment, revision)` 聚合。
- 错误率 = `http_errors_total / http_requests_total`(rate5m)
- P95 = `http_request_duration_seconds` histogram 的 p95

每个服务在 catalog 里配自己的阈值(`maxErrorRate`、`maxP95Seconds`),critical 服务严(errorRate 1%),standard 松(2%)。

还有可选的**关键操作成功率**(`minOperationSuccessRate`),按 route 正则过滤关键接口(比如下单、支付)单独算成功率,critical 服务要求 99%。

---

## 9. 项目真实性与"没上线"应对

> 这块是心态建设,不是技术问答。但必须准备好,否则一被质疑就慌。

### Q9.1 这个项目上线了吗?有真实用户吗?

**答(准备好的诚实口径):**
**没有上线,是个原型。** 我做这个项目的背景是想系统实践 GitOps 渐进式交付的完整链路,所以在一个单仓多模块的 Go 后端上设计和验证了这套流程。它**跑通了离线契约测试**(promtool 验证告警规则、amtool 验证路由、yq 验证抑制契约、promtool unit test 验证 alert 行为),CI 流水线也在 Jenkins 上验证过,但**没有跑在真实生产集群,没有真实用户流量**。

我能讲清楚的是:每个设计决策的 why、实现细节、权衡取舍。我讲不了的是:线上 QPS、真实故障复盘、大规模下的性能数据。后者我实话实说没有。

**为什么这样答最好:** 主动框定边界,把面试官的追问引向你准备充分的方向(设计 why),避开你没素材的方向(线上数据)。

### Q9.2 面试官说"这看起来像简历包装,你真的做过吗?"

**答:**
可以现场展示:
1. **代码级别**:任何一段你让我展开讲,告警规则、Rollout 模板、回退脚本、缓存配置,我都能说清楚哪一行为什么这么写;
2. **设计权衡**:为什么低流量用 BlueGreen 不用 Canary、为什么 CrashLooping 不能被抑制、为什么回退要先切流量再修 Git——这些 trade-off 是自己想过才会有;
3. **诚实边界**:我也会告诉你哪些没做完,比如契约测试没接进 CI、Alertmanager receiver 是空的没接通知渠道、Loki 不是 HA。我不藏短板。

**心态:** 真做过的人不怕被问细节,也不怕承认没做的部分。包装的人才会回避细节、什么都说"做好了"。你反着来。

### Q9.3 这套东西真的能跑起来吗?资源够吗?

**答(如果对方追问部署可行性):**
我评估过部署可行性。这套东西在 2c2g 的小机器上跑不了全套(光平台组件就 2.5-3.5G 内存),但在 2×2c4g + 1×2c8g 上跑"最小垂直切片"(Argo CD + Rollouts + 2 个 demo 服务 + 监控栈)是可行的,我做过资源核算。我没全量部署是因为 ROI——13 个服务全起的边际收益对验证设计没必要,而且构建侧的量化指标(缓存命中率、构建时间)可以离线测,不依赖集群。

### Q9.4 你最大的收获是什么?

**答(真诚版,别背):**
最大的收获是理解了"**可验证性**"这个词的份量。以前觉得发布就是"部署上去能跑就行",做完这个项目才明白:低流量服务能不能用 Canary、样本不足时该 fail 还是 pause、回退后 Git 要不要同步——这些问题的核心都是"你的验证手段可不可信"。不可信的时候,最安全的动作是暂停,不是强行决策。这个思维方式对我影响挺大。

### Q9.5 如果重做你会改什么?

**答(准备 2-3 个真实的改进点):**
1. **契约测试接进 CI**:`test-observability.sh` 现在没在 GitHub Actions 跑,因为 runner 没装 docker。重做我会用 GitHub Actions 的 container action 或换自托管 runner,让告警契约每次 PR 都验证;
2. **告警通知渠道落地**:Alertmanager 的 receiver 现在是空的,重做会接钉钉/飞书 webhook,让告警真的能发出去;
3. **Loki 上 HA**:现在单副本 filesystem,重做会上 Simple Scalable + 对象存储;
4. **加 Grafana**:现在靠 Prometheus/Loki 原生 UI,重做会上 Grafana 做统一可视化。

---

## 10. 通用运维八股(本项目相关)

> 这些是高频八股,但结合项目讲比纯背强。

### Q10.1 GitOps 和传统 CI/CD 区别?

**答:**
传统 push 模式:CI 构建完直接 `kubectl apply` 到集群,CI 持有集群凭证。
GitOps pull 模式:CI 只推 Git,集群里的 Argo CD 拉 Git 应用变更。

| 维度 | 传统 push | GitOps pull |
|---|---|---|
| 事实源 | 集群当前状态 | Git 仓库 |
| CI 权限 | 需要集群写权限 | 只需 Git 写权限 |
| 状态漂移 | 人工 kubectl 改了没人知道 | selfHeal 自动收敛回 Git |
| 审计 | 看集群 event | 看 git log |
| 回滚 | kubectl rollout undo(Git 不一致) | Git revert(天然一致) |

我项目选 GitOps 的关键原因就是回滚一致性——这是项目第三条的核心。

### Q10.2 Argo CD 的工作原理?

**答:**
核心是 **reconcile loop**:Argo CD 持续比较 Git 仓库的"期望状态"(desired)和集群的"实际状态"(live),两者不一致就触发 sync。
- **Application**:定义一个部署单元(Git 仓库 + 路径 + 目标集群);
- **AppProject**:权限隔离,限制 Application 能部署到哪些 namespace、能用哪些 repo;
- **syncPolicy.automated.selfHeal**:集群状态被人改了(drift),自动重新 sync 回 Git;
- **触发方式**:默认 3 分钟轮询 Git,配 webhook 后 push 即时触发。

我项目里 Jenkins 提的 PR 一合并,Argo CD 通过 webhook 即时感知,selfHeal 把新 values sync 到集群。

### Q10.3 Argo Rollouts 和 K8s Deployment 区别?

**答:**
Deployment 只支持 RollingUpdate 和 Recreate,没有 Canary/BlueGreen 的流量切分能力。
Rollouts 是 CRD + 控制器,替代 Deployment,支持:
- **Canary**:按流量百分比渐进放量,集成 analysis 自动决策;
- **BlueGreen**:维护 activeService/previewService,切流秒级;
- **trafficRouting**:对接 Nginx/Istio/ALB 做真实流量切分(不是 Pod 数比例);
- **AnalysisTemplate**:每个阶段跑 Prometheus 查询做门禁。

我项目里 fast-rolling 服务用原生 Deployment,其他三个 profile 用 Rollouts。

### Q10.4 Prometheus 的基本架构?

**答:**
- **Prometheus Server**:核心,pull 模式抓指标,存时序数据库;
- **Alertmanager:接收 Prometheus 推来的告警,做分组/抑制/去重/路由;
- **Pushgateway**:临时任务推指标的中转(可选);
- **Exporter**:被采集端的指标暴露组件(node-exporter、kube-state-metrics);
- **Grafana**:可视化(⚠️ 我项目没用)。

采集靠 service discovery 发现目标(K8s 里靠 Pod/Service 的 annotation)。告警规则在 Prometheus 端评估,触发后推给 Alertmanager。

### Q10.5 Pod 一直 CrashLoopBackOff 怎么排查?

**答(标准排查路径):**
1. `kubectl describe pod <name>` 看 Events,看是镜像拉不下来、探针失败、还是 OOM;
2. `kubectl logs <pod> --previous` 看**上一次**崩溃时的日志(关键,不加 `--previous` 看的是当前这次还没崩的);
3. 看退出码:137=OOMKilled(内存不够),1=应用错误,255=配置问题;
4. 如果是 OOM,调 resources.limits 或查内存泄漏;
5. 如果是配置,查 ConfigMap/Secret 挂载对不对;
6. 如果是探针失败,看 liveness/readiness 配置的路径、端口、超时。

**结合项目讲:** 我项目里的 `ReleasePodCrashLooping` 告警就是 `increase(kube_pod_container_status_restarts_total[10m]) >= 3`,CrashLoop 会触发这个告警,而且**永远不被发布抑制规则吞掉**。

### Q10.6 K8s 的 Service 和 Ingress 区别?

**答:**
- **Service**:四层负载均衡,给一组 Pod 一个稳定的虚拟 IP/域名,用 label selector 选 Pod。ClusterIP(集群内)、NodePort(节点端口)、LoadBalancer(云 LB);
- **Ingress**:七层(HTTP/HTTPS)路由,根据 host/path 把流量导到不同 Service。需要 Ingress Controller(nginx/traefik)。

我项目里 Argo Rollouts 的 Canary 流量切分就靠 Nginx Ingress 的 canary 注解,改权重实现百分比切流。

### Q10.7 镜像分层结构和为什么分层?

**答:**
Docker 镜像是一堆**只读层**(layer)叠加,每条 Dockerfile 指令产生一层。容器启动时在上面加一层可写层(CoW,Copy-on-Write)。

**为什么分层:**
1. **复用**:不同镜像共享相同的基础层(比如都基于 ubuntu),只在仓库存一份;
2. **缓存**:BuildKit 构建时,某层 inputs 没变就命中缓存不重跑。我项目里 BuildKit registry cache 就是利用这个——`go mod download` 那层只要 go.mod 没变就缓存命中;
3. **传输**:拉镜像时本地已有的层不重传。

**CoW:** 修改容器里的文件时,会从下面的只读层复制到可写层再改,不改镜像本身。

---

## 附录 A:数字与参数速记表

面试时随手能报的数,背熟:

### 服务目录 profile 参数(`service-catalog.yaml`)
| 参数 | critical-canary | standard-canary | controlled-bluegreen | fast-rolling |
|---|---|---|---|---|
| strategy | canary | canary | bluegreen | rolling |
| waitTimeout | 75m | 30m | 45m | 15m |
| manualPromotion | false | false | **true** | false |
| previewReplicaCount | — | — | **2** | — |
| analysis.minSamples | **3000** | 1000 | 1000 | — |
| analysis.maxErrorRate | **0.01** | 0.02 | 0.01 | — |
| analysis.maxP95Ratio | **1.2** | 1.5 | 1.3 | — |
| inconclusiveTimeout | **15m** | **0s** | — | — |

### Canary 步长(`values.yaml`)
- critical: 1%→5%→20%→50%→100%,每个 pause 5m
- standard: 20%→50%→100%,每个 pause 3m

### 告警 for 时长(`prometheus.yaml`)
| 告警 | for | 可否抑制 |
|---|---|---|
| ReleaseDeployNoiseWindow | 无(立即) | 永不抑制(它是抑制源) |
| ReleasePodRestarting | 无(立即) | **可抑制** |
| ReleasePodCrashLooping | 2m | 永不抑制 |
| ReleasePodStuckTerminating | 2m | 永不抑制 |
| ReleaseReplicaShortage | 5m | 永不抑制 |
| ReleasePodNotReady | 5m | 永不抑制 |

### 资源(原型估算)
| 组件 | 内存 |
|---|---|
| Argo CD controller+repoServer | ~512Mi |
| Prometheus | ~1.5Gi |
| Loki singleBinary | 几百 Mi |
| 13 个业务服务 ×128Mi | 1.6Gi |
| 平台组件合计 | 2.5-3.5Gi |

---

## 附录 B:被问到这些就老实承认

提前想好这些问题的诚实回答,别临场编:

1. **"线上 QPS 多少?"** → 没上线,原型,没有真实流量数据
2. **"故障复盘讲一个?"** → 没有生产故障。开发过程踩的坑可以讲(CN 镜像、Tailscale 组网、告警抑制范围调优)
3. **"Grafana dashboard 长啥样?"** → 没上 Grafana,靠 Prometheus/Loki/Alertmanager 原生 UI
4. **"告警发到哪?钉钉还是邮件?"** → receiver 是空的,原型阶段只验证路由和抑制逻辑,通知渠道是预留位
5. **"契约测试在 CI 里跑吗?"** → 代码完整,本地跑通,没接进 GitHub Actions(CI runner 没 docker),是待办
6. **"应用真的输出了 deploy_id 日志吗?"** → 应用源码在另一个仓库,我定义了规范和采集侧处理,应用侧落地在应用仓库验证
7. **"impact 分析工具是你写的吗?"** → 在应用仓库,我消费它的输出。GitOps 仓库里只消费 impact.json
8. **"Loki 是 HA 的吗?"** → 不是,单副本 filesystem,实验性部署
9. **"platform-server 除了 CLI 还有别的吗?"** → 只有 `/health` 路由和 catalog/release-record 两个 CLI 子命令,没有别的 API
10. **"internal/impact 和 internal/rolloutprober 这俩目录是什么?"** → 如果简历没提就别主动说;如果被翻到,老实说是预留的占位目录,功能靠外部工具实现

**核心原则:不主动暴露短板,但被问到绝不编。** 编一个谎言要用十个谎言圆,面试官一深挖就穿。诚实 + 有设计思考 > 包装 + 怕被问。

---

## 最后:面试节奏建议

1. **开场主动定性**:30 秒陈述里就点明"原型",抢占边界;
2. **被追问设计 why 时**:展开讲,这是你的强项,讲透 trade-off;
3. **被问线上数据时**:诚实说没上线,立刻把话题转向"但我可以这样验证...";
4. **被质疑真实性时**:不辩解,直接说"我给你展开讲讲实现细节",用细节自证;
5. **遇到不会的**:说"这个我没做过,我的理解是...,但没实际验证过"。别硬编。

记住:这个项目的价值不在"上线了",在"每个决策都有理由"。把理由讲清楚,比编一个上线故事有说服力得多。
