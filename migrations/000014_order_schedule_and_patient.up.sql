-- Delivery schedule chosen by staff (NULL = as soon as possible) and optional
-- patient data (the patient can differ from whoever receives the package).
ALTER TABLE orders
    ADD COLUMN scheduled_date DATE NULL AFTER notes,
    ADD COLUMN scheduled_slot VARCHAR(10) NULL AFTER scheduled_date,
    ADD COLUMN patient_name VARCHAR(255) NULL AFTER customer_phone,
    ADD COLUMN medical_record_no VARCHAR(50) NULL AFTER patient_name,
    ADD KEY orders_scheduled_date_index (scheduled_date);
