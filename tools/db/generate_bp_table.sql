\set schema_name meadow_works
create schema if not exists meadow_works;
set search_path to meadow_works;

create table if not exists blueprints (
    item_id bigint primary key,
    location_flag text not null,
    location_id bigint not null,
    material_efficiency int not null,
    quantity int not null,
    runs int not null,
    time_efficiency int not null,
    type_id int not null
);