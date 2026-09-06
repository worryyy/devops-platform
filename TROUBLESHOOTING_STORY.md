# 排障过程讲稿(面试版)

> 配合 `INTERVIEW_QA.md` 使用。核心策略:**项目是原型,没有生产事故,不编线上故事**。要讲的是开发过程中的真实调试与设计加固——这个故事在仓库里有完整代码证据(当前未提交的 5 个文件 diff),怎么追问都穿不了。

---

## 主推故事:发布窗口抑制范围过宽,可能吞掉真故障告警

> 这是仓库当前未提交 diff 的真实内容,涉及 `prometheus.yaml`(规则本体)、`alerting_rules_test.yml`(单测)、`test-observability.sh`(契约断言)、`README.md`(语义文档)、`Jenkinsfile`(失败通知带 deploy_id)。

### 讲稿(约 2 分钟,症状→根因→修复→防回归)

> 我讲一个做告警治理时发现的设计缺陷,整个排查和修复过程。
>
> **发现。** 我给告警规则补 promtool 单测,构造"发布期间 Pod 状态异常"的场景。写到 CrashLoop 场景时推演出一个问题:初版抑制规则里,被抑制的 target 是两条——`ReleasePodRestarting` 和 `ReleasePodTerminating`。CrashLoop 的外在表现恰恰也是"反复重启",会先命中 Restarting;而 Terminating 更糟,它一个告警名混了两种语义——正常发布时 Pod 退出会有几秒 Terminating,卡死时 Pod 会有几分钟 Terminating,初版对这两种一视同仁地抑制。
>
> **推演后果。** 最坏场景是:发版把服务搞挂,Pod 反复崩溃或卡死,但因为发布窗口开着,这些告警全被当成"发布噪声"抑制掉——发布确实会被 AnalysisRun 判失败然后自动回退,但**值班的人全程收不到任何通知**。等发布窗口关闭告警才冒出来,或者干脆服务已经挂了一阵了。告警系统"静默吞故障"是最恶劣的事故形态,比告警风暴还危险。
>
> **根因。** 初版是按"告警类别"划抑制范围的——凡是 deploy_noise 类、发生在发布窗口内的都压。这个划分粒度不够,真正该用的切分维度是**故障语义:瞬时还是持续**。单次重启是发布的正常伴生现象;10 分钟崩 3 次、Terminating 超 5 分钟,是持续性故障,和发布无关,必须透传。
>
> **修复动了四刀:**
> 1. **拆语义**:把 PodTerminating 改名 StuckTerminating,判定条件改成"deletion_timestamp 超过 300 秒"——正常退出的几秒不会触发,只有真卡住才告,severity 也从 info 提到 warning;
> 2. **补盲区**:新增 CrashLooping 告警,10 分钟内重启 ≥3 次才触发,前面带 2 分钟 for,过滤单次抖动;
> 3. **收白名单**:两条抑制规则的 target 收紧到只剩 ReleasePodRestarting 一条;
> 4. **修时序**:还有一个顺带发现的时序 bug——NoiseWindow(抑制源)原来有 1 分钟 for,但 Restarting 是无 for 立即触发的,发布开始后的第一分钟抑制源还没 firing,噪声会漏出去。所以把 NoiseWindow 的 for 去掉,抑制源的可用性必须不晚于被抑制对象。同时把 user_impact 那条抑制的 source 限定为 revision 级,只有精确关联到 deploy_id 的用户影响告警才有资格去压噪声。
>
> **防回归。** 光改完不锁住,以后还会被人改坏。所以同步加了三层:promtool 单测新增三组用例——NoiseWindow 无 grace 立即触发、CrashLooping 必须 3 次以上触发而瞬态重启不触发、StuckTerminating 超 5 分钟触发而 5 分钟内静默;契约测试加了**白名单断言**——抑制 target 里出现 CrashLooping/StuckTerminating/ReplicaShortage/PodNotReady 任何一个直接 fail;README 同步更新了语义文档。
>
> 这件事给我的沉淀是:**抑制规则的安全边界应该用"永不抑制清单"来定义,而不是"允许抑制清单";测试要断言语义而不是语法**——YAML 语法合法和告警行为正确是两回事。

### 这段讲稿对应简历哪句

"仅抑制短时 Pod 重启通知,对持续未就绪、副本不足及 CrashLoop 等故障始终保留告警,并通过 CI 契约测试防止抑制范围被意外扩大"——**整句就是这次修复后的状态**。面试官听到这里会对上号。

