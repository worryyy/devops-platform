# 三节点部署与指标采样 · 执行计划(Runbook)

> 前提:两台 2c4g + 一台 4c8g(可升 4c16g),Tailscale 组网,Mac 可 SSH 三台。
> 指标口径与简历占位见 `BENCHMARK_PLAN.md`,本文只管"怎么一步步做完"。
> 每个阶段末尾有验收门槛,**不过门槛不进下一阶段**;每个命令旁标注预计耗时。
> 踩坑即记:按 `TROUBLESHOOTING_STORY.md` 储备 H 模板记录,事后挑选入面试素材。

---

## Phase 0 · 环境核查与角色定型(15 min)

- [ ] Mac 安装并登录 Tailscale;`tailscale status` 三台机器在线
- [ ] `ping` 三个 Tailscale IP 全通;`ssh nodeX` 免密全通
- [ ] 逐台采集规格并记录(写入本文末尾"环境记录"):

```shell
ssh nodeX 'nproc; free -h | head -2; df -h / ; uname -a; ip -4 addr show tailscale0'
```

- [ ] **磁盘红线**:server 节点 `/` 可用 ≥ 100G(PVC 合计约 80–100G:Jenkins 20G + BuildKit 30G + Prometheus 8G + Loki 20G + 零头);不足先扩盘或砍 Loki 保留期
- [ ] 节点间互通:node1→node2/node3 互相 `ping` Tailscale IP(flannel 走 tailscale0 的前置)
- [ ] 定角色:**大内存机 = node1(k3s server)**;若现状不符,改 `k3s/inventory/dev.ini` 对调
- [ ] `dev.ini` 里 node3 取消注释,填 IP 与 SSH 用户

**验收**:三台规格/互通性记录在案;inventory 三节点就位。

## Phase 1 · k3s 集群(30–45 min,不含下载波动)

- [ ] 从 Mac 执行(playbooks 含 bootstrap→k3s→kubeconfig→verify):

```shell
cd k3s && ansible-playbook playbooks/site.yml
```

- [ ] 确认安装参数里 `--flannel-iface=tailscale0`、`--node-ip/--advertise-address/--tls-san=<tailscale-ip>` 生效(排障储备 F 的三件套)
- [ ] 验证集群与存储:

```shell
kubectl get nodes -o wide          # 3 台 Ready,INTERNAL-IP 是 100.x
kubectl get storageclass           # local-path 为默认
```

- [ ] 打标签(平台组件与业务副本的调度边界):

```shell
kubectl label node <server> platform-role=control --overwrite
kubectl label node <agent1> platform-role=worker --overwrite
kubectl label node <agent2> platform-role=worker --overwrite
```

**验收**:3 节点 Ready;kubeconfig 在 Mac 本地可用;标签就位。

## Phase 2 · 平台组件(60–120 min,大头是等镜像)

安装顺序有依赖,不要乱序:

- [ ] **Secrets 先行**(缺了它们后面全起不来):
  - `tcr-secret`(镜像拉取,所有业务 ns)与 `tcr-kaniko-secret`(Jenkinsfile 的 buildkitd/trivy 挂载用它,注意是两个名字,内容同一份 dockerconfig)——参照 `k3s/secrets/tcr-secret.example.yaml`
  - `platform-postgresql-auth`(delivery ns,`database-url` key)——参照 example
- [ ] **Argo CD 本体**:`k3s/ci/scripts/install-argocd.sh`,需 `ARGOCD_PUBLIC_HOST`/`ARGOCD_TLS_SECRET`/`ARGOCD_WEBHOOK_SECRET`;没有公网域名就先不配 webhook(Argo CD 走轮询同步,基准测试手动触发不依赖 webhook)
- [ ] **注册 GitOps 应用**:apply `k3s/gitops/projects/` 与 `k3s/gitops/applications/`(platform 组件 + 13 个 workload + delivery 侧 jenkins),让 Argo CD 拉起其余一切:ingress-nginx → prometheus → argo-rollouts → loki/alloy → workloads
- [ ] **PostgreSQL 迁移**:apply `platform/server/migrations/001_release_records.sql`(一次性,psql 走 port-forward 或 one-off pod)
- [ ] 盯同步直到收敛:

```shell
kubectl get applications -n argocd     # 全部 Synced/Healthy
kubectl get pods -A                    # 无 CrashLoop/Pending
```

**验收**:所有 Application Healthy;Prometheus/Alertmanager 就绪(后续 AnalysisTemplate 依赖);13 个服务 Running。

**风险**:国内拉 quay/dl.k8s.io 慢 → Phase 3 的 Prepare tools 只首次慢(缓存到 PVC);Argo CD 镜像已走 ACR。

## Phase 3 · CI 链路冒烟(30–60 min)

- [ ] 缓存与权限(README 既定顺序):

```shell
kubectl apply -f k3s/ci/jenkins/agent-cache-pvc.yaml
kubectl apply -f k3s/ci/jenkins/buildkit-cache.yaml
kubectl apply -f k3s/ci/jenkins/release-rbac.yaml
```

- [ ] Jenkins 一次性配置:凭据 `git-https`(GitHub PAT);建 Pipeline 任务 `ecampus-pipeline`,SCM 指向本仓库 `k3s/ci/jenkins/ecampus.Jenkinsfile`
- [ ] port-forward 后手动触发一次**不带 SHA** 的构建(→ 保守全量),全程盯:

