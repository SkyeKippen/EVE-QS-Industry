-- Adds Discord linking and notification settings.
--   discord_links           the Discord account a user linked, one per user;
--                           a Discord account links to at most one user
--   notification_settings   events a user turned off; an event with no row
--                           is on
-- Safe to run more than once.
begin;
create schema if not exists meadow_works;
set search_path to meadow_works;

create table if not exists discord_links (
    user_id bigint primary key references users (user_id) on delete cascade,
    discord_user_id text not null unique,
    discord_name text not null,
    linked_at timestamptz not null default now()
);

create table if not exists notification_settings (
    user_id bigint not null references users (user_id) on delete cascade,
    event text not null,
    enabled boolean not null,
    primary key (user_id, event)
);
commit;
