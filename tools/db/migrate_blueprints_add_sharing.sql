-- Adds blueprint sharing to meadow_works.blueprints:
--   is_shared     true when the owner has added the blueprint to the
--                 library, so it shows under All Blueprints. Every blueprint
--                 starts out not shared, including the ones already saved.
-- and what the Share Blueprints page groups blueprints by, filled in when
-- blueprints are fetched and saved (see src/location):
--   place_id      the station or structure ID it is in
--   hangar        the hangar it (or its outermost container) is in: a
--                 corporation division, CorpSAG1 to CorpSAG7, or Hangar for
--                 a character's own hangar
--   hangar_name   that hangar's name for display, such as the corporation's
--                 name for the division
--   container_id  the item ID of the innermost container it is in
-- The last four stay NULL until the blueprints are saved again.
--
-- Also creates meadow_works.shared_containers, the containers an owner has
-- shared: every blueprint in one is shared, including ones put in it later.
-- Safe to run more than once.
begin;
alter table meadow_works.blueprints
    add column if not exists is_shared bool not null default false,
    add column if not exists place_id bigint,
    add column if not exists hangar text,
    add column if not exists hangar_name text,
    add column if not exists container_id bigint;
create index if not exists blueprints_is_shared_idx on meadow_works.blueprints (owner_id) where is_shared;
create table if not exists meadow_works.shared_containers (
    container_id bigint primary key,
    owner_id bigint not null,
    added_at timestamptz not null default now()
);
create index if not exists shared_containers_owner_id_idx on meadow_works.shared_containers (owner_id);
commit;
