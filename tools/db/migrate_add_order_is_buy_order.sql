-- Adds order_is_buy_order: true for a buy order, false for a sell order.
-- Orders created before this column existed become buy orders. The default
-- is dropped afterwards so the app has to say which side every new order is.
-- Safe to run more than once.
begin;
alter table meadow_works.industry_orders
    add column if not exists order_is_buy_order bool not null default true;
alter table meadow_works.industry_orders
    alter column order_is_buy_order drop default;
commit;
