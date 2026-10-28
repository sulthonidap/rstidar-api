ALTER TABLE orders
    DROP FOREIGN KEY orders_ward_id_foreign,
    DROP KEY orders_care_type_index,
    DROP COLUMN ward_id,
    DROP COLUMN pharmacy_queue_no,
    DROP COLUMN care_type;

DROP TABLE wards;
