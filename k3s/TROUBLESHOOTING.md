# k3s 集群部署与运行排障实录

> 来自 2026-09 三节点部署与首次端到端运行（22+ 个问题中的精选）。
> 格式：症状 → 定位 → 根因 → 修复 → 一句话沉淀。重搭集群（如升配 2×4c8g）
> **先走 [DEPLOY_RUNBOOK.md](./DEPLOY_RUNBOOK.md) 的预检与分阶段验证门**，
> 本文件是卡住时的排查字典；每踩新坑按文末模板补一条。
> 原始完整版（含发布策略类排障）在 git 历史：
> `git show HEAD:TROUBLESHOOTING_STORY.md`。

## 实战案例库

### 案例 1：安全组掐断阿里云系统服务，DNS 全灭但"看起来像 Tailscale 的锅"
- **症状**：节点 Ready 但 Pod 无法解析任何外网域名；`dig @100.100.2.138` 超时。第一反应是 Tailscale 劫持了 CGNAT 段（100.64/10）——`ip route get` 证明路由其实走 eth0，排除。
- **定位**：`ip route get 100.100.2.138`（路由正确）→ `dig @223.5.5.5`（公共 DNS 正常）→ 差异只在阿里云内网 DNS。
- **根因**：安全组出方向没放行到阿里云系统服务段（100.100.2.136/138 DNS、100.100.100.200 元数据），VPC 内 egress 策略一刀切。
- **修复**：节点 systemd-resolved 全局切换公共 AliDNS（223.5.5.5/119.29.29.29, Domains=~.），ACR VPC 端点同样被挡 → 镜像地址全改公网端点。
- **一句话**：overlay 网络和 CGNAT 段重叠时先看路由再看安全组，"最常见的嫌疑人不是真凶"。

### 案例 2：镜像加速的三层漏配——containerd/ctr/wget 各走各的路
- **症状**：配了 k3s registries.yaml 镜像加速，Pod 拉镜像还是超时；更怪的是同一个 docker.io 镜像，kubelet 拉得动、`ctr pull` 拉不动、Pod 里 `wget quay.io` 也挂。
- **根因**：三个组件三套解析——registries.yaml 只作用于 CRI 镜像拉取；ctr CLI 默认连另一个 socket 且不走 mirror；构建容器里的 wget 是裸 HTTP 请求谁也帮不了。
- **修复**：分层治——containerd 加 mirror（1ms.run 主 + daocloud 备）；CI 容器改用预烘焙镜像（golang-ci/alpine-tools，零运行时 apk）；外部二进制工具（kubectl）Mac 下载后直接预置进 PVC。
- **一句话**："配了镜像加速"要能说清加速的是哪一层的流量——镜像拉取、CLI 操作、构建内网络是三条独立通路。

### 案例 3：buildkitd 在高负载下被 kubelet"误杀"
- **症状**：4 服务并行构建时整个 agent Pod 突然消失，构建中止。事件里是 liveness probe failed: `buildctl debug workers` timed out。
- **根因**：探针默认 1s exec 超时，而 buildkitd 忙于 4 路构建时连本地 socket 响应都超过 1s，连续 3 次失败触发重启，级联杀死整个构建 Pod。
- **修复**：liveness/readiness timeout 10s + failureThreshold 6 + initialDelay 30s（已固化在 Jenkinsfile，注释写明"负载下探针误杀健康的 busy 进程"）。
- **一句话**：给重负载守护进程配探针，超时必须按"最忙时刻"而不是"空闲时刻"定。

### 案例 4：两层 OOM 的先后误诊——先修显性的，再抓隐藏的
- **症状**：13 服务并行编译，go 容器 OOM（6Gi 上限）；改 4 个一批后过，几轮后 buildkitd 又 OOM（3Gi）。
- **根因**：第一批修的是 go 编译器内存（cgo 下单编译进程 300-800MB）；第二层是 buildkitd 自身缓存 4 路构建状态的内存，初始 3Gi 上限未经实测校验。
- **修复**：go 8Gi + buildkitd 6Gi + 批次 4→1（本地缓存导出器不支持并发写，见案例 5）。
- **一句话**：并行度 × 单实例内存才是真实预算，逐层 OOM 是分批暴露的——第一次修完不代表没有第二层。

