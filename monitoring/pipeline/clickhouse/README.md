# 数据管道（Kafka + ClickHouse）· 数据字典与部署说明

## 部署时执行（幂等，可重复跑）

```sh
# 集群内执行（clickhouse-client 在 pod 内）：
for f in monitoring/pipeline/clickhouse/*.sql; do
  kubectl exec -n platform platform-clickhouse-0 -- \
    clickhouse-client --user platform --ask-password < "$f"
done
```

> `platform` 账号密码来自 Secret `clickhouse-admin`（模板
> `k3s/secrets/clickhouse.example.yaml`，chart values `auth.existingSecret`）。

## 数据流

```
业务 Pod（slog JSON）
  └─ /var/log/containers/*.log（k3s CRI 行格式）
       └─ fluent-bit DaemonSet：cri parser → k8s filter(Merge_Log) 
            └─ Kafka topic logs.raw（key=service，JSON 整条记录）
                 └─ ClickHouse logs_kafka（JSONAsObject → raw）
                      └─ logs_mv（JSON* 抽取 + 兜底）→ logs（MergeTree）
alertgw / delivery / inspection → events（双写，不经 Kafka）
```

## 表：`platform.logs`（目标表）

| 字段 | 类型 | 说明 |
|---|---|---|
| service | LowCardinality(String) | 业务 slog `service` 字段；解析失败 → `unparsed` |
| ts | DateTime64(3) | slog `ts`（RFC3339 解析）；缺失/非法 → 入库时刻 |
| level | LowCardinality(String) | slog `level`；缺失 → `INFO` |
| route | String | slog `route`（接口路径） |
| trace_id | String | OTel trace id（P6 埋点后非空） |
| deploy_id | String | 发布标识（go-service chart 注入） |
| pod | String | k8s filter 注入 `kubernetes.pod_name` |
| namespace | String | k8s filter 注入 `kubernetes.namespace_name` |
| msg | String | slog `msg`；非 JSON 行回退 `message`/原始行（截 8000 字符） |
| attrs | String | **整行原始 JSON 文本**（含全部额外字段，`JSONExtractString(attrs,'x')` 按需取） |
| ingestion_ts | DateTime | 入库时刻 |

- 引擎：`MergeTree PARTITION BY toMonday(ts) ORDER BY (service, level, ts)`
- **TTL 30 天**（`ts + 30 DAY`），周分区自动滚动清理。

## 表：`platform.logs_kafka`（Kafka 引擎）

- `raw String`，`JSONAsString` 格式整行入一列（注：`JSONAsObject` 要求
  JSON 类型列，25.7 实测启动即 BAD_ARGUMENTS，勿用）
- broker `platform-kafka.platform.svc.cluster.local:9092`，topic `logs.raw`
- group **`platform-sinker`**（固定；CH 重启从 committed offset 续消费，
  配合 Kafka 72h 保留构成防丢边界）
- `kafka_handle_error_mode='stream'`：毒消息进 `logs_kafka_errors`（TTL 7d），
  不阻塞消费

## 表：`platform.events`

| 字段 | 类型 | 说明 |
|---|---|---|
| kind | LowCardinality(String) | `alert` / `release` / `inspection`(P6) / `ai_verdict`(P5) / `report` |
| source | String | 写入方标识（alertmanager / platform-server / inspection-engine） |
| payload | String | JSON 文档；kind=alert：`{alertname, service, severity, status, fingerprint, labels}` |
| ts | DateTime64(3) | 事件时刻 |

- **TTL 90 天**，按月分区。

## 演进备注

- **Go sinker（P4.5 可选）**：CH Kafka 引擎在 CH 长停（>Kafka 保留 72h）时
  会丢窗口内数据；如不可接受，改 platform-server 内 go consumer
  （DLQ + 重试）替代 MV。当前 v1 用引擎 + 固定 group 已满足验收。
- `attrs` 为 String（JSON 文本）而非 JSON 类型：跨 CH 版本稳妥；
  升级到 JSON 类型可直接 `ALTER ... MODIFY COLUMN`（数据兼容）。
