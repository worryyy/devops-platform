# k6 压测场景（P4 管道压测 / P5 评估素材）

统一入口：`k6 run --env BASE_URL=http://100.115.204.94.nip.io <scene>.js`

> **纪律：压测窗口禁止触发 Jenkins 构建**——node1 上 Kafka/ClickHouse 与
> 构建容器抢内存（P4 计划风险表）。跑任何场景前后 30 分钟不要发布。

| 场景 | 形态 | 用途 |
|---|---|---|
| `baseline.js` | 单服务（topic）阶梯 10→50 VU | 错误率拐点、慢接口样本（A2/A3、P6 A2）；管道吞吐基线 |
| `daily.js` | 13 服务混合，读写 7:3，早晚高峰系数（晚 19-22 点 1.8x / 早 8-10 点 1.5x / 深夜 0.3x），`--duration 24h` | A5：24h Kafka lag 稳定 <1 万条、CH 持续增长 |
| `jitter.js` | 抖动段（40 VU×90s spike→静默）+ 劣化段（12 分钟后持续升压 10 分钟）+ 15% 混沌路由 | P5 评估集：抖动/劣化各半的告警样本 |

## 输出约定

```sh
# 摘要进终端 + 完整数据落盘（供 benchmarks/results/pipeline.md 引用）
k6 run --out json=benchmarks/results/raw/<scene>-$(date +%m%d%H%M).json <scene>.js
```

- 管道侧数据另取：Kafka lag（kafka-exporter / Prometheus）、CH
  `system.parts`、日志检索页查询延迟（平台 /logs 手测记录）。
- P5 评估集标注：jitter.js 跑完后从 `alerts` + `ai_verdicts` 对照人工
  gold label，见 `benchmarks/ai-verdicts/`（P5 建立）。