---

## 备选故事 B:样本不足导致误回滚 → NaN sentinel

**症状:** AnalysisTemplate 查 canary 错误率,低流量时段或 canary 刚起时查询返回空向量,Argo Rollouts 的 Prometheus provider 拿不到结果会按 error 计,叠加到 errorLimit 后发布直接判 Failed 自动回滚——一个可能完全没问题的版本被"测不到数据"误杀。

**根因:** 把"测不到"和"测出来是坏的"混为一谈。查询空、Stable 基线自己坏了、Prometheus 抖了,这三种情况都不构成"新版本有问题"的证据。

**修复:** 每个 metric 查询末尾加 `or on() (vector(0) / vector(0))`——空结果时兜底返回 NaN。NaN 和任何阈值比较都是 false,successCondition 和 failureCondition 同时不满足,Argo Rollouts 判 **Inconclusive** 而不是 Failed:发布暂停等人决策,不自动回滚。再配 inconclusiveTimeout 兜底(critical 15 分钟没人工介入就终止发布,不会无限挂)。

**防回归:** promtool 用例覆盖"低流量不误报""P95 需要足够样本""rev3 没有 release_info 就不告警"。

**一句话版:** "误杀好版本和漏放坏版本都糟,测量工具不可信时最安全的动作是暂停,不是强行决策。"

---

## 备选故事 C:蓝绿回退可能拿到"刚失败的版本"当目标

**症状(推演 + fixture 验证):** 蓝绿 post-promotion 失败时(流量已切到新版本、切完后分析才失败),Argo Rollouts 的 `status.stableRS` 可能已经更新成新版本。回退 L1 直接拿 stableRS 抠 digest,会把**刚失败的版本**当回退目标——回了个寂寞。

**修复:** `rollback-release.sh` 的 resolve-target 里加了校验,检测 stableRS 与当前 activeSelector 的关系,命中"stable 就是当前活跃 RS"的情况就不采信 L1,降级到 L2 查 PostgreSQL 的 stable 记录;同时蓝绿 post-promotion 失败的动作用 `rollouts undo`(回上一个 RS)而不是 `abort`。

**防回归:** `ci/fixtures/rollback/` 下 11 个 fixture JSON 覆盖各回退场景,`test-rollback-release.sh` 离线跑。

> ⚠️ 讲这个故事前,自己先读一遍 `k3s/ci/scripts/rollback-release.sh` 第 140-165 行,把 stableRS/activeSelector 的判断方向看明白——面试官可能追问具体判断逻辑。

---

## 部署与运行期排障储备(D/E/F/G)

> 与主推故事同一原则:这些是搭建和调试过程中真实踩过/推演并加固的配置点,仓库里都有代码证据,不是编造的线上事故。被问"部署时踩过什么坑"时按需取用。

### 储备 D:rootless BuildKit 在 K8s Pod 里起不来

- **症状**: Jenkins agent Pod 里 buildkitd sidecar 反复重启,`buildctl debug workers` 探活失败,构建全部卡在排队。
- **根因**: rootless BuildKit 期望用户命名空间(容器内 uid 0 映射宿主非特权 uid),而 k3s 默认没开 per-pod userns;同时 seccomp/AppArmor 默认 profile 会拦截它创建 overlay/挂载的系统调用。
- **修复**(证据 `k3s/ci/jenkins/ecampus.Jenkinsfile` buildkitd 容器): `--oci-worker-no-process-sandbox`(关掉进程沙箱,容器内不需要嵌套 namespace)、`seccompProfile/appArmorProfile: Unconfined`、`runAsUser: 1000`。每个开关都是为了把"无 root 的镜像构建"塞进受安全策略约束的 Pod。
- **一句话**: "rootless 换来了安全边界,代价是和 K8s 安全策略打架——我要能讲清楚每个开关买到了什么、放弃了什么。"

### 储备 E:带缓存 PVC 的 agent Pod 启动极慢

- **症状**: 挂 `jenkins-agent-cache` PVC 的构建 Pod Ready 前要挂住几分钟,越用越慢。
- **根因**: kubelet 对 fsGroup 卷默认递归 chown 整个卷;Go module/build 缓存和 BuildKit 状态是几十万个小文件,每次挂载都全量 chown 一遍,io 被打满。
- **修复**(证据 Jenkinsfile Pod securityContext): `fsGroup: 1000` 保证 uid 1000 可写,`fsGroupChangePolicy: OnRootMismatch` 只在根目录属主不符时才 chown。
- **一句话**: "缓存卷的性能问题不在读写,在挂载。"

