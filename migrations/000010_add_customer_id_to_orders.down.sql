ALTER TABLE orders
    DROP FOREIGN KEY orders_customer_id_foreign,
    DROP COLUMN customer_id;
