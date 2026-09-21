-- P4 日志管道建表：logs 目标表 + Kafka 引擎表 + 物化视图 + 消费错误表。
-- 幂等可重复执行（IF NOT EXISTS）。字段与 TTL 见同目录 README.md（数据字典）。
--
-- 链路：fluent-bit（CRI tail + k8s filter Merge_Log）→ Kafka logs.raw
--   → logs_kafka（JSONAsObject 整行入 raw）→ logs_mv 抽取字段 → logs。
-- 解析失败行（业务日志非 JSON / 字段缺失）落 service='unparsed' 兜底，
-- 同一行完整保留在 attrs（原始 JSON 文本）供追溯。

CREATE DATABASE IF NOT EXISTS platform;

-- 目标表：按 (service, level, ts) 排序，周分区，30 天 TTL
CREATE TABLE IF NOT EXISTS platform.logs
(
    service      LowCardinality(String),
    ts           DateTime64(3),
    level        LowCardinality(String),
    route        String,
    trace_id     String,
    deploy_id    String,
    pod          String,
    namespace    String,
    msg          String,
    attrs        String DEFAULT '',
    ingestion_ts DateTime DEFAULT now()
)
ENGINE = MergeTree
PARTITION BY toMonday(ts)
ORDER BY (service, level, ts)
TTL toDateTime(ts) + INTERVAL 30 DAY
SETTINGS index_granularity = 8192;

-- Kafka 引擎表：group 固定（重启续消费，配合 Kafka 72h 保留防丢）；
-- JSONAsString 把整条消息读进 raw 一列（JSONAsObject 要求 JSON 类型列，
-- 25.7 实测报 BAD_ARGUMENTS——踩坑记录），字段抽取全部放到 MV 做
-- （新字段免改引擎表，风险见表 README「演进」节）
CREATE TABLE IF NOT EXISTS platform.logs_kafka
(
    raw String
)
ENGINE = Kafka
SETTINGS kafka_broker_list = 'platform-kafka.platform.svc.cluster.local:9092',
         kafka_topic_list = 'logs.raw',
         kafka_group_name = 'platform-sinker',
         kafka_format = 'JSONAsString',
         kafka_num_consumers = 1,
         kafka_row_delimiter = '\n',
         kafka_handle_error_mode = 'stream';

-- 消费/解析错误落表（管道可观测：unparsed 行也有出口）
CREATE TABLE IF NOT EXISTS platform.logs_kafka_errors
(
    raw  String,
    error String,
    ts   DateTime DEFAULT now()
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(ts)
ORDER BY ts
TTL toDateTime(ts) + INTERVAL 7 DAY;

CREATE MATERIALIZED VIEW IF NOT EXISTS platform.logs_kafka_errors_mv
TO platform.logs_kafka_errors
AS
SELECT raw, _error AS error
FROM platform.logs_kafka
WHERE _error != '';

-- 主物化视图：JSON 抽取 + 兜底
-- 实测业务 slog 字段为 time/level/caller/msg（无 ts/service，与计划假设
-- 有偏差）；且 fluent-bit k8s filter 的 Merge_Log 未提升内层 JSON（message
-- 仍是字符串）——统一在 MV 做「源切换」：顶层有 msg 用 raw，否则若
-- message 是合法 JSON 用其内容，否则按非结构化行兜底。service 从 pod 名
-- 推导（Deployment/StatefulSet 命名 <name>-<hash>[-<hash>]），level 归一
-- 大写（slog 小写 warn/error）。
CREATE MATERIALIZED VIEW IF NOT EXISTS platform.logs_mv
TO platform.logs
AS
WITH
    JSONExtractString(raw, 'kubernetes', 'pod_name') AS pod_name,
    multiIf(
        notEmpty(JSONExtractString(raw, 'msg')), raw,
        isValidJSON(JSONExtractString(raw, 'message')), JSONExtractString(raw, 'message'),
        raw) AS src,
    multiIf(
        notEmpty(JSONExtractString(src, 'service')), JSONExtractString(src, 'service'),
        notEmpty(regexpExtract(pod_name, '^(.+)-[a-z0-9]+-[a-z0-9]{5}$')), regexpExtract(pod_name, '^(.+)-[a-z0-9]+-[a-z0-9]{5}$'),
        notEmpty(regexpExtract(pod_name, '^(.+)-[0-9]+$')), regexpExtract(pod_name, '^(.+)-[0-9]+$'),
        'unparsed') AS svc,
    multiIf(
        upperUTF8(JSONExtractString(src, 'level')) = 'WARNING', 'WARN',
        notEmpty(upperUTF8(JSONExtractString(src, 'level'))), upperUTF8(JSONExtractString(src, 'level')),
        'INFO') AS lvl
SELECT
    svc                                                                                                              AS service,
    coalesce(
        nullIf(parseDateTime64BestEffortOrNull(JSONExtractString(src, 'ts')), toDateTime64(0, 3)),
        nullIf(parseDateTime64BestEffortOrNull(JSONExtractString(src, 'time')), toDateTime64(0, 3)),
        now64(3))                                                                                                   AS ts,
    lvl                                                                                                              AS level,
    JSONExtractString(src, 'route')                                                                                  AS route,
    JSONExtractString(src, 'trace_id')                                                                               AS trace_id,
    JSONExtractString(src, 'deploy_id')                                                                              AS deploy_id,
    pod_name                                                                                                         AS pod,
    JSONExtractString(raw, 'kubernetes', 'namespace_name')                                                           AS namespace,
    if(empty(JSONExtractString(src, 'msg')),
       if(empty(JSONExtractString(raw, 'message')), substring(raw, 1, 8000), JSONExtractString(raw, 'message')),
       JSONExtractString(src, 'msg'))                                                                                 AS msg,
    raw                                                                                                               AS attrs
FROM platform.logs_kafka;