### 储备 F:跨机房 Tailscale 组网,flannel 绑错网卡节点 NotReady

- **症状**(典型形态): agent join 成功但节点反复 NotReady,或跨节点 Pod 网络不通——flannel autodetect 选了默认路由的 eth0(公网/内网 NAT 后地址),而节点间真正可达的是 Tailscale 100.x 地址。
- **修复**(证据 `k3s/roles/k3s_server/tasks/main.yml` + `group_vars/all.yml`): 安装参数显式钉死 `--flannel-iface=tailscale0 --advertise-address/--node-ip/--tls-san=<tailscale-ip>`,不依赖自动探测。
- **附带防御**(同文件): playbook 开头检测 server 节点上残留 `k3s-agent.service` 直接 fail 并提示跑 `k3s-agent-uninstall.sh`——这个防御本身就是踩过"agent/server 混装导致 token 和状态错乱"的坑之后加的。
- **一句话**: "overlay 网络上组 k3s,三件套(iface/node-ip/tls-san)必须一起钉死,少一个就是 NotReady 或证书不匹配。"

### 储备 G:发布标签差点打爆 Prometheus 基数

- **症状(推演 + explain 验证)**: 如果把 deploy_id/git_sha 等标签直接留在业务指标上,每次发布 deploy_id 都会变,一批全新的 `ecampus_http_*` 时间序列产生又变成孤儿,Prometheus 内存和查询随发布次数线性劣化。
- **修复三层**(证据 `k3s/helm-values/platform/prometheus.yaml`): ① 通用 pod 抓取 `metric_relabel_configs labeldrop` 掉全部 delivery_platform_* 标签;② release 元信息走独立的 2 分钟低频 job,只保留 `app_deploy_info` 单指标;③ SLI recording rule 只按 `namespace/service/environment/revision` 聚合,release 身份在告警表达式里 `group_left` 现场关联——新发布不产生新 SLI 序列。
- **一句话**: "高基数标签治理的原则是:元数据可以存在,但不能进入被 rate() 聚合的原始序列。"——这正好是简历第四条"SLI 仅保留必要维度,触发时再关联"的实现。

### 储备 H:三节点部署期新坑记录模板(占位)

> 2026-09 起做基准测试部署时,每踩一个坑按此格式记一条,事后挑最典型的 1~2 个替换掉上面较弱的储备:

```
### 储备 X:<一句话现象>
- 症状:<看到的表象 + 定位命令>
- 根因:<真实原因,不是第一个猜的方向>
- 修复:<改了什么,证据 commit/文件>
- 一句话:<可复述的沉淀>
```

---

## 追问预案

| 追问 | 应答 |
|---|---|
| "这是线上发生的事故吗?" | 不是,是写单测推演场景时发现的。正因为是原型,我的做法是用契约测试把语义锁住——比等线上出事再修便宜一个数量级。主动补一句:原型阶段的价值就是把这类设计缺陷在测试里暴露,而不是在生产里 |
| "为什么 Restarting 可以被压?" | 它是单次重启、无 grace,滚动更新必然产生;CrashLoop 是 10 分钟 ≥3 次的持续模式,是病不是现象 |
| "NoiseWindow 为什么不能有 for?" | 抑制时序:Restarting 无 for 立即触发,抑制源如果延迟 1 分钟才 firing,发布头一分钟的噪声全漏出去。抑制源的可用性必须不早于被抑制对象(口误修正:必须**不晚于**) |
| "抑制的告警去哪了?" | 抑制≠消失,告警照常触发、Alertmanager UI 里可见,只是不发通知。事后可以在 Status 页看到被抑制的告警,做复盘 |
| "真发生了误抑制的事故怎么应急?" | Alertmanager 支持热 reload 配置,紧急情况删 inhibition 规则 reload 即恢复通知;根治靠"永不抑制清单"进契约测试 |
| "怎么验证修复没引入新问题?" | 三层:promtool 单测(行为)、amtool 路由测试(路由)、yq 断言(for 时长、白名单)——全部本地可跑,不需要集群 |
| "如果面试官非要问线上故障复盘呢?" | 诚实:项目是原型没有线上事故。然后说"但我可以讲我推演过的最坏场景和防御设计"——把话筒引回主推故事 |

