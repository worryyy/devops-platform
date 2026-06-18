import type { ApiResponse, BuildRecord, DashboardSummary, DeployRecord, Service, StageRecord } from "./types";

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    headers: { "Content-Type": "application/json", ...(options?.headers ?? {}) },
    ...options
  });
  const body = (await response.json()) as ApiResponse<T>;
  if (!response.ok) {
    throw new Error(body.message || `Request failed with ${response.status}`);
  }
  return body.data;
}

export const api = {
  summary: () => request<DashboardSummary>("/api/dashboard/summary?range=7d"),
  services: () => request<{ version: string; services: Service[] }>("/api/services"),
  service: (name: string) => request<Service>(`/api/services/${encodeURIComponent(name)}`),
  builds: () => request<BuildRecord[]>("/api/builds?limit=50"),
  build: (id: string) => request<BuildRecord>(`/api/builds/${encodeURIComponent(id)}`),
  buildStages: (id: string) => request<StageRecord[]>(`/api/builds/${encodeURIComponent(id)}/stages`),
  deploys: () => request<DeployRecord[]>("/api/deploys?limit=50"),
  deploy: (id: string) => request<DeployRecord>(`/api/deploys/${encodeURIComponent(id)}`),
  deployStages: (id: string) => request<StageRecord[]>(`/api/deploys/${encodeURIComponent(id)}/stages`),
  createBuild: (payload: unknown) => request<BuildRecord>("/api/builds", { method: "POST", body: JSON.stringify(payload) }),
  dryRun: (payload: unknown) => request<DeployRecord>("/api/deploys/dry-run", { method: "POST", body: JSON.stringify(payload) }),
  confirmDeploy: (payload: unknown) => request<DeployRecord>("/api/deploys/confirm", { method: "POST", body: JSON.stringify(payload) })
};

