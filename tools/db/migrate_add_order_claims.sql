-- Adds order claiming: a pilot can reserve an open order for 24 hours.
-- A claim is active while order_claimed_by is set and order_claimed_at is
-- less than 24 hours old; expired claims are ignored by the app, so no
-- cleanup job is needed. Safe to run more than once.
alter table meadow_works.industry_orders
    add column if not exists order_claimed_by text,
    add column if not exists order_claimed_at timestamptz;