---

## 实战排障案例库（2026-09 三节点部署与首次端到端运行实录）

> 以下全部来自把这套平台第一次真正跑通的过程（22+ 个问题中的精选）,每个都有 git 提交或 benchmarks/evidence 证据。格式:症状→根因→修复→一句话沉淀。面试讲法:挑 2-3 个与岗位相关的,按"发现→定位命令→根因→修复→防回归"展开。

### 案例 1:安全组掐断阿里云系统服务,DNS 全灭但"看起来像 Tailscale 的锅"
- **症状**:节点 Ready 但 Pod 无法解析任何外网域名;`dig @100.100.2.138` 超时。第一反应是 Tailscale 劫持了 CGNAT 段(100.64/10)——`ip route get` 证明路由其实走 eth0,排除。
- **定位**:`ip route get 100.100.2.138`(路由正确)→ `dig @223.5.5.5`(公共 DNS 正常)→ 差异只在阿里云内网 DNS。
- **根因**:安全组出方向没放行到阿里云系统服务段(100.100.2.136/138 DNS、100.100.100.200 元数据),VPC 内 egress 策略一刀切。
- **修复**:节点 systemd-resolved 全局切换公共 AliDNS(223.5.5.5/119.29.29.29, Domains=~.),ACR VPC 端点同样被挡→镜像地址全改公网端点。
- **一句话**:overlay 网络和 CGNAT 段重叠时先看路由再看安全组,"最常见的嫌疑人不是真凶"。

### 案例 2:镜像加速的三层漏配——containerd/ctr/wget 各走各的路
- **症状**:配了 k3s registries.yaml 镜像加速,Pod 拉镜像还是超时;更怪的是同一个 docker.io 镜像,kubelet 拉得动、`ctr pull` 拉不动、Pod 里 `wget quay.io` 也挂。
- **根因**:三个组件三套解析——registries.yaml 只作用于 CRI 镜像拉取;ctr CLI 默认连另一个 socket 且不走 mirror;构建容器里的 wget 是裸 HTTP 请求谁也帮不了。
- **修复**:分层治——containerd 加 mirror(1ms.run 主+daocloud 备);CI 容器改用预烘焙镜像(golang-ci/alpine-tools,零运行时 apk);外部二进制工具(kubectl)Mac 下载后直接预置进 PVC。
- **一句话**:"配了镜像加速"要能说清加速的是哪一层的流量——镜像拉取、CLI 操作、构建内网络是三条独立通路。

### 案例 3:buildkitd 在高负载下被 kubelet"误杀"
- **症状**:4 服务并行构建时整个 agent Pod 突然消失,构建中止。事件里是 liveness probe failed: `buildctl debug workers` timed out。
- **根因**:探针默认 1s exec 超时,而 buildkitd 忙于 4 路构建时连本地 socket 响应都超过 1s,连续 3 次失败触发重启,级联杀死整个构建 Pod。
- **修复**:liveness/readiness timeout 10s + failureThreshold 6 + initialDelay 30s;注释写明"负载下探针误杀健康的 busy 进程"。
- **一句话**:给重负载守护进程配探针,超时必须按"最忙时刻"而不是"空闲时刻"定。

### 案例 4:两层 OOM 的先后误诊——先修显性的,再抓隐藏的
- **症状**:13 服务并行编译,go 容器 OOM(6Gi 上限);改 4 个一批后过,几轮后 buildkitd 又 OOM(3Gi)。
- **根因**:第一批修的是 go 编译器内存(cgo 下单编译进程 300-800MB);第二层是 buildkitd 自身缓存 4 路构建状态的内存,初始 3Gi 上限拍脑袋定的。
- **修复**:go 8Gi + buildkitd 6Gi + 批次 4→1(本地缓存导出器不支持并发写,详见案例 5)。
- **一句话**:并行度×单实例内存才是真实预算,逐层 OOM 是分批暴露的——第一次修完不代表没有第二层。

### 案例 5:buildkit 本地缓存的"不对称 API"和并发踩踏
- **症状**:换本地缓存后先报 `local cache exporter requires dest`(参数叫 dir),改完又报 `importer requires src`(导入叫 src);再改对又出现 ingest 目录 rename 失败。
- **根因**:buildctl 的 local exporter/importer 参数名不对称(dest/src);且 local 目录导出对多进程并发写不安全——4 个并行 buildctl 同时往一个目录写 ingest 互相覆盖。
- **修复**:导出 dest/导入 src + 批次降为 1(构建串行)。
- **一句话**:读文档要看参数表而不是想当然;共享目录型缓存的并发安全性要看实现,不能假设。

