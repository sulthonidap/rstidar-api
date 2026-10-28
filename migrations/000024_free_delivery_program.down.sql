ALTER TABLE orders
    DROP FOREIGN KEY orders_free_verified_by_foreign,
    DROP KEY orders_is_free_index,
    DROP COLUMN free_verified_at,
    DROP COLUMN free_verified_by,
    DROP COLUMN normal_price,
    DROP COLUMN is_free,
    DROP COLUMN domicile_checked,
    DROP COLUMN patient_category;
