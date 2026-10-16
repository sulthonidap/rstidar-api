ALTER TABLE orders
    ADD COLUMN customer_id BIGINT UNSIGNED NULL AFTER organization_id,
    ADD CONSTRAINT orders_customer_id_foreign FOREIGN KEY (customer_id) REFERENCES customers (id) ON DELETE SET NULL;
