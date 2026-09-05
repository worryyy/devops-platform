# 项目架构口述讲稿(面试版)

> 配合 `INTERVIEW_QA.md` / `TECH_GLOSSARY.md` 使用。这份是"架构 + Jenkins"专题的口述稿,按面试官真实问法组织,可以直接照着练。
> 每段都有【长版】(2 分钟完整讲)和【短版】(30 秒应急),练的时候先练长版,熟了自然能压缩成短版。

---

## 目录

- [一、项目架构总述](#一项目架构总述)
- [二、Jenkins 在整个流程中的作用](#二jenkins-在整个流程中的作用)
- [三、Jenkins 是怎么启动的](#三jenkins-是怎么启动的)
- [四、为什么选 Jenkins 而不是 GitLab CI](#四为什么选-jenkins-而不是-gitlab-ci)
- [五、追问预案](#五追问预案)

---

## 一、项目架构总述

### 【长版】照着讲,约 2 分钟

> 整套系统分五个部分,我按一次发布的旅程讲:
>
> **第一,两个 Git 仓库。** 源码仓放 13 个 Go 服务的业务代码,还有影响分析工具 ecampus-impact;GitOps 仓是右边一切的家:Jenkinsfile、服务目录、通用 Helm chart、13 个服务各自的 values、Argo CD Application 定义、监控告警规则,全部声明式地放里面。**右边这套系统的全部状态,都能在 Git 里找到。**
>
> **第二,Jenkins 做 CI。** 它被 GitHub webhook 触发,拿到 push 事件的 before/after 两个 SHA,先做增量影响分析——变更文件和每个服务的 `go list -deps` 依赖闭包求交集,算出这次到底要构建哪些服务;SHA 校验不过就保守回退全量。然后并行地对每个受影响服务跑测试、BuildKit 构建镜像、推腾讯云 TCR、Trivy 按 digest 扫 CRITICAL 漏洞。构建用三层缓存:Go module 缓存和 Go 编译缓存在 Jenkins agent 的共享 PVC 上,BuildKit 的 Dockerfile 层缓存在 TCR 里,每个服务一个独立 cache ref。
>
> **第三,CD 是 GitOps 拉动模式。** Jenkins 构建完**不碰集群**,它只做一件事:用 yq 把 GitOps 仓里该服务 values 的 `image.digest` 改成刚构建出来的 digest,提一个 release PR。普通服务自动合并,蓝绿的人工审批服务等审批。PR 合并进 main,Argo CD 通过 webhook 即时感知,selfHeal 把新 values 渲染成 Helm chart 应用到集群。
>
> **第四,集群里干活的是 Argo Rollouts。** 13 个服务按流量可验证性和故障影响范围分成四个 profile:高流量的 critical-canary 走 1%→5%→20%→50%→100% 渐进放量,每步跑 AnalysisTemplate 查 Prometheus,双门禁——canary 自己错误率不能超 1%,相对 stable 退化不能超 0.3%,样本不足或 stable 基线坏了就返回 NaN 判 Inconclusive,发布暂停等人;低流量的用 bluegreen,preview 副本探活加人工审批;低风险的直接 Deployment 滚动。
>
> **第五,记录和监控。** 每次发布的状态写进 PostgreSQL 的 service_releases 表,走 releasing→stable/failed→compensating 状态机;Prometheus 用 recording rule 把 Pod 上的 deploy_id、git_sha、digest 关联进 SLI,告警触发时自动带发布身份;发布窗口的抑制规则用 deploy_id 做 equal key,只压瞬时 Pod 重启,CrashLoop、副本不足这些持续故障永远透传。
>
> **发布失败的回退也是闭环的:** 先从 Rollouts status 拿 stableRS 抠出 digest,拿不到降级查 PG 的 stable 记录,再拿不到就遍历 GitOps 的 git 历史;拿到目标后先 `rollouts abort` 切流量回 stable,CI 再提补偿 PR 把 values 里的 digest 改回去——必须改 Git,不然 Argo CD 的 selfHeal 会把集群又拉回失败版本。

### 【短版】30 秒

> 两个仓库:源码仓放 13 个 Go 服务,GitOps 仓放一切部署声明。Jenkins 被 webhook 触发,git diff 加依赖闭包算出受影响服务,并行构建扫描,然后只改 Git——用 yq 把 digest 写进 values 提 PR。PR 合并,Argo CD sync,Argo Rollouts 按四个 profile 做灰度,高流量 canary 双门禁,低流量蓝绿加人工审批。发布状态记 PostgreSQL,告警用 deploy_id 精确抑制。失败了三层兜底找 stable digest,先切流量再改 Git。

### 架构的"分层心智模型"(帮你记住长版)

讲的时候脑子里放这张图,五层从左到右:

```
[源码仓 GitHub]          [GitOps 仓 devops-platform]
  ecampus-go 13 服务        chart + values + catalog + Jenkinsfile + 告警规则
        │ webhook                  ▲ PR(yq 改 digest)
        ▼                          │
  ┌─────────── Jenkins(CI)───────┘
  │ 影响分析 → 测试 → BuildKit+Trivy → 提 release PR
  └──────────────────────────────────
                                     │ PR 合并
                                     ▼
                        [Argo CD]──sync──▶ [k3s 集群]
                                          Argo Rollouts(4 profile)
                                          Nginx Ingress(流量切分)
                                          │
                    [Prometheus/Alertmanager]←─AnalysisTemplate 门禁
                    [PostgreSQL service_releases]←─发布状态机
```

记忆口诀:**两个仓库、一个 CI 指挥、一个 CD 拉动、四个发布档位、三层回退兜底。**

---

## 二、Jenkins 在整个流程中的作用

### 【长版】

> Jenkins 是 CI 侧的总指挥,但它的**权限边界刻意收得很窄**:它管构建、扫描、改 Git、提 PR、盯发布进度、做回退,但它**从不直接 kubectl apply 到集群**——部署权完全在 Argo CD 手里。这是 GitOps 的权限收敛原则:Jenkins 连集群的写权限都很有限,RBAC 里只给了 rollouts 的 abort/undo/promote 这类发布操作子资源、workload 只读、Argo CD Application 只读。真正能改集群的只有 Argo CD。
>
> 一个 build 里它具体干八件事:
> 1. **接 webhook 参数做影响分析**:before/after SHA 进 `ecampus-impact`,产出受影响服务矩阵;
> 2. **解析服务目录**:`platform-server catalog` 把每个服务的发布策略、阈值、环境覆盖渲染成流水线可消费的 JSON;
> 3. **并行跑测试**:按 test_matrix,只测不构建的服务也测;
> 4. **BuildKit 构建**:buildkitd sidecar 常驻,buildctl 构建推送,registry 层缓存导入导出;
> 5. **Trivy 扫描**:按 digest 扫,CRITICAL 即 fail;
> 6. **提 GitOps PR**:yq patch values,普通服务开 autoMerge,人工审批服务等 input;
> 7. **waitForRelease 验证**:五道校验——Argo CD sync revision 对不对、运行的 Pod image 是不是目标 digest、rollout 健康状态、Prometheus SLI 达标、stable service 的 /health 探活;
> 8. **记录与回退**:发布状态写 PostgreSQL(releasing/stable/failed/compensating 状态机);失败则走回退——三层找 stable digest、abort 切流量、提补偿 PR。蓝绿的还有个人工审批环节。
>
> 还有一个细节:同 job 不并发(`disableConcurrentBuilds`),保证同一个服务的发布是串行的,不会两个 build 抢着发。

### 【短版】

> Jenkins 管 CI 全流程:影响分析、构建、扫描、改 GitOps 仓提 PR、发布后验证、失败回退,外加蓝绿的人工审批。但它不直接部署集群——没有 kubectl apply,只有窄 RBAC,部署权全在 Argo CD,这是刻意的权限收敛。

### 讲"作用"时的高光点(面试官爱听的边界感)

- **"Jenkins 只对 Git 有写权限,对集群几乎只读"** —— 这句话本身就是一个加分回答
- 发布状态不存 Jenkins 自己的状态文件里,存 PostgreSQL —— pipeline 崩了记录还在,回退的 L2 兜底就靠它
- Jenkins 里的凭据只有一个(`git-https` 的 GitHub token),其余全在 K8s Secret —— 凭据面最小化

---

## 三、Jenkins 是怎么启动的

> 这题分 controller 和 agent 两层答,再加触发链路,显得层次清楚。

### 【长版】

> Jenkins 分 controller 和 agent 两层,启动方式完全不同。
>
> **controller 是 Helm 部署的**,用官方 jenkins/jenkins chart,镜像是自维护的 Jenkins LTS 加 JDK21,推在腾讯云 TCR。跑在 control 角色的节点上,512Mi 到 1Gi 内存,ClusterIP 不对外暴露。这里要诚实说明:controller 是**手工 helm install 的,没有纳进 Argo CD 管理**——因为 Argo CD 自己也是这套系统的部署者,总得先有鸡才能有蛋。Jenkins、Argo CD 这类"装系统的工具"是一次性手工装,装完之后,系统里其他一切(13 个服务、Rollouts、监控)都由 Argo CD 按声明式管理。
>
> **agent 是动态的,每次 build 现场起。** Jenkinsfile 里 agent 块用的是 kubernetes 插件,每个 build 动态创建一个 pod,pod 里按职责切了**七个容器**:go 容器跑测试和影响分析;buildkitd 是 rootless 的 BuildKit 守护进程 sidecar;trivy 扫镜像;git 容器做 Git 操作和 GitHub API 调用;yq 容器专门改 values;rollouts 容器装了 kubectl-argo-rollouts 做发布等待和回退;curl 容器做健康检查。pod 挂两个 PVC——10Gi 的 jenkins-agent-cache 放 Go 两层缓存,30Gi 的 buildkit-cache 放 BuildKit 本地缓存。**build 结束 pod 销毁,但 PVC 留着,这就是缓存跨 build 复用的关键。** pod 用 ecampus-release 这个 ServiceAccount 跑,RBAC 刻意收紧。
>
> **触发链路**:源码仓的 GitHub webhook 指向 Jenkins,service catalog 里每个服务的 jenkins 配置就是 `mode: webhook, jobName: ecampus-pipeline`。开发 push,push event 带着 before/after SHA 打过来,job 启动。
>
> **前置依赖**也很明确,启用 job 前要先 apply 三个东西:两个 PVC 的 yaml、release-rbac 的 ServiceAccount 和 Role,再配上 TCR 拉镜像/推镜像的 secret 和连 PG 的 secret。

### 【短版】

> controller 用 Helm 手工装一次(jenkins/jenkins chart,自维护 LTS 镜像,不归 Argo CD 管,因为是"装系统的工具");agent 是每个 build 动态起的 K8s pod,七个专职容器共享两个缓存 PVC,build 完销毁、缓存留下;触发靠 GitHub webhook 打 before/after SHA 到 ecampus-pipeline 这个 job。

---

## 四、为什么选 Jenkins 而不是 GitLab CI

> 这题要答得有层次:先给最硬的理由,再给技术理由,最后主动说代价 —— 显得是权衡过,不是信仰站队。

### 【长版】

> 四个理由,按重要性排:
>
> **第一,源码托管在 GitHub 上。** GitLab CI 和 GitLab 平台是深度绑定的,要用它要么自建一整套 GitLab 实例——为了 CI 引入整个代码托管平台,不成比例;要么把仓库镜像到 gitlab.com 再配外部 runner,链路绕。Jenkins 天生仓库无关,GitHub、GitLab、Gitea 都一样接。
>
> **第二,构建模型不匹配。** 我的流水线一次 build 需要七种工具环境,而且 buildkitd 必须作为**守护进程 sidecar 常驻**,buildctl 在同一个 pod 生命周期里反复调它,还要共享 PVC、要特殊的 securityContext(rootless buildkit 要 seccomp Unconfined)。Jenkins 的 kubernetes 插件对此是原生模型:一个 pod 多容器、共享 volume、生命周期一致。GitLab CI 的执行单元是 job,一个 job 一个 pod,job 之间不共享 pod;它的 services 机制是为数据库、Redis 这种有网络端口的服务设计的,对构建守护进程这种形态支持得很别扭——真要做得拼一个大而全的胖镜像,工具版本互相污染。
>
> **第三,人工审批。** 蓝绿发布的 promote 环节要 pipeline 暂停在半路等人确认,Jenkins 的 input 步骤加 timeout 是原生能力。GitLab CI 要靠手动 job 模拟,语义和体验都差一截。
>
> **第四,动态编排逻辑。** 影响分析产出的矩阵是运行时才知道的——构建几个服务、谁要人工审批,全是动态的。Groovy 是完整的编程语言,写这种逻辑很自然;GitLab CI 的 YAML 表达能力有限,复杂编排得上 child pipeline 和模板拆分,可读性下降。
>
> **代价我也说一句**:Jenkins 维护成本比 YAML 类 CI 高——controller 要养、插件要管、Groovy 有学习曲线。所以我的取舍其实是分层的:**PR 门禁这种轻量的用 GitHub Actions,merge 后的重发布流水线才用 Jenkins。** 如果源码本来就在 GitLab,或者构建需求简单,GitLab CI 是更省心的选择。

### 【短版】

> 最硬的一条:代码在 GitHub,GitLab CI 跟 GitLab 平台绑定,为了 CI 引入整套 GitLab 不成比例。技术上:我的构建需要 buildkitd 守护进程做 sidecar 加多容器共享 PVC,Jenkins 的 pod 模型原生支持;蓝绿人工审批要 input 步骤;影响分析矩阵是动态的,Groovy 写起来自然。代价是 Jenkins 维护成本高,所以我分层——PR 门禁用 GitHub Actions,重发布流水线才用 Jenkins。

---

## 五、追问预案

这一节列面试官顺着"架构/Jenkins"往下追的常见问题,答案都在 QA 文档和词汇表里,这里给索引:

| 追问 | 去哪找答案 |
|---|---|
| "Jenkins 挂了发布怎么办?" | 发布状态在 PG 不在 Jenkins,重新触发即可;已在跑的 Rollouts 不依赖 Jenkins 继续灰度 |
| "Jenkins 怎么保证不发错环境?" | catalog 的 EnvironmentOverrides + values 按 environment 区分,digest 跨环境一致 |
| "agent pod 的 RBAC 具体给了什么?" | release-rbac.yaml:app ns 的 rollouts get/list/watch/patch + promote/abort/restart/undo 子资源,analysisruns 只读,deployments/pods/services 只读,argocd ns 的 Application 只读 |
| "为什么 controller 不纳进 GitOps?" | 引导问题(bootstrap):Argo CD 装其他一切,Jenkins 和 Argo CD 属于一次性手工装的"系统引导层" |
| "BuildKit 为什么 rootless?" | 安全:构建过程不跑 root,即使 Dockerfile 里有恶意指令也拿不到节点 root;代价是要放宽 seccomp |
| "两个 PVC 为什么分开?" | 职责:jenkins-agent-cache 是 Go/工具缓存(小而频繁),buildkit-cache 是 BuildKit 内容寻址缓存(大,30Gi,配了 GC 策略 25GB 上限) |
| "GitHub Actions 为什么只做门禁?" | 轻:每个 PR 跑 lint/测试/chart 校验,YAML 声明式够用;重的发布编排才值得上 Jenkins |
| "webhook 挂了怎么办?" | 会退回保守路径:BEFORE_SHA 空 → ecampus-impact --all 全量构建,宁慢勿漏 |