### 案例 5：buildkit 本地缓存的"不对称 API"和并发踩踏
- **症状**：换本地缓存后先报 `local cache exporter requires dest`（参数叫 dir），改完又报 `importer requires src`（导入叫 src）；再改对又出现 ingest 目录 rename 失败。
- **根因**：buildctl 的 local exporter/importer 参数名不对称（dest/src）；且 local 目录导出对多进程并发写不安全——4 个并行 buildctl 同时往一个目录写 ingest 互相覆盖。
- **修复**：导出 dest / 导入 src + 批次降为 1（构建串行）。
- **一句话**：读文档要看参数表而不是想当然；共享目录型缓存的并发安全性要看实现，不能假设。

### 案例 6：ACR 个人版的三连击——Bearer 流程、仓库名层级、cacheconfig 清单
- **症状**：(a) curl 带 basic auth 打 /v2/ 返回 401；(b) 推送 `pulseops/buildkit-cache/comment` 报 insufficient_scope；(c) 缓存导出报 `unknown manifest class for application/vnd.buildkit.cacheconfig.v0`。
- **根因**：(a) ACR 走 registry Bearer token 协议，basic 只在换 token 时用；(b) 个人版仓库名不允许斜杠，只有命名空间/仓库单层；(c) 个人版清单校验白名单不认 buildkit 的 cacheconfig 媒体类型。
- **修复**：(a) 探测脚本实现 token 舞步（probe-registry-cache.sh，脚本已于 2026-09 清理移除，历史在 git）；(b) 缓存仓库拍平成 `buildkit-cache-<svc>`；(c) registry 层缓存整体改走 PVC 本地目录。
- **一句话**：云厂商 registry 的"兼容 Docker Registry API"都有方言——鉴权流、命名规则、清单白名单三处都可能不兼容，选型前用真实工作流压一遍。

### 案例 7：Jenkins 的四个"反直觉"行为连环坑
- **症状与根因**：
  - withEnv 给变量赋**空字符串等于删除该变量** → 下游 `set -u` 直接爆 "parameter not set"；
  - 流水线 parameters{} 的 defaultValue **只在参数首次注册时生效**，之后改 Jenkinsfile 不更新任务里存的旧默认值；
  - Groovy `'''` 字符串里 `\(` 是非法转义，shell 正则原样写会编译失败——复杂 shell 挪进 .sh 文件；
  - Go flag 包对 `--help` 打印用法后**以退出码 2 退出**，拿它当健康断言必挂。
- **修复**：空值 shell 侧 `${VAR:-}` 兜底；重建任务或触发时显式传参；复杂 shell 独立脚本化；--help 断言加 `|| true`。
- **一句话**：CI 系统的"文档化行为"和"实际行为"之间隔着 N 个 JIRA——踩过的每个坑都要沉淀成文档。

### 案例 8：DNS 解析的地址族陷阱——goproxy.cn 的 AAAA 记录
- **症状**：go mod download 全量报 `dial tcp [240e:...]: network is unreachable`，但同一镜像里 curl 同域名正常。
- **根因**：goproxy.cn 双栈解析，Go 的模块下载路径优先选了 AAAA，而 Pod 网络无 IPv6 出口；curl 走 Happy Eyeballs 回退了 v4 所以没事。
- **修复**：GODEBUG=netdns=go 强制纯 Go 解析器（带回退）+ GOPROXY 多代理链（goproxy.cn 主、aliyun 备——备用源还救过一次阿里云镜像返回损坏 zip 的问题）。
- **一句话**：无 IPv6 的网络里，任何"偶尔挂"的下载问题先查 AAAA 记录和解析器行为。

