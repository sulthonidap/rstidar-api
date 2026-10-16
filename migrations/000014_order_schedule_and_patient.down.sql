ALTER TABLE orders
    DROP KEY orders_scheduled_date_index,
    DROP COLUMN medical_record_no,
    DROP COLUMN patient_name,
    DROP COLUMN scheduled_slot,
    DROP COLUMN scheduled_date;
