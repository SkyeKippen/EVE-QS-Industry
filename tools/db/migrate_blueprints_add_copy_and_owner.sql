-- Adds to meadow_works.blueprints:
--   is_copy     true for a blueprint copy (ESI quantity -2), false for an
--               original or a stack of originals
--   owner_id    character or corporation ID the blueprint was fetched for
--   owner_type  'character' or 'corporation'
--   owner_name  that character's or corporation's name, for display
-- Existing rows get is_copy from their quantity. Their owner stays NULL
-- until the blueprints are fetched and saved again. Safe to run more than once.
begin;
alter table meadow_works.blueprints
    add column if not exists is_copy bool not null default false,
    add column if not exists owner_id bigint,
    add column if not exists owner_type text,
    add column if not exists owner_name text;
update meadow_works.blueprints set is_copy = (quantity = -2);
create index if not exists blueprints_owner_id_idx on meadow_works.blueprints (owner_id);
commit;
