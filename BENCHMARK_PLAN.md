# 三台服务器部署与指标测量方案

> 目标:在两台 2c4g + 一台 4c8g(可升 4c16g)上跑起整套交付平台,实测两组简历数字:
> ① 变更影响分析 + 多级缓存对"合并 → 首批 Canary Pod 就绪"的提效;
> ② 发布期告警治理的降噪效果与抑制边界安全性。
> 所有数字必须实测,本文档中的数值范围只是预估,用于判断实测结果是否在合理区间。

---

## 1. 当前阻塞(2026-09-02)

两台服务器(100.67.223.96 / 100.101.243.0,均为 Tailscale 内网地址)当前不可达:
本机没有运行 Tailscale,`ping` 100% 丢包。解锁步骤:

1. 三台服务器开机;
2. Mac 安装并登录 Tailscale(`brew install --cask tailscale` 或 App Store 版),确保和服务器在同一 tailnet;
3. `ping 100.67.223.96` 通了之后,`ssh node1` / `ssh node2` 验证免密可用;
4. 第三台服务器在 Tailscale 管理页拿到 IP,填入 `k3s/inventory/dev.ini` 的 node3 注释行。

## 2. 集群拓扑(结论:用 k3s,维持现有选型)

| 节点 | 角色 | 说明 |
|---|---|---|
| node1(4c8g,建议升 4c16g) | k3s server(control-plane + etcd)+ `platform-role=control` | Jenkins controller、CI Pod(含 BuildKit)、Argo CD、Rollouts、Prometheus、Alertmanager、Loki、PostgreSQL 全部调度到此节点(配置里已用 nodeSelector 钉死) |
| node2 / node3(2c4g × 2) | k3s agent | 13 个 ecampus 服务副本 + Canary 流量,吃剩余负载 |

选型理由(面试可讲):
- 集群只有 3 节点、单 control-plane,k3s 内置 containerd + flannel + CoreDNS,单二进制 + SQLite(etcd 模式可选),运维面最小;标准 k8s(kubeadm)在这个规模只会多出 etcd/组件升级成本,没有收益;
- 现有仓库(playbooks、roles、flannel 走 tailscale0、国内镜像 rancher-mirror)全部按 k3s 设计,推倒重来没有意义;
- 跨机房组网用 Tailscale,flannel iface 指向 tailscale0,节点无需公网 IP 互通——这本身就是一个可讲的部署决策。

内存评估(为什么要升 4c16g):
- 控制节点常驻:k3s server ~1G + Jenkins 0.5~1G + Argo CD ~0.7G + Prometheus 1~2G + Loki ~1G + PostgreSQL/Alertmanager/ingress ~0.7G ≈ **5~6G**;
- 一次发布 13 个服务**并行** Go 编译(受 CPU 核数限制排队,但内存峰值仍会 +2~4G),8G 会触发 swap 或 OOMKill,构建数字会抖;
- 结论:**先用 4c8g 跑通全链路,正式采样前把 server 节点升级到 4c16g,三组数据在相同配置下采**,否则冷/热对比不可信。
- 上线后先 `nproc && free -h` 确认哪台是 4c8g/4c16g;如果当前 node1 不是大内存那台,调整 inventory 让大内存机当 server(重装 k3s 成本约 10 分钟)。

## 3. 部署步骤(服务器可达后)

