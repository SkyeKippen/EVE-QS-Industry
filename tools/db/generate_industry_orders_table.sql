\set schema_name meadow_works
create schema if not exists meadow_works;
set search_path to meadow_works;

create table if not exists industry_orders (
    internal_order_id int primary key,
    order_type_id int not null,
    order_is_buy_order bool not null,
    order_quantity bigint not null,
    order_price numeric(20,2) not null,
    order_location text not null,
    order_contract_to text,
    order_created_by text not null,
    order_fulfilled bool not null,
    order_denied bool default false not null,
    order_completed bool default false not null,
    order_claimed_by text,
    order_claimed_at timestamptz
);