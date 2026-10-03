-- Makes order_contract_to optional. Sell orders have no Contract To, and the
-- app stores NULL whenever the field is left empty. Safe to run more than once.
alter table meadow_works.industry_orders
    alter column order_contract_to drop not null;
