create table if not exists users (
  id bigserial primary key,
  username text unique not null,
  password_hash text not null,
  role text not null default 'viewer',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);