```shell
kubectl -n delivery port-forward svc/jenkins 8080:8080
# 浏览器开 Blue Ocean 或控制台,关注五段:
# 影响分析 → 并行构建(BuildKit 首跑无缓存+Trivy 首拉漏洞库最慢)→ GitOps PR
# → Argo sync → Rollout 就绪 → release_record 落库
```

- [ ] 常驻流量起来(Canary 分析 minSamples=1000,无流量会 Inconclusive 暂停——设计行为):

```shell
brew install hey   # 若未装
kubectl -n app port-forward svc/<某服务>-stable 18080:80
hey -z 30m -c 8 -q 20 http://localhost:18080/health
```

**验收**:一次全量发布端到端绿;PostgreSQL 里出现 `stable` 记录;SLI 指标在 Prometheus 可查(`ecampus:http_requests_total:rate5m`)。

## Phase 4 · 容量决策门(10 min)

- [ ] 构建期间与结束后各采一次:

```shell
kubectl top node; ssh node1 'free -h'
kubectl get events -A --field-selector reason=OOMKilling   # 全量构建时段
```

- [ ] **判定规则**:峰值可用内存 <1.5G 或出现任何 OOMKill → 升级 server 节点到 4c16g(云控制台操作,重启后 k3s 自愈,回本阶段复测);数字必须三组同配置采,升级后重跑冒烟。
- [ ] 决策结果记录到本文末尾。

## Phase 5 · 指标采样(半天,大部分是等待)

### 5.1 指标一:CI 提效三组消融(详细口径见 BENCHMARK_PLAN.md §4)

```shell
export JENKINS_URL=http://localhost:8080 JENKINS_AUTH=user:token
k3s/ci/scripts/bench-pipeline-timing.sh reset-go-caches        # 可选,更严格
k3s/ci/scripts/bench-pipeline-timing.sh cold-full              # ×2,记下 cache tag
CACHE_TAG=bench-cold-<ts> .../bench-pipeline-timing.sh warm-full   # ×2
BEFORE_SHA=<base> AFTER_SHA=<head> .../bench-pipeline-timing.sh warm-impact  # ×3
```

- [ ] sanity:cold/warm 差距 <30% → 查构建日志 `--import-cache` 是否命中,别急着下结论
- [ ] 结果自动落 `benchmarks/results/ci-timing.md`

### 5.2 指标二:告警降噪两个场景(口径见 BENCHMARK_PLAN.md §5)

```shell
# port-forward Prometheus/Alertmanager,export PROM_URL/AM_URL
# 场景 A(自然噪声):先起 observe,再触发一次 warm-impact 发布
.../bench-alert-noise.sh observe 30
# 场景 B(抑制边界注入):
.../bench-alert-noise.sh noise-window comment
.../bench-alert-noise.sh inject-restart comment     # 应被抑制
.../bench-alert-noise.sh inject-crashloop comment   # 必须透传
.../bench-alert-noise.sh observe 20                 # 末尾安全断言须 PASS
```

- [ ] 注入前提:目标镜像内有 `/bin/sh`;distroless 则改用临时失败 liveness 探针方案(记录在案即可)
- [ ] 结果自动落 `benchmarks/results/alert-noise.md`

### 5.3 白捡的数字(10 min)

```sql
SELECT release_status, count(*) FROM service_releases GROUP BY 1;
-- 简历第二条:"累计发布 N 次,X 次门禁拦截自动回退"
```

- [ ] (可选)回退时长:发布中注入 CrashLoop 触发自动回退,从 Jenkins 日志抠"abort-traffic → verify-traffic 通过"的时间差

**验收**:简历两个占位符全部可填,注入断言 PASS。

## Phase 6 · 收尾(30 min)

- [ ] 实测数字回填 `BENCHMARK_PLAN.md` §7 占位符 → 定稿简历四条
- [ ] 本次部署新坑按模板入 `TROUBLESHOOTING_STORY.md`,替换较弱储备
- [ ] `benchmarks/results/` 提交入库(简历数字的证据链);集群保留运行便于复测

---

## 风险清单(按概率排序)

| # | 风险 | 缓解 |
|---|---|---|
| 1 | 国内网络拉 quay.io/dl.k8s.io 慢或失败(Prepare tools) | 只首次;失败则 Mac 下载后 `kubectl cp` 进 /cache/jenkins-tools |
| 2 | server 节点磁盘 <100G | Phase 0 拦截;Loki PVC/保留期可砍 |
| 3 | 4c8g 构建期 OOM | Phase 4 决策门升级 16G |
| 4 | Tailscale 掉线导致节点 NotReady | `tailscale status` 巡检;服务器设置 tailscale 开机自启 |
| 5 | Jenkins 首跑 Trivy 拉漏洞库超时 | 重跑即可(DB 已部分缓存);或 TRIVY_CACHE_DIR 预热 |
| 6 | 镜像内无 shell,注入失败 | 改失败 liveness 探针方案 |
| 7 | Argo CD 无公网入口,webhook 不通 | 不阻塞:轮询同步 + 手动触发;webhook 只影响实时性 |

## 时间总账

顺利 1 天;含 1–2 次排障 1.5–2 天。等待集中在 Phase 5(三次全量构建 + 告警观察窗)。

## 环境记录(Phase 0 填写)

```
node1: IP= 规格= 磁盘= 角色=server
node2: IP= 规格= 磁盘= 角色=agent
node3: IP= 规格= 磁盘= 角色=agent
Phase 4 决策: □ 维持 4c8g   □ 已升级 4c16g(时间: )
```
