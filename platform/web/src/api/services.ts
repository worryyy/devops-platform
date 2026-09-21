import { api, unwrap } from './client';

export interface LoginResponse {
  token: string;
  expiresAt: string;
  user: { username: string; role: string };
}

export interface ServiceSummary {
  name: string;
  displayName: string;
  owner: string;
  kind: string;
  environmentCount: number;
  updatedAt: string;
}

export interface SLIPolicy {
  requestRouteRegex: string;
  operationRouteRegex?: string;
  maxP95Seconds: number;
}

export interface ServiceEnvironment {
  name: string;
  namespace: string;
  branchPolicy: { defaultBranch: string; allowedBranches: string[] };
  git: { repo: string; chartPath: string; valuesFile: string };
  image: { repository: string; tagPolicy: string; requireDigest: boolean };
  jenkins: { mode: string; jobName: string };
  argocd: { application: string; namespace: string };
  kubernetes: { namespace: string; workload: string; service: string; container: string };
  health: { healthPath: string; readyPath: string };
}

export interface ServiceDetail {
  name: string;
  displayName: string;
  owner: string;
  kind: string;
  sli: SLIPolicy;
  environments: ServiceEnvironment[];
}

export const authApi = {
  login: (username: string, password: string) =>
    unwrap<LoginResponse>(api.post('/auth/login', { username, password })),
};

export const servicesApi = {
  list: (query?: string) =>
    unwrap<ServiceSummary[]>(api.get('/services', { params: query ? { query } : {} })),
  detail: (name: string) => unwrap<ServiceDetail>(api.get(`/services/${name}`)),
  update: (name: string, body: { displayName?: string; owner?: string; sli?: SLIPolicy }) =>
    unwrap<ServiceDetail>(api.put(`/services/${name}`, body)),
  importCatalog: (path: string) =>
    unwrap<{ imported: number }>(api.post('/catalog/import', { path })),
};
