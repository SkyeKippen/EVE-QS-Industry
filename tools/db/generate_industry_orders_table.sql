\set schema_name meadow_works
create schema if not exists meadow_works;
set search_path to meadow_works;

create table if not exists industry_orders (
    internal_order_id int primary key,
    order_type_id int not null,
    order_quantity bigint not null,
    order_price bigint not null,
    order_location text not null,
    order_contract_to text not null,
    order_created_by text not null,
    order_fulfilled bool not null
);