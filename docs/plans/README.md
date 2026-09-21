# 分阶段执行计划索引

> 总蓝图见 [../../PLATFORM_PLAN.md](../../PLATFORM_PLAN.md)（架构分层、逐项取舍、资源预算）。
> 本目录是各阶段的**开工级计划**：任务拆到文件/接口/表结构/命令级，照单执行。
> 用法：开工前读对应计划 → 逐项勾选任务 → 验收标准全过才算阶段完成 → 状态回写本表。

## 总览

| 阶段 | 计划文档 | 工期 | 依赖 | 状态 |
|---|---|---|---|---|
| P0 | （见下方完成记录） | — | — | ✅ 已完成 |
| P1 | [P1-平台骨架.md](./P1-平台骨架.md) | 2-3 周 | P0 | ✅ 已完成（2026-09-21，CICD 冒烟按指示跳过） |
| P2 | [P2-CICD集成.md](./P2-CICD集成.md) | 2 周 | P1 验收 A1-A6 | ⬜ 未开始 |
| P3 | [P3-可观测与告警.md](./P3-可观测与告警.md) | 2-3 周 | P2 验收 A1-A4 | ⬜ 未开始 |
| P4 | [P4-数据管道.md](./P4-数据管道.md) | 2 周 | P1 验收 A1/A5、P3 验收 A1 | ⬜ 未开始 |
| P5 | [P5-AI盯盘降噪.md](./P5-AI盯盘降噪.md) | 2 周 | P4 验收 A1-A4 | ⬜ 未开始 |
| P6 | [P6-链路与巡检.md](./P6-链路与巡检.md) | 3 周 | P1 验收 A1、P4 验收 A1 | ⬜ 未开始 |
| P7 | [P7-打磨与云集成.md](./P7-打磨与云集成.md) | 持续 | P2/P3/P6 | ⬜ 未开始 |

阶段并行性说明：P4 只依赖 P1 的部署底座和 P3 的告警链路骨架，可与 P2/P3
部分并行；P5 强依赖 P4（日志进 CH）；P6 前半（链路）只依赖 P1，可提前。

## P0 完成记录（2026-09）

- 删除灰度发布（Argo Rollouts Canary/BlueGreen/Analysis）与集群回退
  （rollback-release、补偿 PR、selfHeal 竞态防护）全部代码，13 服务转为
  普通 Deployment，保留最小 CICD 流水线。
- 清理旧文档/benchmarks/bench 脚本/死代码（约 -2 万行）；恢复部署排障
  实录为 [k3s/TROUBLESHOOTING.md](../../k3s/TROUBLESHOOTING.md)。
- 新增部署预检与 Runbook：
  [k3s/DEPLOY_RUNBOOK.md](../../k3s/DEPLOY_RUNBOOK.md)、
  [k3s/playbooks/preflight.yml](../../k3s/playbooks/preflight.yml)。
- 验证：`go build/test`、`sh k3s/ci/scripts/test-delivery-contract.sh`、
  `sh k3s/ci/scripts/test-wait-release.sh`、helm 渲染 13 Deployment、
  promtool 规则单测全绿。

## 通用约定（各阶段计划共用）

- 后端模块新建位置：`platform/server/internal/<module>/`；API 统一挂
  `/api` 前缀，沿用现有 gin + 统一响应封装模式。
- 前端工程：`platform/web/`（Vite + React + TS + Ant Design）。
- 新增组件的 helm values 一律放 `k3s/helm-values/platform/<name>.yaml`，
  gitops Application 放 `k3s/gitops/applications/platform/<name>.yaml`。
- 节点标签沿用现有约定：`platform-role=control`（16G 控制节点）/
  `light`（轻组件）/ `worker`（观测与业务）。资源预算见蓝图 §3。
- 每阶段的数据库变更写迁移文件 `platform/server/migrations/NNN_*.sql`
  （编号递增），GORM 只做读写在手模型的映射。
- 验收命令默认在仓库根执行；涉及集群的用 kubectl（kubeconfig 指向
  dev 集群）。
