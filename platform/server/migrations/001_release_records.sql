create table if not exists service_releases (
  id bigserial primary key,

  service text not null,
  environment text not null default 'dev',

  git_revision text not null,
  image_digest text not null,
  config_revision text not null default '',
  rollout_strategy text not null default '',

  -- releasing | stable | failed | compensating
  release_status text not null,
  released_at timestamptz,

  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

-- At most one verified stable version per service and environment.
create unique index if not exists service_releases_stable_unique
  on service_releases (service, environment)
  where release_status = 'stable';

create index if not exists service_releases_service_status_idx
  on service_releases (service, environment, release_status, released_at desc);
