# k3s 集群部署 Runbook

> 目标：把"上次部署了很久"变成"按单执行、每步有验证门"。
> 上次的 22+ 个问题里，代码/配置类的已固化在仓库（探针超时、串行构建、
> fsGroup 策略、GODEBUG、镜像 mirror、残留检测、flannel 钉死），环境类的
> 由 preflight 预检拦截。排障字典见 [TROUBLESHOOTING.md](./TROUBLESHOOTING.md)。
> 适用场景：全新重搭（含升配 2×4c8g 换机）。旧集群数据无需迁移。

## 时间预算总览

| 阶段 | 理想 | 含一轮修复 |
|---|---|---|
| Phase 0 准备清单 | 15 min | 30 min |
| Phase 1 预检 | 5 min | 30 min |
| Phase 2 集群安装 | 25 min | 60 min |
| Phase 3 平台组件 | 60 min | 2 h |
| Phase 4 冒烟验收 | 45 min | 1.5 h |
| **合计** | **~2.5 h** | **半天～一天** |

上次拖成数天的主因是"从未端到端跑过的流水线"（案例 9）和环境类问题
（案例 1/2/6）没有检查手段；现在前者已被真实发布验证过、后者进了 preflight。

## Phase 0：准备清单（开机器后、ansible 前）

- [ ] **先提交当前仓库**（known-good 基线，出问题可对 diff）
- [ ] 新机器加入 Tailscale，记下三台的 Tailscale IP
- [ ] 更新 `inventory/dev.ini` 的 ansible_host（必须全是 100.64/10 段；
      preflight 会拦公网 IP）
- [ ] SSH key 放到 `~/.ssh/ha_ecs_ed25519`，`ansible all -m ping` 通
- [ ] 安全组出方向放行：443（ACR 公网端点/quay/github）、53 或改用公共
      DNS（见案例 1）、Tailscale 所需端口（已有网格则只需确认互通）
- [ ] 节点角色分配：4c16g = control+构建；2×4c8g = agent（内存预算见
      PLATFORM_PLAN.md §3；控制节点给 Jenkins 构建 Pod 留峰值余量）
- [ ] 磁盘：control 独立数据盘 80-120G，agent 40-60G
- [ ] 凭据备好：ACR token（tcr-secret / tcr-kaniko-secret）、GitHub
      token（git-https）、PG auth（platform-postgresql-auth）

## Phase 1：预检

```bash
cd k3s && ansible-playbook -i inventory/dev.ini playbooks/preflight.yml
```

**验证门：全绿（内网 DNS 不可达仅为 warn）。**
任何 fail 按提示修完重跑。它拦的正是上次的时间黑洞：安全组/DNS（案例 1）、
flannel 网卡（案例 F）、内存/磁盘（案例 4）、残留安装（加固 F）。

## Phase 2：集群安装

```bash
ansible-playbook -i inventory/dev.ini playbooks/site.yml
# 等价于 preflight → bootstrap → k3s-cluster → kubeconfig → verify
```

**验证门：**
```bash
kubectl get nodes -o wide        # 三台 Ready，IP 均为 Tailscale 段
kubectl -n kube-system get pods  # coredns/flannel 全 Running
```

集群 Ready 后打平台节点标签（16G 控制 / 两台 agent 分 light/worker，
节点名以 `kubectl get nodes` 实际输出为准）：
```bash
kubectl label node <control-node> platform-role=control --overwrite
kubectl label node <agent-1>     platform-role=light   --overwrite
kubectl label node <agent-2>     platform-role=worker  --overwrite
```

然后做两件环境级修复（重搭必做，详见 TROUBLESHOOTING 案例 G/H）：
1. systemd-resolved 全局切公共 DNS（223.5.5.5/119.29.29.29, Domains=~.）
   ——本 VPC 的阿里云内网 DNS 被安全组掐断。
2. 在 k3s server 的 `/var/lib/rancher/k3s/server/manifests/coredns.yaml`
   的 NodeHosts hosts 块内钉扎 github.com 可达 IP（写进该文件才能在
   k3s 重启后保留；改 ConfigMap 会被重置）。
卡住 → 案例 1（DNS）、案例 F（NotReady）、加固 F（agent/server 混装）。

## Phase 3：平台组件

```bash
# Argo CD（详见 k3s/README.md）
ARGOCD_PUBLIC_HOST=... ARGOCD_TLS_SECRET=... ARGOCD_WEBHOOK_SECRET=... \
  sh k3s/ci/scripts/install-argocd.sh
# Jenkins 持久化与权限
kubectl apply -f k3s/ci/jenkins/agent-cache-pvc.yaml \
               -f k3s/ci/jenkins/buildkit-cache.yaml \
               -f k3s/ci/jenkins/release-rbac.yaml
# 平台 PG + secret
kubectl apply -f k3s/secrets/platform-postgresql-auth.example.yaml  # 改成真实值
```

