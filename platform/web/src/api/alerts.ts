import { api, unwrap } from './client';

export type Alert = {
  id: number;
  fingerprint: string;
  alertname: string;
  service: string;
  severity: string;
  signalType: string;
  labels: { set: Record<string, string> };
  annotations: { set: Record<string, string> };
  status: string;
  startsAt: string;
  endsAt: string;
  receivedAt: string;
};

export const alertsApi = {
  list: (params?: {
    status?: string;
    service?: string;
    severity?: string;
    signalType?: string;
    limit?: number;
  }) => unwrap<Alert[]>(api.get('/alerts', { params })),
  ack: (id: number) => unwrap<{ ok: boolean; ackedBy: string }>(api.post(`/alerts/${id}/ack`)),
};
