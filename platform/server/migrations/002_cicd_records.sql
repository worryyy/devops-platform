create table if not exists build_records (
  build_id uuid primary key,

  service_name varchar(128) not null,
  repo_url text not null,
  branch varchar(255) not null,
  commit_sha varchar(64) not null,

  status varchar(32) not null,
  trigger_type varchar(32) not null,
  builder varchar(128) not null,

  jenkins_job varchar(255),
  jenkins_build_number integer,
  jenkins_build_url text,
  source_build_id uuid references build_records(build_id),

  image_repo text,
  image_tag varchar(255),
  image_digest varchar(255),

  failed_stage varchar(128),
  error_message text,
  params_snapshot jsonb,

  started_at timestamptz,
  finished_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists idx_build_records_service_status_finished
  on build_records(service_name, status, finished_at desc);

create index if not exists idx_build_records_service_branch_created
  on build_records(service_name, branch, created_at desc);

create index if not exists idx_build_records_commit_sha
  on build_records(commit_sha);

create index if not exists idx_build_records_image_digest
  on build_records(image_digest);

create index if not exists idx_build_records_success_images
  on build_records(service_name, finished_at desc)
  where status = 'success' and image_digest is not null;

create table if not exists deploy_records (
  deploy_id uuid primary key,

  service_name varchar(128) not null,
  environment varchar(64) not null,
  namespace varchar(128) not null,

  status varchar(32) not null,
  deploy_type varchar(32) not null,
  delivery_mode varchar(32) not null,
  deployer varchar(128) not null,
  confirmed_by varchar(128),
  confirmed_at timestamptz,

  build_id uuid references build_records(build_id),

  image_repo text not null,
  image_tag varchar(255) not null,
  image_digest varchar(255) not null,
  commit_sha varchar(64),
  current_image text,

  argocd_application varchar(255),
  gitops_commit varchar(64),

  deploy_params_snapshot jsonb,
  values_yaml_snapshot text,

  rollback_from_deploy_id uuid references deploy_records(deploy_id),
  rollback_to_deploy_id uuid references deploy_records(deploy_id),

  failed_stage varchar(128),
  error_message text,

  started_at timestamptz,
  finished_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists idx_deploy_records_service_env_status_created
  on deploy_records(service_name, environment, status, created_at desc);

create index if not exists idx_deploy_records_build_id
  on deploy_records(build_id);

create index if not exists idx_deploy_records_rollback_from
  on deploy_records(rollback_from_deploy_id);

create index if not exists idx_deploy_records_rollback_to
  on deploy_records(rollback_to_deploy_id);

create table if not exists pipeline_stage_records (
  id uuid primary key,

  build_id uuid references build_records(build_id),
  deploy_id uuid references deploy_records(deploy_id),

  stage varchar(128) not null,
  stage_order integer not null,
  status varchar(32) not null,
  message text,
  detail jsonb,

  started_at timestamptz,
  finished_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),

  constraint chk_pipeline_stage_owner check (
    (build_id is not null and deploy_id is null)
    or
    (build_id is null and deploy_id is not null)
  )
);

create index if not exists idx_pipeline_stage_records_build_order
  on pipeline_stage_records(build_id, stage_order);

create index if not exists idx_pipeline_stage_records_deploy_order
  on pipeline_stage_records(deploy_id, stage_order);
