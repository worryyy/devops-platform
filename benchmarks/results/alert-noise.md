# 告警抑制实测（V2）— 2026-09-06 演练窗口

数据源：Prometheus `/api/v1/query_range`（ALERTS 序列，3 小时窗口覆盖 5 轮发布/回退演练）+ Alertmanager `/api/v2/alerts` 实时抑制状态。

## 触发统计（Prometheus 侧）

| 告警 | 活跃时长（firing） | 说明 |
|---|---|---|
| ReleaseDeployNoiseWindow | ~94 分钟 | 发布窗口上下文信号（设计上路由到无配置 receiver，永不通知）|
| ReleasePodRestarting | ~122 分钟 | 瞬时重启类（**可抑制**类）|
| ReleasePodCrashLooping | ~44 分钟 firing + 14 分钟 pending | 持续故障（**永不抑制**类）|

## 抑制行为（Alertmanager 侧，实测快照）

- `ReleasePodRestarting` → **suppressed，inhibitedBy=1**（被同 deploy_id 的噪声窗精确抑制）
- `ReleasePodCrashLooping` → **active，inhibited=0**（持续故障在噪声窗内仍完整透传）
- `ReleaseDeployNoiseWindow` → active（仅作抑制源，路由到空 receiver，不产生通知）

## 结论（可直接写入简历）

- 抑制规则**双向正确**：该抑制的（瞬时重启）被抑制，不该抑制的（CrashLoop）零抑制
- 抑制精确匹配 `namespace/service/environment/deploy_id` 四元组，跨发布互不干扰
- 演练窗口内 Prometheus 共触发 3 类发布期告警、峰值并发 4 条，通知面（排除被抑制+空路由后）仅剩持续故障 1 类