### 案例 9："从未跑过"的幽灵配置——`--impact` 旗标不存在
- **症状**：流水线在 catalog 解析处报 `flag provided but not defined: -impact`。
- **根因**：Jenkinsfile 调用了一个**代码里从未实现过的旗标**——流水线此前从未端到端执行过，纸面设计和代码实现从未对齐（同类还有：服务目录的 PG Service 名用的旧 bitnami 命名、Dockerfile 内 apk 用境外源）。
- **修复**：删旗标（impact 数据流水线直接读 impact.json）；PG URL 改实际 Service 名。
- **一句话**：流水线改完必须真实端到端跑一次——"从未跑通的配置"里藏着无数这类裂缝。

## 配置加固点（部署时容易漏的防御，均有代码证据）

### D：rootless BuildKit 在 K8s Pod 里起不来
- **症状**：Jenkins agent Pod 里 buildkitd sidecar 反复重启，`buildctl debug workers` 探活失败，构建全部卡在排队。
- **根因**：rootless BuildKit 期望用户命名空间（容器内 uid 0 映射宿主非特权 uid），而 k3s 默认没开 per-pod userns；同时 seccomp/AppArmor 默认 profile 会拦截它创建 overlay/挂载的系统调用。
- **修复**（证据 `k3s/ci/jenkins/ecampus.Jenkinsfile` buildkitd 容器）：`--oci-worker-no-process-sandbox`（关掉进程沙箱）、`seccompProfile/appArmorProfile: Unconfined`、`runAsUser: 1000`。每个开关都是为了把"无 root 的镜像构建"塞进受安全策略约束的 Pod。

### E：带缓存 PVC 的 agent Pod 启动极慢
- **症状**：挂 `jenkins-agent-cache` PVC 的构建 Pod Ready 前要挂住几分钟，越用越慢。
- **根因**：kubelet 对 fsGroup 卷默认递归 chown 整个卷；Go module/build 缓存和 BuildKit 状态是几十万个小文件，每次挂载都全量 chown 一遍，io 被打满。
- **修复**（证据 Jenkinsfile Pod securityContext）：`fsGroup: 1000` 保证 uid 1000 可写，`fsGroupChangePolicy: OnRootMismatch` 只在根目录属主不符时才 chown。
- **一句话**：缓存卷的性能问题不在读写，在挂载。

### F：跨机房 Tailscale 组网，flannel 绑错网卡节点 NotReady
- **症状**：agent join 成功但节点反复 NotReady，或跨节点 Pod 网络不通——flannel autodetect 选了默认路由的 eth0（公网/内网 NAT 后地址），而节点间真正可达的是 Tailscale 100.x 地址。
- **修复**（证据 `k3s/roles/k3s_server/tasks/main.yml` + `group_vars/all.yml`）：安装参数显式钉死 `--flannel-iface=tailscale0`、`--advertise-address/--node-ip/--tls-san=<tailscale-ip>`，不依赖自动探测。
- **附带防御**（同文件）：playbook 开头检测 server 节点上残留 `k3s-agent.service` 直接 fail 并提示跑 `k3s-agent-uninstall.sh`——这个防御是踩过"agent/server 混装导致 token 和状态错乱"之后加的。
- **一句话**：overlay 网络上组 k3s，三件套（iface/node-ip/tls-san）必须一起钉死，少一个就是 NotReady 或证书不匹配。

### G：发布标签差点打爆 Prometheus 基数
- **症状**（推演 + explain 验证）：如果把 deploy_id/git_sha 等标签直接留在业务指标上，每次发布都会产生一批全新又立即变孤儿的 `ecampus_http_*` 时间序列，Prometheus 内存和查询随发布次数线性劣化。
- **修复三层**（证据 `k3s/helm-values/platform/prometheus.yaml`）：① 通用 pod 抓取 `metric_relabel_configs labeldrop` 掉全部 delivery_platform_* 标签；② release 元信息走独立的 2 分钟低频 job 只保留单指标；③ SLI recording rule 只按 `namespace/service/environment/revision` 聚合，release 身份在告警表达式里 `group_left` 现场关联——新发布不产生新 SLI 序列。
- **一句话**：高基数标签治理的原则是——元数据可以存在，但不能进入被 rate() 聚合的原始序列。

