create table if not exists report_runs (
  id bigserial primary key,
  kind text not null,

  -- e.g. 2026-W38
  period text not null,
  object_path text,

  -- running | success | failed
  status text not null default 'running',
  error text,
  created_at timestamptz not null default now()
);

create index if not exists report_runs_kind_idx
  on report_runs (kind, created_at desc);
