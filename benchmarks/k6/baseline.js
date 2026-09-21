// P4 baseline：单服务阶梯加压（ecampus-topic），找错误率拐点。
// 用途：A2/A3 验收的慢接口/错误日志样本来源；管道吞吐基线。
// 跑法：k6 run --env BASE_URL=http://100.115.204.94.nip.io baseline.js
//
// 【压测窗口禁止触发 Jenkins 构建】——node1 上 CH/Kafka 与构建抢内存
//（P4 计划风险表）；跑本脚本前后 30 分钟内不要手动/自动触发发布。
import http from 'k6/http';
import { check, sleep } from 'k6';

const BASE = __ENV.BASE_URL || 'http://100.115.204.94.nip.io';
const TAG = { service: 'topic', scenario: 'baseline' };

export const options = {
  scenarios: {
    ladder: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '2m', target: 10 },   // 温和起步
        { duration: '3m', target: 10 },
        { duration: '2m', target: 25 },   // 阶梯 2
        { duration: '3m', target: 25 },
        { duration: '2m', target: 50 },   // 阶梯 3：预期接近拐点
        { duration: '3m', target: 50 },
        { duration: '2m', target: 0 },    // 回落
      ],
      gracefulRampDown: '30s',
    },
  },
  thresholds: {
    // 加压脚本自身不断言 SLO（那是平台/告警的事）；只做网络级兜底
    http_req_failed: ['rate<0.5'],
  },
};

export default function () {
  // 读路径（ingress 实际路由：/api/topic /api/like /api/collection）
  http.get(`${BASE}/api/topic?page=1&size=20`, { tags: TAG });
  sleep(0.4);
  // 写路径（未登录 POST 预期 4xx——日志仍会产出，供管道/聚合验收）
  http.post(`${BASE}/api/topic`, JSON.stringify({ title: 'k6-baseline', content: 'load' }),
    { tags: TAG, headers: { 'Content-Type': 'application/json' } });
  sleep(0.6);
}

export function handleSummary(data) {
  // 阶梯各段的 RPS/p95 摘要打到 stdout，完整数据用 --out json 落盘
  const p95 = data.metrics.http_req_duration ? data.metrics.http_req_duration.values['p(95)'] : -1;
  const rate = data.metrics.http_reqs ? data.metrics.http_reqs.values.rate : -1;
  console.log(`baseline: p95=${(p95 / 1000).toFixed(2)}s rps=${rate.toFixed(1)}`);
  return {};
}
