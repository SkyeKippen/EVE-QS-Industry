-- Adds users, so one person can sign in with a main character and add alts.
--   users            one row per person using the app
--   user_characters  each character linked to a user; the character with the
--                    earliest added_at is the one they first signed in with
-- Every character that already has a saved token becomes its own user, in
-- the order they first signed in. Safe to run more than once.
begin;
create schema if not exists meadow_works;
set search_path to meadow_works;

create table if not exists users (
    user_id bigserial primary key,
    created_at timestamptz not null default now()
);

create table if not exists user_characters (
    character_id bigint primary key,
    user_id bigint not null references users (user_id) on delete cascade,
    character_name text not null,
    added_at timestamptz not null default now()
);
create index if not exists user_characters_user_id_idx on user_characters (user_id);

-- backfill: one user per character that signed in before users existed
do $$
declare
    token record;
    new_user_id bigint;
begin
    for token in
        select character_id, character_name, created_at from esi_tokens t
        where not exists (select 1 from user_characters c where c.character_id = t.character_id)
        order by created_at
    loop
        insert into users (created_at) values (token.created_at) returning user_id into new_user_id;
        insert into user_characters (character_id, user_id, character_name, added_at)
        values (token.character_id, new_user_id, token.character_name, token.created_at);
    end loop;
end $$;
commit;