**注意：Jenkins 任务要重建，不能沿用**——parameters 的 defaultValue 只在
首次注册时生效，改 Jenkinsfile 不会更新任务里存的旧默认值（案例 7）。

Jenkins job `ecampus-pipeline` 的注册步骤（2026-09-21 实操）：
1. 用 admin 密码 + crumb（带 cookie）POST `createItem?name=ecampus-pipeline`
   一个 CpsScmFlowDefinition（SCM=app-test main，scriptPath=
   k3s/ci/jenkins/ecampus.Jenkinsfile，credentialsId=git-https）。
2. **参数注册**：直接 POST 该 job 的 `config.xml`（带 parameterDefinitions
   块，字段与 Jenkinsfile parameters{} 一致）。空跑一次让 Jenkinsfile 自注册
   会先卡在跨境 clone，不可靠；config.xml 一次到位。
3. Jenkins API token：POST
   `/user/admin/descriptorByName/jenkins.security.ApiTokenProperty/generateNewToken`。
4. 依赖 Secret：delivery ns 需有 `tcr-kaniko-secret`（agent pod 挂载的
   registry pull secret），缺失时 agent Pod FailedMount 卡 1000s 超时；
   `gitea-credentials`（镜像仓库 basic auth）与 argocd ns 的 `repo-gitea`
   （Argo 读镜像仓）见 k3s/secrets/platform-server-cicd.example.yaml。
5. kubectl 工具预置：从 k8s.m.daocloud.io/kubectl 镜像导出二进制写入
   jenkins-agent-cache PVC（详见 TROUBLESHOOTING 案例 N.3）。
6. 首次构建前经 script console 预批 JsonSlurperClassic 沙箱签名（案例 N.2）。

**验证门：** Argo CD 所有 Application Healthy；Jenkins 能登录；
`kubectl -n app get pods` 13 个服务 Running。

## Phase 4：冒烟验收（必须跑完，不允许"应该能通"）

1. **构建链冒烟**：Jenkins 触发一次 `SKIP_RELEASE=true` 的构建 →
   确认 ACR 出现新 digest（验证构建/推送/缓存三层）。
2. **发布链冒烟**：真实发布 `theme`（改动任意一行）→ 确认走完
   GitOps PR → auto-merge → Argo sync → wait-for-release 四层验证 →
   PG `service_releases` 出现 `stable` 记录。
3. **告警链冒烟**：向 Alertmanager POST 一条测试告警 → 确认抑制/路由
   行为符合预期（test-observability.sh 本地已验证规则本体）。

**全部通过才算部署完成。** 这一步是对案例 9（幽灵配置）的制度性防御：
流水线任何改动，以真实端到端跑通为唯一验收标准。

## 后续阶段的新组件（Kafka/ClickHouse/Jaeger 等）

它们是**新的未知数**，不在本 runbook 内——到 P4 时按同样的模式各配一份
安装+验收清单（部署步骤 / 验证命令 / 已知坑位），避免旧坑复踩、新坑无册可查。

## 2026-09-21 重搭新增环境坑位（详见 TROUBLESHOOTING.md 案例字母段）

- 公网出网走共享 NAT（无独立 EIP），访问入口全靠 Tailscale；
  registry-1.docker.io 与 github.com 部分 IP 被掐，靠 registries.yaml
  mirror（docker.1ms.run + k8s/quay.m.daocloud.io）与 coredns hosts 钉扎。
- ACR 个人版会自动清理 `:dev` 等 tag（digest 引用仍有效）——长期方案是
  发布流水线 digest 钉扎；重搭后需重新构建/推送镜像。
- Argo CD repo-server 的 go-git 超时（ARGOCD_GIT_HTTP_TIMEOUT=60）与
  chart vendor（bitnami index 27MB 拉不完）是跨境环境两个必踩点。
- ecampus 业务依赖 mysql/redis/mongo/rabbitmq 四件中间件
  （k3s/helm-values/dependencies/ + k3s/manifests/dependencies/），
  首次启动需先跑 migrate Job（app ns 的 ecampus-migrate 模板在本
  Runbook 历史记录中）；redis/mongo 的 service 名与配置期望不同，
  已用 ExternalName 别名（redis/mongo）补齐。
- 开发 Mac 有本地代理（127.0.0.1:7897），访问 Tailscale 段需 --noproxy
  或系统代理直连规则，否则平台入口 502 假象。
