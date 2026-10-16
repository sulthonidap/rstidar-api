DROP TABLE IF EXISTS app_settings;
ALTER TABLE transactions
    DROP COLUMN note,
    DROP COLUMN proof_uploaded_at,
    DROP COLUMN proof,
    DROP COLUMN payment_method;
