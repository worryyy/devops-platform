# P2 · CICD 集成

> **目标**：平台成为发布的入口与事实记录面——页面触发 Jenkins、实时看
> stage 进度、发布历史可查、失败可一键 revert PR。Jenkins/Argo CD 引擎
> 角色不变。
> **工期**：2 周。
> **前置依赖**：P1 验收 A1-A6（平台可登录访问、DB 就绪）。
> **蓝图引用**：§6 CICD 与平台交互、§1 现有流水线。
> **状态**：⬜ 未开始

## 1. 任务清单

### 1.1 凭据与配置
- [ ] Jenkins 生成 API token（用户设置页），写入 Secret
      `delivery/jenkins-api-token`（平台经 envFrom 读取）；
      `k3s/secrets/` 增 example 模板
- [ ] `internal/config/config.go` 增加：`JENKINS_URL`
      （默认 `http://jenkins.delivery.svc.cluster.local:8080`，集群内直连，
      无需暴露公网）、`JENKINS_USER`、`JENKINS_TOKEN`、`GITHUB_TOKEN`、
      `GITOPS_OWNER=worryyy`、`GITOPS_REPO=app-test`

### 1.2 后端 delivery 模块
- [ ] `internal/delivery/jenkins.go`：Jenkins 客户端（纯 HTTP）：
  - `TriggerBuild(service, beforeSha, afterSha) (queueURL, error)`：
    POST `/job/ecampus-pipeline/buildWithParameters`
    （参数 SOURCE_REPO/BEFORE_SHA/AFTER_SHA/TARGET_ENV/SKIP_RELEASE）
  - `BuildStatus(buildID)`：GET `/job/ecampus-pipeline/<id>/api/json`
    （取 result/timestamp/duration）
  - Crumb 非 API-token 场景才需要，API token 直连免 crumb
- [ ] `migrations/004_pipeline_runs.sql`：
      `pipeline_runs(id bigserial pk, service text not null, jenkins_build
      bigint, status text not null default 'queued', stages jsonb not null
      default '[]', triggered_by text not null, git_revision text,
      config_revision text, image_digest text, started_at, finished_at,
      created_at default now())`
- [ ] `internal/delivery/` handler 与 service：
  - `POST /api/pipelines/trigger` {services:[], beforeSha, afterSha}
    → 逐服务入 pipeline_runs + TriggerBuild
  - `GET /api/pipelines?service=&status=&limit=` → 列表（join
    service_releases 最新状态）
  - `GET /api/pipelines/:id` → 详情（stages 时间线）
  - `POST /api/webhooks/jenkins` {build, service, stage, status, durationMs}
    → 更新 pipeline_runs.stages（JSONB 合并）；鉴权用共享 secret
    （`PLATFORM_WEBHOOK_SECRET`）
  - `POST /api/pipelines/:id/revert` → GitHub API 创建 revert 分支与 PR：
    POST `/repos/{owner}/{repo}/reverts`（body: commit_sha=
    pipeline_runs.git_revision, branch=`revert/<service>/<build>`）；
    成功返回 PR url 存入 pipeline_runs
- [ ] `ecampus.Jenkinsfile` 增加 `Notify platform` 收尾 stage：pipeline
      结束（无论成败）POST stage 汇总到
      `$PLATFORM_WEBHOOK_URL/api/webhooks/jenkins`（env 从 Jenkins
      全局 env / credentials 注入；body 用 jq 组装）

### 1.3 Argo CD 状态读取
- [ ] `k3s/ci/jenkins/release-rbac.yaml` 旁新增（或复用）只读 SA：
      platform-server 使用 in-cluster SA，Role（argocd ns）读
      applications——沿用 ecampus-release 的 argocd Role 即可，
      新建 RoleBinding 绑 platform SA
- [ ] `internal/delivery/argocd.go`：in-cluster clientset 读
      Application status（sync revision/health），发布详情页展示
      「GitOps 同步状态」卡片

### 1.4 前端
- [ ] `/pipelines` 页：触发表单（服务多选来自 services 表、SHA 对填一
      个或默认 HEAD）、运行列表（状态徽标：queued/running/success/
      failed）、行点开 stage 时间线（横向 stepper，耗时标注）
- [ ] `/pipelines/:id` 详情：GitOps PR 链接、Argo 状态卡、digest、
      release 记录时间线（releasing→stable/failed）；失败态显示
      「一键 Revert」按钮（确认弹窗 → POST revert → 展示 PR 链接）
- [ ] `/services/:name` 详情页加「最近发布」区块（复用列表组件）

### 1.5 测试
- [ ] jenkins 客户端单测（httptest 模拟 Jenkins API）
- [ ] webhook handler 单测（secret 鉴权、stages 合并）
- [ ] 契约测试更新：Jenkinsfile 断言新增 `PLATFORM_WEBHOOK_URL`/
      `Notify platform` stage 存在

## 2. 验收标准
- [ ] A1 触发：页面选 `theme` 触发构建 → pipeline_runs 出现记录且
      Jenkins 对应 build 启动（`Jenkins 构建队列可见`）
- [ ] A2 进度：构建期间详情页 stage 时间线随 webhook 推进更新（≤30s
      延迟）；构建结束状态与 Jenkins 一致
- [ ] A3 发布全流程：页面触发 → 合并 → Argo 同步 → wait-for-release
      验证 → 详情页显示 stable + digest 与 `service_releases` 一致
- [ ] A4 一键 revert：人为让一次发布失败（如注入坏 tag），详情页点
      Revert → GitHub 出现 `revert/theme/<build>` PR，人工合并后集群
      回到上一版且 `service_releases` 记录链完整
- [ ] A5 权限：viewer 角色触发/回退按钮不可用（403），admin 可用

## 3. 交付结果
- **代码**：internal/delivery（jenkins/argocd/webhook/revert）、
  migration 004、Jenkinsfile Notify stage、前端 pipelines 两页
- **部署物**：jenkins-api-token secret、platform SA 的 argocd 读绑定、
  webhook secret 模板
- **数据**：pipeline_runs 全量流水记录（后续周报数据源）
- **文档**：蓝图 §6 Phase1 落地勾选

## 4. 风险与回退
| 风险 | 缓解 |
|---|---|
| GitHub revert API 兼容性 | 备选实现：创建分支 + GH API 无 revert 原语时退化为
  生成 `git revert` 指令卡片（人执行）；两者都保留 PR 链接语义 |
| webhook 乱序/丢失 | stages 按 stage 名幂等覆盖；补拉 BuildStatus 兜底刷新 |
| Jenkins 无外部 URL | 平台在集群内直连 svc，不受影响；人看 Jenkins 仍 port-forward |
