import { api, unwrap } from './client';

export type LogRow = {
  service: string;
  ts: string;
  level: string;
  route: string;
  trace_id: string;
  pod: string;
  msg: string;
};

export type PatternRow = {
  pattern: string;
  count: number;
  first_seen: string;
  last_seen: string;
  pods: string[];
  sample: string;
};

export type HistogramPoint = {
  minute: string;
  count: number;
};

export type LogsFilterParams = {
  service?: string;
  level?: string;
  q?: string;
  trace_id?: string;
  start?: string; // RFC3339
  end?: string; // RFC3339
  limit?: number;
};

export const logsApi = {
  search: (params: LogsFilterParams) => unwrap<LogRow[]>(api.get('/logs', { params })),
  patterns: (params: LogsFilterParams) => unwrap<PatternRow[]>(api.get('/logs/patterns', { params })),
  histogram: (params: LogsFilterParams) => unwrap<HistogramPoint[]>(api.get('/logs/histogram', { params })),
};
