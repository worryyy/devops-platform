# P4 采集链路真实格式样例（3 条）

来自 2026-09-21 集群实测（kubectl logs / Kafka logs.raw 消费确认），
供 fluent-bit 解析路径与 CH logs_mv 抽取逻辑的对拍测试使用（CI 可选）。

- `slog-json.log`：业务 slog JSON 行（实际字段 time/level/caller/msg，
  无计划假设的 ts/service——MV 已按此适配，service 从 pod 名推导）
- `gorm-text.log`：GORM console 格式（非 JSON，走 service 推导 + msg 回退）
- `kafka-record.json`：fluent-bit 写入 Kafka logs.raw 后的完整记录形态
  （CRI 头 + kubernetes 元数据 + message 内嵌 JSON）
