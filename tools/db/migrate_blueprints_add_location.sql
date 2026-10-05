-- Adds where each blueprint is to meadow_works.blueprints, filled in when
-- blueprints are fetched and saved (see src/location):
--   location_name   the station or structure it is in
--   container_name  the container it is in, or 'N/A' when it isn't in one
-- Both stay NULL until the blueprints are saved again. Safe to run more
-- than once.
alter table meadow_works.blueprints
    add column if not exists location_name text,
    add column if not exists container_name text;
