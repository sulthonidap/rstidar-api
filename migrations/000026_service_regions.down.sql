DELETE FROM app_settings WHERE name = 'service_area';

ALTER TABLE customers DROP COLUMN region_code;

ALTER TABLE orders
    DROP KEY orders_dropoff_region_code_index,
    DROP COLUMN dropoff_region,
    DROP COLUMN dropoff_region_code;

DROP TABLE regions;
