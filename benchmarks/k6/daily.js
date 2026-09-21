// P4 daily：13 服务混合负载（读写比 7:3），早晚高峰系数，持续 24h。
// 用途：A5 验收——Kafka lag 在 24h daily 场景稳定 <1 万条；CH 持续增长。
// 跑法：k6 run --duration 24h --env BASE_URL=... daily.js
//
// 【压测窗口禁止触发 Jenkins 构建】（P4 计划风险表）。
// 说明：写操作未带登录态，业务上多为 4xx——本场景目标是产生「真实形态」的
// 访问日志流（route/status/耗时分层）喂管道，非业务正确性验证。
import http from 'k6/http';
import { sleep } from 'k6';

const BASE = __ENV.BASE_URL || 'http://100.115.204.94.nip.io';

// 13 服务 × ingress 实际路由（见 k3s/helm-values/workloads/）
const SERVICES = [
  { name: 'user',          reads: ['/api/user/1'],                      writes: ['/api/user'], },
  { name: 'topic',         reads: ['/api/topic?page=1&size=20'],        writes: ['/api/topic'], },
  { name: 'comment',       reads: ['/api/comment?topicId=1'],           writes: ['/api/comment'], },
  { name: 'chat',          reads: ['/api/conversation'],                writes: ['/api/message'], },
  { name: 'agentchat',     reads: ['/api/agent'],                       writes: ['/api/agent'], },
  { name: 'file',          reads: ['/file'],                            writes: ['/file'], },
  { name: 'marketplace',   reads: ['/api/marketplace'],                 writes: ['/api/marketplace'], },
  { name: 'moderation',    reads: ['/api/moderation'],                  writes: ['/api/moderation'], },
  { name: 'notification',  reads: ['/api/notification'],                writes: ['/api/notify'], },
  { name: 'reservation',   reads: ['/api/reservation'],                 writes: ['/api/reservation'], },
  { name: 'school',        reads: ['/api/term'],                        writes: ['/api/user/authentication'], },
  { name: 'theme',         reads: ['/api/theme'],                       writes: ['/api/theme'], },
  { name: 'academic',      reads: ['/api/academic'],                    writes: ['/api/academic'], },
];

// 早晚高峰系数：深夜 0.3x，早高峰 08-10 点 1.5x，晚高峰 19-22 点 1.8x
function peakFactor() {
  const h = new Date().getHours();
  if (h >= 19 && h < 22) return 1.8;
  if (h >= 8 && h < 10) return 1.5;
  if (h >= 0 && h < 7) return 0.3;
  return 1.0;
}

let execIteration = 0;

export const options = {
  scenarios: {
    daily: {
      executor: 'per-vu-iterations', // 与 peakFactor() 配合：VU 内自律节奏
      vus: 10,
      iterations: 1e9,               // 由 --duration 控制总时长
      maxDuration: '24h',
      gracefulStop: '30s',
    },
  },
};

export default function () {
  // 读写 7:3：每个迭代 10 次请求，7 读 3 写，服务均匀轮转
  for (let i = 0; i < 7; i++) {
    const svc = SERVICES[(execIteration * 7 + i) % SERVICES.length];
    http.get(`${BASE}${svc.reads[0]}`, { tags: { service: svc.name, scenario: 'daily', op: 'read' } });
  }
  for (let i = 0; i < 3; i++) {
    const svc = SERVICES[(execIteration * 3 + i) % SERVICES.length];
    http.post(`${BASE}${svc.writes[0]}`, JSON.stringify({ from: 'k6-daily' }),
      { tags: { service: svc.name, scenario: 'daily', op: 'write' },
        headers: { 'Content-Type': 'application/json' } });
  }
  execIteration++;
  // 高峰多打、深夜少打：基准间隔 3s / 系数
  sleep(3 / peakFactor());
}