## 新坑记录模板

每踩一个新坑按此格式记一条：

```
### 案例 N：<一句话现象>
- 症状：<看到的表象 + 定位命令>
- 根因：<真实原因，不是第一个猜的方向>
- 修复：<改了什么，证据 commit/文件>
- 一句话：<可复述的沉淀>
```

## 2026-09-21 重搭实录（升配 4c16g + 2×4c8g，案例 G-L）

### 案例 G：阿里云内网 DNS（100.100.2.136/138）被安全组整段掐断
- **症状**：节点 TCP 443 到公网 IP 全通，但任何域名解析超时；`dig @223.5.5.5` 正常。
- **修复**：三台 systemd-resolved 全局切公共 DNS
  （`/etc/systemd/resolved.conf.d/public-dns.conf`: DNS=223.5.5.5 119.29.29.29, Domains=~.）。

### 案例 H：github.com 的部分 A 记录（如 20.205.243.166）TCP 层被掐，且 coredns 的 Corefile 改 ConfigMap 会被 k3s 重置
- **症状**：节点/pod 内 git clone 间歇超时；改 kube-system/coredns ConfigMap 加 hosts
  后一次 k3s 重启（registries 变更触发）配置被还原，Argo CD 全部 app 变 Unknown。
- **修复**：把 github.com 钉扎进 k3s server 的
  `/var/lib/rancher/k3s/server/manifests/coredns.yaml`（k3s 不覆盖已存在的该文件）。
  另注意 coredns 的 hosts 插件每个 server block 只能用一次（与 NodeHosts 合并写）。
- **一句话**：CoreDNS 定制要写进 k3s manifest 而不是 ConfigMap；"改了没生效"先看是不是被 k3s 还原了。

### 案例 I：ACR 个人版自动清理 tag，`<repo>:dev` 一夜消失
- **症状**：昨天能拉的业务镜像今天 NotFound，但 digest 引用仍可拉。
- **修复**：重搭时从 Ecampus-go 源码本地交叉编译（GOARCH=amd64，注意 QEMU 模拟编译
  Go 会段错误，改原生交叉编译 + 轻量 Dockerfile）重新推 tag；长期靠流水线 digest 钉扎。

### 案例 J：Argo CD repo-server 拉远端 chart 仓库超时（bitnami index 27MB）+ go-git 默认超时小于跨境 TLS 尖峰
- **症状**：redis/prometheus app 长期 Unknown：`error fetching chart` /
  `awaiting headers: context deadline exceeded`；同节点 git CLI 正常。
- **修复**：① repo-server env `ARGOCD_GIT_HTTP_TIMEOUT=60`；② chart vendor 进
  GitOps 仓库（k3s/charts/vendor/），不再引用远端 chart 仓库。

### 案例 K：bitnami chart 与官方镜像混用会炸 + bitnami/redis:latest 在 mirror 上不存在
- **症状**：rabbitmq 官方镜像起在 bitnami chart 里，init 容器找
  /opt/bitnami/scripts/liblog.sh 失败；redis chart 默认 bitnami/redis:latest 拉不到。
- **修复**：rabbitmq 改原生 StatefulSet（k3s/manifests/dependencies/rabbitmq.yaml，
  注意 readiness 的 rabbitmq-diagnostics 需 timeoutSeconds≥8）；redis 钉
  bitnamilegacy/redis:8.2.1-debian-12-r0。bitnami 系镜像经 docker.1ms.run 拉取，
  需要 `global.security.allowInsecureImages: true`。

### 案例 L：ecampus 服务对中间件的 service 名有硬编码期望（redis/mongo），helm release fullname 对不上
- **症状**：服务 panic `lookup mongo ... no such host`，而 mongo 实际叫 mongo-mongodb。
- **修复**：ExternalName 别名（app ns 内 `redis`→redis-master、`mongo`→mongo-mongodb）。
  另：envFrom 的 secret 键名会原样成为环境变量名，必须大写
  （platform-server-auth 用 DATABASE_URL/JWT_SECRET，不是 database-url）。
