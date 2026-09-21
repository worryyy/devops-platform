# P5 · AI 盯盘降噪

> **目标**：告警网关长出 AI 第三道闸——单次多模态调用判决「劣化趋势 vs
  瞬时抖动」+ 错误日志模式摘要，随飞书卡片下发；判决可评估（混淆矩阵）、
  可复盘（落库）、fail-open（AI 故障不吞告警）。
> **工期**：2 周。
> **前置依赖**：P4 验收 A1-A4（日志可查可聚合、事件入 CH）。
> **蓝图引用**：§7.2 全部（三道闸、证据包边界与质量保障、两处红线）。
> **状态**：⬜ 未开始

## 1. 任务清单

### 1.1 LLM 基础设施
- [ ] `internal/ai/client.go`：OpenAI 兼容 chat 客户端（图文混合输入，
  图片走 base64 data URL），配置 `LLM_API_BASE/LLM_API_KEY/LLM_MODEL`；
  超时 15s、重试 1 次、JSON mode；输出严格按 schema 解析，解析失败视为
  调用失败
- [ ] Grafana 渲染：安装 grafana-image-renderer 插件（grafana values
  sidecar/独立 deployment，~512M，nodeSelector worker）；
  `internal/obs/render.go`：按 dashboard uid+时间窗出 PNG（render API
  带服务变量），失败时证据包降级为纯数值

### 1.2 证据包组装（internal/ai/evidence.go）
- [ ] 指标段：Prom range 查询该服务核心序列（QPS/错误率/P95），
  `max_over_time[1m]` 降采样，窗口 60min，top ≤3 序列
- [ ] 日志段（P4 patterns 复用）：窗口推导
  `[startsAt - max(for 时长,20min), endsAt]`；`GET patterns` top 5 +
  每模式 7 天基线计数（CH 再查一次）→ 标注 新增/长期存在
- [ ] sanity 前置：窗口内该服务日志总量查询——总量 0 → evidence_quality=
  missing（采集故障路径，不进 LLM，直接按纯指标判决）；总量正常但
  ERROR=0 → 放宽 WARN+ 复查一次
- [ ] 上下文硬上限：各段字符预算（数值 ~1.5K / 模式 ~1K / 元数据 ~0.5K），
  超限按优先级整段丢弃（告警历史→第二序列→样本截断）；组装后 token
  估算（chars/4）记录日志
- [ ] 元数据段：告警 labels、关联发布（service_releases 最近一条，发布
  窗口内标注）、该服务近 1h 告警次数（alerts 表）

### 1.3 判决与发送
- [ ] prompt（版本化存 `internal/ai/prompts/verdict.md`，带版本号进
  日志）：输入证据包 + 范围说明；要求输出
  `{degrade: bool, confidence: 0-1, trend_type: ramp|spike|sawtooth|flat,
  evidence_quality: sufficient|partial|missing, log_summary: str,
  reason: str, suggested_action: str}`
- [ ] 判决缓存：同 alertname+service 24h 内复用（PG 表或内存 TTL，
  `ai_verdicts` 见下）
- [ ] `migrations/007_ai_verdicts.sql`：
      `ai_verdicts(id, alert_fingerprint, service, prompt_version,
      verdict jsonb, latency_ms int, tokens_in/out int, created_at)`
      ——同时写 CH events（供周报复盘）
- [ ] 网关接线（internal/alertgw 扩展）：
  - 路由规则：高危白名单与 user_impact → 直发（不进 AI）；确定性
    deploy_noise（单次重启等）→ 规则闸处理；其余 → AI 判决
  - fail-open 双保险：LLM 失败 / 输出不合规 / evidence_quality=missing
    → 一律照发
- [ ] 飞书卡片升级：判决徽标（劣化/抖动+置信度）、log_summary、趋势
  类型、溯源行（service·窗口·窗口内日志总量）、建议动作、
  「查看原始日志」深链（/logs 预填同过滤条件）

### 1.4 评估（必须产出数据）
- [ ] 评估集构建：用 P4 `jitter.js` 场景跑 20+ 条告警（抖动/劣化各半，
  人工标注 gold label 存 `benchmarks/ai-verdicts/labeled.json`）
- [ ] 评估脚本 `benchmarks/ai-verdicts/eval.sh`：重放评估集 → 输出
  混淆矩阵（误杀=把劣化判为抖动且照降噪、漏报=反之外理）+ 各 prompt
  版本对比；结果写 `benchmarks/results/ai-verdicts.md`
- [ ] 红线校验：注入「CrashLoop + LLM 超时」用例 → 告警仍直发（单测）

### 1.5 前端
- [ ] `/alerts` 行展开增加：AI 判决卡片（verdict/置信度/log_summary/
  evidence_quality/prompt 版本）
- [ ] `/reports` 周报接入 AI 段：本周判决统计（误杀/漏报/平均置信度/
  token 成本）

## 2. 验收标准
- [ ] A1 判决在流：k6 jitter 场景下，非白名单告警的飞书卡片带判决与
  log_summary；`ai_verdicts` 表有对应记录（latency/tokens 可查）
- [ ] A2 fail-open：手动断开 LLM_API_BASE（改错配置发布）→ 触发告警
  → 飞书仍收到（卡片标注「AI 不可用，按原样通知」）
- [ ] A3 高危直通：CrashLoop 告警不经 AI（ai_verdicts 无记录）且飞书
  即时收到
- [ ] A4 证据质量：停 fluent-bit → 触发服务告警 → 卡片 evidence_quality
  =missing 且提示采集异常（不误判「无错误」）
- [ ] A5 评估数据：`benchmarks/results/ai-verdicts.md` 有 ≥20 条标注
  样本的混淆矩阵；误杀率 0（红线），漏报率有数字与改进记录
- [ ] A6 成本：token 用量统计入周报；判决缓存命中率 >60%（jitter
  场景 24h 窗口）

## 3. 交付结果
- **代码**：internal/ai（client/evidence/prompts/verdict）、alertgw 三道
  闸接线、migration 007、grafana-renderer 部署、前端卡片
- **部署物**：LLM/飞书 secret 模板、renderer values+app
- **数据**：ai-verdicts 评估集与混淆矩阵结果（周报复盘闭环）
- **文档**：prompt 版本记录表（改 prompt 必须跑评估集对比）

## 4. 风险与回退
| 风险 | 缓解 |
|---|---|
| LLM 输出不稳定 | JSON schema 严格解析 + 失败即 fail-open；prompt 版本化+评估回归 |
| 成本超预期 | 三重限流：规则闸前置、24h 缓存、上下文硬预算；token 周报监控 |
| grafana renderer 内存 | 512M 上限 + 失败降级纯数值（证据包设计已允许缺图） |
| 误杀真劣化 | 评估集红线（误杀=0 才可上线降噪路径）；降噪只降「通知级别」不改状态 |
