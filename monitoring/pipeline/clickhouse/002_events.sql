-- P4 事件表：alert/发布/巡检（P6）/AI 判决（P5）统一入此表，
-- 供周报与 P5 证据包跨域查询。写入方：platform-server（alertgw 双写、
-- delivery、inspection），payload 为 JSON 文本（String，版本安全）。
CREATE DATABASE IF NOT EXISTS platform;

CREATE TABLE IF NOT EXISTS platform.events
(
    kind    LowCardinality(String),  -- alert | release | inspection | ai_verdict | report
    source  String,                  -- alertmanager | platform-server | inspection-engine ...
    payload String,                  -- JSON 文档（各 kind 的 schema 见 README）
    ts      DateTime64(3) DEFAULT now()
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(ts)
ORDER BY (kind, ts)
TTL toDateTime(ts) + INTERVAL 90 DAY;
