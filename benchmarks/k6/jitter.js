// P4 jitter：周期性加减压 + 故障形态流量，P5 AI 盯盘评估素材。
// 目标：制造「瞬时抖动」与「持续劣化」两类可标注的流量形态——
//   - 抖动：短时 spike 后立即回落（预期 AI 判 degrade=false）
//   - 劣化：高压持续段 + 5xx 比例抬升（预期 AI 判 degrade=true）
// 跑法：k6 run --env BASE_URL=... jitter.js
//
// 【压测窗口禁止触发 Jenkins 构建】（P4 计划风险表）。
// 真实故障注入（停 fluent-bit / 重启 Pod / 注入延迟）另行用 kubectl 手工
// 执行——见 docs/plans/P5-AI盯盘降噪.md §1.4 评估集构建。
import http from 'k6/http';
import { sleep } from 'k6';

const BASE = __ENV.BASE_URL || 'http://100.115.204.94.nip.io';

export const options = {
  scenarios: {
    // 抖动段：尖峰 40 VU × 90s，随后静默
    jitter_spike: {
      executor: 'ramping-vus',
      startTime: '0s',
      stages: [
        { duration: '30s', target: 40 },
        { duration: '90s', target: 40 },
        { duration: '10s', target: 0 },
        { duration: '10m', target: 0 },  // 静默：观察告警恢复
      ],
    },
    // 劣化段：延迟开始 12 分钟后，持续高压 10 分钟（p95 抬升、错误率上升）
    degrade_ramp: {
      executor: 'ramping-vus',
      startTime: '12m',
      startVUs: 0,
      stages: [
        { duration: '2m', target: 30 },
        { duration: '10m', target: 60 },
        { duration: '1m', target: 0 },
      ],
    },
  },
  thresholds: { http_req_failed: ['rate<0.8'] },
};

const ROUTES = ['/api/topic?page=1&size=20', '/api/comment?topicId=1', '/api/user/1'];

export default function () {
  // 高压下混入必然失败的请求（不存在的路由 → 404/405），
  // 让错误日志模式聚合（patterns）有真实素材
  if (Math.random() < 0.15) {
    http.get(`${BASE}/api/k6-chaos-${Math.floor(Math.random() * 5)}`,
      { tags: { scenario: 'jitter', op: 'chaos' } });
  } else {
    http.get(`${BASE}${ROUTES[Math.floor(Math.random() * ROUTES.length)]}`,
      { tags: { scenario: 'jitter', op: 'normal' } });
  }
  sleep(Math.random() * 0.5);
}