```shell
# 0) 确认规格,大内存机 = node1(k3s server)
ssh node1 'nproc; free -h'
ssh node2 'nproc; free -h'

# 1) 装集群(bootstrap + k3s + kubeconfig + verify 一把梭)
cd k3s && ansible-playbook playbooks/site.yml

# 2) 给 server 节点打标签(Jenkins/平台组件钉在 control 节点)
kubectl label node <server-node> platform-role=control --overwrite
kubectl label node <agent1> platform-role=worker --overwrite
kubectl label node <agent2> platform-role=worker --overwrite

# 3) CI 缓存与发布权限(k3s/README.md 已有)
kubectl apply -f k3s/ci/jenkins/agent-cache-pvc.yaml
kubectl apply -f k3s/ci/jenkins/buildkit-cache.yaml
kubectl apply -f k3s/ci/jenkins/release-rbac.yaml

# 4) secrets(参照 k3s/secrets/*.example.yaml):ACR 镜像凭据 + PostgreSQL auth
# 5) Argo CD 安装 + GitHub webhook(参照 README「Install or upgrade public Argo CD」一节)
# 6) Jenkins:创建 ecampus-pipeline 任务(Pipeline script from SCM → k3s/ci/jenkins/ecampus.Jenkinsfile),
#    凭据 git-https,以及 port-forward 后跑一次全量构建做冒烟
```

验证全链路的顺序:先手动触发一次不带 SHA 的构建(conservative full build),确认
影响分析 → 构建 → GitOps PR 合并 → Argo CD 同步 → Rollout 就绪 → release_record 落库
整条链路绿,再开始采样。

## 4. 指标一:CI 提效(冷/热 × 全量/影响分析 三组消融)

工具:`k3s/ci/scripts/bench-pipeline-timing.sh`(Jenkinsfile 已新增 `BUILDKIT_CACHE_TAG` 参数支持冷缓存运行,默认值不变)。

口径定义(面试时能一句话讲清):
- **e2e = Jenkins 构建 API 的 timestamp → 该 deploy_id 下第一个新版本 Pod Ready 的 lastTransitionTime**;
  deploy_id 标签只有新发布的 Pod 才有,所以不含 Stable 老副本,测的就是"合并 → 首批 Canary 就绪";
- 三组消融把两个优化因子拆开:
  - `cold-full`(基线,优化前):全新缓存 tag + 空 SHA 逼出全量构建;
  - `warm-full`(只开缓存):复用 cold 的缓存 tag 再跑一次全量;
  - `warm-impact`(优化后日常路径):main-amd64 生产缓存 + 小提交的 BEFORE/AFTER SHA(只影响 1~2 个服务)。
- 每组至少跑 2~3 次,取中位数;结果自动追加到 `benchmarks/results/ci-timing.md`。

```shell
# 终端 A:port-forward Jenkins
kubectl -n delivery port-forward svc/jenkins 8080:8080
# 终端 B:采样
export JENKINS_URL=http://localhost:8080 JENKINS_AUTH=user:apitoken
k3s/ci/scripts/bench-pipeline-timing.sh reset-go-caches      # 冷跑前清 Go 缓存(可选,更严格)
k3s/ci/scripts/bench-pipeline-timing.sh cold-full            # 记下输出的 cache tag
CACHE_TAG=bench-cold-<ts> k3s/ci/scripts/bench-pipeline-timing.sh warm-full
BEFORE_SHA=<base> AFTER_SHA=<head> k3s/ci/scripts/bench-pipeline-timing.sh warm-impact
```

注意事项:
- `warm-impact` 的 BEFORE/AFTER 选一个只改 1 个服务的小提交(Ecampus-go 仓库 `git log` 找),这样"影响分析把 13 个服务裁到 1 个"的贡献才成立;
- 经典(非 multibranch)任务下 deploy_id 是 `ecampus-pipeline-<build>-<service>-1`,脚本按此拼;如果你建的是 multibranch 任务,deploy_id 会多一段 branch,需改脚本里的拼接;
- 采样期间别同时跑别的构建(`disableConcurrentBuilds` 已保证)。

预估区间(仅供 sanity check,以实测为准):cold-full 15~30min;warm-full 6~12min;
warm-impact 3~6min;e2e 在 build 之上再加 GitOps PR + Argo sync + 拉镜像 1~3min。
若 cold 与 warm 差距 < 30%,先怀疑 BuildKit registry 缓存未命中(检查 `--import-cache` 日志)。

## 5. 指标二:发布期告警降噪

工具:`k3s/ci/scripts/bench-alert-noise.sh`。

