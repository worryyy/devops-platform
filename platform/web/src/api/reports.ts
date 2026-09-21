import { api, unwrap } from './client';

export type ReportRun = {
  id: number;
  kind: string;
  period: string;
  objectPath: string;
  status: string;
  error: string;
  createdAt: string;
};

export type ReportStats = {
  window: { start: string; end: string };
  releases: {
    total: number;
    stable: number;
    failed: number;
    ongoing: number;
    byService: Record<string, number>;
  };
  pipelines: { total: number; success: number; failed: number; running: number; queued: number };
  alerts: { total: number; top: { alertname: string; service: string; count: number }[] };
};

export const reportsApi = {
  list: (limit = 50) => unwrap<ReportRun[]>(api.get('/reports', { params: { limit } })),
  trigger: (period?: string) => unwrap<ReportRun>(api.post('/reports/trigger', { period })),
  previewUrl: (id: number) => `/api/reports/${id}/preview`,
};
