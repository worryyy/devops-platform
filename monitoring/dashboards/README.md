# Grafana 大盘（uid 对照表）

`monitoring/dashboards/*.json` 是大盘源文件；部署时由
`k3s/ci/scripts/sync-dashboards.sh` 生成 chart values
（`k3s/helm-values/platform/grafana-dashboards.yaml`），随 grafana
Application 一起由 Argo CD 下发。

## uid 对照

| uid | 文件 | 内容 | 平台入口 |
|---|---|---|---|
| `node-res` | node-resources.json | 节点 CPU/内存/磁盘/PVC 使用率 | `/dashboards` → 节点资源 |
| `pod-topn` | pod-topn.json | 容器 CPU/内存 Top10 | `/dashboards` → Pod TopN |
| `ecampus-sli` | ecampus-sli.json | 服务 QPS/错误率/P95/路由成功率 | `/dashboards` → 服务 SLI |

前端 `/dashboards` 页 hardcode 了这三个 uid；新增大盘时：放 json → 跑
sync 脚本 → 提交两个文件 → 前端补一个 uid 选项。

## 依赖的采集

- `node-res`：node-exporter（chart 自带 kubelet job 的 volume 指标）
- `pod-topn`：kubelet cadvisor —— prometheus values 里该 job 的
  metric_relabel keep 已含 `container_cpu_usage_seconds_total` /
  `container_memory_working_set_bytes`（P3 放开）
- `ecampus-sli`：`ecampus:*` recording rules（P0 已有）

## 镜像与部署备注

- 镜像走 ACR 转存：`docker pull grafana/grafana:<tag>` →
  `pulseops/grafana:12.0.0`（chart 9.0.0 的 appVersion），values 里钉 tag。
- 嵌入：`serve_from_sub_path: true` + 平台 `/grafana` 反代（服务账号
  凭据注入，iframe 免二跳登录）；`allow_embedding: true` 放行 iframe。
- grafana-admin Secret：`k3s/secrets/grafana.example.yaml` 模板。