口径定义:
- **fired**:Prometheus `/api/v1/alerts` 里 firing 的去重告警(alertname+pod+container+deploy_id);
- **inhibited**:Alertmanager `/api/v2/alerts` 中 `status.inhibitedBy` 非空的同指纹告警;
- **notified = fired - inhibited**(当前 default-receiver 未配 webhook,notified 是"会发通知"的口径,这在面试里反而好讲:通知渠道是可插拔的,治理逻辑先被验证);
- 降噪由两个机制构成,分开报数:① deploy_context 信号路由到无配置 receiver(结构性静默);② ReleasePodRestarting 被发布窗口抑制(条件性静默)。

两个采集场景:

```shell
# 终端 A/B:port-forward
kubectl -n monitoring port-forward svc/prometheus-server 9090:80
kubectl -n monitoring port-forward svc/prometheus-alertmanager 9093:9093
export PROM_URL=http://localhost:9090 AM_URL=http://localhost:9093

# 场景 1(真实发布):先起 observe 再触发一次 warm-impact 发布,窗口盖住整个发布期
k3s/ci/scripts/bench-alert-noise.sh observe 30
# 场景 2(注入验证抑制边界):不依赖发布,直接制造瞬时重启与 CrashLoop
k3s/ci/scripts/bench-alert-noise.sh noise-window comment
k3s/ci/scripts/bench-alert-noise.sh inject-restart comment    # 单次重启 → 应被抑制
k3s/ci/scripts/bench-alert-noise.sh inject-crashloop comment  # 4 次重启 → 必须透传
k3s/ci/scripts/bench-alert-noise.sh observe 20
```

脚本末尾有**安全断言**:CrashLooping / StuckTerminating / NotReady / ReplicaShortage /
user_impact 任一被抑制即 FAIL。这个 PASS/FAIL 本身就是面试素材("我用故障注入验证了
抑制边界,不是靠读配置自嗨")。

注意:
- 注入依赖容器里有 `/bin/sh`(`kubectl exec ... kill 1`);若镜像是 distroless,改用临时给该服务
  values 加一个会失败 3 次的 liveness 探针再撤掉(方案文档记录即可,不必改脚本);
- 若要出 user_impact 数字,需要持续流量(见下);纯降噪场景不需要。

## 6. 压测流量(Canary 分析与 user_impact 都需要)

Argo Rollouts 的 AnalysisTemplate 默认 `minSamples=1000`(5m 窗口 ≈ 3.3 rps),没有流量时
查询为空 → NaN → Inconclusive → 发布暂停(这是设计行为,不是 bug)。给一条最省事的流量:

```shell
brew install hey
kubectl -n app port-forward svc/comment-stable 18080:80   # 端口名以 chart 为准
hey -z 20m -c 8 -q 20 http://localhost:18080/health       # ~160 rps,足够过采样门槛
```

采样门槛:50 req/5m(user_impact)与 1000 samples(Analysis)。要压出 user_impact 告警,
把流量打到会 5xx 的路由(或临时把某服务 /health 改成 503 再恢复)。

## 7. 数字怎么写进简历(占位符,实测后替换)

第一条(提效):
> 基于 Git Diff 与 go list -deps 依赖闭包识别受影响 Go 服务……**全量构建(13 服务)从
> __min 降至单服务变更 __min,首个 Canary Pod 就绪时间(e2e)从 __min 降至 __min(-__%),
> 其中构建缓存贡献 __min、影响分析贡献 __min(三组消融实测)**,……

第四条(告警):
> ……**发布窗口内瞬时 Pod 重启告警抑制率 __%(__/__ 条),CrashLoop/持续未就绪等故障告警
> 0 抑制(故障注入验证),发布期告警通知量从 __ 条降至 __ 条(-__%)**,并通过 CI 契约测试……

面试讲法:主动交代测量口径(Jenkins API 时间戳 → Pod Ready lastTransitionTime;
Prometheus fired vs Alertmanager inhibitedBy),一句"数字是三组消融实测的"比数字本身更加分。
