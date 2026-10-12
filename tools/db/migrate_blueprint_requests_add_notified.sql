-- Adds notified_at to meadow_works.blueprint_requests: when the owner was
-- sent the Discord DM listing the request, or NULL while it is waiting.
-- Requests wait 15 minutes from the first one so a run of them goes out as
-- one message. Requests made before the column existed are marked as sent,
-- so nobody is DMed about old ones. Safe to run more than once.
begin;
do $$
begin
    if not exists (select 1 from information_schema.columns
                   where table_schema = 'meadow_works' and table_name = 'blueprint_requests'
                     and column_name = 'notified_at') then
        alter table meadow_works.blueprint_requests add column notified_at timestamptz;
        update meadow_works.blueprint_requests set notified_at = now();
    end if;
end $$;
create index if not exists blueprint_requests_waiting_idx
    on meadow_works.blueprint_requests (requested_at) where notified_at is null;
commit;
