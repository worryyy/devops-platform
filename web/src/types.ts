export type ApiResponse<T> = {
  code: number;
  message: string;
  data: T;
};

export type Service = {
  name: string;
  displayName?: string;
  owner?: string;
  environments: Environment[];
};

export type Environment = {
  name: string;
  namespace: string;
  branchPolicy: {
    defaultBranch: string;
    allowedBranches: string[];
  };
  git: {
    repo: string;
    chartPath: string;
    valuesFile: string;
  };
  image: {
    repository: string;
    tagPolicy: string;
    requireDigest: boolean;
  };
  jenkins: {
    mode: string;
    jobName: string;
  };
  argocd: {
    application: string;
    namespace: string;
  };
  kubernetes: {
    namespace: string;
    deployment: string;
    service: string;
    container: string;
  };
  health: {
    healthPath: string;
    readyPath: string;
  };
};

export type BuildRecord = {
  build_id: string;
  service_name: string;
  repo_url: string;
  branch: string;
  commit_sha: string;
  status: string;
  trigger_type: string;
  builder: string;
  image_repo?: string;
  image_tag?: string;
  image_digest?: string;
  failed_stage?: string;
  error_message?: string;
  created_at: string;
  started_at?: string;
  finished_at?: string;
};

export type DeployRecord = {
  deploy_id: string;
  service_name: string;
  environment: string;
  namespace: string;
  status: string;
  deploy_type: string;
  delivery_mode: string;
  deployer: string;
  build_id?: string;
  image_repo: string;
  image_tag: string;
  image_digest: string;
  current_image?: string;
  failed_stage?: string;
  error_message?: string;
  created_at: string;
  started_at?: string;
  finished_at?: string;
};

export type StageRecord = {
  id: string;
  build_id?: string;
  deploy_id?: string;
  stage: string;
  stage_order: number;
  status: string;
  message?: string;
  detail?: Record<string, unknown>;
  created_at: string;
  started_at?: string;
  finished_at?: string;
};

export type DashboardSummary = {
  today_builds: number;
  today_deploys: number;
  build_success_rate: number;
  deploy_success_rate: number;
  running_builds: number;
  running_deploys: number;
  recent_failed_builds: number;
  recent_failed_deploys: number;
  recent_builds: BuildRecord[];
  recent_deploys: DeployRecord[];
  active_locks: Array<Record<string, unknown>>;
};

