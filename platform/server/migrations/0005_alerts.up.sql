create table if not exists alerts (
  id bigserial primary key,
  fingerprint text unique,
  alertname text,
  service text,
  severity text,
  signal_type text,
  labels jsonb not null default '{}',
  annotations jsonb not null default '{}',

  -- firing | resolved
  status text not null default 'firing',
  starts_at timestamptz,
  ends_at timestamptz,
  received_at timestamptz not null default now()
);

create index if not exists alerts_status_idx
  on alerts (status, received_at desc);

create index if not exists alerts_service_idx
  on alerts (service, received_at desc);

create index if not exists alerts_alertname_idx
  on alerts (alertname, received_at desc);
