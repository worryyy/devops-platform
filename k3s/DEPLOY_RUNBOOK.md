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
