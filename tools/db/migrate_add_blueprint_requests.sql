-- Adds meadow_works.blueprint_requests: one row each time someone asks a
-- blueprint's owner for runs from the All Blueprints page.
--   requester_*      the user and character who asked
--   owner_*          the character or corporation that owns the blueprints
--   type_id, is_copy, runs_per_copy, material_efficiency, time_efficiency
--                    which line of All Blueprints was requested, so the
--                    request still makes sense after those blueprints change
--   runs_requested   how many runs they asked for
--   status           open for now; later steps can mark requests as handled
-- Safe to run more than once.
begin;
create table if not exists meadow_works.blueprint_requests (
    request_id bigserial primary key,
    requested_at timestamptz not null default now(),
    requester_user_id bigint not null,
    requester_character_id bigint not null,
    requester_character_name text not null,
    owner_id bigint not null,
    owner_name text not null,
    type_id int not null,
    is_copy bool not null,
    runs_per_copy int not null,
    material_efficiency int not null,
    time_efficiency int not null,
    runs_requested int not null check (runs_requested > 0),
    status text not null default 'open'
);
create index if not exists blueprint_requests_owner_id_idx on meadow_works.blueprint_requests (owner_id);
create index if not exists blueprint_requests_requester_user_id_idx on meadow_works.blueprint_requests (requester_user_id);
commit;
