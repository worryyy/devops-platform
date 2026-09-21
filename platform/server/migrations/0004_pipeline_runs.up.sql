create table if not exists pipeline_runs (
  id bigserial primary key,
  service text not null,
  jenkins_build bigint,

  -- queued | running | success | failed
  status text not null default 'queued',
  stages jsonb not null default '[]',
  triggered_by text not null,
  git_revision text,
  config_revision text,
  image_digest text,
  revert_pr_url text,

  started_at timestamptz,
  finished_at timestamptz,
  created_at timestamptz not null default now()
);

create index if not exists pipeline_runs_service_idx
  on pipeline_runs (service, created_at desc);

create index if not exists pipeline_runs_status_idx
  on pipeline_runs (status, created_at desc);
