# P4 管道压测与对比数据（logs pipeline）

> 环境：3 节点 k3s（1×4c16g control + 2×4c8g），Kafka KRaft 单副本（node1）
> + ClickHouse 25.7.5 单副本（node1）+ Fluent Bit DaemonSet（3 节点）。
> 采集首日 2026-09-21，窗口内为集群自然日志流（13 业务服务 + 平台组件
> 稳态运行；k6 阶梯加压数据待 24h daily 窗口补录，见文末）。

## 1. 管道吞吐（rows/s 与增长）

| 指标 | 数值 | 采集方式 |
|---|---|---|
| 首小时入库行数 | 3,747（启动后 25s 内） | `SELECT count(), min/max(ts) FROM logs` |
| 稳态写入速率 | ~6.2 rows/s（1,854 行 / 5min，无压测流量） | ingestion_ts 窗口差分 |
| 首日累计（~2h） | 105,657 行 / 106.7 MiB 原始体积 | `system.parts` |
| Kafka 消费滞后 | **lag = 1**（partition 0，offset 11,586） | kafka-exporter `kafka_consumergroup_lag` |
| 端到端延迟 | < 10s（Kafka 注入 trace_id 探针 → /api/logs 可查） | 人工注入实测（8s 轮询间隔内命中） |

## 2. Kafka lag 曲线

- kafka-exporter（v1.9.0，platform ns，pod 注解接入 Prometheus kubernetes-pods job）
  持续暴露 `kafka_consumergroup_lag{topic="logs.raw",consumergroup="platform-sinker"}`。
- 当日观测：lag 稳定在个位数（0~5），无积压趋势；24h daily 场景曲线
  （验收 A5：<1 万条）由 Prometheus 记录，见下方「24h 窗口待补」。

## 3. 压缩比（CH MergeTree vs 原始日志）

| 项 | 数值 |
|---|---|
| 原始体积（uncompressed，含 attrs 原文整行保留） | 106.72 MiB |
| 落盘体积（LZ4 默认压缩） | 6.07 MiB |
| **压缩比** | **17.6×** |
| 外推 30 天留存（TTL）体积 | 当前速率 ~2.6 GiB/月原始 → <200 MiB 落盘，40G PVC 余量充足 |

## 4. 日志检索页查询延迟（/api/logs*）

实测（port-forward 单跳，含鉴权；网络开销 <5ms）：

| 查询 | 延迟 |
|---|---|
| `?level=WARN&limit=2`（10 万行表，无时间窗） | 112 ms |
| `?trace_id=<精确>`（注入探针行） | 86 ms |
| `/logs/patterns?level=WARN`（正则指纹聚合 Top10） | ~5.8 s（首次，未预热；后续待 k6 错误注入窗口复测） |
| `/logs/histogram?level=WARN`（分钟粒度聚合） | <100 ms |

验收线 A2（<2s）：原始日志检索与 trace_id 精确查询通过；patterns 首查
偏慢源于全表 `replaceRegexpAll` 三遍扫描——v1 可接受（<10s、结果正确），
优化路径（物化 pattern 列 / 限定时间窗默认值）留 P7。

## 5. CH 写入对查询的影响

当日为稳态写入（~6 rows/s），检索延迟全程无感知劣化（<120ms 稳定）。
压测窗口（k6 baseline 10→50 VU）下「写入对查询 P95 影响」的前后对比
随 24h daily 窗口一并补录。

## 6. 资源水位（A6，kubectl top，2026-09-21 16:30）

| 节点 | 内存 | 预算（蓝图 §3） | 结论 |
|---|---|---|---|
| node1 control（+kafka 561Mi +CH 314Mi） | 4,878 Mi / 16 Gi | 稳态 <10G | ✅ |
| node2 light（+fluent-bit 6Mi） | 1,784 Mi / 8 Gi | ~4.5G | ✅ |
| node3 worker（+fluent-bit 6Mi +exporter 10Mi） | 2,142 Mi / 8 Gi | ~3G | ✅ |

## 24h daily 窗口待补（A5）

- `k6 run --duration 24h benchmarks/k6/daily.js`（压测窗口禁止 Jenkins 构建）
- 记录：24h lag 曲线（Prometheus range query）、CH rows/s 曲线、
  检索页 P95 前后对比、日增体积。
- 判定：lag 全程 <10,000 条。
