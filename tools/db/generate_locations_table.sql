-- Caches the stations and structures that blueprints (and later other
-- assets) sit in, so the app doesn't ask ESI about the same location every
-- time. Rows are re-checked with ESI once checked_at is 30 days old, or
-- 1 day old when accessible is false (the token couldn't read the
-- structure). When a structure that used to be readable stops being
-- readable, its last known name and system are kept and accessible is set
-- to false. Containers are not cached since they move. Safe to run more
-- than once.
create schema if not exists meadow_works;
set search_path to meadow_works;

create table if not exists locations (
    location_id bigint primary key,
    kind text not null check (kind in ('station', 'structure')),
    accessible boolean not null,
    name text not null default '',
    type_id int not null default 0,
    owner_id bigint not null default 0,
    owner_name text not null default '',
    solar_system_id int not null default 0,
    solar_system_name text not null default '',
    region_id int not null default 0,
    region_name text not null default '',
    checked_at timestamptz not null default now()
);
