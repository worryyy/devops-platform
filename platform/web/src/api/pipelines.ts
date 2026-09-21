import { api, unwrap } from './client';

export type StageEntry = {
  name: string;
  status: string;
  durationMs: number;
  at?: string;
};

export type PipelineRun = {
  id: number;
  service: string;
  jenkinsBuild: number;
  status: 'queued' | 'running' | 'success' | 'failed';
  stages: StageEntry[];
  triggeredBy: string;
  gitRevision: string;
  configRevision: string;
  imageDigest: string;
  revertPrUrl: string;
  startedAt: string;
  finishedAt: string;
  createdAt: string;
}

export type ArgocdStatus = {
  name: string;
  syncStatus: string;
  health: string;
  syncVersion: string;
};

export const pipelinesApi = {
  list: (params?: { service?: string; status?: string; limit?: number }) =>
    unwrap<PipelineRun[]>(api.get('/pipelines', { params })),
  detail: (id: number) => unwrap<PipelineRun>(api.get(`/pipelines/${id}`)),
  argocd: (id: number) => unwrap<ArgocdStatus>(api.get(`/pipelines/${id}/argocd`)),
  trigger: (body: {
    services: string[];
    beforeSha?: string;
    afterSha?: string;
    targetEnv?: string;
    skipRelease?: boolean;
  }) => unwrap<PipelineRun[]>(api.post('/pipelines/trigger', body)),
  revert: (id: number) =>
    unwrap<{ prUrl: string; number: number }>(api.post(`/pipelines/${id}/revert`)),
};