### 案例 6:ACR 个人版的三连击——Bearer 流程、仓库名层级、cacheconfig 清单
- **症状**:(a) curl 带 basic auth 打 /v2/ 返回 401;(b) 推送 `pulseops/buildkit-cache/comment` 报 insufficient_scope;(c) 缓存导出报 `unknown manifest class for application/vnd.buildkit.cacheconfig.v0`。
- **根因**:(a) ACR 走 registry Bearer token 协议,basic 只在换 token 时用;(b) 个人版仓库名不允许斜杠,只有命名空间/仓库单层;(c) 个人版清单校验白名单不认 buildkit 的 cacheconfig 媒体类型。
- **修复**:(a) 探测脚本实现 token 舞步(probe-registry-cache.sh);(b) 缓存仓库拍平成 `buildkit-cache-<svc>`;(c) registry 层缓存整体改走 PVC 本地目录。
- **一句话**:云厂商 registry 的"兼容 Docker Registry API"都有方言——鉴权流、命名规则、清单白名单三处都可能不兼容,选型前用真实工作流压一遍。

### 案例 7:Jenkins 的四个"反直觉"行为连环坑
- **症状与根因**:
  - withEnv 给变量赋**空字符串等于删除该变量** → 下游 set -u 直接爆 "parameter not set";
  - 流水线 parameters{} 的 defaultValue **只在参数首次注册时生效**,之后改 Jenkinsfile 不更新任务里存的旧默认值;
  - Groovy ''' 字符串里 `\(` 是非法转义,shell 正则原样写会编译失败——复杂 shell 挪进 .sh 文件;
  - Go flag 包对 `--help` 打印用法后**以退出码 2 退出**,拿它当健康断言必挂。
- **修复**:空值 shell 侧 `${VAR:-}` 兜底;重建任务或触发时显式传参;复杂 shell 独立脚本化;--help 断言加 `|| true`。
- **一句话**:CI 系统的"文档化行为"和"实际行为"之间隔着 N 个 JIRA——踩过的每个坑都要沉淀成团队 wiki。

### 案例 8:DNS 解析的地址族陷阱——goproxy.cn 的 AAAA 记录
- **症状**:go mod download 全量报 `dial tcp [240e:...]: network is unreachable`,但同一镜像里 curl 同域名正常。
- **根因**:goproxy.cn 双栈解析,Go 的模块下载路径优先选了 AAAA,而 Pod 网络无 IPv6 出口;curl 走 Happy Eyeballs 回退了 v4 所以没事。
- **修复**:GODEBUG=netdns=go 强制纯 Go 解析器(带回退)+ GOPROXY 多代理链(goproxy.cn 主、aliyun 备——备用源还救过一次阿里云镜像返回损坏 zip 的问题)。
- **一句话**:无 IPv6 的网络里,任何"偶尔挂"的下载问题先查 AAAA 记录和解析器行为。

### 案例 9:"从未跑过"的幽灵配置——`--impact` 旗标不存在
- **症状**:流水线在 catalog 解析处报 `flag provided but not defined: -impact`。
- **根因**:Jenkinsfile 调用了一个**代码里从未实现过的旗标**——这套流水线此前从未端到端执行过,纸面设计和代码实现从未对齐(同类还有:服务目录的 PG Service 名用的旧 bitnami 命名、Dockerfile 内 apk 用境外源)。
- **修复**:删旗标(impact 数据流水线直接读 impact.json);PG URL 改实际 Service 名。
- **一句话**:面试被问"你这流水线跑过吗"时,能回答"我把从未跑通的它跑通了,并修了 22 个这样的裂缝"比"跑过"有力得多。

---

## 使用建议

1. **只主推一个故事**(抑制范围),备选 B/C 是同一面试里被要求"再讲一个"时的储备;
2. 讲之前把当前未提交的 diff 自己过一遍(`git diff`),确保讲的每个细节你都眼见为实;
3. 练到能脱稿 2 分钟讲完"发现→根因→修复→防回归"四段,中间不打磕巴;
4. 语速放慢,讲到"值班的人全程收不到任何通知"这种后果句时停顿一下——排障故事的说服力在后果,不在术语密度。
