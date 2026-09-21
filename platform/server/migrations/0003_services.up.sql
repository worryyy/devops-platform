create table if not exists services (
  id bigserial primary key,
  name text unique not null,
  display_name text,
  owner text,
  kind text,
  sli jsonb not null default '{}',
  environments jsonb not null default '[]',
  source text not null default 'yaml',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists services_owner_idx on services (owner);
